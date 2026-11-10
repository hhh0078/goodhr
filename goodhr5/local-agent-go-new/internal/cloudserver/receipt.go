// Package cloudserver 本文件在确认设备回执之前将真实简历附件保存进服务器受管卷，PG 行只保存摘要和附件编号。
package cloudserver

import (
	"context"

	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/replybridge"
	"goodhr5/local-agent-go-new/internal/storage"
)

// fileReceipts 核验每份附件的源消息、哈希与大小，复用正常入库路径，重复回执不会复制原件。
func (r *activeRun) fileReceipts(ctx context.Context, tx *storage.AutoReplyTx, bound commandState, c replybridge.Command, result replybridge.Result) ([]string, error) {
	if result.Error != "" || len(result.Value) == 0 || c.Action != "collect_resume" {
		return nil, nil
	}
	var answer replybridge.Response
	if err := json.Unmarshal(result.Value, &answer); err != nil {
		return nil, fmt.Errorf("无法核验简历回执")
	}
	if len(answer.Files) == 0 {
		return nil, nil
	}
	if err := replybridge.ValidateReceivedFiles(answer.Files); err != nil {
		return nil, err
	}
	if bound.ConversationID == "" || bound.ThreadID == "" || bound.AccountID == "" {
		return nil, fmt.Errorf("无法核验附件数量或候选人身份")
	}
	row, ok, err := tx.Get("conversations", bound.ConversationID)
	if err != nil {
		return nil, err
	}
	var conversation cloud.AutoReplyConversation
	if !ok || json.Unmarshal(row.Data, &conversation) != nil || conversation.ID != bound.ConversationID || conversation.PlatformID != r.platform || conversation.PlatformThreadID != bound.ThreadID || conversation.PlatformAccountID != bound.AccountID {
		return nil, fmt.Errorf("简历来源会话已变化")
	}
	source := bound.SourceKey
	if source != "" && answer.Resume.ResumeSourceMessageID != "" && answer.Resume.ResumeSourceMessageID != source {
		return nil, fmt.Errorf("简历回执的消息来源已变化")
	}
	if source == "" {
		source = answer.Resume.ResumeSourceMessageID
	}
	original, found, err := tx.MessageByKey(conversation.ID, source)
	if err != nil {
		return nil, err
	}
	var msg cloud.AutoReplyMessage
	if !found || json.Unmarshal(original.Data, &msg) != nil || msg.ConversationID != conversation.ID || msg.Direction != "candidate" || msg.MessageType != "resume" {
		return nil, fmt.Errorf("附件来源不是本次会话已核实的简历卡")
	}
	for _, file := range answer.Files {
		if file.Name == "" || file.Name == "." || file.Name == ".." || filepath.Base(file.Name) != file.Name || strings.ContainsAny(file.Name, "/\\") {
			return nil, fmt.Errorf("服务器未能核对附件的名称")
		}
	}
	dir, err := os.MkdirTemp("", "goodhr-server-upload-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	var ids []string
	for i, file := range answer.Files {
		sub := filepath.Join(dir, fmt.Sprint(i))
		if err = os.Mkdir(sub, 0700); err != nil {
			return nil, err
		}
		path := filepath.Join(sub, file.Name)
		if err = os.WriteFile(path, file.Data, 0600); err != nil {
			return nil, err
		}
		item, err := r.client.SaveServerReceiptAttachment(ctx, r.cred, cloud.AutoReplyAttachmentUpload{FilePath: path, ConversationID: conversation.ID, PlatformID: r.platform, SourceMessageID: original.ID}, tx)
		if err != nil {
			return nil, err
		}
		ids = append(ids, item.ID)
	}
	return ids, nil
}
