// Package cloud 本文件用独立临时SQLite验证简历待办领取、账号隔离、崩溃恢复及补审事务。
package cloud

import (
	"context"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/storage"
)

// makeResumeRetryDue 仅为合成测试推进持久时间，不依赖等待或用户数据库。
func makeResumeRetryDue(t *testing.T, c *Client, cred AgentCredentials, id string) {
	t.Helper()
	if err := c.autoReplyTransaction(context.Background(), cred, func(tx *storage.AutoReplyTx) error {
		v, _, err := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", id)
		if err != nil {
			return err
		}
		v.ResumeRetry.NextAttemptAt = time.Now().UTC().Add(-time.Minute)
		return tx.Put("conversations", id, v)
	}); err != nil {
		t.Fatal(err)
	}
}

// TestResumeRetryClaimPersistsBeforeWork 验证重启不重复领取、旧页面保存不丢待办、到期无消息恢复且最多三次。
func TestResumeRetryClaimPersistsBeforeWork(t *testing.T) {
	c, cred, store := entityTestClient(t)
	ctx := context.Background()
	v := entityTestConversation(t, c, cred)
	if _, err := c.EnsureLocalAutoReplyCandidate(ctx, cred, v); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := c.ClaimAutoReplyResumeRetry(ctx, cred, v.ID, "resume-card")
	if err != nil || !ok || claimed.ResumeRetry.Attempts != 1 {
		t.Fatal(claimed, ok, err)
	}
	if saved, err := c.SaveAutoReplyConversation(ctx, cred, v); err != nil || saved.ResumeRetry == nil || saved.ResumeRetry.Attempts != 1 {
		t.Fatal("旧快照丢失待办", saved, err)
	}
	restarted := New("")
	restarted.EnableLocalAutoReply(store, c.localAutoDir)
	if _, ok, err := restarted.ClaimAutoReplyResumeRetry(ctx, cred, v.ID, "resume-card"); err != nil || ok {
		t.Fatal("重启重复领取", ok, err)
	}
	items, err := c.ListPendingAutoReplyConversations(ctx, cred, v.PlatformID, v.PlatformAccountID)
	if err != nil || len(items) != 0 {
		t.Fatal("租约内不应再次调度", items, err)
	}
	for attempt := 1; attempt <= 3; attempt++ {
		if err = c.FinishAutoReplyResumeRetry(ctx, cred, v.ID, "resume-card", false, "合成失败"); err != nil {
			t.Fatal(err)
		}
		makeResumeRetryDue(t, c, cred, v.ID)
		items, err = c.ListPendingAutoReplyConversations(ctx, cred, v.PlatformID, v.PlatformAccountID)
		if err != nil || (len(items) == 1) != (attempt < 3) {
			t.Fatal("到期或次数筛选错误", attempt, items, err)
		}
		other := cred
		other.Token = "other-owner"
		foreign, err := c.ListPendingAutoReplyConversations(ctx, other, v.PlatformID, v.PlatformAccountID)
		if err != nil || len(foreign) != 0 {
			t.Fatal("串GoodHR账号", foreign, err)
		}
		foreign, err = c.ListPendingAutoReplyConversations(ctx, cred, v.PlatformID, "another-account")
		if err != nil || len(foreign) != 0 {
			t.Fatal("串招聘账号", foreign, err)
		}
		if _, ok, err = c.ClaimAutoReplyResumeRetry(ctx, cred, v.ID, "resume-card"); err != nil || ok != (attempt < 3) {
			t.Fatal("领取次数错误", attempt, ok, err)
		}
	}
	claimed, ok, err = c.ClaimAutoReplyResumeRetry(ctx, cred, v.ID, "new-resume")
	if err != nil || !ok || claimed.ResumeRetry.Attempts != 1 {
		t.Fatal("新来源未独立计数", claimed, ok, err)
	}
	if err = c.FinishAutoReplyResumeRetry(ctx, cred, v.ID, "resume-card", true, ""); err == nil {
		t.Fatal("旧来源清除了新待办")
	}
	if err = c.FinishAutoReplyResumeRetry(ctx, cred, v.ID, "new-resume", true, ""); err != nil {
		t.Fatal(err)
	}
	items, err = c.ListPendingAutoReplyConversations(ctx, cred, v.PlatformID, v.PlatformAccountID)
	if err != nil || len(items) != 0 {
		t.Fatal("成功后待办未结束", items, err)
	}
}

// TestResumeProposalChecksVersionAndRegistersNewReceipt 验证已答消息补审使用独立收据，过期资料和半份修改均不落库。
func TestResumeProposalChecksVersionAndRegistersNewReceipt(t *testing.T) {
	c, cred, _ := entityTestClient(t)
	ctx := context.Background()
	v := entityTestConversation(t, c, cred)
	candidate, err := c.EnsureLocalAutoReplyCandidate(ctx, cred, v)
	if err != nil {
		t.Fatal(err)
	}
	v.CandidateID = candidate.ID
	syncEntityMessages(t, c, cred, v, AutoReplyMessage{PlatformMessageID: "m1", Direction: "candidate", TextContent: "合成问题"})
	if err = c.CommitAutoReplyProposal(ctx, cred, v.ID, "m1", 1, nil, nil); err != nil {
		t.Fatal(err)
	}
	receipts, err := c.PendingAutoReplyProposals(ctx, cred)
	if err != nil || len(receipts) != 1 {
		t.Fatal(receipts, err)
	}
	if err = c.CompleteAutoReplyProposal(ctx, cred, receipts[0].ID); err != nil {
		t.Fatal(err)
	}
	if err = c.CompleteAutoReplyMessage(ctx, cred, v.ID, "m1", "completed"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := c.ClaimAutoReplyResumeRetry(ctx, cred, v.ID, "resume-card"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	candidate.ResumeStructured, candidate.RawText = true, "合成简历资料"
	candidate.ResumeSourceMessageID = "resume-card"
	if err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error { return tx.Put("candidates", candidate.ID, candidate) }); err != nil {
		t.Fatal(err)
	}
	item := CandidateConfirmationItem{ID: "condition", ConversationID: v.ID, CandidateID: candidate.ID, PositionID: v.PositionID, Content: "条件", ItemType: "required", Status: "matched", SourceType: "ai"}
	for _, stale := range []struct{ message, source, revision string }{{"old", "resume-card", AutoReplyResumeRevision(candidate)}, {"m1", "old-resume", AutoReplyResumeRevision(candidate)}, {"m1", "resume-card", "stale"}} {
		if err = c.CommitAutoReplyResumeProposal(ctx, cred, v.ID, stale.message, stale.source, stale.revision, 1, []CandidateConfirmationItem{item}); err == nil {
			t.Fatal("陈旧补审被接受")
		}
	}
	invalid := item
	invalid.ID = "other"
	invalid.PositionID = "another-position"
	if err = c.CommitAutoReplyResumeProposal(ctx, cred, v.ID, "m1", "resume-card", AutoReplyResumeRevision(candidate), 1, []CandidateConfirmationItem{item, invalid}); err == nil {
		t.Fatal("跨岗位条件未拒绝")
	}
	conditions, err := c.AutoReplyConfirmationItems(ctx, cred, v.ID)
	if err != nil || len(conditions) != 0 {
		t.Fatal("失败补审留下部分条件", conditions, err)
	}
	if err = c.CommitAutoReplyResumeProposal(ctx, cred, v.ID, "m1", "resume-card", AutoReplyResumeRevision(candidate), 1, []CandidateConfirmationItem{item}); err != nil {
		t.Fatal(err)
	}
	receipts, err = c.PendingAutoReplyProposals(ctx, cred)
	if err != nil || len(receipts) != 1 || receipts[0].Conversation.ResumeRetry != nil {
		t.Fatal("补审未登记独立收据或没有结束待办", receipts, err)
	}
	if _, ok, err := c.ClaimAutoReplyResumeRetry(ctx, cred, "unknown", "resume-card"); err == nil || ok {
		t.Fatal("未知会话被领取", ok, err)
	}
}

// TestResumeSourceBindingSurvivesPartialSave 验证新简历来源更新会改变版本，普通空字段保存和头像更新不丢来源。
func TestResumeSourceBindingSurvivesPartialSave(t *testing.T) {
	c, cred, _ := entityTestClient(t)
	ctx := context.Background()
	v := entityTestConversation(t, c, cred)
	candidate, err := c.EnsureLocalAutoReplyCandidate(ctx, cred, v)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"old-resume", "", "new-resume"} {
		_, err = c.SaveAutoReplyCandidate(ctx, cred, AutoReplyCandidateInput{LocalCandidateID: candidate.ID, ResumeSourceMessageID: source, PositionID: v.PositionID, PlatformID: v.PlatformID, PlatformAccountID: v.PlatformAccountID, StructuredCandidate: StructuredCandidate{CandidateName: "合成候选", RawText: "合成资料"}})
		if err != nil {
			t.Fatal(err)
		}
		state, err := c.AutoReplyCandidateState(ctx, cred, AutoReplyCandidateLookup{PlatformID: v.PlatformID, PlatformAccountID: v.PlatformAccountID, PlatformThreadID: v.PlatformThreadID})
		want := source
		if want == "" {
			want = "old-resume"
		}
		if err != nil || state.Candidate == nil || state.Candidate.ResumeSourceMessageID != want {
			t.Fatal("来源绑定丢失", state.Candidate, err)
		}
		candidate = *state.Candidate
	}
	revision := AutoReplyResumeRevision(candidate)
	candidate.AvatarURL = "local-avatar.png"
	if AutoReplyResumeRevision(candidate) != revision {
		t.Fatal("头像变化不应触发简历补审")
	}
	candidate.ResumeSourceMessageID = "third-resume"
	if AutoReplyResumeRevision(candidate) == revision {
		t.Fatal("新简历来源未改变资料版本")
	}
}

// TestResumeManualResetIsOwnerScopedAndDoesNotTouchSends 验证人工重试只重置当前账号耗尽项，第三次运行中仍受租约保护。
func TestResumeManualResetIsOwnerScopedAndDoesNotTouchSends(t *testing.T) {
	c, cred, _ := entityTestClient(t)
	ctx := context.Background()
	other := cred
	other.Token = "another-owner"
	for _, owner := range []AgentCredentials{cred, other} {
		v := entityTestConversation(t, c, owner)
		if err := c.autoReplyTransaction(ctx, owner, func(tx *storage.AutoReplyTx) error {
			v.ResumeRetry = &AutoReplyResumeRetry{Attempts: 3, SourceMessageID: "source", LastError: "合成失败", NextAttemptAt: time.Now().UTC().Add(time.Hour)}
			if err := tx.Put("conversations", v.ID, v); err != nil {
				return err
			}
			return tx.Put("sends", "synthetic-send", localAutoReplySend{ConversationID: v.ID, MessageKey: "answered", Status: "sent", Content: "合成已发回复"})
		}); err != nil {
			t.Fatal(err)
		}
	}
	if count, err := c.CountBlockedAutoReplyResumeRetries(ctx, cred); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if count, err := c.ResetAutoReplyResumeRetries(ctx, cred); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if count, err := c.CountBlockedAutoReplyResumeRetries(ctx, other); err != nil || count != 1 {
		t.Fatal("重置串账号", count, err)
	}
	for _, owner := range []AgentCredentials{cred, other} {
		if err := c.autoReplyTransaction(ctx, owner, func(tx *storage.AutoReplyTx) error {
			send, found, err := getAutoReplyEntity[localAutoReplySend](tx, "sends", "synthetic-send")
			if err != nil {
				return err
			}
			if !found || send.Status != "sent" || send.Content != "合成已发回复" {
				t.Fatal("重置修改了发送记录", send)
			}
			items, err := listAutoReplyEntities[AutoReplyConversation](tx, "conversations")
			if err != nil {
				return err
			}
			want := 0
			if owner.Token == other.Token {
				want = 3
			}
			if items[0].ResumeRetry.Attempts != want || items[0].ResumeRetry.SourceMessageID != "source" {
				t.Fatal("重置丢失来源或次数错误", items[0])
			}
			lease := time.Now().UTC().Add(time.Minute)
			items[0].ResumeRetry.Attempts, items[0].ResumeRetry.LeaseUntil = 3, &lease
			return tx.Put("conversations", items[0].ID, items[0])
		}); err != nil {
			t.Fatal(err)
		}
	}
	if count, err := c.CountBlockedAutoReplyResumeRetries(ctx, cred); err != nil || count != 0 {
		t.Fatal("第三次正在运行却显示耗尽", count, err)
	}
	if count, err := c.ResetAutoReplyResumeRetries(ctx, cred); err != nil || count != 0 {
		t.Fatal("人工重置抢占了运行中尝试", count, err)
	}
}

// TestResumeEnqueueDoesNotSpendAttemptsOrResetExistingWork 验证先答后采集的登记不收费，重复登记不清已有租约或耗尽次数。
func TestResumeEnqueueDoesNotSpendAttemptsOrResetExistingWork(t *testing.T) {
	c, cred, _ := entityTestClient(t)
	ctx := context.Background()
	v := entityTestConversation(t, c, cred)
	if _, err := c.EnsureLocalAutoReplyCandidate(ctx, cred, v); err != nil {
		t.Fatal(err)
	}
	v, err := c.EnqueueAutoReplyResumeRetry(ctx, cred, v.ID, "source")
	if err != nil || v.ResumeRetry == nil || v.ResumeRetry.Attempts != 0 || v.ResumeRetry.LeaseUntil != nil || v.ResumeRetry.NextAttemptAt.After(time.Now()) {
		t.Fatal("登记提前占用尝试", v, err)
	}
	claimed, ok, err := c.ClaimAutoReplyResumeRetry(ctx, cred, v.ID, "source")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	v, err = c.EnqueueAutoReplyResumeRetry(ctx, cred, v.ID, "source")
	if err != nil || v.ResumeRetry.Attempts != 1 || v.ResumeRetry.LeaseUntil == nil || !v.ResumeRetry.LeaseUntil.Equal(*claimed.ResumeRetry.LeaseUntil) {
		t.Fatal("重复登记重置了执行中待办", v, err)
	}
	v, err = c.EnqueueAutoReplyResumeRetry(ctx, cred, v.ID, "new-source")
	if err != nil || v.ResumeRetry.Attempts != 0 || v.ResumeRetry.LeaseUntil != nil || v.ResumeRetry.SourceMessageID != "new-source" {
		t.Fatal("新来源未独立登记", v, err)
	}
}
