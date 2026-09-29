// Package runtime 文件作用：验证鼠标安全补丁拒绝未知资源且重复应用不会损坏资源。
package runtime

import (
	"bytes"
	"testing"
)

// TestMouseSafetyPatch 验证已知资源替换、幂等和未知版本拒绝。
func TestMouseSafetyPatch(t *testing.T) {
	original := []byte("prefix\n  async ['Page.dispatchMouseEvent']({type, x, y, button, clickCount, modifiers, buttons}) {\n    const win = this._pageTarget._window;\n    const sendEvents = async (types) => {\nChromeUtils.camouGetMouseTrajectory(); watcher.ensureEvent(eventType);\n};\n    // We must switch to proper tab in the tabbed browser so that\n    await this._pageTarget.activateAndRun(async () => {\n        await sendEvents(['mousemove']);\n    });\n  }\n  async ['Page.dispatchWheelEvent'](params) { oldWheel(); }\n  async ['Page.insertText'](options) {}\nsuffix")
	updated, err := patchMouseSafety(original)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(updated, []byte("suffix")) || !bytes.Contains(updated, []byte("GOODHR_MOUSE_ACK_TIMEOUT")) {
		t.Fatal("安全补丁不完整")
	}
	repeated, err := patchMouseSafety(updated)
	if err != nil || !bytes.Equal(repeated, updated) {
		t.Fatal("重复安装改写资源", err)
	}
	if _, err := patchMouseSafety([]byte("unknown version")); err == nil {
		t.Fatal("未知资源没有拒绝")
	}
	legacy := append([]byte("// GoodHR native mouse safety v6\n"), original...)
	upgraded, err := patchMouseSafety(legacy)
	if err != nil || !bytes.HasPrefix(upgraded, []byte(mouseSafetyMarker)) || !bytes.Contains(upgraded, []byte("_goodhrEnsureWindowReady(")) || bytes.Contains(upgraded, []byte("oldWheel()")) {
		t.Fatal("已有 v6 组件没有升级到含滚轮和激活诊断的版本", err)
	}
	stable, err := patchMouseSafety(upgraded)
	if err != nil || !bytes.Equal(stable, upgraded) {
		t.Fatal("升级后重复安装不稳定", err)
	}
}

// TestMouseSafetyV7Upgrade 验证已经安装的 v7 只更新公共输入保护，保留鼠标、滚轮及其他修正并拒绝损坏资源。
func TestMouseSafetyV7Upgrade(t *testing.T) {
	legacy := []byte("// GoodHR native mouse safety v7\nunchanged-browser-settings\n")
	legacy = append(legacy, []byte("// 文件作用：给浏览器原生输入增加激活阶段保护和窗口证据，超时后禁止迟到的输入，不读取招聘页面。\n  async _goodhrRunInput(diagnostic, operation) {}\n")...)
	tail := []byte("  async ['Page.dispatchMouseEvent']({type, x, y, button, clickCount, modifiers, buttons}) { GOODHR_MOUSE_ACK_TIMEOUT: }\n  async ['Page.dispatchWheelEvent']() { GOODHR_MOUSE_APZ_TIMEOUT:wheel }\nunchanged-download-settings")
	legacy = append(legacy, tail...)
	updated, err := patchMouseSafety(legacy)
	if err != nil || !bytes.HasPrefix(updated, []byte(mouseSafetyMarker+"\nunchanged-browser-settings\n")) || !bytes.HasSuffix(updated, tail) || !bytes.Contains(updated, []byte("GOODHR_MOUSE_WINDOW_NOT_READY:")) {
		t.Fatal("v7 公共保护没有安全升级", err)
	}
	repeated, err := patchMouseSafety(updated)
	if err != nil || !bytes.Equal(repeated, updated) {
		t.Fatal("v7 升级后重复安装改写资源", err)
	}
	if _, err := patchMouseSafety(bytes.Replace(legacy, []byte("_goodhrRunInput"), []byte("brokenHelper"), 1)); err == nil {
		t.Fatal("损坏的 v7 没有拒绝升级")
	}
}
