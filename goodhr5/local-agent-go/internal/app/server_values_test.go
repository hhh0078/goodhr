// Package app 文件作用：测试本地接口请求参数的默认值解析。
package app

import (
	"testing"
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

// TestPrepareBrowserPayloadDoesNotInjectViewport 验证浏览器入口不再补充固定窗口尺寸。
func TestPrepareBrowserPayloadDoesNotInjectViewport(t *testing.T) {
	payload := map[string]any{"downloads_path": "D:/goodhr-downloads"}
	new(Server).prepareBrowserPayload("/api/v1/browser/start", payload)
	if _, ok := payload["viewport_width"]; ok {
		t.Fatalf("浏览器入口不应写入 viewport_width：%v", payload["viewport_width"])
	}
	if _, ok := payload["viewport_height"]; ok {
		t.Fatalf("浏览器入口不应写入 viewport_height：%v", payload["viewport_height"])
	}
}
