// Package runtime 文件作用：回归验证浏览器标签修正保留原主题、重复启动不重复写入及组件更新后恢复修正。
package runtime

import (
	"archive/zip"
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

// TestMacCamoufoxResourceLayout 验证 .app 内的主题、策略、布局和中文语言包会被实际修正。
func TestMacCamoufoxResourceLayout(t *testing.T) {
	root := t.TempDir()
	resources := filepath.Join(root, "Camoufox.app", "Contents", "Resources")
	for _, dir := range []string{"browser", "distribution"} {
		if err := os.MkdirAll(filepath.Join(resources, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stylePath := filepath.Join(resources, "chrome.css")
	if err := os.WriteFile(stylePath, []byte("#TabsToolbar {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(resources, "distribution", "policies.json")
	if err := os.WriteFile(policyPath, []byte(`{"policies":{"DisplayBookmarksToolbar":"never"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(resources, "browser", "omni.ja")
	archive, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(archive)
	entry, err := writer.Create(browserLayoutResource)
	if err == nil {
		_, err = entry.Write([]byte(`<toolbaritem id="personal-bookmarks" removable="true"/>`))
	}
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	if closeErr := archive.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	writeVersionFile(t, root, "152.0.4")
	for _, ensure := range []func(string) error{
		EnsureCamoufoxTabInteraction,
		EnsureCamoufoxBookmarkToolbar,
		EnsureCamoufoxBookmarkWidget,
		EnsureChineseLanguagePack,
	} {
		if err := ensure(root); err != nil {
			t.Fatal(err)
		}
	}
	style, err := os.ReadFile(stylePath)
	if err != nil || !strings.Contains(string(style), "no-drag !important") {
		t.Fatal(".app 标签修正没有生效", err)
	}
	policy, err := os.ReadFile(policyPath)
	if err != nil || !strings.Contains(string(policy), `"DisplayBookmarksToolbar": "always"`) {
		t.Fatal(".app 书签栏策略没有生效", err)
	}
	if _, err := os.Stat(archivePath + ".goodhr-original"); err != nil {
		t.Fatal(".app 布局修正没有生效", err)
	}
	if _, err := os.Stat(filepath.Join(resources, "browser", "extensions", langpackExtensionID+".xpi")); err != nil {
		t.Fatal(".app 中文语言包没有安装", err)
	}
}
