// Package runtime 文件作用：验证运行组件校验、平台选择和解压路径安全规则。
package runtime

import (
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

// TestVerifySHA256 验证正确校验值通过、错误校验值失败。
func TestVerifySHA256(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asset.zip")
	content := []byte("goodhr-runtime")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	if err := verifySHA256(path, hex.EncodeToString(sum[:])); err != nil {
		t.Fatalf("verifySHA256() error = %v", err)
	}
	if err := verifySHA256(path, "bad"); err == nil {
		t.Fatal("verifySHA256() accepted wrong digest")
	}
	if err := verifySHA256(path, ""); err == nil {
		t.Fatal("verifySHA256() accepted empty digest")
	}
}

// TestValidateAssetURLRequiresHTTPS 验证运行组件不能通过明文 HTTP 下载。
func TestValidateAssetURLRequiresHTTPS(t *testing.T) {
	if err := validateAssetURL("https://oss.example.com/runtime.zip"); err != nil {
		t.Fatalf("HTTPS 下载地址被拒绝：%v", err)
	}
	if err := validateAssetURL("http://oss.example.com/runtime.zip"); err == nil {
		t.Fatal("HTTP 下载地址不应被接受")
	}
}

// TestSafeJoinRejectsTraversal 验证组件压缩包不能越界写文件。
func TestSafeJoinRejectsTraversal(t *testing.T) {
	if _, err := safeJoin(t.TempDir(), "../outside"); err == nil {
		t.Fatal("safeJoin() accepted traversal path")
	}
}

// TestWorkerDependencyPathFindsParentNodeModules 验证 Worker 编译入口可以向父目录找到 CloakBrowser 依赖。
func TestWorkerDependencyPathFindsParentNodeModules(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "worker", "dist", "main.js")
	dependency := filepath.Join(root, "worker", "node_modules", "cloakbrowser", "package.json")
	files := map[string]string{
		entry: "",
		filepath.Join(root, "worker", "package.json"): `{"dependencies":{"cloakbrowser":"0.5.9"}}`,
		dependency: `{"version":"0.5.9"}`,
		filepath.Join(root, "worker", "node_modules", "playwright-core", "package.json"): `{"version":"1.61.1"}`,
		filepath.Join(root, "worker", "node_modules", "mmdb-lib", "package.json"):        `{"version":"3.0.2"}`,
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manager := &Manager{entryPath: entry}
	if got := manager.WorkerDependencyPath(); got != dependency {
		t.Fatalf("WorkerDependencyPath() = %s, want %s", got, dependency)
	}
	if err := manager.CheckWorkerBuild(); err != nil {
		t.Fatalf("CheckWorkerBuild() error = %v", err)
	}
}

// TestPrivateSettingsProtectAndHideLicenseKey 验证本机 Key 使用私密权限保存，普通状态不会返回明文。
func TestPrivateSettingsProtectAndHideLicenseKey(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	manager := &Manager{runtimeDir: runtimeDir}
	licenseKey := "cb_test_private_settings_123456"
	if err := manager.SaveCloakBrowserLicenseKey(licenseKey); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(manager.privateSettingsPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("private settings mode = %o, want 600", info.Mode().Perm())
	}
	encoded, err := json.Marshal(manager.Status())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), licenseKey) {
		t.Fatal("运行组件状态泄露了 CloakBrowser Key")
	}
	loaded := &Manager{runtimeDir: runtimeDir}
	loaded.loadPrivateSettings()
	if !loaded.CloakBrowserLicenseConfigured() || loaded.cloakBrowserLicenseKey() != licenseKey {
		t.Fatal("重新启动后没有正确读取本机私密 Key")
	}
}

// TestOfficialDownloadProgressParsing 验证官方十等分进度会转换为前端百分比和字节数。
func TestOfficialDownloadProgressParsing(t *testing.T) {
	manager := &Manager{}
	manager.updateOfficialInstallProgress("[cloakbrowser] Download progress: 50% (120/240 MB)", 2)
	progress := manager.InstallProgress()
	if progress.Stage != "download" || progress.Percent != 64 {
		t.Fatalf("progress = %+v", progress)
	}
	if progress.Attempt != 2 || progress.MaxAttempts != officialInstallMaxAttempts {
		t.Fatalf("attempt progress = %d/%d", progress.Attempt, progress.MaxAttempts)
	}
	if progress.Received != 120*1024*1024 || progress.Total != 240*1024*1024 {
		t.Fatalf("download bytes = %d/%d", progress.Received, progress.Total)
	}
}

// TestRetryableOfficialInstallError 验证官方短暂网络故障会重试，校验失败不会被掩盖。
func TestRetryableOfficialInstallError(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{name: "版本接口暂时不可用", output: "Could not determine latest Pro version from server", want: true},
		{name: "官方限流", output: "HTTP 429 Too Many Requests", want: true},
		{name: "连接重置", output: "fetch failed: ECONNRESET", want: true},
		{name: "校验失败", output: "SHA256 checksum mismatch", want: false},
		{name: "Key 无效", output: "license key invalid", want: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := retryableOfficialInstallError(testCase.output); got != testCase.want {
				t.Fatalf("retryableOfficialInstallError(%q) = %v, want %v", testCase.output, got, testCase.want)
			}
		})
	}
}

// TestCommandFailureKeepsUsefulContext 验证安装失败会保留末尾多行错误。
func TestCommandFailureKeepsUsefulContext(t *testing.T) {
	result := commandFailure("准备安装\n下载失败\nECONNRESET\n请检查网络", os.ErrPermission)
	if !strings.Contains(result, "下载失败") || !strings.Contains(result, "ECONNRESET") || !strings.Contains(result, "请检查网络") {
		t.Fatalf("commandFailure() 丢失错误上下文：%q", result)
	}
}

// TestDownloadAssetRetriesTemporaryServerError 验证组件下载遇到短暂服务故障会自动重试。
func TestDownloadAssetRetriesTemporaryServerError(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("runtime-data"))
	}))
	defer server.Close()
	manager := &Manager{runtimeDir: t.TempDir()}
	target := filepath.Join(manager.runtimeDir, "downloads", "node.zip")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := manager.downloadAsset(t.Context(), "node_runtime", "Node 运行环境", server.URL, target, 3, 20); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("download attempts = %d, want 2", attempts)
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "runtime-data" {
		t.Fatalf("downloaded content=%q err=%v", content, err)
	}
}

// TestRemoveComponentOnlyDeletesManagedDirectory 验证删除浏览器组件不会碰账号和 Key。
func TestRemoveComponentOnlyDeletesManagedDirectory(t *testing.T) {
	dataDir := t.TempDir()
	runtimeDir := filepath.Join(dataDir, "runtime")
	entryPath := filepath.Join(dataDir, "worker", "dist", "main.js")
	manager := &Manager{runtimeDir: runtimeDir, entryPath: entryPath}
	browserFile := filepath.Join(runtimeDir, "cloakbrowser", "chromium-test-pro", "chrome")
	profileFile := filepath.Join(dataDir, "profiles", "default", "Cookies")
	if err := os.MkdirAll(filepath.Dir(browserFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(browserFile, []byte("browser"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(profileFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profileFile, []byte("profile"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manager.SaveCloakBrowserLicenseKey("cb_test_remove_component_123456"); err != nil {
		t.Fatal(err)
	}
	if err := manager.saveCloakBrowserVersion("test", browserFile); err != nil {
		t.Fatal(err)
	}
	if err := manager.RemoveComponent("cloakbrowser"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runtimeDir, "cloakbrowser")); !os.IsNotExist(err) {
		t.Fatalf("浏览器目录仍存在：%v", err)
	}
	if _, err := os.Stat(profileFile); err != nil {
		t.Fatalf("账号目录被误删：%v", err)
	}
	if !manager.CloakBrowserLicenseConfigured() {
		t.Fatal("删除浏览器时不应顺便清除 Key")
	}
	if _, exists := manager.loadVersions()["cloakbrowser"]; exists {
		t.Fatal("浏览器版本记录仍存在")
	}
}

// TestOverrideEnvironmentClearsLegacyCloakBrowserValues 验证安装命令会清空系统遗留的旧版路径和版本锁定。
func TestOverrideEnvironmentClearsLegacyCloakBrowserValues(t *testing.T) {
	result := overrideEnvironment(
		[]string{"PATH=/usr/bin", "CLOAKBROWSER_BINARY_PATH=/legacy/chrome", "cloakbrowser_version=146"},
		[]string{"CLOAKBROWSER_BINARY_PATH=", "CLOAKBROWSER_VERSION=", "CLOAKBROWSER_RELEASE_CHANNEL=stable"},
	)
	joined := strings.Join(result, "\n")
	if strings.Contains(joined, "/legacy/chrome") || strings.Contains(joined, "=146") {
		t.Fatalf("旧版环境变量没有被覆盖：%s", joined)
	}
	if !strings.Contains(joined, "CLOAKBROWSER_BINARY_PATH=") || !strings.Contains(joined, "CLOAKBROWSER_VERSION=") {
		t.Fatalf("清空变量没有写入子进程环境：%s", joined)
	}
}

// TestStatusHidesRemovedLegacyCloakBrowser 验证浏览器文件已删除后不再展示旧版本记录。
func TestStatusHidesRemovedLegacyCloakBrowser(t *testing.T) {
	runtimeDir := t.TempDir()
	manager := &Manager{runtimeDir: runtimeDir}
	if err := manager.saveVersion("cloakbrowser", Asset{Version: "145.0.7632.109.2", URL: "https://example.com/legacy.zip"}); err != nil {
		t.Fatal(err)
	}
	status := manager.Status()
	if status.CloakBrowserVersion != "" {
		t.Fatalf("已删除浏览器仍展示旧版本：%s", status.CloakBrowserVersion)
	}
	if _, exists := status.InstalledVersions["cloakbrowser"]; exists {
		t.Fatal("状态接口仍返回已删除的旧浏览器记录")
	}
}

// TestStatusReportsCachedBrowserWithoutLicense 验证清除 Key 后仍能识别并删除浏览器缓存。
func TestStatusReportsCachedBrowserWithoutLicense(t *testing.T) {
	runtimeDir := t.TempDir()
	manager := &Manager{runtimeDir: runtimeDir}
	browserPath := filepath.Join(runtimeDir, "cloakbrowser", "chromium-test-pro", "chrome")
	if err := os.MkdirAll(filepath.Dir(browserPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(browserPath, []byte("browser"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := manager.saveCloakBrowserVersion("test", browserPath); err != nil {
		t.Fatal(err)
	}
	status := manager.Status()
	if !status.CloakBrowserCached {
		t.Fatal("已下载的浏览器缓存没有被识别")
	}
	if status.CloakBrowserInstalled {
		t.Fatal("没有 Key 时浏览器不应被标记为可运行")
	}
}
