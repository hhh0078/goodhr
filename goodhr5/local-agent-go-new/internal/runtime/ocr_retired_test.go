// Package runtime 本文件验证当前安装忽略旧OCR资源且保留用户已有文件，不改变历史组件安装工具。
package runtime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestCurrentInstallIgnoresOCR 验证无效旧OCR资源不会阻止Node与浏览器安装，也不会改动旧OCR文件。
func TestCurrentInstallIgnoresOCR(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("本机没有Node，安装规则在有Node的测试环境验证")
	}
	root := t.TempDir()
	legacy := filepath.Join(root, "ocr", "preserve-existing.txt")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("existing-ocr-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	asset, downloads := serveNodeTestArchive(t, makeComponentTestArchive(t, componentTestFiles("camoufox", "synthetic-browser")))
	manager := &Manager{runtimeDir: root, nodePath: node}
	manifest := Manifest{Camoufox: map[string]Asset{platformKey(): asset}, OCR: map[string]Asset{platformKey(): {URL: "invalid-retired-ocr-url", SHA256: "invalid"}}}
	if err := manager.install(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	if downloads.Load() != 1 {
		t.Fatalf("浏览器应只下载一次，实际%d次", downloads.Load())
	}
	if data, err := os.ReadFile(legacy); err != nil || string(data) != "existing-ocr-data" {
		t.Fatal("旧OCR文件被改动", err)
	}
	if manifestHasAssets(Manifest{OCR: manifest.OCR}) {
		t.Fatal("只含OCR的旧清单不能触发安装")
	}
}
