// Package app 测试本地文件打开接口的路径安全校验。
package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSafeDownloadFilePathAllowsDownloadFile 验证下载目录内文件可以通过校验。
func TestSafeDownloadFilePathAllowsDownloadFile(t *testing.T) {
	downloadsDir := t.TempDir()
	filePath := filepath.Join(downloadsDir, "resume.pdf")
	if err := os.WriteFile(filePath, []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := safeDownloadFilePath(filePath, downloadsDir)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got = %s, want %s", got, want)
	}
}

// TestSafeDownloadFilePathRejectsOutsideFile 验证下载目录外文件会被拒绝。
func TestSafeDownloadFilePathRejectsOutsideFile(t *testing.T) {
	downloadsDir := t.TempDir()
	outsideDir := t.TempDir()
	filePath := filepath.Join(outsideDir, "secret.pdf")
	if err := os.WriteFile(filePath, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := safeDownloadFilePath(filePath, downloadsDir)
	if err == nil || !strings.Contains(err.Error(), "GoodHR 下载目录") {
		t.Fatalf("err = %v", err)
	}
}
