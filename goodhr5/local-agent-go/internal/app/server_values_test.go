// Package app 文件作用：测试本地接口请求参数的默认值解析。
package app

import (
	"testing"

	"goodhr5/local-agent-go/internal/browser"
)

// TestBoolValueDefault 验证打招呼开关缺失时使用默认值，明确关闭时仍保持关闭。
func TestBoolValueDefault(t *testing.T) {
	if !boolValueDefault(nil, true) {
		t.Fatal("字段缺失时应使用 true 默认值")
	}
	if boolValueDefault(false, true) {
		t.Fatal("明确传 false 时不应被默认值覆盖")
	}
	if !boolValueDefault("true", false) {
		t.Fatal("字符串 true 应被正确识别")
	}
	if boolValueDefault("false", true) {
		t.Fatal("字符串 false 应被正确识别")
	}
}

// TestPrepareBrowserViewport 验证所有浏览器入口都会覆盖为统一的固定视口。
func TestPrepareBrowserViewport(t *testing.T) {
	payload := map[string]any{"viewport_width": 1920, "viewport_height": 1080}
	new(Server).prepareBrowserViewport(payload)
	if payload["viewport_width"] != browser.FixedViewportWidth || payload["viewport_height"] != browser.FixedViewportHeight {
		t.Fatalf("浏览器入口应统一使用 %dx%d，实际为 %vx%v", browser.FixedViewportWidth, browser.FixedViewportHeight, payload["viewport_width"], payload["viewport_height"])
	}
}
