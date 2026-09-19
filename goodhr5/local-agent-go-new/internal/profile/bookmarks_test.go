// Package profile 验证 Firefox 书签扩展资源、旧书签迁移及重复初始化不破坏用户配置。
package profile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveCreatesDefaultBookmarks 验证新配置生成五个 Firefox 默认入口且不再写 Chromium 文件。
func TestResolveCreatesDefaultBookmarks(t *testing.T) {
	t.Setenv("GOODHR_CONSOLE_URL", "https://goodhr-test.ai.58it.cn")
	root := t.TempDir()
	profilePath, err := New(root).Resolve("default")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(profilePath, bookmarkExtensionDir, "bookmarks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var configuration bookmarkConfiguration
	if err := json.Unmarshal(data, &configuration); err != nil {
		t.Fatal(err)
	}
	if len(configuration.Defaults) != 5 || configuration.Defaults[0].URL != "https://goodhr-test.ai.58it.cn/" {
		t.Fatalf("默认入口错误：%+v", configuration.Defaults)
	}
	if _, err := os.Stat(filepath.Join(profilePath, "Default", "Bookmarks")); !os.IsNotExist(err) {
		t.Fatal("不应再生成 Chromium 书签")
	}
	preference, err := os.ReadFile(filepath.Join(profilePath, "user.js"))
	if err != nil || !strings.Contains(string(preference), `"always"`) {
		t.Fatal("书签栏显示配置缺失", err)
	}
	extensions := WithBookmarkExtension(profilePath, []string{"custom-addon"})
	if len(extensions) != 2 || extensions[0] != "custom-addon" {
		t.Fatal("用户扩展未保留")
	}
	if len(WithBookmarkExtension(profilePath, extensions)) != 2 {
		t.Fatal("重复添加扩展")
	}
}

// TestResolvePreservesUserBookmarksAndDoesNotRewrite 验证旧文件夹迁移及配置资源幂等性。
func TestResolvePreservesUserBookmarksAndDoesNotRewrite(t *testing.T) {
	root := t.TempDir()
	profileDir := filepath.Join(root, "old")
	if err := os.MkdirAll(filepath.Join(profileDir, "Default"), 0755); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"roots":{"bookmark_bar":{"children":[{"type":"folder","name":"我的文件夹","children":[{"type":"url","name":"我的书签","url":"https://example.com/"}]}]},"other":{"children":[{"type":"url","name":"其他入口","url":"https://example.org/"}]}}}`)
	legacyPath := filepath.Join(profileDir, "Default", "Bookmarks")
	if err := os.WriteFile(legacyPath, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "user.js"), []byte("user_pref(\"other.setting\", true);\n"), 0600); err != nil {
		t.Fatal(err)
	}
	manager := New(root)
	if _, err := manager.Resolve("old"); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(profileDir, bookmarkExtensionDir, "bookmarks.json")
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var config bookmarkConfiguration
	if err := json.Unmarshal(before, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Toolbar) != 1 || config.Toolbar[0].Title != "我的文件夹" || config.Toolbar[0].Children[0].URL != "https://example.com/" || len(config.Other) != 1 {
		t.Fatal("旧书签结构丢失")
	}
	if _, err := manager.Resolve("old"); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(configPath)
	if err != nil || string(before) != string(after) {
		t.Fatal("重复初始化改变配置", err)
	}
	original, err := os.ReadFile(legacyPath)
	if err != nil || string(original) != string(legacy) {
		t.Fatal("旧书签文件被修改", err)
	}
	prefs, err := os.ReadFile(filepath.Join(profileDir, "user.js"))
	if err != nil || !strings.Contains(string(prefs), `"other.setting"`) || strings.Count(string(prefs), "bookmarks.visibility") != 1 {
		t.Fatal("用户配置被覆盖或重复添加", err)
	}
}

// TestRepairBookmarkToolbar 验证书签组件误放地址栏时恢复位置，并保留其他布局、清理临时覆盖。
func TestRepairBookmarkToolbar(t *testing.T) {
	dir := t.TempDir()
	state := `{"placements":{"nav-bar":["back-button","personal-bookmarks","downloads-button"],"PersonalToolbar":[]},"currentVersion":24}`
	encoded, _ := json.Marshal(state)
	pref := append([]byte("user_pref(\"browser.uiCustomization.state\", "), encoded...)
	pref = append(pref, []byte(");\n")...)
	if err := os.WriteFile(filepath.Join(dir, "prefs.js"), pref, 0o600); err != nil {
		t.Fatal(err)
	}
	userJS, err := repairBookmarkToolbar(dir, []byte("user_pref(\"other.setting\", true);\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(userJS), `\"PersonalToolbar\":[\"personal-bookmarks\"]`) || !strings.Contains(string(userJS), `\"nav-bar\":[\"back-button\",\"downloads-button\"]`) || !strings.Contains(string(userJS), `\"currentVersion\":24`) {
		t.Fatal("书签组件未恢复或其他布局丢失", string(userJS))
	}
	// 模拟浏览器保存了修复后的布局，下一次初始化应删除一次性覆盖。
	patternStart := strings.Index(string(userJS), "user_pref(\"browser.uiCustomization.state\"")
	if err := os.WriteFile(filepath.Join(dir, "prefs.js"), userJS[patternStart:], 0o600); err != nil {
		t.Fatal(err)
	}
	cleaned, err := repairBookmarkToolbar(dir, userJS)
	if err != nil || strings.Contains(string(cleaned), "uiCustomization") || !strings.Contains(string(cleaned), "other.setting") {
		t.Fatal("临时布局覆盖未清理", err)
	}
}
