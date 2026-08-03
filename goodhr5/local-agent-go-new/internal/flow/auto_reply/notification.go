// Package auto_reply 本文件负责无法唯一确认或不应自动回答时通知 HR，通知失败不暂停其他候选人。
package auto_reply

import (
	"context"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// notifyUnresolved 发送包含岗位、候选人、性别、平台和原因的幂等人工接管通知。
func (f *Flow) notifyUnresolved(ctx context.Context, prepared shared.PreparedTask, position cloud.AutoReplyPositionSnapshot, conversation cloud.AutoReplyConversation, snapshot model.AutoReplyConversationSnapshot, reason string, reasonKey string, basedOnMessageKey string) error {
	f.saveLocalReplyRecord(ctx, prepared, conversation, replyRecordHash(basedOnMessageKey, "manual:"+reasonKey), "manual")
	latestMessage := ""
	for index := len(snapshot.Messages) - 1; index >= 0; index-- {
		if strings.EqualFold(strings.TrimSpace(snapshot.Messages[index].Direction), "candidate") {
			latestMessage = snapshot.Messages[index].TextContent
			break
		}
	}
	result, err := f.Cloud.NotifyAutoReplyManualHandoff(context.WithoutCancel(ctx), credentials(prepared), cloud.AutoReplyNotification{
		ConversationID: conversation.ID, PositionID: position.Position.ID,
		BasedOnMessageKey: basedOnMessageKey, ReasonKey: reasonKey,
		CandidateName: snapshot.CandidateName, Gender: snapshot.Gender,
		PlatformID: prepared.Platform.ID, Reason: reason, LatestMessage: latestMessage,
	})
	if err != nil {
		f.log(prepared.Request.TaskID, "notify_manual_handoff", "warning", time.Now(), err)
	}
	if result.Warning != "" {
		f.log(prepared.Request.TaskID, "notify_manual_handoff", "warning", time.Now(), fmt.Errorf("%s", result.Warning))
	}
	shared.ReportAnalysis(f.Logger, prepared.Request.TaskID, shared.AnalysisStatus{
		Kind: "auto_reply", Phase: "result", Stage: "manual", Terminal: true,
		CandidateName: snapshot.CandidateName, Accepted: boolPointer(false), Reason: reason,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	return nil
}
