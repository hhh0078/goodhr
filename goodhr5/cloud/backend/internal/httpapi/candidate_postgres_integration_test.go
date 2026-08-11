// Package httpapi 本文件使用独立 PostgreSQL 测试库验证简历筛选、稳定排序和删除级联。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// openCandidatePostgresIntegrationDB 打开 GOODHR_TEST_DATABASE_URL 指定的专用测试库并执行迁移。
func openCandidatePostgresIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("GOODHR_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("GOODHR_TEST_DATABASE_URL is not configured")
	}
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位候选人 PostgreSQL 测试文件")
	}
	originalWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	backendDirectory := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "../.."))
	if err = os.Chdir(backendDirectory); err != nil {
		t.Fatal(err)
	}
	db, openErr := (Config{PostgresDSN: dsn}).PostgresDB()
	restoreErr := os.Chdir(originalWorkingDirectory)
	if openErr != nil {
		t.Fatal(openErr)
	}
	if restoreErr != nil {
		_ = db.Close()
		t.Fatal(restoreErr)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// registerCandidatePostgresTenantCleanup 注册测试团队及其关联数据的级联清理。
func registerCandidatePostgresTenantCleanup(t *testing.T, db *sql.DB, tenantID string) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := db.Exec(`DELETE FROM tenants WHERE id=$1`, tenantID); err != nil {
			t.Errorf("清理候选人测试团队失败：%v", err)
		}
	})
}

// assertCandidatePostgresMigrationReady 验证头像迁移真实执行，而不是只留下迁移标记。
func assertCandidatePostgresMigrationReady(t *testing.T, db *sql.DB) {
	t.Helper()
	var migrationApplied, constraintExists bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE filename='0078_candidate_avatar_self_hosting.sql')`).Scan(&migrationApplied); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='candidate_profiles_avatar_url_self_hosted')`).Scan(&constraintExists); err != nil {
		t.Fatal(err)
	}
	if !migrationApplied || !constraintExists {
		t.Fatalf("候选人头像迁移未完整执行：migration=%t constraint=%t", migrationApplied, constraintExists)
	}
}

// TestPostgresCandidateFiltersAndPagination 验证真实数据库组合筛选、总数、评分和同时间排序稳定。
func TestPostgresCandidateFiltersAndPagination(t *testing.T) {
	db := openCandidatePostgresIntegrationDB(t)
	assertCandidatePostgresMigrationReady(t, db)
	ctx := context.Background()
	suffix := strings.ReplaceAll(time.Now().UTC().Format("20060102150405.000000000"), ".", "")
	email := "candidate-filter-" + suffix + "@example.com"
	tenantStore := NewPostgresTenantStore(db)
	positionStore := NewPostgresPositionStore(db)
	accountStore := NewPostgresPlatformAccountStore(db)
	candidateStore := NewPostgresCandidateStore(db)
	autoReplyStore := NewPostgresAutoReplyStore(db)
	tenant, err := tenantStore.GetOrCreateTenant(email)
	if err != nil {
		t.Fatal(err)
	}
	registerCandidatePostgresTenantCleanup(t, db, tenant.ID)
	position, err := positionStore.SavePosition(Position{UserEmail: email, PlatformID: "zhaopin", Name: "筛选测试岗位"})
	if err != nil {
		t.Fatal(err)
	}
	accountA, err := accountStore.SavePlatformAccount(PlatformAccount{UserEmail: email, PlatformID: "zhaopin", DisplayName: "智联账号甲", LocalProfileID: "filter-a-" + suffix})
	if err != nil {
		t.Fatal(err)
	}
	accountB, err := accountStore.SavePlatformAccount(PlatformAccount{UserEmail: email, PlatformID: "zhaopin", DisplayName: "智联账号乙", LocalProfileID: "filter-b-" + suffix})
	if err != nil {
		t.Fatal(err)
	}
	high, err := candidateStore.SaveCandidateProfile(CandidateProfileInput{UserEmail: email, PlatformID: "boss", PlatformCandidateID: "high-" + suffix, CandidateName: "高分候选人", Phone: "13800000001"})
	if err != nil {
		t.Fatal(err)
	}
	low, err := candidateStore.SaveCandidateProfile(CandidateProfileInput{UserEmail: email, PlatformID: "boss", PlatformCandidateID: "low-" + suffix, CandidateName: "低分候选人", Phone: "13800000002"})
	if err != nil {
		t.Fatal(err)
	}
	noPhone, err := candidateStore.SaveCandidateProfile(CandidateProfileInput{UserEmail: email, PlatformID: "boss", PlatformCandidateID: "no-phone-" + suffix, CandidateName: "待确认候选人"})
	if err != nil {
		t.Fatal(err)
	}
	sourceOnly, err := candidateStore.SaveCandidateProfile(CandidateProfileInput{UserEmail: email, PlatformID: "zhaopin", PlatformCandidateID: "source-only-" + suffix, CandidateName: "只有来源平台"})
	if err != nil {
		t.Fatal(err)
	}
	identityOnly, err := candidateStore.SaveCandidateProfile(CandidateProfileInput{UserEmail: email, PlatformID: "boss", PlatformCandidateID: "identity-only-" + suffix, CandidateName: "只有平台身份"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = autoReplyStore.UpsertCandidatePlatformIdentity(ctx, CandidatePlatformIdentity{TenantID: tenant.ID, CandidateID: identityOnly.ID, PlatformID: "zhaopin", PlatformCandidateID: "identity-zhaopin-" + suffix, CandidateName: identityOnly.CandidateName}); err != nil {
		t.Fatal(err)
	}
	highEngagementA, err := candidateStore.UpsertCandidateEngagement(CandidateEngagement{CandidateID: high.ID, UserEmail: email, PositionID: position.ID, PlatformAccountID: accountA.ID, PlatformID: "zhaopin"})
	if err != nil {
		t.Fatal(err)
	}
	highEngagementB, err := candidateStore.UpsertCandidateEngagement(CandidateEngagement{CandidateID: high.ID, UserEmail: email, PositionID: position.ID, PlatformAccountID: accountB.ID, PlatformID: "zhaopin"})
	if err != nil {
		t.Fatal(err)
	}
	lowEngagement, err := candidateStore.UpsertCandidateEngagement(CandidateEngagement{CandidateID: low.ID, UserEmail: email, PositionID: position.ID, PlatformAccountID: accountA.ID, PlatformID: "zhaopin"})
	if err != nil {
		t.Fatal(err)
	}
	noPhoneEngagement, err := candidateStore.UpsertCandidateEngagement(CandidateEngagement{CandidateID: noPhone.ID, UserEmail: email, PositionID: position.ID, PlatformAccountID: accountA.ID, PlatformID: "zhaopin"})
	if err != nil {
		t.Fatal(err)
	}
	tieTime := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	if _, err = db.ExecContext(ctx, `UPDATE candidate_engagements SET created_at=$1 WHERE id IN ($2,$3)`, tieTime, highEngagementA.ID, highEngagementB.ID); err != nil {
		t.Fatal(err)
	}
	winner := highEngagementA
	if highEngagementB.ID > highEngagementA.ID {
		winner = highEngagementB
	}
	scoreA, scoreB, lowScore, pendingScore := 86.0, 92.0, 70.0, 60.0
	eventA, err := candidateStore.SaveCandidateEvent(CandidateEvent{CandidateID: high.ID, EngagementID: winner.ID, PositionID: position.ID, PlatformID: "zhaopin", EventType: "greet_analysis", Score: &scoreA})
	if err != nil {
		t.Fatal(err)
	}
	eventB, err := candidateStore.SaveCandidateEvent(CandidateEvent{CandidateID: high.ID, EngagementID: winner.ID, PositionID: position.ID, PlatformID: "zhaopin", EventType: "greet_analysis", Score: &scoreB})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE candidate_events SET created_at=$1 WHERE id IN ($2,$3)`, tieTime, eventA.ID, eventB.ID); err != nil {
		t.Fatal(err)
	}
	expectedHighScore := scoreA
	if eventB.ID > eventA.ID {
		expectedHighScore = scoreB
	}
	if _, err = candidateStore.SaveCandidateEvent(CandidateEvent{CandidateID: low.ID, EngagementID: lowEngagement.ID, PositionID: position.ID, PlatformID: "zhaopin", EventType: "greet_analysis", Score: &lowScore}); err != nil {
		t.Fatal(err)
	}
	if _, err = candidateStore.SaveCandidateEvent(CandidateEvent{CandidateID: noPhone.ID, EngagementID: noPhoneEngagement.ID, PositionID: position.ID, PlatformID: "zhaopin", EventType: "greet_analysis", Score: &pendingScore}); err != nil {
		t.Fatal(err)
	}
	for _, reviewState := range []struct {
		candidateID string
		status      string
	}{{high.ID, "qualified"}, {low.ID, "qualified"}, {noPhone.ID, "collecting"}} {
		review, reviewErr := autoReplyStore.EnsureCandidatePositionReview(ctx, tenant.ID, reviewState.candidateID, position.ID)
		if reviewErr != nil {
			t.Fatal(reviewErr)
		}
		if _, reviewErr = db.ExecContext(ctx, `UPDATE candidate_position_reviews SET status=$2 WHERE id=$1`, review.ID, reviewState.status); reviewErr != nil {
			t.Fatal(reviewErr)
		}
	}
	query := PositionCandidateQuery{PositionID: position.ID, PlatformID: "zhaopin", PhoneStatus: "has", ConditionStatus: "all_matched", Sort: "second_score_desc", PageSize: 1}
	for attempt := 0; attempt < 5; attempt++ {
		firstPage, listErr := candidateStore.ListPositionCandidates(tenant.ID, query)
		if listErr != nil {
			t.Fatal(listErr)
		}
		query.Page = 2
		secondPage, secondErr := candidateStore.ListPositionCandidates(tenant.ID, query)
		query.Page = 0
		if secondErr != nil {
			t.Fatal(secondErr)
		}
		if firstPage.Total != 2 || secondPage.Total != 2 || len(firstPage.Items) != 1 || len(secondPage.Items) != 1 || firstPage.Items[0].ID != high.ID || secondPage.Items[0].ID != low.ID {
			t.Fatalf("第 %d 次稳定分页不正确：first=%+v second=%+v", attempt+1, firstPage, secondPage)
		}
		if firstPage.Items[0].EngagementID != winner.ID || firstPage.Items[0].AIGreetScore == nil || *firstPage.Items[0].AIGreetScore != expectedHighScore {
			t.Fatalf("同时间触达或评分选择不稳定：%+v", firstPage.Items[0])
		}
	}
	pending, err := candidateStore.ListPositionCandidates(tenant.ID, PositionCandidateQuery{PositionID: position.ID, PlatformID: "zhaopin", PhoneStatus: "none", ConditionStatus: "pending"})
	if err != nil || pending.Total != 1 || pending.Items[0].ID != noPhone.ID {
		t.Fatalf("待确认且无手机号筛选错误：items=%+v err=%v", pending.Items, err)
	}
	platformOnly, err := candidateStore.ListPositionCandidates(tenant.ID, PositionCandidateQuery{PlatformID: "zhaopin", PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, item := range platformOnly.Items {
		found[item.ID] = true
	}
	if platformOnly.Total != 4 || found[sourceOnly.ID] || !found[high.ID] || !found[low.ID] || !found[noPhone.ID] || !found[identityOnly.ID] {
		t.Fatalf("平台筛选错误：total=%d found=%v", platformOnly.Total, found)
	}
}

// TestPostgresDeleteCandidateCascadeAndTenantIsolation 验证删除严格限定团队并清理所有候选人关联记录。
func TestPostgresDeleteCandidateCascadeAndTenantIsolation(t *testing.T) {
	db := openCandidatePostgresIntegrationDB(t)
	assertCandidatePostgresMigrationReady(t, db)
	ctx := context.Background()
	suffix := strings.ReplaceAll(time.Now().UTC().Format("20060102150405.000000000"), ".", "")
	tenantStore := NewPostgresTenantStore(db)
	positionStore := NewPostgresPositionStore(db)
	candidateStore := NewPostgresCandidateStore(db)
	autoReplyStore := NewPostgresAutoReplyStore(db)
	emailA, emailB := "candidate-delete-a-"+suffix+"@example.com", "candidate-delete-b-"+suffix+"@example.com"
	tenantA, err := tenantStore.GetOrCreateTenant(emailA)
	if err != nil {
		t.Fatal(err)
	}
	registerCandidatePostgresTenantCleanup(t, db, tenantA.ID)
	tenantB, err := tenantStore.GetOrCreateTenant(emailB)
	if err != nil {
		t.Fatal(err)
	}
	registerCandidatePostgresTenantCleanup(t, db, tenantB.ID)
	position, err := positionStore.SavePosition(Position{UserEmail: emailA, PlatformID: "liepin", Name: "删除测试岗位"})
	if err != nil {
		t.Fatal(err)
	}
	profileAvatar := candidateAvatarPublicPrefix + strings.Repeat("a", 64) + ".png"
	snapshotAvatar := candidateAvatarPublicPrefix + strings.Repeat("b", 64) + ".png"
	candidateA, err := candidateStore.SaveCandidateProfile(CandidateProfileInput{UserEmail: emailA, PlatformID: "liepin", PlatformCandidateID: "delete-a-" + suffix, CandidateName: "待删除候选人", Phone: "17607080935", AvatarURL: profileAvatar})
	if err != nil {
		t.Fatal(err)
	}
	candidateB, err := candidateStore.SaveCandidateProfile(CandidateProfileInput{UserEmail: emailB, PlatformID: "liepin", PlatformCandidateID: "delete-b-" + suffix, CandidateName: "其他团队候选人", Phone: "17607080936"})
	if err != nil {
		t.Fatal(err)
	}
	engagement, err := candidateStore.UpsertCandidateEngagement(CandidateEngagement{CandidateID: candidateA.ID, UserEmail: emailA, PositionID: position.ID, PlatformID: "liepin"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = candidateStore.SaveCandidateEvent(CandidateEvent{CandidateID: candidateA.ID, EngagementID: engagement.ID, PositionID: position.ID, PlatformID: "liepin", EventType: "greet_analysis"}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO candidate_phone_identities (tenant_id, normalized_phone, candidate_id) VALUES ($1,$2,$3)`, tenantA.ID, candidateA.NormalizedPhone, candidateA.ID); err != nil {
		t.Fatal(err)
	}
	identity, err := autoReplyStore.UpsertCandidatePlatformIdentity(ctx, CandidatePlatformIdentity{TenantID: tenantA.ID, CandidateID: candidateA.ID, PlatformID: "liepin", PlatformCandidateID: "delete-identity-" + suffix, CandidateName: candidateA.CandidateName, NormalizedPhone: candidateA.NormalizedPhone})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := autoReplyStore.UpsertAutoReplyConversation(ctx, AutoReplyConversation{TenantID: tenantA.ID, CandidateID: candidateA.ID, PlatformIdentityID: identity.ID, EngagementID: engagement.ID, PositionID: position.ID, PlatformID: "liepin", PlatformThreadID: "delete-thread-" + suffix, CandidateName: candidateA.CandidateName})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = autoReplyStore.SyncAutoReplyMessages(ctx, tenantA.ID, conversation.ID, true, []AutoReplyMessage{{PlatformMessageID: "delete-message-" + suffix, Fingerprint: "delete-fingerprint-" + suffix, Direction: "candidate", MessageType: "text", TextContent: "测试消息"}}); err != nil {
		t.Fatal(err)
	}
	messages, err := autoReplyStore.ListAutoReplyMessages(ctx, tenantA.ID, conversation.ID, 10)
	if err != nil || len(messages) != 1 {
		t.Fatalf("保存删除测试消息失败：messages=%+v err=%v", messages, err)
	}
	attachmentPath := "resumes/" + tenantA.ID + "/delete-test.pdf"
	if _, err = autoReplyStore.SaveResumeAttachment(ctx, StoredResumeAttachment{TenantID: tenantA.ID, CandidateID: candidateA.ID, ConversationID: conversation.ID, SourceMessageID: messages[0].ID, PlatformID: "liepin", OriginalName: "删除测试简历.pdf", StoragePath: attachmentPath, SHA256: strings.Repeat("c", 64), MIMEType: "application/pdf", SizeBytes: 128}); err != nil {
		t.Fatal(err)
	}
	confirmation, err := autoReplyStore.UpsertConfirmationItem(ctx, CandidateConfirmationItem{TenantID: tenantA.ID, ConversationID: conversation.ID, CandidateID: candidateA.ID, PositionID: position.ID, ItemType: "required", Content: "必须本科", Status: "matched", StatusReason: "简历显示本科", SourceType: "resume", SourceRef: "education", EvidenceText: "本科", CreatedByKind: "ai"})
	if err != nil {
		t.Fatal(err)
	}
	aiRun, err := autoReplyStore.StartAutoReplyAIRun(ctx, AutoReplyAIRun{TenantID: tenantA.ID, ConversationID: conversation.ID, CandidateID: candidateA.ID, PositionID: position.ID, TraceID: "delete-ai-" + suffix, Model: "test", BasedOnMessageKey: "id:delete-message-" + suffix, InputMessages: json.RawMessage(`[{"role":"user","content":"测试"}]`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = autoReplyStore.SaveAutoReplyToolCall(ctx, AutoReplyToolCall{TenantID: tenantA.ID, AIRunID: aiRun.ID, ToolCallID: "delete-tool-" + suffix, SequenceNo: 1, ToolName: "send_message", ArgumentsJSON: json.RawMessage(`{"message":"测试"}`), ResultJSON: json.RawMessage(`{"ok":true}`), Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO auto_reply_config_suggestions (tenant_id,conversation_id,position_id,suggestion_type,operation,reason) VALUES ($1,$2,$3,'position','update','删除测试')`, tenantA.ID, conversation.ID, position.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO auto_reply_notifications (tenant_id,conversation_id,position_id,based_on_message_key,reason_key,candidate_name,platform_id,reason,recipient_email) VALUES ($1,$2,$3,$4,$5,$6,'liepin','删除测试',$7)`, tenantA.ID, conversation.ID, position.ID, "id:delete-message-"+suffix, "delete-reason-"+suffix, candidateA.CandidateName, emailA); err != nil {
		t.Fatal(err)
	}
	reportData, err := json.Marshal(map[string]any{"candidate": map[string]any{"avatar_url": snapshotAvatar}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO candidate_recommendations (public_id,tenant_id,review_id,candidate_id,position_id,version,input_hash,report_data) VALUES ($1,$2,$3,$4,$5,1,$6,$7::jsonb)`, "delete-public-"+suffix, tenantA.ID, confirmation.ReviewID, candidateA.ID, position.ID, "delete-hash-"+suffix, string(reportData)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO candidate_recommendation_jobs (tenant_id,review_id,candidate_id,position_id,input_hash) VALUES ($1,$2,$3,$4,$5)`, tenantA.ID, confirmation.ReviewID, candidateA.ID, position.ID, "delete-job-"+suffix); err != nil {
		t.Fatal(err)
	}
	if _, err = candidateStore.DeleteCandidate(tenantB.ID, candidateA.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("其他团队删除应返回未找到：%v", err)
	}
	for tenantID, candidateID := range map[string]string{tenantA.ID: candidateA.ID, tenantB.ID: candidateB.ID} {
		var count int
		if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM candidate_profiles WHERE tenant_id=$1 AND id=$2`, tenantID, candidateID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("错误团队删除影响了候选人：tenant=%s candidate=%s count=%d err=%v", tenantID, candidateID, count, err)
		}
	}
	deleted, err := candidateStore.DeleteCandidate(tenantA.ID, candidateA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Deleted != 1 || len(deleted.AttachmentPaths) != 1 || deleted.AttachmentPaths[0] != attachmentPath {
		t.Fatalf("删除返回的附件信息不正确：%+v", deleted)
	}
	avatarURLs := map[string]bool{}
	for _, avatarURL := range deleted.AvatarURLs {
		avatarURLs[avatarURL] = true
	}
	if !avatarURLs[profileAvatar] || !avatarURLs[snapshotAvatar] {
		t.Fatalf("删除返回的头像不完整：%v", deleted.AvatarURLs)
	}
	for _, table := range []string{"candidate_profiles", "candidate_phone_identities", "candidate_platform_identities", "candidate_engagements", "candidate_events", "candidate_conversations", "candidate_messages", "candidate_resume_attachments", "candidate_confirmation_items", "candidate_confirmation_events", "auto_reply_ai_runs", "auto_reply_tool_calls", "auto_reply_config_suggestions", "auto_reply_notifications", "candidate_position_reviews", "candidate_recommendations", "candidate_recommendation_jobs"} {
		var count int
		if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE tenant_id=$1`, tenantA.ID).Scan(&count); err != nil {
			t.Fatalf("检查表 %s 失败：%v", table, err)
		}
		if count != 0 {
			t.Fatalf("删除后表 %s 仍有 %d 条团队候选人关联数据", table, count)
		}
	}
	var tenantBCandidateCount int
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM candidate_profiles WHERE tenant_id=$1 AND id=$2`, tenantB.ID, candidateB.ID).Scan(&tenantBCandidateCount); err != nil || tenantBCandidateCount != 1 {
		t.Fatalf("删除误伤其他团队候选人：count=%d err=%v", tenantBCandidateCount, err)
	}
}
