// 本文件验证自动回复云端的数据边界、人工字段保护及独立报告迁移。
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAutoReplyStructuredAuthorization 验证只有真实布尔开关开启时才允许同步完整结构化结果。
func TestAutoReplyStructuredAuthorization(t *testing.T) {
	for _, value := range []any{nil, false, "true", true} {
		w := httptest.NewRecorder()
		allowed := allowAutoReplyStructuredResult(w, Position{CommonConfig: map[string]any{"output_structured_resume": value}})
		if allowed != (value == true) {
			t.Fatalf("value=%v allowed=%v", value, allowed)
		}
	}
}

// TestAutoReplySensitiveRoutesRemainLocal 验证旧聊天、附件和云端AI入口已全部关闭。
func TestAutoReplySensitiveRoutesRemainLocal(t *testing.T) {
	service := &AutoReplyService{}
	for _, path := range []string{"identities", "candidate-state", "conversations", "messages/sync", "messages", "confirmations", "attachments", "avatars", "resume-structure", "ai-runs/start", "ai-runs/finish", "tool-calls", "suggestions"} {
		w := httptest.NewRecorder()
		service.Agent(w, httptest.NewRequest(http.MethodPost, "/api/auto-reply/agent/"+path, strings.NewReader(`{}`)))
		if w.Code != http.StatusNotFound {
			t.Fatalf("sensitive route %s status=%d", path, w.Code)
		}
	}
	if err := validateAutoReplyNotice(autoReplyNoticeRequest{LatestMessage: "候选人聊天全文"}); err == nil {
		t.Fatal("notification accepted candidate message text")
	}
}

// TestAutoReplyReportRejectsChatAndInvalidScore 验证报告合同拒绝聊天字段及越界结果。
func TestAutoReplyReportRejectsChatAndInvalidScore(t *testing.T) {
	request := autoReplyCompletedReportRequest{CandidateID: "candidate", InputHash: strings.Repeat("a", 64), Report: CandidateRecommendationReport{MatchScore: 88, ExecutiveSummary: "符合岗位关键要求"}}
	if err := validateAutoReplyCompletedReport(request); err != nil {
		t.Fatal(err)
	}
	request.Report.MatchScore = 101
	if err := validateAutoReplyCompletedReport(request); err == nil {
		t.Fatal("accepted score above 100")
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"report_data":{"messages":[{"text_content":"聊天全文"}]}}`))
	if err := decodeAutoReplyJSON(w, r, &autoReplyCompletedReportRequest{}); err == nil {
		t.Fatal("report accepted unknown chat field")
	}
}

// TestAutoReplyKeepsManualFieldsAndDecimalSalary 验证自动回复结果更新继续保护人工字段、版本和小数薪资。
func TestAutoReplyKeepsManualFieldsAndDecimalSalary(t *testing.T) {
	store := NewMemoryCandidateStore()
	scope := CandidateLibraryScope{TenantID: "team", UserEmail: "owner@example.com", IsAdmin: true}
	profile, err := store.SaveCandidateProfile(CandidateProfileInput{TenantID: scope.TenantID, UserEmail: scope.UserEmail, PlatformID: "boss", PlatformCandidateID: "auto-person", CandidateName: "初次姓名"})
	if err != nil {
		t.Fatal(err)
	}
	profile, err = store.SaveLibraryCandidate(scope, profile.ID, map[string]any{"candidate_name": "人工姓名", "wechat": "人工微信", "expected_salary_min": 16.5}, profile.RowVersion, "manual-update")
	if err != nil {
		t.Fatal(err)
	}
	version := profile.RowVersion
	profile, err = store.SaveCandidateProfile(CandidateProfileInput{TenantID: scope.TenantID, UserEmail: scope.UserEmail, PlatformID: "boss", PlatformCandidateID: "auto-person", CandidateName: "自动姓名", Ext: map[string]any{"structured_fields": map[string]any{"wechat": "自动微信", "gender": "女"}}})
	if err != nil || profile.CandidateName != "人工姓名" || candidateString(profile.LibraryFields, "wechat") != "人工微信" || candidateString(profile.LibraryFields, "gender") != "女" || profile.RowVersion != version+1 || profile.ExpectedSalaryMin == nil || *profile.ExpectedSalaryMin != 16.5 {
		t.Fatalf("manual protection lost: profile=%+v err=%v", profile, err)
	}
}

// TestAutoReplyPostgresConfigurationAndReports 验证旧简历表升级、新报告幂等及分享撤销，保持人工推荐表独立。
func TestAutoReplyPostgresConfigurationAndReports(t *testing.T) {
	db := candidatePostgresTestDB(t)
	if _, err := db.Exec(`ALTER TABLE users ADD COLUMN status TEXT NOT NULL DEFAULT 'active'; ALTER TABLE positions ADD COLUMN user_id UUID REFERENCES users(id);`); err != nil {
		t.Fatal(err)
	}
	for repeat := 0; repeat < 2; repeat++ {
		for _, name := range []string{"0104_auto_reply_configuration.sql", "0105_auto_reply_result_summaries.sql"} {
			raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", name))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(string(raw)); err != nil {
				t.Fatalf("migration %s: %v", name, err)
			}
		}
	}
	var tenantID, userID, positionID string
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users(email,tenant_id) VALUES('auto-owner@example.com',$1) RETURNING id::text`, tenantID).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO positions(name,user_id) VALUES('报告岗位',$1) RETURNING id::text`, userID).Scan(&positionID); err != nil {
		t.Fatal(err)
	}
	candidates := NewPostgresCandidateStore(db)
	candidate, err := candidates.SaveCandidateProfile(CandidateProfileInput{UserEmail: "auto-owner@example.com", PlatformID: "boss", PlatformCandidateID: "auto-person", CandidateName: "结构化姓名", Phone: "13800138000"})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err = candidates.SaveLibraryCandidate(CandidateLibraryScope{TenantID: tenantID, UserEmail: "auto-owner@example.com", IsAdmin: true}, candidate.ID, map[string]any{"candidate_name": "人工保护姓名", "expected_salary_min": 16.5}, candidate.RowVersion, "manual-before-auto")
	if err != nil {
		t.Fatal(err)
	}
	originalID, originalVersion := candidate.ID, candidate.RowVersion
	candidate, err = candidates.SaveCandidateProfile(CandidateProfileInput{UserEmail: "auto-owner@example.com", PlatformID: "liepin", PlatformCandidateID: "second-platform-person", CandidateName: "自动姓名", Phone: "13800138000", Ext: map[string]any{"structured_fields": map[string]any{"wechat": "本地结构化微信"}}})
	if err != nil || candidate.ID != originalID || candidate.RowVersion != originalVersion+1 || candidate.CandidateName != "人工保护姓名" || candidate.ExpectedSalaryMin == nil || *candidate.ExpectedSalaryMin != 16.5 || len(candidate.Sources) != 2 {
		t.Fatalf("cross-platform automatic update lost existing protections: candidate=%+v err=%v", candidate, err)
	}
	store := NewPostgresAutoReplyStore(db)
	company, err := store.SaveCompanyProfile(context.Background(), tenantID, "auto-owner@example.com", CompanyProfile{Name: "测试公司"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SavePositionAutoReplyConfig(context.Background(), "auto-owner@example.com", PositionAutoReplyConfig{TenantID: tenantID, PositionID: positionID, CompanyProfileID: company.ID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	report := CandidateAutoReplyReport{TenantID: tenantID, CandidateID: candidate.ID, PositionID: positionID, InputHash: strings.Repeat("a", 64), Report: CandidateRecommendationReport{MatchScore: 88.5, ExecutiveSummary: "本地生成的结构化报告", GeneratedAt: time.Now().UTC()}}
	saved, err := store.SaveAutoReplyReport(context.Background(), report)
	if err != nil || saved.ShareEnabled {
		t.Fatalf("save report=%+v err=%v", saved, err)
	}
	retried, err := store.SaveAutoReplyReport(context.Background(), report)
	if err != nil || retried.ID != saved.ID || retried.Version != 1 {
		t.Fatalf("report retry was not idempotent: %+v %v", retried, err)
	}
	if _, err = store.GetPublicAutoReplyReport(context.Background(), saved.PublicID); err != ErrNotFound {
		t.Fatalf("new report unexpectedly public: %v", err)
	}
	if err = store.SetAutoReplyReportShare(context.Background(), tenantID, "other@example.com", saved.PublicID, true); err != ErrNotFound {
		t.Fatalf("other member enabled report share: %v", err)
	}
	if err = store.SetAutoReplyReportShare(context.Background(), tenantID, "auto-owner@example.com", saved.PublicID, true); err != nil {
		t.Fatal(err)
	}
	if _, err = store.GetPublicAutoReplyReport(context.Background(), saved.PublicID); err != nil {
		t.Fatal(err)
	}
	if err = store.SetAutoReplyReportShare(context.Background(), tenantID, "auto-owner@example.com", saved.PublicID, false); err != nil {
		t.Fatal(err)
	}
	if _, err = store.GetPublicAutoReplyReport(context.Background(), saved.PublicID); err != ErrNotFound {
		t.Fatalf("revoked report remains public: %v", err)
	}
	var columns []byte
	if err := db.QueryRow(`SELECT json_agg(column_name) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='candidate_recommendations'`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	var names []string
	if err := json.Unmarshal(columns, &names); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(columns), "stage") || strings.Contains(string(columns), "report_data") {
		t.Fatalf("manual recommendation table was overwritten: %v", names)
	}
}
