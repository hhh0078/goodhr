// Package runtime 文件作用：回归验证浏览器标签修正保留原主题、重复启动不重复写入及组件更新后恢复修正。
package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnsureCamoufoxTabInteraction 验证首次、重复启动和组件重新下载时的主题修正。
func TestEnsureCamoufoxTabInteraction(t *testing.T) {
	dir := t.TempDir()
	stylePath := filepath.Join(dir, "chrome.css")
	original := "#TabsToolbar {-moz-window-dragging:drag !important;}\n.tab-content {pointer-events:none !important;}\n"
	if err := os.WriteFile(stylePath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := EnsureCamoufoxTabInteraction(dir); err != nil {
			t.Fatal(err)
		}
	}
	content, err := os.ReadFile(stylePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != original+camoufoxTabInteractionCSS {
		t.Fatal("原主题被破坏或修正被重复添加")
	}
	if !strings.Contains(camoufoxTabInteractionCSS, "no-drag !important") || !strings.Contains(camoufoxTabInteractionCSS, "pointer-events: auto !important") {
		t.Fatal("标签点击被阻断的规则没有覆盖")
	}
	if !strings.Contains(camoufoxTabInteractionCSS, `#navigator-toolbox #PersonalToolbar:not([collapsed="true"])`) {
		t.Fatal("书签栏仍被主题强制隐藏，或没有保留用户隐藏书签栏的选择")
	}
	if err := os.WriteFile(stylePath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCamoufoxTabInteraction(dir); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(stylePath)
	if err != nil || string(updated) != string(content) {
		t.Fatal("组件更新后没有恢复标签修正", err)
	}
}

// TestEnsureCamoufoxTabInteractionWithoutTheme 验证缺少自定义主题或目录为空的边界。
func TestEnsureCamoufoxTabInteractionWithoutTheme(t *testing.T) {
	if err := EnsureCamoufoxTabInteraction(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCamoufoxTabInteraction(""); err == nil {
		t.Fatal("空目录应该返回错误")
	}
}
