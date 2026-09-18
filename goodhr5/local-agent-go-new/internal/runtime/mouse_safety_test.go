// Package runtime 文件作用：验证鼠标安全补丁拒绝未知资源且重复应用不会损坏资源。
package runtime

import (
	"bytes"
	"testing"
)

// TestMouseSafetyPatch 验证已知资源替换、幂等和未知版本拒绝。
func TestMouseSafetyPatch(t *testing.T) {
	original := []byte("prefix\n    const sendEvents = async (types) => {\nChromeUtils.camouGetMouseTrajectory(); watcher.ensureEvent(eventType);\n};\n    // We must switch to proper tab in the tabbed browser so that\nsuffix")
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
}
