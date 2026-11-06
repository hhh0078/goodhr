// Package api 本文件验证失败报告的人工重试仅作用当前登录账号，不恢复聊天发送且切号时拒绝旧请求。
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/lifecycle"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/storage"
)

// TestAutoReplyStatusPreservesFailureReason 验证任务收尾步骤为 finished 时仍向前端返回真实错误。
func TestAutoReplyStatusPreservesFailureReason(t *testing.T) {
	s := newAutoReplyRetryServer(t, nil)
	reason := "当前平台还不能核对单会话稳定身份，本次限制任务没有启动"
	if err := s.store.SaveTask(context.Background(), storage.TaskRun{
		TaskID: "failed-reply", PositionID: "position", PlatformID: "liepin", TaskType: "auto_reply",
		Status: "failed", CurrentStep: "finished", ErrorCode: "TASK_FLOW_FAILED", ErrorMessage: reason,
		StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.handleLocalAutoReplyStatus(w, httptest.NewRequest(http.MethodGet, "/api/v1/local/auto-reply/status", nil))
	var response struct {
		Data storage.TaskRun `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || w.Code != http.StatusOK {
		t.Fatalf("读取状态失败：status=%d err=%v", w.Code, err)
	}
	if response.Data.Status != "failed" || response.Data.CurrentStep != "finished" || response.Data.ErrorCode != "TASK_FLOW_FAILED" || response.Data.ErrorMessage != reason {
		t.Fatalf("状态没有保留真实失败原因：%+v", response.Data)
	}
}

// newAutoReplyRetryServer 创建仅有本机SQLite与合成登录态的报告重试接口。
func newAutoReplyRetryServer(t *testing.T, remote http.Handler) *Server {
	t.Helper()
	if remote == nil {
		remote = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			kind := "web"
			if token == "extension" {
				kind = "browser_extension"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"user": map[string]string{"id": token}, "session": map[string]string{"kind": kind}})
		})
	}
	s := newExtensionPairTestServer(t, remote)
	s.cloud.EnableLocalAutoReply(s.store, t.TempDir())
	s.deviceID = func() (string, error) { return "synthetic-machine", nil }
	s.runner = lifecycle.New(nil, nil, nil, s.store, nil, nil, s.cloud, nil, nil)
	return s
}

// seedBlockedAutoReplyReport 在已验证的合成账号下登记一份需要人工重试的报告。
func seedBlockedAutoReplyReport(t *testing.T, s *Server, token string) cloud.AgentCredentials {
	t.Helper()
	ctx := context.Background()
	if _, err := s.cloud.ValidateSession(ctx, token); err != nil {
		t.Fatal(err)
	}
	cred := cloud.AgentCredentials{Token: token, MachineID: "synthetic-machine"}
	if err := s.cloud.EnqueueAutoReplyRecommendation(ctx, cred, cloud.AutoReplyRecommendationJob{ID: "report", InputHash: "source", ConversationID: "conversation", CandidateID: "candidate"}); err != nil {
		t.Fatal(err)
	}
	if err := s.cloud.BlockAutoReplyRecommendation(ctx, cred, "report", "三次模型失败"); err != nil {
		t.Fatal(err)
	}
	return cred
}

// TestAutoReplyRetryReportsIsAccountScoped 验证状态数量、当前账号人工重试，以及已确认发送记录均保持隔离。
func TestAutoReplyRetryReportsIsAccountScoped(t *testing.T) {
	s := newAutoReplyRetryServer(t, nil)
	ctx := context.Background()
	first := seedBlockedAutoReplyReport(t, s, "account-a")
	second := seedBlockedAutoReplyReport(t, s, "account-b")
	conversation, err := s.cloud.SaveAutoReplyConversation(ctx, first, cloud.AutoReplyConversation{PlatformID: "boss", PlatformAccountID: "bound-account", PlatformThreadID: "thread", PositionID: "position"})
	if err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := s.cloud.ClaimAutoReplySend(ctx, first, conversation.ID, "message", "已经发送", 0); err != nil || !claimed {
		t.Fatal("无法建立发送记录", claimed, err)
	}
	if err = s.cloud.FinishAutoReplySend(ctx, first, conversation.ID, "message", 0, "sent"); err != nil {
		t.Fatal(err)
	}
	s.extensionToken, s.extensionUserID = "account-a", "account-a"
	w := httptest.NewRecorder()
	s.handleLocalAutoReplyStatus(w, httptest.NewRequest(http.MethodGet, "/api/v1/local/auto-reply/status", nil))
	var status struct {
		Data struct {
			Status  string `json:"status"`
			Blocked int    `json:"blocked_report_count"`
		} `json:"data"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &status); err != nil || w.Code != http.StatusOK || status.Data.Status != "stopped" || status.Data.Blocked != 1 {
		t.Fatalf("状态未返回当前账号待处理数量：%d %s %v", w.Code, w.Body.String(), err)
	}
	w = extensionPairTestRequest(s, "/api/v1/local/auto-reply/retry-reports", agentBindRequest{Token: "account-a"})
	var result struct {
		Data struct {
			Count int `json:"retried_count"`
		} `json:"data"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != http.StatusOK || result.Data.Count != 1 {
		t.Fatalf("重试结果不正确：%d %s %v", w.Code, w.Body.String(), err)
	}
	firstCount, err := s.cloud.CountBlockedAutoReplyRecommendations(ctx, first)
	if err != nil || firstCount != 0 {
		t.Fatal("当前账号未恢复", firstCount, err)
	}
	secondCount, err := s.cloud.CountBlockedAutoReplyRecommendations(ctx, second)
	if err != nil || secondCount != 1 {
		t.Fatal("重试改动了其他账号", secondCount, err)
	}
	if status, claimed, err := s.cloud.ClaimAutoReplySend(ctx, first, conversation.ID, "message", "已经发送", 0); err != nil || claimed || status != "sent" {
		t.Fatal("报告重试重置了聊天发送记录", status, claimed, err)
	}
	s.extensionToken, s.extensionUserID = "", ""
	if count, resumes, err := s.currentBlockedAutoReplyReports(ctx); err != nil || count != 0 || resumes != 0 {
		t.Fatal("未绑定账号仍显示待办数量", count, resumes, err)
	}
}

// TestAutoReplyRetryReportsRejectsOtherSessions 验证旧页面和插件会话不能触发付费报告重试。
func TestAutoReplyRetryReportsRejectsOtherSessions(t *testing.T) {
	s := newAutoReplyRetryServer(t, nil)
	for _, item := range []struct {
		bound, request string
		want           int
	}{
		{"account-a", "account-b", http.StatusConflict},
		{"extension", "extension", http.StatusForbidden},
		{"account-a", "", http.StatusBadRequest},
	} {
		s.extensionToken, s.extensionUserID = item.bound, item.bound
		w := extensionPairTestRequest(s, "/api/v1/local/auto-reply/retry-reports", agentBindRequest{Token: item.request})
		if w.Code != item.want {
			t.Fatalf("绑定 %s 的请求返回 %d，预期 %d：%s", item.bound, w.Code, item.want, w.Body.String())
		}
	}
}

// TestAutoReplyRetryReportsRejectsSessionChange 验证云端校验等待期间切号后，旧请求不会恢复任何报告。
func TestAutoReplyRetryReportsRejectsSessionChange(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	s := newAutoReplyRetryServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		_, _ = w.Write([]byte(`{"user":{"id":"account-a"}}`))
	}))
	s.extensionToken, s.extensionUserID = "account-a", "account-a"
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- extensionPairTestRequest(s, "/api/v1/local/auto-reply/retry-reports", agentBindRequest{Token: "account-a"})
	}()
	<-entered
	s.extensionMu.Lock()
	s.extensionGeneration++
	s.extensionToken, s.extensionUserID = "account-b", "account-b"
	s.extensionMu.Unlock()
	close(release)
	if w := <-done; w.Code != http.StatusConflict {
		t.Fatalf("切号后仍接受旧请求：%d %s", w.Code, w.Body.String())
	}
}
