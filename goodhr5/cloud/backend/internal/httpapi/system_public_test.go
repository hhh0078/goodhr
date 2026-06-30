// 本文件负责测试前端初始化所需的公开系统配置接口。
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAppConfigWithoutLogin 验证未登录时也可以读取前端应用配置。
func TestAppConfigWithoutLogin(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/system/app-config", nil)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}

	var payload struct {
		Config map[string]any `json:"config"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Config["free_daily_greet_limit"] == nil {
		t.Fatalf("unexpected config: %+v", payload.Config)
	}
}

// TestLocalAgentUpdatesWithoutLogin 验证未登录时也可以读取本地程序更新记录。
func TestLocalAgentUpdatesWithoutLogin(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/system/local-agent-updates", nil)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}

	var payload struct {
		LocalAgent []map[string]any `json:"local_agent"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.LocalAgent) == 0 {
		t.Fatalf("unexpected local_agent: %+v", payload.LocalAgent)
	}
}
