// Package taskrunner 文件作用：验证本地任务启动浏览器时的统一视口尺寸。
package taskrunner

import "testing"

// TestTaskBrowserViewport 验证任务启动不再根据电脑分辨率改变视口。
func TestTaskBrowserViewport(t *testing.T) {
	width, height := taskBrowserViewport()
	if width != 1280 || height != 720 {
		t.Fatalf("任务浏览器视口应为 1280x720，实际为 %dx%d", width, height)
	}
}
