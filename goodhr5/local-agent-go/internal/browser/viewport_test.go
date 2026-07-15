// Package browser 文件作用：验证 GoodHR 自动化浏览器的固定视口配置。
package browser

import "testing"

// TestFixedViewport 验证自动化视口始终使用兼容桌面端的 1280x720。
func TestFixedViewport(t *testing.T) {
	width, height := FixedViewport()
	if width != 1280 || height != 720 {
		t.Fatalf("固定视口应为 1280x720，实际为 %dx%d", width, height)
	}
}
