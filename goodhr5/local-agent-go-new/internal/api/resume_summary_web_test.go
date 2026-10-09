// Package api 文件作用：验证主库完整会话的摘要轮询及插件主动撤销对后台收费请求的取消效果。
package api

import (
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestWebSummaryRouteAndPairRevoke 验证网页完整Bearer可用、轮询只返回摘要，以及撤销一个安装会停止该安装请求。
func TestWebSummaryRouteAndPairRevoke(t *testing.T) {
	var expired atomic.Bool
	ai := &summaryRouteAI{}
	s := newSummaryRouteServer(t, ai, &expired)
	body := map[string]any{"request_key": "web-summary-test", "candidate": map[string]string{"raw_text": "职业正文仅留本地"}}
	task := summaryRouteTask(t, summaryRouteRequest(s, "POST", "/api/v1/resume-summary/tasks", "web-private", body))
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		response := summaryRouteRequest(s, "GET", "/api/v1/resume-summary/tasks/"+task.ID, "web-private", nil)
		if strings.Contains(response.Body.String(), "职业正文仅留本地") {
			t.Fatal("网页轮询返回了原文")
		}
		if summaryRouteTask(t, response).Status == "completed" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if ai.calls.Load() != 1 {
		t.Fatal("网页摘要没有单次调用")
	}
	s.resumeSummaries.Close()
	blocked := &summaryRouteAI{entered: make(chan cloud.LibraryCandidate, 1), release: make(chan struct{})}
	s = newSummaryRouteServer(t, blocked, &expired)
	installation, secret := strings.Repeat("i", 32), strings.Repeat("s", 64)
	bindSummaryInstallation(t, s, installation, secret, "account-a")
	paired := map[string]any{"installation_id": installation, "pairing_token": secret, "request_key": "revoke-summary-test", "candidate": map[string]string{"raw_text": "合成职业内容"}}
	task = summaryRouteTask(t, summaryRouteRequest(s, "POST", "/api/v1/extensions/resume-summary", "", paired))
	<-blocked.entered
	response := summaryRouteRequest(s, "POST", "/api/v1/extensions/pair/revoke", "", extensionPairRequest{InstallationID: installation, PairingToken: secret})
	if response.Code != 200 {
		t.Fatalf("撤销失败：%s", response.Body.String())
	}
	s.resumeSummaries.Close()
	stored, err := s.resumeSummaries.Get("account-a", "extension:"+extensionHash(installation), task.ID)
	if err != nil || stored.Status != "failed" || stored.Summary != "" || !stored.PossiblyCharged {
		t.Fatal("撤销后没有停止后台摘要")
	}
	if summaryRouteRequest(s, "POST", "/api/v1/extensions/resume-summary/"+task.ID, "", paired).Code != 401 {
		t.Fatal("已撤销授权可轮询摘要")
	}
}
