// 本文件使用真实 PostgreSQL 验证自动回复配置、会话、简历和 AI 审计的完整存储链路。
package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// TestPostgresAutoReplyStorageFlow 验证自动回复核心数据可以幂等保存并按团队安全关联。
func TestPostgresAutoReplyStorageFlow(t *testing.T) {
	dsn := os.Getenv("GOODHR_AUTO_REPLY_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("GOODHR_AUTO_REPLY_TEST_PG_DSN is not configured")
	}
	originalWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(originalWorkingDirectory) }()
	db, err := (Config{PostgresDSN: dsn}).PostgresDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	suffix := time.Now().Format("150405.000000000")
	ownerEmail := "auto-reply-" + suffix + "@example.com"
	tenantStore := NewPostgresTenantStore(db)
	positionStore := NewPostgresPositionStore(db)
	accountStore := NewPostgresPlatformAccountStore(db)
	candidateStore := NewPostgresCandidateStore(db)
	autoReplyStore := NewPostgresAutoReplyStore(db)

	tenant, err := tenantStore.GetOrCreateTenant(ownerEmail)
	if err != nil {
		t.Fatal(err)
	}
	position, err := positionStore.SavePosition(Position{UserEmail: ownerEmail, PlatformID: "liepin", Name: "自动回复测试岗位"})
	if err != nil {
		t.Fatal(err)
	}
	account, err := accountStore.SavePlatformAccount(PlatformAccount{
		UserEmail: ownerEmail, PlatformID: "liepin", DisplayName: "猎聘企业测试账号", LocalProfileID: "auto-reply-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := candidateStore.SaveCandidateProfile(CandidateProfileInput{
		UserEmail: ownerEmail, PlatformID: "liepin", PlatformCandidateID: "candidate-" + suffix,
		CandidateName: "测试候选人", Phone: "+86 176-0708-0935",
	})
	if err != nil {
		t.Fatal(err)
	}
	var userID string
	if err = db.QueryRowContext(ctx, `SELECT id::text FROM users WHERE LOWER(email)=LOWER($1)`, ownerEmail).Scan(&userID); err != nil {
		t.Fatal(err)
	}

	company, err := autoReplyStore.SaveCompanyProfile(ctx, tenant.ID, ownerEmail, CompanyProfile{
		Name: "德阳测试公司", Address: "德阳", Contact: "HR", Overview: "自动回复集成测试",
	})
	if err != nil {
		t.Fatal(err)
	}
	config, err := autoReplyStore.SavePositionAutoReplyConfig(ctx, ownerEmail, PositionAutoReplyConfig{
		TenantID: tenant.ID, PositionID: position.ID, CompanyProfileID: company.ID, Enabled: true,
		PositionDescription: "本科及以上", ResumeRequestMessage: "你好，能发一份简历吗？",
		Conditions: []PositionReplyCondition{
			{Type: "required", Content: "必须本科及以上", Enabled: true},
			{Type: "confirm", Content: "需要确认到岗时间", Enabled: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !config.Enabled || config.Version != 1 || len(config.Conditions) != 2 {
		t.Fatalf("saved auto reply config = %#v", config)
	}

	canonicalID, err := autoReplyStore.ResolveCanonicalCandidateByPhone(ctx, tenant.ID, candidate.ID, candidate.Phone)
	if err != nil {
		t.Fatal(err)
	}
	if canonicalID != candidate.ID {
		t.Fatalf("canonical candidate = %s, want %s", canonicalID, candidate.ID)
	}
	identity, err := autoReplyStore.UpsertCandidatePlatformIdentity(ctx, CandidatePlatformIdentity{
		TenantID: tenant.ID, CandidateID: candidate.ID, PlatformID: "liepin",
		PlatformAccountID: account.ID, PlatformCandidateID: "candidate-" + suffix,
		CandidateName: "测试候选人", Gender: "女", NormalizedPhone: candidate.Phone,
	})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := autoReplyStore.UpsertAutoReplyConversation(ctx, AutoReplyConversation{
		TenantID: tenant.ID, CandidateID: candidate.ID, PlatformIdentityID: identity.ID,
		PositionID: position.ID, PlatformAccountID: account.ID, PlatformID: "liepin",
		PlatformThreadID: "thread-" + suffix, CandidateName: "测试候选人", Gender: "女",
		PagePositionText: "自动回复测试岗位", Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	messages := []AutoReplyMessage{
		{PlatformMessageID: "message-1", Fingerprint: "fingerprint-1", Direction: "self", MessageType: "text", TextContent: "你好"},
		{PlatformMessageID: "message-2", Fingerprint: "fingerprint-2", Direction: "candidate", MessageType: "text", TextContent: "薪资是多少"},
	}
	firstSync, err := autoReplyStore.SyncAutoReplyMessages(ctx, tenant.ID, conversation.ID, true, messages)
	if err != nil {
		t.Fatal(err)
	}
	secondSync, err := autoReplyStore.SyncAutoReplyMessages(ctx, tenant.ID, conversation.ID, true, messages)
	if err != nil {
		t.Fatal(err)
	}
	if firstSync.Inserted != 2 || secondSync.Inserted != 0 || firstSync.LastCandidateMessageKey != "id:message-2" {
		t.Fatalf("message sync first=%#v second=%#v", firstSync, secondSync)
	}
	savedMessages, err := autoReplyStore.ListAutoReplyMessages(ctx, tenant.ID, conversation.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(savedMessages) != 2 {
		t.Fatalf("saved messages = %d, want 2", len(savedMessages))
	}

	attachment, err := autoReplyStore.SaveResumeAttachment(ctx, StoredResumeAttachment{
		TenantID: tenant.ID, CandidateID: candidate.ID, ConversationID: conversation.ID,
		SourceMessageID: savedMessages[1].ID, PlatformID: "liepin", OriginalName: "测试简历.pdf",
		StoragePath: "resumes/" + tenant.ID + "/resume.pdf", SHA256: strings.Repeat("a", 64),
		MIMEType: "application/pdf", SizeBytes: 1024, ExtractedText: "本科",
		CreatedByUserID: userID,
	})
	if err != nil {
		t.Fatal(err)
	}
	attachments, err := autoReplyStore.ListResumeAttachments(ctx, tenant.ID, candidate.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if attachment.ID == "" || len(attachments) != 1 {
		t.Fatalf("saved attachments = %#v", attachments)
	}

	confirmation, err := autoReplyStore.UpsertConfirmationItem(ctx, CandidateConfirmationItem{
		TenantID: tenant.ID, ConversationID: conversation.ID, CandidateID: candidate.ID,
		PositionID: position.ID, ItemType: "confirm", Content: "需要确认到岗时间",
		Status: "pending", SourceType: "position", SourceRef: config.Conditions[1].ID,
		CreatedByKind: "system",
	})
	if err != nil {
		t.Fatal(err)
	}
	confirmation.Status = "matched"
	confirmation.SourceType = "chat"
	confirmation.SourceRef = savedMessages[1].ID
	confirmation.EvidenceText = "候选人已确认一周内到岗"
	confirmation.CreatedByKind = "ai"
	confirmation, err = autoReplyStore.UpsertConfirmationItem(ctx, confirmation)
	if err != nil {
		t.Fatal(err)
	}
	var confirmationEventCount int
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM candidate_confirmation_events WHERE confirmation_item_id=$1`, confirmation.ID).Scan(&confirmationEventCount); err != nil {
		t.Fatal(err)
	}
	if confirmation.Status != "matched" || confirmationEventCount != 2 {
		t.Fatalf("confirmation=%#v events=%d", confirmation, confirmationEventCount)
	}

	aiRun, err := autoReplyStore.StartAutoReplyAIRun(ctx, AutoReplyAIRun{
		TenantID: tenant.ID, ConversationID: conversation.ID, CandidateID: candidate.ID,
		PositionID: position.ID, TraceID: "trace-" + suffix, Model: "test-model",
		BasedOnMessageKey: firstSync.LastCandidateMessageKey,
		InputMessages:     json.RawMessage(`[{"role":"system","content":"测试"}]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	toolCall, err := autoReplyStore.SaveAutoReplyToolCall(ctx, AutoReplyToolCall{
		TenantID: tenant.ID, AIRunID: aiRun.ID, ToolCallID: "call-1", SequenceNo: 1,
		ToolName: "查看聊天记录", ArgumentsJSON: json.RawMessage(`{"limit":20}`),
		ResultJSON: json.RawMessage(`{"ok":true}`), Status: "completed",
	})
	if err != nil {
		t.Fatal(err)
	}
	suggestion, err := autoReplyStore.SaveAutoReplyConfigSuggestion(ctx, AutoReplyConfigSuggestion{
		TenantID: tenant.ID, ConversationID: conversation.ID, PositionID: position.ID,
		SuggestionType: "position", Operation: "update", TargetID: config.Conditions[1].ID,
		ProposedValue: json.RawMessage(`{"content":"一周内到岗"}`), Reason: "候选人多次询问到岗时间",
	})
	if err != nil {
		t.Fatal(err)
	}
	aiRun.Status = "completed"
	aiRun.OutputMessage = json.RawMessage(`{"reply":"薪资面议"}`)
	aiRun.TokenUsage = 32
	aiRun, err = autoReplyStore.FinishAutoReplyAIRun(ctx, aiRun)
	if err != nil {
		t.Fatal(err)
	}
	if toolCall.ID == "" || suggestion.ID == "" || aiRun.CompletedAt == nil {
		t.Fatalf("audit run=%#v tool=%#v suggestion=%#v", aiRun, toolCall, suggestion)
	}
	notification, created, err := autoReplyStore.ClaimAutoReplyNotification(ctx, AutoReplyNotification{
		TenantID: tenant.ID, ConversationID: conversation.ID, PositionID: position.ID,
		BasedOnMessageKey: firstSync.LastCandidateMessageKey, CandidateName: "测试候选人",
		Gender: "女", PlatformID: "liepin", Reason: "岗位暂时无法唯一确认", RecipientEmail: ownerEmail,
	})
	if err != nil || !created {
		t.Fatalf("claim notification created=%t err=%v", created, err)
	}
	duplicateNotification, duplicateCreated, err := autoReplyStore.ClaimAutoReplyNotification(ctx, notification)
	if err != nil || duplicateCreated || duplicateNotification.ID != notification.ID {
		t.Fatalf("duplicate notification created=%t item=%#v err=%v", duplicateCreated, duplicateNotification, err)
	}
	notification.Status = "sent"
	notification, err = autoReplyStore.FinishAutoReplyNotification(ctx, notification)
	if err != nil || notification.SentAt == nil {
		t.Fatalf("finish notification item=%#v err=%v", notification, err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE auto_reply_ai_runs SET expires_at=now()-INTERVAL '1 second' WHERE id=$1`, aiRun.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE auto_reply_notifications SET expires_at=now()-INTERVAL '1 second' WHERE id=$1`, notification.ID); err != nil {
		t.Fatal(err)
	}
	deleted, err := autoReplyStore.DeleteExpiredAutoReplyAudit(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("deleted audit rows = %d, want 2", deleted)
	}
}
