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
	for _, platform := range []string{"win", "mac", "darwin-x64"} {
		asset, ok := nodeRuntime[platform].(map[string]any)
		if !ok || asset["url"] == "" || asset["sha256"] == "" {
			t.Fatalf("%s 平台缺少 Node 下载地址或校验值：%v", platform, asset)
		}
		if got, _ := asset["url"].(string); !strings.HasPrefix(got, "https://oss2.58it.cn/") {
			t.Fatalf("%s 平台 Node 下载地址 = %v", platform, got)
		}
	}
	for platform, expectedSHA := range map[string]string{
		"mac":        "4f2fdfb47352e3ab3908268dd2552081ce1e72f86b4e0ef45b379c18816af552",
		"darwin-x64": "bae90145c832278b2f93dbcf0ed3865755c6ef726efb9c7a5dc77ed519dec3ca",
	} {
		asset := nodeRuntime[platform].(map[string]any)
		if asset["sha256"] != expectedSHA {
			t.Fatalf("%s 平台 Node 校验值 = %v", platform, asset["sha256"])
		}
	}
	camoufox, ok := components["camoufox"].(map[string]any)
	if !ok {
		t.Fatal("Camoufox 下载配置缺失")
	}
	for _, platform := range []string{"win", "mac", "darwin-x64"} {
		asset, ok := camoufox[platform].(map[string]any)
		if !ok || asset["url"] == "" || asset["sha256"] == "" {
			t.Fatalf("%s 平台缺少 Camoufox 下载地址或校验值：%v", platform, asset)
		}
	}
	intelCamoufox := camoufox["darwin-x64"].(map[string]any)
	if intelCamoufox["url"] != "https://oss2.58it.cn/camoufox-152.0.4-beta.30-mac.x86_64.zip" || intelCamoufox["sha256"] != "f12f90a3650478e670064a2f071c13b998fdd1125f79dc9f6cad5a9b9395b4bc" {
		t.Fatalf("Intel Mac Camoufox 下载配置不完整：%v", intelCamoufox)
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
	for _, platform := range []string{"mac", "darwin-x64"} {
		asset, ok := ocr[platform].(map[string]any)
		if !ok || asset["url"] != "https://oss2.58it.cn/goodhr-ocr-vision-v1.0.0-darwin-universal.zip" || asset["sha256"] != "6a1afd55ce506659b15dc2c88d601e8947a5a20f153959f1c81c8a2e063099e3" {
			t.Fatalf("%s 平台 OCR 下载配置不完整：%v", platform, asset)
		}
	}
	releases, ok := payload.Config["local_agent"].([]any)
	if !ok || len(releases) == 0 {
		t.Fatalf("本地程序发布记录缺失：%v", payload.Config["local_agent"])
	}
	macRelease, ok := releases[0].(map[string]any)
	if !ok || macRelease["version"] != "6.2.10.4" || macRelease["url_mac"] != "https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.2.10.4-mac-universal-d24d8a5e.pkg" || macRelease["sha256"] != "d24d8a5ecfd4fdeef5591903886cd8350f1c5f1fe3541c12d4b1f8f48701bdc2" || macRelease["url_win"] != "" {
		t.Fatalf("Mac 本地程序发布记录不完整：%v", macRelease)
	}
}
