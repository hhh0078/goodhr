// Package httpapi 本文件验证岗位开始前的后端运行检查，覆盖登录、归属及最终并发启动保护。
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPositionRunningPreflight 验证只返回当前账号运行岗位，不能受其他账号任务影响。
func TestPositionRunningPreflight(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "preflight@example.com")
	otherToken := loginForTest(t, routes, "other-preflight@example.com")
	id := createPositionWithConfigForTest(t, routes, token, "当前运行岗位", `{"mode":"candidate_search"}`)
	otherID := createPositionWithConfigForTest(t, routes, otherToken, "其他账号岗位", `{"mode":"candidate_search"}`)
	if err := server.positionExecution.store.ClaimPositionStart("other-preflight@example.com", otherID); err != nil {
		t.Fatal(err)
	}
	// check 通过公开路由读取运行状态，使用与页面相同的会话认证。
	check := func(auth string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/positions/running", nil)
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		response := httptest.NewRecorder()
		routes.ServeHTTP(response, req)
		return response
	}
	if response := check(""); response.Code != 401 {
		t.Fatal("未登录可查看运行状态", response.Code)
	}
	response := check(token)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	var result struct {
		Running  bool `json:"running"`
		Position struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"position"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &result)
	if result.Running {
		t.Fatal("不能把其他账号任务当作本人运行任务")
	}
	if err := server.positionExecution.store.ClaimPositionStart("preflight@example.com", id); err != nil {
		t.Fatal(err)
	}
	response = check(token)
	_ = json.Unmarshal(response.Body.Bytes(), &result)
	if !result.Running || result.Position.ID != id || result.Position.Name != "当前运行岗位" {
		t.Fatal(response.Body.String())
	}
	if err := server.positionExecution.store.ClaimPositionStart("preflight@example.com", id); err != ErrPositionAlreadyRunning {
		t.Fatal("最终原子检查失效", err)
	}
}
