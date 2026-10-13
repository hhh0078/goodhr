// Package runtime 文件作用：验证运行组件校验、平台选择和解压路径安全规则。
package runtime

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
)

// TestOCRInstalledDarwinVisionWithoutModel 验证 macOS Vision 组件不依赖模型文件。
func TestOCRInstalledDarwinVisionWithoutModel(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("只检查 macOS Vision 组件")
	}
	root := t.TempDir()
	path := filepath.Join(root, "ocr", "RapidOCR-json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	manager := New("", "", root, path, nil)
	if !manager.OCRInstalled() {
		t.Fatal("可执行的 Vision 组件应显示已安装")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if manager.OCRInstalled() {
		t.Fatal("不可执行的 Vision 组件不应显示已安装")
	}
}

// TestInstallAssetReplacesSameVersionDifferentSHA 验证跨架构迁移时同版本浏览器仍按新校验值重新安装，旧记录缺少校验值也能升级。
func TestInstallAssetReplacesSameVersionDifferentSHA(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("仅验证 macOS 的 Camoufox.app 双架构目录")
	}
	runtimeDir := t.TempDir()
	binaryPath := filepath.Join(runtimeDir, "camoufox", "Camoufox.app", "Contents", "MacOS", "camoufox")
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, []byte("arm64"), 0o755); err != nil {
		t.Fatal(err)
	}

	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	header := &zip.FileHeader{Name: "Camoufox.app/Contents/MacOS/camoufox", Method: zip.Deflate}
	header.SetMode(0o755)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("x64")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	newSHA := sha256.Sum256(archive.Bytes())
	oldSHA := sha256.Sum256([]byte("old-arm64-package"))
	var downloads atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		_, _ = w.Write(archive.Bytes())
	}))
	defer server.Close()
	previousTransport := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	defer func() { http.DefaultTransport = previousTransport }()

	manager := &Manager{runtimeDir: runtimeDir}
	newAsset := Asset{Version: "152.0.4-beta.30", URL: server.URL + "/intel.zip", SHA256: hex.EncodeToString(newSHA[:])}
	oldAsset := Asset{Version: newAsset.Version, URL: server.URL + "/arm64.zip", SHA256: hex.EncodeToString(oldSHA[:])}
	if err := manager.saveVersion("camoufox", oldAsset, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := manager.installAsset(context.Background(), "camoufox", "Camoufox 浏览器", "camoufox", newAsset); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(manager.CamoufoxPath()); err != nil || string(got) != "x64" {
		t.Fatalf("同版本不同校验值未替换浏览器：content=%q err=%v", got, err)
	}
	if got, err := os.ReadFile(binaryPath); err != nil || string(got) != "arm64" {
		t.Fatalf("原地安装不应修改旧浏览器：content=%q err=%v", got, err)
	}
	if got := downloads.Load(); got != 1 {
		t.Fatalf("跨架构浏览器下载次数=%d，期望 1", got)
	}
	if recorded := manager.loadVersions()["camoufox"]; recorded.SHA256 != newAsset.SHA256 {
		t.Fatalf("安装记录未更新为目标校验值：%+v", recorded)
	}
	if err := manager.installAsset(context.Background(), "camoufox", "Camoufox 浏览器", "camoufox", newAsset); err != nil {
		t.Fatal(err)
	}
	if got := downloads.Load(); got != 1 {
		t.Fatalf("同版本同校验值不应重复下载：实际 %d 次", got)
	}
	oldAsset.SHA256 = "" // 兼容没有记录校验值的旧安装，重新校验并安装。
	if err := manager.saveVersion("camoufox", oldAsset, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := manager.installAsset(context.Background(), "camoufox", "Camoufox 浏览器", "camoufox", newAsset); err != nil {
		t.Fatal(err)
	}
	if got := downloads.Load(); got != 2 {
		t.Fatalf("旧记录缺少校验值时应重装：实际 %d 次", got)
	}
}

// TestLegacyBrowserManifestIgnored 验证旧浏览器清单不能触发组件下载。
func TestLegacyBrowserManifestIgnored(t *testing.T) {
	var manifest Manifest
	if err := json.Unmarshal([]byte(`{"cloakbrowser":{"win-x64":{"url":"https://example.com/old.zip"}}}`), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifestHasAssets(manifest) {
		t.Fatal("旧浏览器地址不应触发组件安装")
	}
}

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

// TestWorkerDependencyPathFindsParentNodeModules 验证 Worker 编译入口可以向父目录找到 Camoufox 依赖。
func TestWorkerDependencyPathFindsParentNodeModules(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "worker", "dist", "main.js")
	dependency := filepath.Join(root, "worker", "node_modules", "camoufox-js", "package.json")
	for _, path := range []string{entry, dependency} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
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
