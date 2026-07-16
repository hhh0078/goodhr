// Package positionrunner 文件作用：验证本地岗位运行启动浏览器时的统一视口尺寸。
package positionrunner

import "testing"

// TestPositionBrowserViewport 验证岗位运行启动不再根据电脑分辨率改变视口。
func TestPositionBrowserViewport(t *testing.T) {
	width, height := positionBrowserViewport()
	if width != 1280 || height != 720 {
		t.Fatalf("岗位运行浏览器视口应为 1280x720，实际为 %dx%d", width, height)
	}
}
