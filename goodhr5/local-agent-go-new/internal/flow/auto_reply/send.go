// Package auto_reply 本文件负责发送前复核、发送后回读和本地重复发送保护。
package auto_reply

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

var errAutoReplySendNeedsReview = errors.New("上次消息发送结果暂时不确定，请在招聘页面确认，我先不重发")

type monitoredReplyResult struct {
	decision ReplyDecision
	err      error
}

// replyWhileMonitoring 等待 AI 决策时每三秒复核最新消息，发现变化后只废弃旧结果，不并发操作页面流程。
func (f *Flow) replyWhileMonitoring(ctx context.Context, prepared shared.PreparedTask, runtime model.AutoReplyRuntime, snapshot model.AutoReplyConversationSnapshot, input ReplyContext) (ReplyDecision, bool, error) {
	replyCtx, cancelReply := context.WithCancel(ctx)
	defer cancelReply()
	resultChannel := make(chan monitoredReplyResult, 1)
	go func() {
		decision, err := f.Responder.Reply(replyCtx, input)
		resultChannel <- monitoredReplyResult{decision: decision, err: err}
	}()
	interval := f.messagePollInterval
	if interval <= 0 {
		interval = defaultAutoReplyMessagePollInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	changed := false
	var monitorErr error
	for {
		select {
		case <-ctx.Done():
			return ReplyDecision{}, changed, ctx.Err()
		case result := <-resultChannel:
			if result.err != nil {
				return ReplyDecision{}, changed, result.err
			}
			if monitorErr != nil {
				return ReplyDecision{}, changed, monitorErr
			}
			return result.decision, changed, nil
		case <-ticker.C:
			if changed || monitorErr != nil {
				continue
			}
			unchanged, err := f.latestCandidateMessageUnchanged(ctx, runtime, prepared, snapshot, input.BasedOnMessageKey)
			if err != nil {
				monitorErr = err
				continue
			}
			changed = !unchanged
			if changed {
				cancelReply()
				return ReplyDecision{}, true, nil
			}
		}
	}
}

// sendVerifiedMessage 发送一条消息并回读确认，结果未知时不直接重发。
func (f *Flow) sendVerifiedMessage(ctx context.Context, prepared shared.PreparedTask, runtime model.AutoReplyRuntime, conversation cloud.AutoReplyConversation, snapshot model.AutoReplyConversationSnapshot, basedOnMessageKey string, message string, verifyCandidateBefore bool, sequence ...int) (bool, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return false, fmt.Errorf("候选人回复内容不能为空")
	}
	if verifyCandidateBefore {
		unchanged, err := f.latestCandidateMessageUnchanged(ctx, runtime, prepared, snapshot, basedOnMessageKey)
		if err != nil || !unchanged {
			if err == nil {
				f.reportChangedMessage(prepared.Request.TaskID, snapshot.CandidateName)
			}
			return false, err
		}
	}
	messageSequence := 0
	if len(sequence) > 0 {
		messageSequence = sequence[0]
	}
	remote, remoteSend := runtime.(interface {
		SendAutoReplyWithReceipt(context.Context, model.Browser, model.Config, model.AutoReplyConversationSnapshot, string) (model.ConversationMessage, error)
	})
	if !remoteSend && prepared.Platform.Behavior.AutoReplySendEnterFirst {
		page, err := common.UniqueCurrentPageURL(ctx, f.Browser)
		if err != nil {
			return false, err
		}
		snapshot.DeliveryPageURL = page
	}
	status, claimed, err := f.Cloud.ClaimAutoReplySend(ctx, credentials(prepared), conversation.ID, basedOnMessageKey, message, messageSequence)
	if err != nil {
		return false, err
	}
	if !claimed {
		if status == "sent" {
			return false, nil
		}
		return false, errAutoReplySendNeedsReview
	}
	if verifyCandidateBefore {
		snapshot.ExpectedSendMessageKey = basedOnMessageKey
	}
	var latest model.ConversationMessage
	if remoteSend {
		latest, err = remote.SendAutoReplyWithReceipt(ctx, nil, prepared.Platform, snapshot, message)
	} else {
		err = runtime.SendAutoReplyMessage(ctx, f.Browser, prepared.Platform, snapshot, message)
	}
	if err != nil {
		if errors.Is(err, model.ErrAutoReplySupersededBeforeSend) {
			if releaseErr := f.Cloud.ReleaseUnsentAutoReply(context.WithoutCancel(ctx), credentials(prepared), conversation.ID, basedOnMessageKey, messageSequence, message); releaseErr != nil {
				return false, releaseErr
			}
			return false, err
		}
		_ = f.Cloud.FinishAutoReplySend(context.WithoutCancel(ctx), credentials(prepared), conversation.ID, basedOnMessageKey, messageSequence, "unknown")
		return false, fmt.Errorf("%w：%v", errAutoReplySendNeedsReview, err)
	}
	if !remoteSend && snapshot.DeliveryPageURL != "" {
		latest, err = f.confirmOpenReplyDelivery(ctx, prepared, runtime, snapshot, message)
	} else if !remoteSend {
		latest, err = runtime.ReadLatestAutoReplyMessage(ctx, f.Browser, prepared.Platform, snapshot)
	}
	if err != nil || !strings.EqualFold(strings.TrimSpace(latest.Direction), "self") || strings.TrimSpace(latest.TextContent) != message {
		_ = f.Cloud.FinishAutoReplySend(context.WithoutCancel(ctx), credentials(prepared), conversation.ID, basedOnMessageKey, messageSequence, "unknown")
		return false, errors.Join(errAutoReplySendNeedsReview, err)
	}
	if err = f.Cloud.FinishAutoReplySend(context.WithoutCancel(ctx), credentials(prepared), conversation.ID, basedOnMessageKey, messageSequence, "sent", firstNonEmpty(latest.PlatformMessageID, latest.Key)); err != nil {
		return false, fmt.Errorf("%w：保存发送结果失败", errAutoReplySendNeedsReview)
	}
	messages, _, err := convertMessages([]model.ConversationMessage{latest})
	if err != nil {
		return false, err
	}
	if _, err = f.Cloud.SyncAutoReplyMessages(ctx, credentials(prepared), cloud.AutoReplyMessageSyncRequest{
		ConversationID: conversation.ID, Messages: messages,
	}); err != nil {
		return false, fmt.Errorf("消息已经发送，但云端记录没同步成功：%w", err)
	}
	return true, nil
}

// latestCandidateMessageUnchanged 在页面动作前确认候选人没有又发来新消息。
func (f *Flow) latestCandidateMessageUnchanged(ctx context.Context, runtime model.AutoReplyRuntime, prepared shared.PreparedTask, snapshot model.AutoReplyConversationSnapshot, basedOnMessageKey string) (bool, error) {
	latest, err := runtime.ReadLatestAutoReplyMessage(ctx, f.Browser, prepared.Platform, snapshot)
	if err != nil {
		return false, fmt.Errorf("发送前复核候选人最新消息失败：%w", err)
	}
	if strings.ToLower(strings.TrimSpace(latest.Direction)) != "candidate" {
		return false, nil
	}
	key := firstNonEmpty(latest.PlatformMessageID, latest.Key, messageFingerprint(latest, 0))
	return key == strings.TrimSpace(basedOnMessageKey), nil
}

// latestMessageAllowsFollowup 确认第一条回复仍是页面最新消息，候选人没有在两条消息之间插入新内容。
func (f *Flow) latestMessageAllowsFollowup(ctx context.Context, runtime model.AutoReplyRuntime, prepared shared.PreparedTask, snapshot model.AutoReplyConversationSnapshot, previousContent string) (bool, string, error) {
	latest, err := runtime.ReadLatestAutoReplyMessage(ctx, f.Browser, prepared.Platform, snapshot)
	if err != nil {
		return false, "", fmt.Errorf("发送第二条消息前复核最新聊天失败：%w", err)
	}
	return strings.EqualFold(strings.TrimSpace(latest.Direction), "self") && strings.TrimSpace(latest.TextContent) == strings.TrimSpace(previousContent), firstNonEmpty(latest.PlatformMessageID, latest.Key, messageFingerprint(latest, 0)), nil
}

// reportChangedMessage 向悬浮窗说明旧回复已废弃，等待下一轮重新分析。
func (f *Flow) reportChangedMessage(taskID string, candidateName string) {
	shared.ReportAnalysis(f.Logger, taskID, shared.AnalysisStatus{
		Kind: "auto_reply", Phase: "result", Stage: "message_changed", Terminal: true,
		CandidateName: candidateName, Accepted: boolPointer(false),
		Reason: "候选人又发来新消息，旧回复已放弃，下一轮重新看", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
}
