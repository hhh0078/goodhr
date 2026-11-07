// Package auto_reply 本文件负责复核最新配置、提交批次结果及在答案后安全索要简历。
package auto_reply

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// canAcknowledgeResumeCard 只在已有沟通依据且整批都是平台明确标识的纯简历卡时走固定签收，不吞候选人的文字问题。
func canAcknowledgeResumeCard(input ReplyContext) bool {
	if len(input.PendingMessages) == 0 {
		return false
	}
	for _, m := range input.PendingMessages {
		if m.Direction != "candidate" || m.MessageType != "resume" {
			return false
		}
		if !standaloneResumeCard(m.CardContent) {
			// 升级前入库的待答卡片可能没有标记，只接受这次实际读到的同编号、同正文平台卡片。
			key := firstNonEmpty(m.PlatformMessageID, m.Fingerprint)
			found := false
			for _, page := range input.PageSnapshot.Messages {
				if key != "" && key == firstNonEmpty(page.PlatformMessageID, page.Key) && page.Direction == m.Direction &&
					page.MessageType == m.MessageType && page.TextContent == m.TextContent && standaloneResumeCard(page.CardContent) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	for _, item := range input.ConfirmationItems {
		if item.Status == "matched" || item.Status == "unmatched" {
			return true
		}
	}
	seenInbound := false
	for _, msg := range input.Messages {
		if msg.Direction == "candidate" {
			seenInbound = true
		} else if seenInbound && msg.Direction == "self" {
			return true
		}
	}
	return false
}

// standaloneResumeCard 仅信任平台适配器对纯简历卡的明确标记；未知或老格式交给正常问答。
func standaloneResumeCard(raw json.RawMessage) bool {
	var card struct {
		Standalone bool `json:"standalone_resume"`
	}
	return json.Unmarshal(raw, &card) == nil && card.Standalone
}

// verifyCurrentReplyConfiguration 在提交决策和每次发送前重读岗位开关与版本，关闭或修改后保留待处理消息。
func (f *Flow) verifyCurrentReplyConfiguration(ctx context.Context, prepared shared.PreparedTask, expected cloud.AutoReplyPositionSnapshot) error {
	current, err := f.Cloud.AutoReplySnapshot(ctx, credentials(prepared), expected.Position.ID)
	if err != nil {
		return fmt.Errorf("复核自动回复配置失败：%w", err)
	}
	if !current.Config.Enabled || !current.Subscription.Active || !current.Subscription.AllowAutoReply {
		return fmt.Errorf("这个岗位的自动回复已关闭或权限已变化，本轮没有继续发送")
	}
	if expected.CompanyProfile.ID != "" {
		before, after := expected.CompanyProfile, current.CompanyProfile
		if before.ID != after.ID || before.Name != after.Name || before.Address != after.Address || before.Contact != after.Contact || before.Overview != after.Overview || before.ExtraInfo != after.ExtraInfo || !before.UpdatedAt.Equal(after.UpdatedAt) {
			return fmt.Errorf("公司资料已修改，本轮先放下旧答案，下一轮重新看")
		}
	}
	if current.Config.Version != expected.Config.Version {
		return fmt.Errorf("岗位回复资料已修改，本轮先放下旧答案，下一轮重新看")
	}
	return nil
}

// completeReplyBatch 仅在明确终态后推进处理水位，并登记已确认结果的后台推荐任务。
func (f *Flow) completeReplyBatch(ctx context.Context, prepared shared.PreparedTask, input ReplyContext, outcome string) error {
	if err := f.Cloud.CompleteAutoReplyMessage(ctx, credentials(prepared), input.Conversation.ID, input.BasedOnMessageKey, outcome); err != nil {
		return err
	}
	// 回复或转人工已落账，再补同份历史简历遗漏的生日，避免补资料占用候选人的等待时间。
	if err := f.reuseStoredStructuredResume(ctx, prepared, input.Position, input.Conversation, input.PageSnapshot, input.CandidateState, false); err != nil {
		f.log(prepared.Request.TaskID, "backfill_resume_birth_month", "warning", time.Now(), err)
	}
	if queue, ok := f.Responder.(interface {
		QueueRecommendation(context.Context, ReplyContext) error
	}); ok {
		if err := queue.QueueRecommendation(ctx, input); err != nil {
			f.log(prepared.Request.TaskID, "queue_recommendation", "warning", time.Now(), err)
		}
	}
	return nil
}

// requestResumeAfterAnswer 在本轮只有一条答案时补充首次简历索要，等待简历不再阻断以后的问答。
func (f *Flow) requestResumeAfterAnswer(ctx context.Context, prepared shared.PreparedTask, runtime model.AutoReplyRuntime, position cloud.AutoReplyPositionSnapshot, conversation *cloud.AutoReplyConversation, snapshot model.AutoReplyConversationSnapshot, state cloud.AutoReplyCandidateState, messageKey, previousContent string) error {
	if state.HasResumeAttachment || snapshot.ResumeCardAvailable || conversation.Status == "waiting_resume" {
		return nil
	}
	if err := f.verifyCurrentReplyConfiguration(ctx, prepared, position); err != nil {
		return err
	}
	ready, _, err := f.latestMessageAllowsFollowup(ctx, runtime, prepared, snapshot, previousContent)
	if err != nil || !ready {
		return err
	}
	// 先落等待状态，按钮结果不明也不在下轮重复索要；已有候选人问题仍会继续回答。
	if err = f.saveResumeConversationStatus(ctx, prepared, conversation, "waiting_resume"); err != nil {
		return err
	}
	if err = runtime.RequestAutoReplyResume(ctx, f.Browser, prepared.Platform, snapshot); err != nil {
		return fmt.Errorf("索要简历结果暂时不确定，请在招聘页面确认：%w", err)
	}
	ready, lastKey, err := f.latestMessageAllowsFollowup(ctx, runtime, prepared, snapshot, previousContent)
	if err != nil || !ready {
		return err
	}
	if err = f.verifyCurrentReplyConfiguration(ctx, prepared, position); err != nil {
		return err
	}
	snapshot.ExpectedSendMessageKey = lastKey
	message := firstNonEmpty(position.Config.ResumeRequestMessage, "方便发一份简历吗？我再认真看看。")
	_, err = f.sendVerifiedMessage(ctx, prepared, runtime, *conversation, snapshot, messageKey, message, false, 2)
	return err
}

// syncReplyChangesBeforeCompletion 在完成旧批次前保存候选人的插话，防止后台推荐沿用已过时的条件快照。
func (f *Flow) syncReplyChangesBeforeCompletion(ctx context.Context, prepared shared.PreparedTask, runtime model.AutoReplyRuntime, conversation cloud.AutoReplyConversation, snapshot model.AutoReplyConversationSnapshot, basedOnKey string, known []cloud.AutoReplyMessage) (bool, error) {
	latest, err := runtime.ReadLatestAutoReplyMessage(ctx, f.Browser, prepared.Platform, snapshot)
	if err != nil {
		return false, err
	}
	key := firstNonEmpty(latest.PlatformMessageID, latest.Key, messageFingerprint(latest, 0))
	if !strings.EqualFold(latest.Direction, "candidate") || key == basedOnKey {
		return false, nil
	}
	items, complete, err := runtime.ReadAutoReplyMessages(ctx, f.Browser, prepared.Platform, snapshot, recentAutoReplyMessageKeys(known), cloud.AutoReplyMaxHistoryMessages)
	if err != nil {
		return true, err
	}
	messages, _, err := convertMessages(items)
	if err != nil {
		return true, err
	}
	_, err = f.Cloud.SyncAutoReplyMessages(ctx, credentials(prepared), cloud.AutoReplyMessageSyncRequest{ConversationID: conversation.ID, HistoryComplete: complete, Messages: messages})
	return true, err
}
