// Package api 文件作用：通过真实HTTP路由和临时SQLite验证摘要配对、完整会话、原文隔离及登录失效，不调用付费AI。
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	summaryflow "goodhr5/local-agent-go-new/internal/flow/resume_summary"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

type summaryRouteAI struct {
	calls   atomic.Int32
	entered chan cloud.LibraryCandidate
	release chan struct{}
}

// SummarizeResume 使用测试替身返回职业摘要，取消时停止等待。
func (a *summaryRouteAI) SummarizeResume(ctx context.Context, _ cloud.AIConfig, candidate cloud.LibraryCandidate) (string, error) {
	a.calls.Add(1)
	if a.entered != nil {
		a.entered <- candidate
	}
	if a.release != nil {
		select {
		case <-a.release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return "合成职业摘要", nil
}

// summaryRouteRequest 同时覆盖网页Bearer和插件POST鉴权，全部请求仅发往内存服务器。
func summaryRouteRequest(s *Server, method, path, token string, body any) *httptest.ResponseRecorder {
	encoded, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, strings.NewReader(string(encoded)))
	r.Header.Set("Origin", "https://goodhr.58it.cn")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.http.Handler.ServeHTTP(w, r)
	return w
}

// summaryRouteTask 读取公开摘要草稿合同。
func summaryRouteTask(t *testing.T, w *httptest.ResponseRecorder) summaryflow.Task {
	t.Helper()
	var result struct {
		Data struct {
			Task summaryflow.Task `json:"task"`
		} `json:"data"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatalf("HTTP%d %s", w.Code, w.Body.String())
	}
	return result.Data.Task
}

// bindSummaryInstallation 在隔离数据库保存配对摘要，与正式流程使用同一存储键。
func bindSummaryInstallation(t *testing.T, s *Server, installation, secret, user string) {
	t.Helper()
	value, _ := json.Marshal(extensionPairIdentity{UserID: user, SecretHash: extensionHash(secret)})
	if err := s.store.SetSetting(context.Background(), extensionIdentityKey(installation), string(value)); err != nil {
		t.Fatal(err)
	}
}

// newSummaryRouteServer 组装真实云端认证替身、SQLite和摘要假AI。
func newSummaryRouteServer(t *testing.T, ai *summaryRouteAI, expired *atomic.Bool) *Server {
	t.Helper()
	s := newExtensionPairTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/me":
			if expired.Load() {
				w.WriteHeader(401)
				return
			}
			kind, user := "web", "account-a"
			if r.Header.Get("Authorization") == "Bearer plugin-only" {
				kind = "browser_extension"
			}
			if r.Header.Get("Authorization") == "Bearer web-b" {
				user = "account-b"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"user": map[string]string{"id": user, "email": "test@example.invalid"}, "session": map[string]string{"kind": kind}})
		case "/api/config/effective-ai":
			_ = json.NewEncoder(w).Encode(map[string]any{"config": cloud.AIConfig{BaseURL: "https://example.invalid", APIKey: "provider-secret", Model: "fake-model"}})
		default:
			t.Errorf("摘要不应请求其它云端写入：%s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	s.resumeSummaries.Close()
	manager, err := summaryflow.New(s.store, s.cloud, ai)
	if err != nil {
		t.Fatal(err)
	}
	s.resumeSummaries = manager
	t.Cleanup(manager.Close)
	s.extensionMu.Lock()
	s.bindExtensionSessionLocked("web-private", "account-a")
	s.extensionMu.Unlock()
	return s
}

// TestSummaryRoutesPairingIsolationAndSessionKind 验证安装隔离、有效配对、受限Token拒绝和账号失效错误码。
func TestSummaryRoutesPairingIsolationAndSessionKind(t *testing.T) {
	var expired atomic.Bool
	ai := &summaryRouteAI{entered: make(chan cloud.LibraryCandidate, 2)}
	s := newSummaryRouteServer(t, ai, &expired)
	installation, secret := strings.Repeat("i", 32), strings.Repeat("s", 64)
	bindSummaryInstallation(t, s, installation, secret, "account-a")
	body := map[string]any{"installation_id": installation, "pairing_token": secret, "request_key": "plugin-summary-1", "candidate": map[string]any{"candidate_name": "合成人员", "phone": "13800138000", "age": "", "avatar_data_url": "data:image/png;base64,private-image", "ai_summary": "旧AI猜测", "raw_text": "不向轮询返回的职业正文", "work_experiences": []map[string]string{{"company_name": "合成公司", "content": "产品设计"}}}}
	task := summaryRouteTask(t, summaryRouteRequest(s, "POST", "/api/v1/extensions/resume-summary", "", body))
	input := <-ai.entered
	if input.CandidateName != "" || input.Phone != "" || input.AISummary != "" {
		t.Fatal("非职业资料进入AI")
	}
	deadline := time.Now().Add(time.Second)
	var result *httptest.ResponseRecorder
	for time.Now().Before(deadline) {
		result = summaryRouteRequest(s, "POST", "/api/v1/extensions/resume-summary/"+task.ID, "", body)
		if summaryRouteTask(t, result).Status == "completed" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	for _, private := range []string{"不向轮询返回的职业正文", "private-image", "web-private", "provider-secret", secret} {
		if strings.Contains(result.Body.String(), private) {
			t.Fatal("轮询泄露私有资料")
		}
	}
	if summaryRouteRequest(s, "POST", "/api/v1/extensions/resume-summary", "", body).Code != 200 || ai.calls.Load() != 1 {
		t.Fatal("重复插件POST重复计费")
	}
	other, otherSecret := strings.Repeat("o", 32), strings.Repeat("t", 64)
	bindSummaryInstallation(t, s, other, otherSecret, "account-a")
	foreign := map[string]any{"installation_id": other, "pairing_token": otherSecret}
	if summaryRouteRequest(s, "POST", "/api/v1/extensions/resume-summary/"+task.ID, "", foreign).Code != 404 {
		t.Fatal("其它安装可读取摘要")
	}
	bad := map[string]any{"installation_id": installation, "pairing_token": strings.Repeat("x", 64)}
	denied := summaryRouteRequest(s, "POST", "/api/v1/extensions/resume-summary/"+task.ID, "", bad)
	if denied.Code != 401 || !strings.Contains(denied.Body.String(), "PAIR_REQUIRED") {
		t.Fatal("错误配对没有稳定错误码")
	}
	denied = summaryRouteRequest(s, "POST", "/api/v1/resume-summary/tasks", "plugin-only", body)
	if denied.Code != 403 || !strings.Contains(denied.Body.String(), "WEB_SESSION_REQUIRED") {
		t.Fatal("受限插件会话可调用网页AI")
	}
	expired.Store(true)
	denied = summaryRouteRequest(s, "POST", "/api/v1/extensions/resume-summary/"+task.ID, "", body)
	if denied.Code != 401 || !strings.Contains(denied.Body.String(), "SESSION_INVALID") {
		t.Fatal("过期会话没有稳定错误码")
	}
}

// TestSummaryRouteChangedSessionCancelsProvider 验证切号会取消正在收费的调用，旧配对不能交付结果。
func TestSummaryRouteChangedSessionCancelsProvider(t *testing.T) {
	var expired atomic.Bool
	ai := &summaryRouteAI{entered: make(chan cloud.LibraryCandidate, 1), release: make(chan struct{})}
	s := newSummaryRouteServer(t, ai, &expired)
	installation, secret := strings.Repeat("i", 32), strings.Repeat("s", 64)
	bindSummaryInstallation(t, s, installation, secret, "account-a")
	body := map[string]any{"installation_id": installation, "pairing_token": secret, "request_key": "changed-session", "candidate": map[string]string{"raw_text": "合成职业正文"}}
	task := summaryRouteTask(t, summaryRouteRequest(s, "POST", "/api/v1/extensions/resume-summary", "", body))
	<-ai.entered
	s.extensionMu.Lock()
	s.bindExtensionSessionLocked("web-b", "account-b")
	s.extensionMu.Unlock()
	s.resumeSummaries.Close()
	stored, err := s.resumeSummaries.Get("account-a", "extension:"+extensionHash(installation), task.ID)
	if err != nil || stored.Status != "failed" || stored.Summary != "" || !stored.PossiblyCharged {
		t.Fatal("切号后接受结果或丢失可能计费状态")
	}
	if summaryRouteRequest(s, "POST", "/api/v1/extensions/resume-summary/"+task.ID, "", body).Code != 401 {
		t.Fatal("旧账号配对可读取摘要")
	}
}
