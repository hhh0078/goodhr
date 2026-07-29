// Package greeting 文件作用：验证 OCR 和 AI 临时截图的安全清理边界。
package greeting

import (
	"os"
	"path/filepath"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
)

// TestCleanupDetailScreenshotsOnlyRemovesTemporaryFiles 验证只删除截图目录内文件，不越界删除其他文件。
func TestCleanupDetailScreenshotsOnlyRemovesTemporaryFiles(t *testing.T) {
	parent := t.TempDir()
	screenshotsDir := filepath.Join(parent, "screenshots")
	if err := os.MkdirAll(screenshotsDir, 0o755); err != nil {
		t.Fatalf("创建截图目录失败：%v", err)
	}
	inside := filepath.Join(screenshotsDir, "inside.png")
	outside := filepath.Join(parent, "outside.png")
	for _, path := range []string{inside, outside} {
		if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
			t.Fatalf("创建测试截图失败：%v", err)
		}
	}
	flow := &Flow{ScreenshotsDir: screenshotsDir}
	flow.cleanupDetailScreenshots("task-cleanup", []contract.ScreenshotPart{
		{Path: inside, Filename: "inside.png"},
		{Path: outside, Filename: "outside.png"},
	})
	if _, err := os.Stat(inside); !os.IsNotExist(err) {
		t.Fatalf("截图目录内文件没有删除：%v", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("截图目录外文件被误删：%v", err)
	}
}
