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
	runtimeComponents, ok := payload.Config["runtime_components"].(map[string]any)
	if !ok {
		t.Fatalf("runtime components invalid: %+v", payload.Config["runtime_components"])
	}
	assertRuntimeAsset(t, runtimeComponents, "node_runtime", "win", "https://oss2.58it.cn/goodhr-node-runtime-win-x64.zip", "ea3fad0e67a991d8477d8c01344b56e69c676ccb733f065b22436994b1253f86")
	assertRuntimeAsset(t, runtimeComponents, "node_runtime", "mac", "https://oss2.58it.cn/goodhr-node-runtime-darwin-arm64.tar.gz", "c59006db713c770d6ec63ae16cb3edc11f49ee093b5c415d667bb4f436c6526d")
	assertRuntimeAsset(t, runtimeComponents, "ocr", "win", "https://oss2.58it.cn/goodhr-rapidocr-json-win-x64-v0.2.0.zip", "4db6867818002f194f79d1edda291efb438ab6f371d4b307bae990232b917a3d")
}

// assertRuntimeAsset 验证默认运行组件地址和校验值保持为已验收的分发包。
func assertRuntimeAsset(t *testing.T, components map[string]any, component string, platform string, wantURL string, wantSHA256 string) {
	t.Helper()
	platforms, ok := components[component].(map[string]any)
	if !ok {
		t.Fatalf("component %s invalid: %+v", component, components[component])
	}
	asset, ok := platforms[platform].(map[string]any)
	if !ok {
		t.Fatalf("component %s platform %s invalid: %+v", component, platform, platforms[platform])
	}
	if asset["url"] != wantURL || asset["sha256"] != wantSHA256 {
		t.Fatalf("component %s platform %s asset=%+v", component, platform, asset)
	}
}
