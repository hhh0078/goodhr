// Package auto_reply 本文件负责重新联系时核对同一招聘账号及来源，只将删除后的候选人来信作为新建档依据。
package auto_reply

import (
	"context"
	"fmt"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// readPostDeletionConversation 先只核实当前页身份，再读时间边界后的对话；旧未读、系统通知和 HR 单方面来信不能重新建档。
func (f *Flow) readPostDeletionConversation(ctx context.Context, prepared shared.PreparedTask, runtime model.AutoReplyRuntime, conversation model.Conversation, open, inspected *model.AutoReplyConversationSnapshot, lookup cloud.AutoReplyCandidateLookup, intent cloud.AutoReplyCaptureIntent, boundary cloud.AutoReplyDeletionBoundary, state cloud.AutoReplyCandidateState, known []string) (model.AutoReplyConversationSnapshot, error) {
	var snapshot model.AutoReplyConversationSnapshot
	inspector, ok := runtime.(model.AutoReplyContactInspector)
	if !ok {
		return snapshot, storage.ErrAutoReplyCandidateDeleted
	}
	cred := credentials(prepared)
	active := state.Conversation != nil && state.Conversation.DeletionBoundary != nil && boundary.Sequence > 0 && state.Conversation.DeletionBoundary.Sequence == boundary.Sequence && state.Conversation.DeletionBoundary.HistoryCutoff().Equal(boundary.HistoryCutoff())
	if active {
		// 打开已重建的会话同样会消费未读角标；先登记本代采集意图，读取失败时才能从原位置恢复。
		if err := f.Cloud.BeginAutoReplyCapture(ctx, cred, lookup.PlatformID, lookup.PlatformAccountID, lookup.PlatformThreadID, intent); err != nil {
			return snapshot, err
		}
	} else if inspected == nil {
		if err := f.Cloud.BeginAutoReplyContactProbe(ctx, cred, lookup, intent); err != nil {
			return snapshot, err
		}
	}
	if inspected != nil {
		snapshot = *inspected
	} else if active && open != nil && open.HistoryAfter != nil && open.HistoryAfter.Equal(boundary.HistoryCutoff()) && open.PlatformThreadID == lookup.PlatformThreadID {
		snapshot = *open
	} else {
		var err error
		snapshot, err = inspector.InspectAutoReplyContact(ctx, f.Browser, prepared.Platform, conversation)
		if err != nil {
			return snapshot, err
		}
	}
	if snapshot.PlatformCandidateID == "" || snapshot.PlatformThreadID != lookup.PlatformThreadID || snapshot.PlatformAccountID != lookup.PlatformAccountID || (lookup.PlatformCandidateID != "" && snapshot.PlatformCandidateID != lookup.PlatformCandidateID) {
		return snapshot, fmt.Errorf("会话与已核实的招聘账号或候选人不一致，稍后再核对")
	}
	lookup.PlatformCandidateID = snapshot.PlatformCandidateID
	verified, err := f.Cloud.AutoReplyDeletionBoundary(ctx, cred, lookup)
	if err != nil {
		return snapshot, err
	}
	if verified == nil || verified.Sequence == 0 || verified.DeletedAt.IsZero() || verified.SourceHash == "" {
		if err = f.Cloud.FinishAutoReplyContactProbe(ctx, cred, lookup); err != nil {
			return snapshot, err
		}
		return snapshot, storage.ErrAutoReplyCandidateDeleted
	}
	if boundary.Sequence != 0 && boundary.Sequence != verified.Sequence {
		return snapshot, storage.ErrAutoReplyCandidateDeleted
	}
	cutoff := verified.HistoryCutoff()
	snapshot.HistoryAfter = &cutoff
	snapshot.ResumeCardAvailable, snapshot.ResumeSourceMessageID = false, ""
	snapshot.Messages, snapshot.HistoryComplete, err = runtime.ReadAutoReplyMessages(ctx, f.Browser, prepared.Platform, snapshot, known, cloud.AutoReplyMaxHistoryMessages)
	if err != nil {
		return snapshot, err
	}
	// 平台能力也需遵守此约束。返回含未知或旧时间的消息时，整批停止，不能据此调用 AI。
	now := time.Now().UTC()
	for _, message := range snapshot.Messages {
		if message.SentAt == nil || !message.SentAt.After(cutoff) || message.SentAt.After(now) {
			return snapshot, fmt.Errorf("新沟通中有一条时间无法核实，暂缓重新建档")
		}
	}
	if active {
		return snapshot, nil
	}
	if !snapshot.HistoryComplete {
		return snapshot, fmt.Errorf("还没核实新沟通的起点，稍后继续")
	}
	for _, message := range snapshot.Messages {
		if message.Direction != "candidate" {
			continue
		}
		items, _, err := convertMessages([]model.ConversationMessage{message})
		if err != nil {
			return snapshot, err
		}
		intent.PlatformCandidateID = snapshot.PlatformCandidateID
		_, err = f.Cloud.ActivateAutoReplyContact(ctx, cred, lookup, *verified, items[0], intent)
		return snapshot, err
	}
	if err = f.Cloud.FinishAutoReplyContactProbe(ctx, cred, lookup); err != nil {
		return snapshot, err
	}
	return snapshot, storage.ErrAutoReplyCandidateDeleted
}

// inspectUnknownContact 先登记仅用于恢复的路由，再核对列表未提供的候选人编号，以识别其他设备上已删除的同一来源。
func (f *Flow) inspectUnknownContact(ctx context.Context, prepared shared.PreparedTask, runtime model.AutoReplyRuntime, conversation model.Conversation, lookup cloud.AutoReplyCandidateLookup, intent cloud.AutoReplyCaptureIntent) (model.AutoReplyConversationSnapshot, error) {
	inspector, ok := runtime.(model.AutoReplyContactInspector)
	if !ok {
		return model.AutoReplyConversationSnapshot{}, fmt.Errorf("当前平台配置需要支持在聊天头核对候选人")
	}
	if err := f.Cloud.BeginAutoReplyContactProbe(ctx, credentials(prepared), lookup, intent); err != nil {
		return model.AutoReplyConversationSnapshot{}, err
	}
	header, err := inspector.InspectAutoReplyContact(ctx, f.Browser, prepared.Platform, conversation)
	if err != nil {
		return header, err
	}
	if header.PlatformCandidateID == "" || header.PlatformThreadID != lookup.PlatformThreadID || header.PlatformAccountID != lookup.PlatformAccountID {
		return header, fmt.Errorf("未能核对新会话身份，请在招聘页面确认账号")
	}
	return header, nil
}
