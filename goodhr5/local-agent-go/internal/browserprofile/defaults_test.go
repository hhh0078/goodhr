// Package browserprofile 测试浏览器 Profile 默认书签和搜索引擎初始化。
package browserprofile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestEnsureDefaultsCreatesDefaultProfile 验证首次启动时可以创建默认 Profile 配置。
// t 为 Go 测试对象。
func TestEnsureDefaultsCreatesDefaultProfile(t *testing.T) {
	profilesDir := t.TempDir()
	if err := EnsureDefaults(profilesDir); err != nil {
		t.Fatalf("EnsureDefaults() error = %v", err)
	}

	bookmarks := readTestJSON(t, filepath.Join(profilesDir, "default", "Default", "Bookmarks"))
	children := pathValue(bookmarks, "roots.bookmark_bar.children").([]any)
	if len(children) < len(recruitBookmarks) {
		t.Fatalf("bookmark count = %d, want at least %d", len(children), len(recruitBookmarks))
	}
	for index, spec := range recruitBookmarks {
		node := children[index].(map[string]any)
		if got := stringValue(node["name"]); got != spec.Name {
			t.Fatalf("bookmark[%d].name = %q, want %q", index, got, spec.Name)
		}
		if got := stringValue(node["url"]); got != spec.URL {
			t.Fatalf("bookmark[%d].url = %q, want %q", index, got, spec.URL)
		}
	}
	if got := stringValue(bookmarks["checksum"]); got == "" {
		t.Fatal("bookmark checksum is empty")
	}

	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		return
	}
	prefs := readTestJSON(t, filepath.Join(profilesDir, "default", "Default", "Preferences"))
	if got := stringValue(pathValue(prefs, "default_search_provider.guid")); got != bingGUID {
		t.Fatalf("default search guid = %q, want %q", got, bingGUID)
	}
	secure := readTestJSON(t, filepath.Join(profilesDir, "default", "Default", "Secure Preferences"))
	if got := stringValue(pathValue(secure, "default_search_provider_data.template_url_data.keyword")); got != "bing.com" {
		t.Fatalf("secure search keyword = %q, want bing.com", got)
	}
	if got := stringValue(pathValue(secure, "protection.macs.default_search_provider_data.template_url_data")); got == "" {
		t.Fatal("secure search mac is empty")
	}
	if got := stringValue(pathValue(secure, "protection.super_mac")); got == "" {
		t.Fatal("secure super_mac is empty")
	}
}

// readTestJSON 读取测试 JSON 文件。
// t 为 Go 测试对象，path 为 JSON 文件路径。
func readTestJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return value
}
