// 本文件验证运行组件配置接口不再依赖旧的新手节点状态。
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	components, ok := payload.Config["runtime_components"].(map[string]any)
	if !ok {
		t.Fatalf("runtime config missing: %+v", payload.Config)
	}
	if _, exists := components["cloakbrowser"]; exists {
		t.Fatal("运行组件配置不应再提供旧浏览器下载地址")
	}
	nodeRuntime, ok := components["node_runtime"].(map[string]any)
	if !ok {
		t.Fatal("Node 下载配置缺失")
	}
	for _, platform := range []string{"win", "mac"} {
		asset, ok := nodeRuntime[platform].(map[string]any)
		if !ok || asset["url"] == "" || asset["sha256"] == "" {
			t.Fatalf("%s 平台缺少 Node 下载地址或校验值：%v", platform, asset)
		}
		if got, _ := asset["url"].(string); !strings.HasPrefix(got, "https://oss2.58it.cn/") {
			t.Fatalf("%s 平台 Node 下载地址 = %v", platform, got)
		}
	}
	camoufox, ok := components["camoufox"].(map[string]any)
	if !ok {
		t.Fatal("Camoufox 下载配置缺失")
	}
	for _, platform := range []string{"win", "mac"} {
		asset, ok := camoufox[platform].(map[string]any)
		if !ok || asset["url"] == "" || asset["sha256"] == "" {
			t.Fatalf("%s 平台缺少 Camoufox 下载地址或校验值：%v", platform, asset)
		}
	}
	ocr, ok := components["ocr"].(map[string]any)
	if !ok {
		t.Fatal("OCR 下载配置缺失")
	}
	ocrWin, ok := ocr["win"].(map[string]any)
	if !ok || ocrWin["url"] == "" || ocrWin["sha256"] == "" {
		t.Fatalf("Windows OCR 下载配置不完整：%v", ocrWin)
	}
	if got := ocrWin["url"]; got != "https://oss2.58it.cn/goodhr-rapidocr-json-win-x64-v0.2.0.zip" {
		t.Fatalf("Windows OCR 下载地址 = %v", got)
	}
}
