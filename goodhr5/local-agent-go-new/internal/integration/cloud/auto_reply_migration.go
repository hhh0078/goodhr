// Package cloud 本文件把旧账号JSON一次性导入实体表，保留原快照、原ID及已发送消息顺序。
package cloud

import (
	"encoding/json"
	"fmt"
	"goodhr5/local-agent-go-new/internal/storage"
	"path/filepath"
	"sort"
	"strings"
)

// importAutoReplyMap 导入一种强类型实体，每个实体保存为单独SQLite行。
func importAutoReplyMap[T any](tx *storage.AutoReplyTx, kind string, items map[string]T) error {
	ids := make([]string, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := tx.Put(kind, id, items[id]); err != nil {
			return err
		}
	}
	return nil
}

// importLocalAutoReply 校验并导入全部旧实体，任意失败交给外层事务整体回滚。
func (c *Client) importLocalAutoReply(tx *storage.AutoReplyTx, credentials AgentCredentials, raw []byte) error {
	var d localAutoReplyData
	if err := json.Unmarshal(raw, &d); err != nil {
		return err
	}
	if err := importAutoReplyMap(tx, "candidates", d.Candidates); err != nil {
		return err
	}
	if err := importAutoReplyMap(tx, "identities", d.Identities); err != nil {
		return err
	}
	if err := importAutoReplyMap(tx, "conversations", d.Conversations); err != nil {
		return err
	}
	for conversationID, messages := range d.Messages {
		for _, m := range messages {
			m.ConversationID = conversationID
			if m.ID == "" {
				m.ID = "message-" + autoReplyDigest(conversationID+localAutoReplyMessageKey(m))
			}
			if localAutoReplyMessageKey(m) == "" {
				return fmt.Errorf("旧消息缺少稳定编号")
			}
			if err := tx.Put("messages", m.ID, m); err != nil {
				return err
			}
			if err := tx.MarkHistoricalMessage(m.ID, "legacy_unverified", 0); err != nil {
				return err
			}
		}
	}
	if err := importAutoReplyMap(tx, "confirmations", d.Confirmations); err != nil {
		return err
	}
	for id, a := range d.Attachments {
		path := d.AttachmentPaths[id]
		relative, err := filepath.Rel(c.localAutoDir, path)
		if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			relative = filepath.Join(autoReplyDigest(c.localAutoReplyKey(credentials)), "attachments", filepath.Base(path))
		}
		if err := tx.Put("attachments", id, localAttachment{StoredResumeAttachment: a, RelativePath: relative}); err != nil {
			return err
		}
	}
	if err := importAutoReplyMap(tx, "runs", d.Runs); err != nil {
		return err
	}
	if err := importAutoReplyMap(tx, "tools", d.Tools); err != nil {
		return err
	}
	if err := importAutoReplyMap(tx, "suggestions", d.Suggestions); err != nil {
		return err
	}
	keys := map[string]bool{}
	for k := range d.Reports {
		keys[k] = true
	}
	for k := range d.ReportAttempts {
		keys[k] = true
	}
	for k := range d.ReportClaims {
		keys[k] = true
	}
	for key := range keys {
		if err := tx.Put("reports", key, localReportState{Report: d.Reports[key], Attempts: d.ReportAttempts[key], LeaseAt: d.ReportClaims[key], Published: d.ReportPublished[key]}); err != nil {
			return err
		}
	}
	for id, value := range d.CloudCandidates {
		if err := tx.Put("cloud_links", "candidate:"+id, localCloudLink{Value: value}); err != nil {
			return err
		}
	}
	for id, value := range d.CloudCandidateHashes {
		if err := tx.Put("cloud_links", "hash:"+id, localCloudLink{Value: value}); err != nil {
			return err
		}
	}
	for id, result := range d.Notifications {
		payload, err := json.Marshal(result.Notification)
		if err != nil {
			return err
		}
		status := "pending"
		if result.EmailSent {
			status = "completed"
		}
		if err = tx.Put("outbox", "notice:"+id, autoReplyOutboxItem{ID: "notice:" + id, Kind: "notice", Status: status, Payload: payload}); err != nil {
			return err
		}
	}
	return tx.PruneAudits()
}

// localAutoReplyMessageKey 优先返回平台消息编号，再使用已保存的稳定指纹。
func localAutoReplyMessageKey(item AutoReplyMessage) string {
	if item.PlatformMessageID != "" {
		return item.PlatformMessageID
	}
	return item.Fingerprint
}
