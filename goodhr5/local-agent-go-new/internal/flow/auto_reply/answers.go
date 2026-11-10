// Package auto_reply 本文件将HR核实后的岗位公开答复一次送回原对话，沿用配置、身份、最新消息及发送账本保护。
package auto_reply

import (
	"context"
	"errors"
	"fmt"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"strings"
)

// readAnswerQueue 核对当前招聘账号、启用岗位与本次运行指定的人选，只有可核实的原会话才能进入页面操作队列。
func (f *Flow) readAnswerQueue(ctx context.Context, prepared shared.PreparedTask, positions []cloud.AutoReplyPositionSnapshot) ([]cloud.AutoReplyConversation, error) {
	rows, err := f.Cloud.PendingAutoReplyAnswers(ctx, credentials(prepared), prepared.Platform.ID, prepared.Request.PlatformAccountID)
	if err != nil {
		return nil, err
	}
	f.serverAnswers = map[string][]cloud.AutoReplyAnswer{}
	var queue []cloud.AutoReplyConversation
	allowed := map[string]bool{}
	for _, p := range positions {
		if p.Config.Enabled /* 会员限制暂停：&& p.Subscription.Active && p.Subscription.AllowAutoReply */ {
			allowed[p.Position.ID] = true
		}
	}
	for _, a := range rows {
		if a.ID == "" || a.SourceMessageKey == "" || a.PlatformThreadID == "" || a.ConversationID == "" || !allowed[a.PositionID] || a.PlatformID != prepared.Platform.ID || a.PlatformAccountID != prepared.Request.PlatformAccountID {
			continue
		}
		c, found, err := f.Cloud.AutoReplyAnswerConversation(ctx, credentials(prepared), a.ConversationID)
		if err != nil {
			return nil, err
		}
		if !found || c.CandidateID == "" || c.PositionID != a.PositionID || c.PlatformThreadID != a.PlatformThreadID || c.PlatformAccountID != a.PlatformAccountID || c.PlatformID != a.PlatformID {
			continue
		}
		item := model.Conversation{Key: c.PlatformThreadID, PlatformThreadID: c.PlatformThreadID, PlatformAccountID: c.PlatformAccountID, Name: c.CandidateName}
		if c.CaptureIntent != nil {
			item.PlatformCandidateID = c.CaptureIntent.PlatformCandidateID
		}
		if !targetConversationAllowed(prepared.Request.AutoReplyTarget, item) {
			continue
		}
		if len(f.serverAnswers[c.ID]) == 0 {
			queue = append(queue, c)
		}
		f.serverAnswers[c.ID] = append(f.serverAnswers[c.ID], a)
	}
	return queue, nil
}

// deliverAnswer 使用独立于候选人新消息的补答编号；程序刚刚同步过页面，仍有未答新消息时此入口不会执行。
func (f *Flow) deliverAnswer(ctx context.Context, p shared.PreparedTask, rt model.AutoReplyRuntime, pos cloud.AutoReplyPositionSnapshot, conv cloud.AutoReplyConversation, page model.AutoReplyConversationSnapshot, state cloud.AutoReplyCandidateState, a cloud.AutoReplyAnswer, stats *shared.Stats) error {
	if conv.ID != a.ConversationID || pos.Position.ID != a.PositionID || page.PlatformAccountID != a.PlatformAccountID || page.PlatformThreadID != a.PlatformThreadID || !targetConversationAllowed(p.Request.AutoReplyTarget, page.Conversation) {
		return fmt.Errorf("补答的原候选人、岗位或招聘账号尚未核对")
	}
	cred := credentials(p)
	key := "question-answer:" + a.ID
	status, outbound, err := f.Cloud.AutoReplySendReceipt(ctx, cred, conv.ID, key, 1)
	if err != nil {
		return err
	}
	if status != "" {
		if status == "sent" && outbound != "" {
			return f.Cloud.FinishAutoReplyAnswer(ctx, cred, a, "sent", outbound)
		}
		return f.Cloud.FinishAutoReplyAnswer(ctx, cred, a, "manual", "")
	}
	history, err := f.Cloud.AutoReplyMessages(ctx, cred, conv.ID)
	if err != nil {
		return err
	}
	_, found := findBasedOnMessage(history, a.SourceMessageKey)
	if !found {
		return f.Cloud.FinishAutoReplyAnswer(ctx, cred, a, "manual", "")
	}
	answer := common.NormalizeAutoReplyOutgoingText(p.Platform, a.Answer)
	if answer == "" || len([]rune(answer)) > maxAutoReplyMessageRunes || strings.ContainsAny(answer, "{}｛｝") || checkPhoneReply(inspectPhoneContact(ReplyContext{Position: pos, CandidateState: state, PageSnapshot: page, Messages: history}), pos.Config, answer) != nil {
		return f.Cloud.FinishAutoReplyAnswer(ctx, cred, a, "manual", "")
	}
	latest, stillOpen, err := rt.ReadOpenAutoReplyLatestMessage(ctx, f.Browser, p.Platform, page)
	if err != nil {
		return err
	}
	if !stillOpen {
		return nil
	}
	anchor := firstNonEmpty(latest.PlatformMessageID, latest.Key)
	if anchor == "" || len(history) == 0 {
		return nil
	}
	stored := history[len(history)-1]
	if anchor != firstNonEmpty(stored.PlatformMessageID, stored.Fingerprint, stored.ID) || stored.Direction != latest.Direction {
		return nil
	}
	if latest.Direction == "self" && strings.TrimSpace(latest.TextContent) == answer {
		return f.Cloud.FinishAutoReplyAnswer(ctx, cred, a, "manual", "")
	}
	if err = f.verifyCurrentReplyConfiguration(ctx, p, pos); err != nil {
		return err
	}
	claimed, err := f.Cloud.ClaimAutoReplyAnswer(ctx, cred, a)
	if err != nil || !claimed {
		return err
	}
	page.ExpectedSendMessageKey = anchor
	_, err = f.sendVerifiedMessage(cloud.WithAutoReplyTextPolicy(ctx, pos, ""), p, rt, conv, page, key, answer, false, 1)
	if errors.Is(err, model.ErrAutoReplySupersededBeforeSend) {
		return nil
	}
	if errors.Is(err, errAutoReplySendNeedsReview) {
		return f.Cloud.FinishAutoReplyAnswer(ctx, cred, a, "manual", "")
	}
	if err != nil {
		return err
	}
	status, outbound, err = f.Cloud.AutoReplySendReceipt(ctx, cred, conv.ID, key, 1)
	if err != nil {
		return err
	}
	if status != "sent" || outbound == "" {
		return f.Cloud.FinishAutoReplyAnswer(ctx, cred, a, "manual", "")
	}
	if err = f.Cloud.FinishAutoReplyAnswer(ctx, cred, a, "sent", outbound); err != nil {
		return err
	}
	stats.Succeeded++
	return nil
}
