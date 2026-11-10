// Package cloud 本文件用独立临时库验证原生简历邀请的旧卡补齐、精确来源、回填文件待办及一次性账本。
package cloud

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/storage"
)

// resumeOfferCloudFixture 建立原生邀请已被旧程序回复、但平台仍待同意的兼容场景。
func resumeOfferCloudFixture(t *testing.T) (*Client, AgentCredentials, AutoReplyConversation, AutoReplyMessage, AutoReplyMessage) {
	t.Helper()
	c, cred, _ := entityTestClient(t)
	ctx := context.Background()
	conv := entityTestConversation(t, c, cred)
	if _, err := c.EnsureLocalAutoReplyCandidate(ctx, cred, conv); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 10, 1, 0, 0, 0, time.UTC)
	offer := AutoReplyMessage{Fingerprint: "offer", Direction: "candidate", MessageType: "card", TextContent: "对方想发送附件简历给您，您是否同意", CardContent: json.RawMessage(`{"summary":"原邀约摘要"}`), PlatformSentAt: &at}
	tail := AutoReplyMessage{Fingerprint: "hr-tail", Direction: "self", MessageType: "text", TextContent: "好的，您方便时发送。"}
	syncEntityMessages(t, c, cred, conv, offer, tail)
	if err := c.CompleteAutoReplyMessage(ctx, cred, conv.ID, "offer", "sent"); err != nil {
		t.Fatal(err)
	}
	offer.CardContent = json.RawMessage(`{"info_type":"resume","info_status":"offered"}`)
	syncEntityMessages(t, c, cred, conv, offer, tail)
	conv, _, _ = c.AutoReplyAnswerConversation(ctx, cred, conv.ID)
	return c, cred, conv, offer, tail
}

// TestResumeOfferLegacyMetadataAndBackfillCommit 验证旧已回复卡不换ID、不重放问答，新PDF插到HR前仍原子登记待办。
func TestResumeOfferLegacyMetadataAndBackfillCommit(t *testing.T) {
	c, cred, conv, offer, tail := resumeOfferCloudFixture(t)
	ctx := context.Background()
	if err := c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		if err := ValidateAutoReplyResumeOfferSource(tx, conv.ID, "offer"); err != nil {
			return err
		}
		outcome, err := tx.MessageOutcome(conv.ID, "offer")
		if outcome != "sent" {
			t.Fatal("补齐改写了原处理结果", outcome)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	messages, _ := c.AutoReplyMessages(ctx, cred, conv.ID)
	if len(messages) != 2 || !IsAutoReplyResumeOfferMessage(messages[0]) {
		t.Fatal("旧摘要邀约没有精确补齐", messages)
	}
	var card map[string]any
	_ = json.Unmarshal(messages[0].CardContent, &card)
	if card["summary"] != "原邀约摘要" {
		t.Fatal("原摘要丢失")
	}
	key := AutoReplyResumeOfferSendKey("offer")
	if _, ok, err := c.ClaimAutoReplySend(ctx, cred, conv.ID, key, AutoReplyResumeOfferContent, 0); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, err := c.CommitAutoReplyResumeOffer(ctx, cred, conv.ID, "offer", "pdf"); err == nil {
		t.Fatal("未同步文件就确认接收")
	}
	file := AutoReplyMessage{Fingerprint: "pdf", Direction: "candidate", MessageType: "resume", TextContent: "候选人附件.pdf", CardContent: json.RawMessage(`{"info_type":"resume","info_status":"received"}`), PlatformSentAt: offer.PlatformSentAt}
	syncEntityMessages(t, c, cred, conv, file, tail)
	if pending, err := c.PendingAutoReplyMessages(ctx, cred, conv.ID); err != nil || len(pending) != 0 {
		t.Fatal("回填被当作新问答", pending, err)
	}
	saved, err := c.CommitAutoReplyResumeOffer(ctx, cred, conv.ID, "offer", "pdf")
	if err != nil || saved.ResumeRetry == nil || saved.ResumeRetry.SourceMessageID != "pdf" || saved.InfoRequests["resume"].Status != "received" {
		t.Fatal("回填文件没有待办", saved, err)
	}
	if status, source, err := c.AutoReplySendReceipt(ctx, cred, conv.ID, key, 0); err != nil || status != "sent" || source != "pdf" {
		t.Fatal("接收回执错误", status, source, err)
	}
	claimed, ok, err := c.ClaimAutoReplyResumeRetry(ctx, cred, conv.ID, "pdf")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	repeated, err := c.CommitAutoReplyResumeOffer(ctx, cred, conv.ID, "offer", "pdf")
	if err != nil || repeated.ResumeRetry.Attempts != claimed.ResumeRetry.Attempts || !repeated.ResumeRetry.NextAttemptAt.Equal(claimed.ResumeRetry.NextAttemptAt) {
		t.Fatal("重复提交清空了下载租约", err)
	}
	if _, ok, err := c.ClaimAutoReplySend(ctx, cred, conv.ID, key, AutoReplyResumeOfferContent, 0); err != nil || ok {
		t.Fatal("已同意邀请可再次领取", ok, err)
	}
	if _, err := c.EnqueueAutoReplyResumeRetry(ctx, cred, conv.ID, "file-B"); err != nil {
		t.Fatal(err)
	}
	current, err := c.CommitAutoReplyResumeOffer(ctx, cred, conv.ID, "offer", "pdf")
	if err != nil || current.ResumeRetry == nil || current.ResumeRetry.SourceMessageID != "file-B" {
		t.Fatal("重复同意回执覆盖了后来文件B", current.ResumeRetry, err)
	}
}

// TestResumeOfferRejectsWrongSourceAndGeneration 验证邀请本身、旧文件、普通卡、跨会话和删除前来源均不能冒充新附件。
func TestResumeOfferRejectsWrongSourceAndGeneration(t *testing.T) {
	for _, mode := range []string{"offer_as_file", "older_file", "generic_card", "unknown_intent", "closed", "deleted", "other_conversation"} {
		t.Run(mode, func(t *testing.T) {
			c, cred, conv, offer, tail := resumeOfferCloudFixture(t)
			ctx := context.Background()
			file := AutoReplyMessage{Fingerprint: "pdf", Direction: "candidate", MessageType: "resume", TextContent: "附件.pdf", CardContent: json.RawMessage(`{"info_type":"resume","info_status":"received"}`), PlatformSentAt: offer.PlatformSentAt}
			source := "pdf"
			if mode == "older_file" {
				earlier := offer.PlatformSentAt.Add(-time.Minute)
				file.PlatformSentAt = &earlier
			}
			if mode == "generic_card" {
				file.MessageType = "card"
			}
			if mode == "other_conversation" {
				other, err := c.SaveAutoReplyConversation(ctx, cred, AutoReplyConversation{PlatformID: "boss", PlatformAccountID: "account-one", PlatformThreadID: "other"})
				if err != nil {
					t.Fatal(err)
				}
				syncEntityMessages(t, c, cred, other, file)
			} else {
				syncEntityMessages(t, c, cred, conv, file, tail)
			}
			if mode == "offer_as_file" {
				source = "offer"
			}
			key := AutoReplyResumeOfferSendKey("offer")
			if _, _, err := c.ClaimAutoReplySend(ctx, cred, conv.ID, key, AutoReplyResumeOfferContent, 0); err != nil {
				t.Fatal(err)
			}
			if mode == "unknown_intent" {
				if err := c.FinishAutoReplySend(ctx, cred, conv.ID, key, 0, "unknown"); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "closed" || mode == "deleted" {
				if err := c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
					current, _, err := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", conv.ID)
					if err != nil {
						return err
					}
					if mode == "closed" {
						current.Status = "closed"
					} else {
						current.DeletionBoundary = &AutoReplyDeletionBoundary{DeletedAt: *offer.PlatformSentAt}
					}
					return tx.Put("conversations", current.ID, current)
				}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := c.CommitAutoReplyResumeOffer(ctx, cred, conv.ID, "offer", source); err == nil {
				t.Fatal("不可信来源被确认", mode)
			}
			current, _, _ := c.AutoReplyAnswerConversation(ctx, cred, conv.ID)
			if current.ResumeRetry != nil {
				t.Fatal("失败创建了下载待办")
			}
		})
	}
}
