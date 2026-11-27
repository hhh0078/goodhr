// Package httpapi 本文件验证启动余额边界，以及绕过前端请求自动回复时仍被云端阻止，停止操作不受余额限制。
package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestStartBalanceThreshold 验证0.1元精确单位边界和未读到钱包时不放行。
func TestStartBalanceThreshold(t *testing.T) {
	wallet := NewMemoryAIWalletStore()
	for _, units := range []int64{0, 522, 999, 1000, 1001} {
		wallet.balances["synthetic@example.test"] = units
		failure := validateStartAIBalance(wallet, "synthetic@example.test")
		if (units < 1000) != (failure != nil) {
			t.Fatalf("门槛不符：%d %+v", units, failure)
		}
		if failure != nil && (failure.code != "AI_BALANCE_INSUFFICIENT" || failure.message != "余额不足0.1元，请充值后再试。") {
			t.Fatal("余额不足提示未统一")
		}
	}
	if failure := validateStartAIBalance(nil, "synthetic@example.test"); failure == nil || failure.code != "AI_BALANCE_UNAVAILABLE" {
		t.Fatal("没有钱包却允许启动")
	}
}

// TestAutoReplyStartBalanceCannotBypassFrontend 经真实账号和设备认证后，低余额请求不能进入执行器；0.1元允许，停止不拦截。
func TestAutoReplyStartBalanceCannotBypassFrontend(t *testing.T) {
	f, s, machine := newRolloutHTTPFixture(t)
	wallet := NewMemoryAIWalletStore()
	s.aiWallet = wallet
	forwarded := 0
	s.runtime = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded++; w.WriteHeader(http.StatusAccepted) })
	t.Setenv("GOODHR_AUTO_REPLY_SERVICE_TOKEN", "synthetic-runtime-service")
	t.Setenv("GOODHR_AUTO_REPLY_RUNTIME_URL", "http://127.0.0.1:1")
	for _, units := range []int64{522, 999, 1000} {
		wallet.balances["fact-hr@example.test"] = units
		req := httptest.NewRequest(http.MethodPost, "/api/auto-reply/runtime/start", strings.NewReader(`{"position_id":"`+f.pos+`","platform_id":"liepin"}`))
		req.Header.Set("Authorization", "Bearer rollout-web-token")
		req.Header.Set("X-GoodHR-Machine-ID", machine)
		w := httptest.NewRecorder()
		s.runtimeProxy(w, req)
		if units < 1000 {
			if w.Code != http.StatusPaymentRequired || forwarded != 0 || !strings.Contains(w.Body.String(), "AI_BALANCE_INSUFFICIENT") {
				t.Fatalf("低余额触发执行：%d %d", w.Code, forwarded)
			}
		} else if w.Code != http.StatusAccepted || forwarded != 1 {
			t.Fatalf("0.1元未放行：%d %s", w.Code, w.Body.String())
		}
	}
	wallet.balances["fact-hr@example.test"] = 0
	req := httptest.NewRequest(http.MethodPost, "/api/auto-reply/runtime/stop", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer rollout-web-token")
	req.Header.Set("X-GoodHR-Machine-ID", machine)
	w := httptest.NewRecorder()
	s.runtimeProxy(w, req)
	if w.Code != http.StatusAccepted || forwarded != 2 {
		t.Fatal("低余额挡住了安全停止", w.Code, forwarded)
	}
}
