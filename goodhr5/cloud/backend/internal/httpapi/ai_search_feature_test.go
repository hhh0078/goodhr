// 本文件验证暂缓功能默认关闭且不会请求真实上游，既有岗位筛选不使用该开关。
package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestAISearchFeatureDefaultOff 验证只有明确的1能启用，误配值不会启动暂缓功能。
func TestAISearchFeatureDefaultOff(t *testing.T) {
	for _, value := range []string{"", "0", "true", "yes", " 1"} {
		t.Setenv("GOODHR_AI_SEARCH_ENABLED", value)
		if aiSearchEnabled() {
			t.Fatalf("误启用了暂缓功能: %q", value)
		}
	}
	t.Setenv("GOODHR_AI_SEARCH_ENABLED", "1")
	if !aiSearchEnabled() {
		t.Fatal("明确启用无效")
	}
}

// TestAISearchPauseStopsPaidRequests 验证网页入口和内部调用全部拒绝新增付费请求，缓存引用核对仍可免费使用。
func TestAISearchPauseStopsPaidRequests(t *testing.T) {
	server, routes, token, scope := assistantFixture(t)
	var calls atomic.Int32
	server.aiWallet.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		t.Fatal("暂停期间调用了模型上游")
		return nil, nil
	})}
	t.Setenv("GOODHR_AI_SEARCH_ENABLED", "")
	for _, path := range []string{"/api/assistant/sessions", "/api/resume-library/vectors", "/api/ai-compatible/v1/embeddings"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		routes.ServeHTTP(response, req)
		if response.Code != 503 {
			t.Fatalf("暂停入口没有关闭: %s status=%d", path, response.Code)
		}
	}
	if _, err := server.aiWallet.EmbedForUser(context.Background(), scope.UserEmail, []string{"合成测试"}); err == nil {
		t.Fatal("内部向量调用绕过暂停")
	}
	if _, err := server.assistant.builtinForScope(scope); err == nil {
		t.Fatal("后台助手调用绕过暂停")
	}
	status, _ := candidateTestRequest(t, routes, token, http.MethodPost, "/api/assistant/references/validate", map[string]any{"references": []any{}})
	if status != 200 || calls.Load() != 0 {
		t.Fatal("免费缓存核对被暂停或产生模型请求")
	}
}
