// Package auto_reply 本文件只发送新证据事件产生的一次目标追问，不扫描时间或循环催问候选人。
package auto_reply

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// deliverGoalFollowup 从权威会话恢复单个待发目标，候选人插话、人工接管或目标变化均放弃旧问题。
func (f *Flow) deliverGoalFollowup(ctx context.Context, prepared shared.PreparedTask, runtime model.AutoReplyRuntime, position cloud.AutoReplyPositionSnapshot, conversation cloud.AutoReplyConversation, page model.AutoReplyConversationSnapshot) error {
	cred := credentials(prepared)
	current, goal, valid, err := f.Cloud.AutoReplyGoalFollowupState(ctx, cred, conversation.ID)
	if err != nil {
		return err
	}
	if current.GoalFollowup == nil {
		return nil
	}
	intent := *current.GoalFollowup
	finish := func() error { return f.Cloud.FinishAutoReplyGoalFollowup(ctx, cred, current.ID, intent.EventKey) }
	if !valid {
		return finish()
	}
	if intent.ContextRevision == "" || intent.ContextRevision != cloud.AutoReplyGoalContextRevision(position) {
		// 升级前无事实版本的草稿同样失效，避免重启把 HR 已编辑或删除的事实重新说出去。
		return finish()
	}
	if current.PositionID != position.Position.ID || current.PlatformID != prepared.Platform.ID || current.PlatformThreadID != page.PlatformThreadID || current.PlatformAccountID != page.PlatformAccountID || !targetConversationAllowed(prepared.Request.AutoReplyTarget, page.Conversation) {
		return fmt.Errorf("主动追问的候选人、岗位或招聘账号尚未核对")
	}
	history, err := f.Cloud.AutoReplyMessages(ctx, cred, current.ID)
	if err != nil {
		return err
	}
	if latest, found := findBasedOnMessage(history, intent.BasedOnMessageKey); !found || candidateEndedContact([]cloud.AutoReplyMessage{latest}) {
		return finish()
	}
	if strings.TrimSpace(intent.Content) == "" || len([]rune(intent.Content)) > maxAutoReplyMessageRunes || strings.ContainsAny(intent.Content, "{}｛｝") || checkPhoneReply(inspectPhoneContact(ReplyContext{Position: position, Conversation: current, PageSnapshot: page, Messages: history}), position.Config, intent.Content) != nil {
		return finish()
	}
	latest, opened, err := runtime.ReadOpenAutoReplyLatestMessage(ctx, f.Browser, prepared.Platform, page)
	if err != nil {
		return err
	}
	if !opened {
		return nil
	}
	key := firstNonEmpty(latest.PlatformMessageID, latest.Key)
	if key == "" || key != intent.ExpectedLatestKey {
		return finish()
	}
	if err = f.verifyCurrentReplyConfiguration(ctx, prepared, position); err != nil {
		return err
	}
	messageKey := "goal-event:" + intent.EventKey
	status, _, err := f.Cloud.AutoReplySendReceipt(ctx, cred, current.ID, messageKey, 1)
	if err != nil {
		return err
	}
	if status != "" {
		if status != "sent" {
			f.log(prepared.Request.TaskID, "goal_followup_unconfirmed", "warning", time.Now(), errAutoReplySendNeedsReview)
		}
		return finish()
	}
	page.ExpectedSendMessageKey = key
	binding := &cloud.AutoReplyGoalSend{ConfirmationItemID: goal.ID, ExpectedVersion: intent.ExpectedGoalVersion, BasedOnMessageKey: intent.BasedOnMessageKey, ExpectedLatestKey: intent.ExpectedLatestKey, AllowNoPending: true}
	sent, err := f.sendVerifiedMessageWithGoal(ctx, prepared, runtime, current, page, messageKey, intent.Content, false, binding, 1)
	if errors.Is(err, model.ErrAutoReplySupersededBeforeSend) || errors.Is(err, cloud.ErrAutoReplyGoalChanged) {
		return finish()
	}
	if errors.Is(err, errAutoReplySendNeedsReview) {
		if clearErr := finish(); clearErr != nil {
			return clearErr
		}
		return f.notifyUnresolved(ctx, prepared, position, current, page, err.Error(), "goal_send_unknown", intent.BasedOnMessageKey)
	}
	if err != nil {
		return err
	}
	if err = finish(); err != nil {
		return err
	}
	if sent {
		shared.ReportAnalysis(f.Logger, prepared.Request.TaskID, shared.AnalysisStatus{PositionID: position.Position.ID, Kind: "auto_reply", Phase: "result", Stage: "goal_followup", Terminal: true, CandidateName: page.CandidateName, Reason: "新资料已核对，已追问一个待确认事项", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	}
	return nil
}

// needsMessageGoalReview 只补充尚未规划的普通答复；已经问过仍在等回答时不重复调用模型催问。
func needsMessageGoalReview(decision ReplyDecision, goals []cloud.CandidateConfirmationItem) bool {
	if len(decision.Messages) != 1 || decision.Messages[0].Type != "answer" || decision.Messages[0].RequestsResume || len(decision.ConfirmationUpdates) != 0 || decision.ManualReason != "" {
		return false
	}
	for _, goal := range goals {
		if goal.ArchivedAt == nil && goal.Status == "pending" && (goal.AskCount > 0 || goal.LastAskedAt != nil) && !cloud.AutoReplyGoalCanAsk(goal) {
			return false
		}
	}
	return true
}

// reviewGoalsAfterAnswer 先让候选人拿到答案，再为这条新消息最多运行一次受限目标检查，不建立定时任务。
func (f *Flow) reviewGoalsAfterAnswer(ctx context.Context, prepared shared.PreparedTask, runtime model.AutoReplyRuntime, input ReplyContext, previousContent string) (planned, sent bool, err error) {
	ready, anchor, err := f.latestMessageAllowsFollowup(ctx, runtime, prepared, input.PageSnapshot, previousContent)
	if err != nil || !ready {
		return false, false, err
	}
	claimed, err := f.Cloud.ClaimAutoReplyMessageGoalReview(ctx, input.Credentials, input.Conversation.ID, input.BasedOnMessageKey, anchor, input.Position.Config.Version)
	if err != nil || !claimed {
		return false, false, err
	}
	input.Messages, err = f.Cloud.AutoReplyMessages(ctx, input.Credentials, input.Conversation.ID)
	if err != nil {
		return false, false, err
	}
	input.ConfirmationItems, err = f.Cloud.AutoReplyConfirmationItems(ctx, input.Credentials, input.Conversation.ID)
	if err != nil {
		return false, false, err
	}
	input.PendingMessages = nil
	input.ConditionsOnly, input.AllowGoalFollowup, input.MessageFollowup = true, true, true
	input.PageSnapshot.ExpectedSendMessageKey = anchor
	reviewCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	decision, changed, err := f.replyWhileMonitoring(reviewCtx, prepared, runtime, input.PageSnapshot, input)
	if err != nil || changed {
		return false, false, err
	}
	if decision.ManualReason != "" || len(decision.Suggestions) != 0 || len(decision.Messages) > 1 || len(decision.Messages) == 1 && decision.Messages[0].Type != "confirmation" {
		return false, false, fmt.Errorf("答复后的复核只能维护目标并准备一条未问追问")
	}
	ready, after, err := f.latestMessageAllowsFollowup(ctx, runtime, prepared, input.PageSnapshot, previousContent)
	if err != nil || !ready || after != anchor {
		return false, false, err
	}
	if err = f.verifyCurrentReplyConfiguration(ctx, prepared, input.Position); err != nil {
		return false, false, err
	}
	var followup []cloud.AutoReplyGoalFollowup
	if len(decision.Messages) == 1 {
		message := decision.Messages[0]
		followup = append(followup, cloud.AutoReplyGoalFollowup{EventKind: "message", ContextRevision: cloud.AutoReplyGoalContextRevision(input.Position), ConfirmationItemID: message.ConfirmationItemID, Content: message.Content, ExpectedContentKey: message.GoalContentKey})
	}
	if err = f.Cloud.CommitAutoReplyMessageGoalReview(ctx, input.Credentials, input.Conversation.ID, input.BasedOnMessageKey, anchor, decision.ConfirmationUpdates, followup...); err != nil {
		return false, false, err
	}
	if len(followup) == 0 {
		return false, false, nil
	}
	planned = true
	current, _, _, err := f.Cloud.AutoReplyGoalFollowupState(ctx, input.Credentials, input.Conversation.ID)
	if err != nil || current.GoalFollowup == nil {
		return planned, false, err
	}
	event := current.GoalFollowup.EventKey
	if err = f.deliverGoalFollowup(ctx, prepared, runtime, input.Position, current, input.PageSnapshot); err != nil {
		return planned, false, err
	}
	status, _, err := f.Cloud.AutoReplySendReceipt(ctx, input.Credentials, input.Conversation.ID, "goal-event:"+event, 1)
	return planned, status == "sent", err
}
