// Package greeting 文件作用：验证当前任务截图目录覆盖和文件编号规则。
package greeting

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPrepareScreenshotWorkspaceOnlyReplacesCurrentTask 验证新任务只覆盖固定截图目录且重置候选人编号。
func TestPrepareScreenshotWorkspaceOnlyReplacesCurrentTask(t *testing.T) {
	parent := t.TempDir()
	screenshotsDir := filepath.Join(parent, "screenshots")
	currentDir := filepath.Join(screenshotsDir, "current-task")
	if err := os.MkdirAll(currentDir, 0o755); err != nil {
		t.Fatalf("创建截图目录失败：%v", err)
	}
	inside := filepath.Join(currentDir, "candidate-001.png")
	outside := filepath.Join(parent, "outside.png")
	for _, path := range []string{inside, outside} {
		if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
			t.Fatalf("创建测试截图失败：%v", err)
		}
	}
	flow := &Flow{ScreenshotsDir: screenshotsDir}
	flow.screenshotSeq = 9
	if err := flow.prepareScreenshotWorkspace(); err != nil {
		t.Fatalf("准备当前任务截图目录失败：%v", err)
	}
	if _, err := os.Stat(inside); !os.IsNotExist(err) {
		t.Fatalf("上一任务截图没有覆盖：%v", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("截图目录外文件被误删：%v", err)
	}
	if filename := flow.nextCandidateScreenshotFilename(); filename != "candidate-001.png" {
		t.Fatalf("候选人截图编号没有重置：%s", filename)
	}
}
