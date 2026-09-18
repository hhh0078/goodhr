// Package runtime 文件作用：验证原生下载资源修改、备份、未知版本拒绝与资源完整性。
package runtime

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// TestNativeDownloadsResource 验证原生下载只在首选项启用时生效，并保留其他资源与原始备份。
func TestNativeDownloadsResource(t *testing.T) {
	dir := t.TempDir()
	originalRegistry := []byte("interceptDownloadRequest(externalAppHandler, request, browsingContext, outFile) {\nreturn true;\n}\nthis._defaultContext = new BrowserContext(this, undefined, undefined);")
	resources := map[string][]byte{nativeRegistryResource: originalRegistry, "unchanged.bin": bytes.Repeat([]byte{0, 1, 255}, 1000)}
	resources[nativeHandlerResource] = []byte("    this._eventListeners = [\n original events\n ];")
	var original bytes.Buffer
	writer := zip.NewWriter(&original)
	for name, content := range resources {
		stream, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := stream.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "omni.ja")
	if err := os.WriteFile(target, original.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCamoufoxNativeDownloads(dir); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(updated), int64(len(updated)))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range reader.File {
		stream, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(stream)
		stream.Close()
		if err != nil {
			t.Fatal(err)
		}
		switch entry.Name {
		case nativeRegistryResource:
			if !bytes.Contains(content, []byte("getBoolPref('goodhr.downloads.native', false)) return false")) || bytes.Count(content, []byte(nativeDownloadMarker)) != 1 {
				t.Fatal("没有安装可选的原生下载通道")
			}
		case nativeObserverResource:
			if !bytes.Equal(content, nativeDownloadsModule) {
				t.Fatal("观察器内容不完整")
			}
		case nativeHandlerResource:
			if !bytes.Contains(content, []byte("await startGoodHRNativeDownloads()")) {
				t.Fatal("没有在首选项生效后初始化")
			}
		default:
			if !bytes.Equal(content, resources[entry.Name]) {
				t.Fatal("其他资源被修改")
			}
		}
	}
	backup, err := os.ReadFile(target + ".goodhr-original")
	if err != nil || !bytes.Equal(backup, original.Bytes()) {
		t.Fatal("原资源备份不正确", err)
	}
	if err := EnsureCamoufoxNativeDownloads(dir); err != nil {
		t.Fatal(err)
	}
	repeated, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(repeated, updated) {
		t.Fatal("重复调用改写了资源", err)
	}
}

// TestNativeDownloadsUnknownVersion 验证不匹配的浏览器资源不被强行改写。
func TestNativeDownloadsUnknownVersion(t *testing.T) {
	dir := t.TempDir()
	var original bytes.Buffer
	writer := zip.NewWriter(&original)
	stream, _ := writer.Create(nativeRegistryResource)
	stream.Write([]byte("unknown resource"))
	writer.Close()
	target := filepath.Join(dir, "omni.ja")
	os.WriteFile(target, original.Bytes(), 0o600)
	if err := EnsureCamoufoxNativeDownloads(dir); err == nil {
		t.Fatal("应拒绝未知资源版本")
	}
	after, _ := os.ReadFile(target)
	if !bytes.Equal(after, original.Bytes()) {
		t.Fatal("未知资源被修改")
	}
	if err := EnsureCamoufoxNativeDownloads(""); err == nil {
		t.Fatal("应拒绝空目录")
	}
	if err := EnsureCamoufoxNativeDownloads(t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

// TestNativeDownloadsInstalledBrowser 仅对明确指定的隔离浏览器目录安装资源，供真实浏览器集成测试使用。
func TestNativeDownloadsInstalledBrowser(t *testing.T) {
	dir := os.Getenv("GOODHR_NATIVE_DOWNLOAD_TEST_RUNTIME")
	if dir == "" {
		t.Skip("没有指定隔离浏览器组件")
	}
	if err := EnsureCamoufoxNativeDownloads(dir); err != nil {
		t.Fatal(err)
	}
}
