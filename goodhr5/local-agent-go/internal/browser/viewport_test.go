// Package browser 文件作用：验证 GoodHR 自动化浏览器的固定视口配置。
package browser

import "testing"

// TestFixedViewport 验证自动化视口始终使用更宽敞的 1440x900。
func TestFixedViewport(t *testing.T) {
	width, height := FixedViewport()
	if width != 1440 || height != 900 {
		t.Fatalf("固定视口应为 1440x900，实际为 %dx%d", width, height)
	}
}
