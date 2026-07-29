// Package profile 验证 GoodHR 默认书签、书签栏显示、用户书签保留和重复初始化幂等性。
package profile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestResolveCreatesDefaultBookmarks 验证首次准备 Profile 时会写入旧版的五个默认书签。
func TestResolveCreatesDefaultBookmarks(t *testing.T) {
	root := t.TempDir()
	profilePath, err := New(root).Resolve("default")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	bookmarks := readBookmarkTestJSON(t, filepath.Join(profilePath, "Default", "Bookmarks"))
	children := bookmarkArray(bookmarkMap(bookmarkMap(bookmarks, "roots"), "bookmark_bar"), "children")
	if len(children) != len(defaultBookmarks) {
		t.Fatalf("bookmark count = %d, want %d", len(children), len(defaultBookmarks))
	}
	for index, spec := range defaultBookmarks {
		node, ok := children[index].(map[string]any)
		if !ok {
			t.Fatalf("bookmark[%d] type = %T", index, children[index])
		}
		if bookmarkString(node["name"]) != spec.Name || bookmarkString(node["url"]) != spec.URL {
			t.Fatalf("bookmark[%d] = %v, want %s %s", index, node, spec.Name, spec.URL)
		}
	}
	if bookmarkString(bookmarks["checksum"]) == "" {
		t.Fatal("bookmark checksum is empty")
	}
	preferences := readBookmarkTestJSON(t, filepath.Join(profilePath, "Default", "Preferences"))
	if visible, ok := bookmarkMap(preferences, "bookmark_bar")["show_on_all_tabs"].(bool); !ok || !visible {
		t.Fatal("bookmark bar should be visible on all tabs")
	}
}

// TestResolvePreservesUserBookmarksAndDoesNotRewrite 验证用户书签会保留，并且配置稳定后不重复写文件。
func TestResolvePreservesUserBookmarksAndDoesNotRewrite(t *testing.T) {
	root := t.TempDir()
	manager := New(root)
	profilePath, err := manager.Resolve("account-1")
	if err != nil {
		t.Fatalf("first Resolve() error = %v", err)
	}
	path := filepath.Join(profilePath, "Default", "Bookmarks")
	bookmarks := readBookmarkTestJSON(t, path)
	bar := bookmarkMap(bookmarkMap(bookmarks, "roots"), "bookmark_bar")
	bar["children"] = []any{map[string]any{
		"date_added": "1",
		"guid":       "11111111-1111-4111-8111-111111111111",
		"id":         "99",
		"name":       "我的书签",
		"type":       "url",
		"url":        "https://example.com/",
	}}
	bookmarks["checksum"] = bookmarkChecksum(bookmarks)
	writeBookmarkTestJSON(t, path, bookmarks)

	if _, err = manager.Resolve("account-1"); err != nil {
		t.Fatalf("second Resolve() error = %v", err)
	}
	afterUpdate, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read updated bookmarks: %v", err)
	}
	updated := readBookmarkTestJSON(t, path)
	updatedChildren := bookmarkArray(bookmarkMap(bookmarkMap(updated, "roots"), "bookmark_bar"), "children")
	if len(updatedChildren) != len(defaultBookmarks)+1 {
		t.Fatalf("bookmark count = %d, want %d", len(updatedChildren), len(defaultBookmarks)+1)
	}
	userBookmark := updatedChildren[len(updatedChildren)-1].(map[string]any)
	if bookmarkString(userBookmark["name"]) != "我的书签" {
		t.Fatalf("user bookmark was not preserved: %v", userBookmark)
	}

	if _, err = manager.Resolve("account-1"); err != nil {
		t.Fatalf("third Resolve() error = %v", err)
	}
	afterRepeat, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read repeated bookmarks: %v", err)
	}
	if string(afterRepeat) != string(afterUpdate) {
		t.Fatal("stable bookmarks should not be rewritten")
	}
}

// readBookmarkTestJSON 读取测试 Profile 中的 JSON 文件。
func readBookmarkTestJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var value map[string]any
	if err = json.Unmarshal(data, &value); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return value
}

// writeBookmarkTestJSON 写入测试 Profile JSON 文件。
func writeBookmarkTestJSON(t *testing.T, path string, value map[string]any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
	if err = os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
