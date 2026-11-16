// 本文件验证私有部署的离线账号、内网 AI、组件完整性和线上策略隔离，不调用公网或付费模型。
package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPrivateAIStartWithoutWallet 验证私有自定义接口无本站余额可启动，线上相同配置仍被拦截。
func TestPrivateAIStartWithoutWallet(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	email := "private-start@example.com"
	token := loginForTest(t, routes, email)
	bindPositionDeviceForTest(t, routes, token)
	positionID := createPositionWithConfigForTest(t, routes, token, "私有AI测试", `{"mode_default":"ai","detail_mode":"dom"}`)
	_, err := server.positionExecution.aiConfigs.SaveUserConfig(email, AIConfig{BaseURL: "http://192.168.1.100:8000/v1/chat/completions", Model: "private-model", APIKey: "private-key", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	response := postPositionExecutionForTest(t, routes, token, "/api/positions/"+positionID+"/start", `{"task_type":"greeting","machine_id":"`+positionTestMachineID+`"}`)
	if response.Code != http.StatusForbidden {
		t.Fatalf("线上外部AI未拦截：%s", response.Body.String())
	}
	t.Setenv("GOODHR_DEPLOYMENT_MODE", "private")
	t.Setenv("GOODHR_PUBLIC_BASE_URL", "http://192.168.1.100:8080")
	server.positionExecution.aiWallet = nil
	response = postPositionExecutionForTest(t, routes, token, "/api/positions/"+positionID+"/start", `{"task_type":"greeting","machine_id":"`+positionTestMachineID+`"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("私有AI被余额拦截：%s", response.Body.String())
	}
}

// TestPrivateAIValidationAndSameHostPorts 验证内网测试及同IP不同端口的模型不被当作本站计费接口。
func TestPrivateAIValidationAndSameHostPorts(t *testing.T) {
	request := aiConfigRequest{BaseURL: "http://192.168.1.100:8000/v1", Model: "private-model", APIKey: "unused"}
	if validateAIConfigTestRequest(request) == nil {
		t.Fatal("普通版放行了内网HTTP")
	}
	t.Setenv("GOODHR_DEPLOYMENT_MODE", "private")
	t.Setenv("GOODHR_PUBLIC_BASE_URL", "http://192.168.1.100:8080")
	if err := validateAIConfigTestRequest(request); err != nil {
		t.Fatal(err)
	}
	if isCurrentSiteAIURL(request.BaseURL) {
		t.Fatal("同IP模型端口被误判为内置AI")
	}
	if isCurrentSiteAIURL("http://192.168.1.100:8080/v1") {
		t.Fatal("同源非内置路径被误判")
	}
	for _, address := range []string{"http://user:pass@192.168.1.100/v1", "file:///etc/passwd", "http://192.168.1.100/v1#fragment"} {
		request.BaseURL = address
		if validateAIConfigTestRequest(request) == nil {
			t.Fatalf("不安全地址通过：%s", address)
		}
	}
}

// TestPrivateAITestUsesLANHTTP 验证私有接口实际能请求内网 HTTP 模型，跨源重定向不会泄漏 Key。
func TestPrivateAITestUsesLANHTTP(t *testing.T) {
	t.Setenv("GOODHR_DEPLOYMENT_MODE", "private")
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer synthetic-key" {
			t.Error("模型请求没有使用本人配置")
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"成功"}}]}`))
	}))
	defer model.Close()
	service := NewAIConfigService(nil, NewMemoryAIConfigStore(), NewMemorySystemConfigStore())
	request := aiConfigRequest{BaseURL: model.URL + "/v1/chat/completions", Model: "internal-model", APIKey: "synthetic-key"}
	if result, err := service.requestAITest(context.Background(), request); err != nil || result != "成功" {
		t.Fatalf("内网模型不可用：%q %v", result, err)
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, model.URL+"/v1/chat/completions", http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	request.BaseURL = redirect.URL + "/v1/chat/completions"
	if _, err := service.requestAITest(context.Background(), request); err == nil {
		t.Fatal("私有AI允许了跨源重定向")
	}
}

// TestPrivateAdminBootstrapKeepsPassword 验证离线初始化不覆盖已有密码，也不能改变线上账号。
func TestPrivateAdminBootstrapKeepsPassword(t *testing.T) {
	store := NewMemoryPasswordStore()
	t.Setenv("GOODHR_PRIVATE_ADMIN_PASSWORD", "synthetic-password")
	if err := bootstrapPrivateAdmin(store, []string{"ADMIN@Example.com"}); err != nil {
		t.Fatal(err)
	}
	if exists, _, _ := store.AccountStatus("admin@example.com"); exists {
		t.Fatal("普通部署创建了私有管理员")
	}
	t.Setenv("GOODHR_DEPLOYMENT_MODE", "private")
	if err := bootstrapPrivateAdmin(store, []string{"admin@example.com"}); err != nil {
		t.Fatal(err)
	}
	before, err := store.PasswordHash("admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOODHR_PRIVATE_ADMIN_PASSWORD", "changed-startup-password")
	if err := bootstrapPrivateAdmin(store, []string{"admin@example.com"}); err != nil {
		t.Fatal(err)
	}
	after, _ := store.PasswordHash("admin@example.com")
	if before != after {
		t.Fatal("重启覆盖了密码")
	}
}

// TestDeploymentAssetsDoNotExposeUnknownFields 验证公开组件清单只输出下载字段，不带系统配置里的额外密钥。
func TestDeploymentAssetsDoNotExposeUnknownFields(t *testing.T) {
	server := mustNewServer(t)
	item, err := server.systemConfigs.Get("system.onboarding_config")
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(item.ConfigValue), &value); err != nil {
		t.Fatal(err)
	}
	components := value["runtime_components"].(map[string]any)
	components["node_runtime"].(map[string]any)["win"].(map[string]any)["private_key"] = "must-not-publish"
	encoded, _ := json.Marshal(value)
	item.ConfigValue = string(encoded)
	if err := server.systemConfigs.Save(item); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.Routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/system/deployment-assets", nil))
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "must-not-publish") {
		t.Fatalf("组件清单泄漏或不可用：%d", response.Code)
	}
}

// TestPrivateAssetCacheIntegrity 验证完整下载才发布，错误摘要和HTTP源不会生成可用组件。
func TestPrivateAssetCacheIntegrity(t *testing.T) {
	content := []byte("synthetic-runtime-archive")
	hash := sha256.Sum256(content)
	digest := hex.EncodeToString(hash[:])
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(content) }))
	defer source.Close()
	directory := t.TempDir()
	asset := deploymentAsset{Version: "test", URL: source.URL + "/runtime.zip", SHA256: digest}
	local, err := cachePrivateAsset(context.Background(), source.Client(), directory, asset)
	if err != nil {
		t.Fatal(err)
	}
	if local.SHA256 != digest || !strings.HasPrefix(local.URL, "/downloads/private/") {
		t.Fatalf("无效本地组件：%+v", local)
	}
	asset.SHA256 = strings.Repeat("a", 64)
	if _, err := cachePrivateAsset(context.Background(), source.Client(), directory, asset); err == nil {
		t.Fatal("错误摘要被发布")
	}
	entries, _ := os.ReadDir(directory)
	if len(entries) != 1 {
		t.Fatal("下载失败留下了临时文件")
	}
	asset.URL = "http://example.invalid/runtime.zip"
	if _, err := cachePrivateAsset(context.Background(), source.Client(), directory, asset); err == nil {
		t.Fatal("公网源可降级HTTP")
	}
}

// TestPrivateAssetsOfflineRestart 验证已有完整缓存时不访问公网，三个平台均返回内网绝对地址。
func TestPrivateAssetsOfflineRestart(t *testing.T) {
	t.Setenv("GOODHR_DEPLOYMENT_MODE", "private")
	t.Setenv("GOODHR_PRIVATE_ASSETS_DIR", t.TempDir())
	t.Setenv("GOODHR_PRIVATE_ASSET_SOURCE", "https://unreachable.invalid")
	t.Setenv("GOODHR_PUBLIC_BASE_URL", "http://192.168.1.100:8080")
	content := []byte("cached-component")
	hash := sha256.Sum256(content)
	asset := deploymentAsset{Version: "test", URL: "/downloads/private/cached.zip", SHA256: hex.EncodeToString(hash[:])}
	manifest := deploymentAssets{RuntimeComponents: map[string]map[string]deploymentAsset{}}
	for _, component := range []string{"node_runtime", "camoufox"} {
		manifest.RuntimeComponents[component] = map[string]deploymentAsset{"win-x64": asset, "darwin-arm64": asset, "darwin-x64": asset}
	}
	encoded, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(privateAssetsDir(), privateManifestName), encoded, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(privateAssetsDir(), "cached.zip"), content, 0644); err != nil {
		t.Fatal(err)
	}
	store := NewMemorySystemConfigStore()
	if err := preparePrivateAssets(store); err != nil {
		t.Fatal(err)
	}
	item, _ := store.Get("system.onboarding_config")
	if strings.Contains(item.ConfigValue, "oss2.58it.cn") || !strings.Contains(item.ConfigValue, "http://192.168.1.100:8080/downloads/private/") {
		t.Fatal("离线组件仍有公网地址")
	}
	if err := os.WriteFile(filepath.Join(privateAssetsDir(), "cached.zip"), []byte("corrupt"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := preparePrivateAssets(store); err == nil {
		t.Fatal("损坏缓存仍可启动")
	}
}
