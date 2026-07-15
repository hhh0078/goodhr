// 本文件验证运行组件配置接口不再依赖旧的新手节点状态。
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRuntimeConfigCurrent 验证登录用户可以读取运行组件配置。
func TestRuntimeConfigCurrent(t *testing.T) {
	server, err := NewServer()
	if err != nil {
		t.Fatal(err)
	}
	routes := server.Routes()
	token := loginForTest(t, routes, "runtime-config@qq.com")
	req := httptest.NewRequest(http.MethodGet, "/api/runtime/config", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	var payload struct {
		Config map[string]any `json:"config"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Config["runtime_components"] == nil {
		t.Fatalf("runtime config missing: %+v", payload.Config)
	}
}
