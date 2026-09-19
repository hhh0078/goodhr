// Package profile 负责通过 Firefox 书签扩展初始化默认入口并迁移旧 Chromium 书签。
package profile

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"goodhr5/local-agent-go-new/internal/config"
)

const bookmarkExtensionDir = "goodhr-bookmark-extension"

// bookmarkBackground 是只操作浏览器书签的扩展脚本。
//
//go:embed bookmark-extension/background.js
var bookmarkBackground []byte

var defaultBookmarks = []bookmarkNode{
	{Title: "GoodHR"},
	{Title: "BOSS直聘", URL: "https://www.zhipin.com/web/chat/recommend"},
	{Title: "猎聘猎头端", URL: "https://h.liepin.com/"},
	{Title: "猎聘", URL: "https://www.liepin.com/"},
	{Title: "智联招聘", URL: "https://rd6.zhaopin.com/app/recommend"},
}

// bookmarkNode 描述默认或待迁移的书签与文件夹。
type bookmarkNode struct {
	Title    string         `json:"title"`
	URL      string         `json:"url,omitempty"`
	Children []bookmarkNode `json:"children,omitempty"`
}

// bookmarkConfiguration 描述默认入口及旧书签分组。
type bookmarkConfiguration struct {
	Defaults []bookmarkNode `json:"defaults"`
	Toolbar  []bookmarkNode `json:"toolbar"`
	Other    []bookmarkNode `json:"other"`
	Mobile   []bookmarkNode `json:"mobile"`
}

// chromiumBookmarkNode 仅读取旧格式中迁移需要的字段。
type chromiumBookmarkNode struct {
	Name     string                 `json:"name"`
	Type     string                 `json:"type"`
	URL      string                 `json:"url"`
	Children []chromiumBookmarkNode `json:"children"`
}

// ensureProfileBookmarks 准备 Firefox 书签扩展与偏好，不直接写浏览器数据库。
func ensureProfileBookmarks(profileDir string) error {
	defaults := append([]bookmarkNode(nil), defaultBookmarks...)
	consoleURL := strings.TrimSpace(os.Getenv("GOODHR_CONSOLE_URL"))
	if consoleURL == "" {
		consoleURL = config.DefaultConsoleURL
	}
	defaults[0].URL = strings.TrimRight(consoleURL, "/") + "/"
	configuration := bookmarkConfiguration{Defaults: defaults}
	legacy, err := os.ReadFile(filepath.Join(profileDir, "Default", "Bookmarks"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("读取旧书签失败：%w", err)
	}
	if err == nil {
		var data struct {
			Roots struct {
				Toolbar chromiumBookmarkNode `json:"bookmark_bar"`
				Other   chromiumBookmarkNode `json:"other"`
				Mobile  chromiumBookmarkNode `json:"synced"`
			} `json:"roots"`
		}
		if err := json.Unmarshal(legacy, &data); err != nil {
			return fmt.Errorf("读取旧书签格式失败：%w", err)
		}
		configuration.Toolbar = convertChromiumBookmarks(data.Roots.Toolbar.Children)
		configuration.Other = convertChromiumBookmarks(data.Roots.Other.Children)
		configuration.Mobile = convertChromiumBookmarks(data.Roots.Mobile.Children)
	}
	extensionDir := filepath.Join(profileDir, bookmarkExtensionDir)
	if err := os.MkdirAll(extensionDir, 0o700); err != nil {
		return err
	}
	manifest := []byte(`{"manifest_version":2,"name":"GoodHR 书签助手","version":"1.0.0","permissions":["bookmarks"],"background":{"scripts":["background.js"]},"browser_specific_settings":{"gecko":{"id":"bookmarks@goodhr.local"}}}`)
	payload, err := json.Marshal(configuration)
	if err != nil {
		return err
	}
	for name, content := range map[string][]byte{"manifest.json": manifest, "background.js": bookmarkBackground, "bookmarks.json": payload} {
		if err := writeBookmarkResource(filepath.Join(extensionDir, name), content); err != nil {
			return fmt.Errorf("准备书签扩展失败：%w", err)
		}
	}
	preferencePath := filepath.Join(profileDir, "user.js")
	preferences, err := os.ReadFile(preferencePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// 只更新书签栏配置，保留用户的其他浏览器设置。
	pattern := regexp.MustCompile(`(?m)^user_pref\("browser\.toolbars\.bookmarks\.visibility",[^\r\n]*\);\r?\n?`)
	preferences = pattern.ReplaceAll(preferences, nil)
	preferences = append(bytes.TrimRight(preferences, "\r\n"), []byte("\nuser_pref(\"browser.toolbars.bookmarks.visibility\", \"always\");\n")...)
	preferences, err = repairBookmarkToolbar(profileDir, preferences)
	if err != nil {
		return fmt.Errorf("准备书签栏布局失败：%w", err)
	}
	return writeBookmarkResource(preferencePath, preferences)
}

// convertChromiumBookmarks 保留旧书签层级，忽略无法识别的节点。
func convertChromiumBookmarks(nodes []chromiumBookmarkNode) []bookmarkNode {
	result := make([]bookmarkNode, 0, len(nodes))
	for _, node := range nodes {
		switch node.Type {
		case "url":
			if node.URL != "" {
				result = append(result, bookmarkNode{Title: node.Name, URL: node.URL})
			}
		case "folder":
			result = append(result, bookmarkNode{Title: node.Name, Children: convertChromiumBookmarks(node.Children)})
		}
	}
	return result
}

// writeBookmarkResource 仅在内容变化时写入资源，减少重复初始化的文件写入。
func writeBookmarkResource(path string, content []byte) error {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, content) {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.WriteFile(path, content, 0o600)
}

// WithBookmarkExtension 补齐当前 Profile 的书签扩展，保留调用方提供的扩展列表。
func WithBookmarkExtension(profileDir string, extensions []string) []string {
	result := append([]string(nil), extensions...)
	if profileDir == "" {
		return result
	}
	extension := filepath.Join(profileDir, bookmarkExtensionDir)
	if _, err := os.Stat(filepath.Join(extension, "manifest.json")); err != nil {
		return result
	}
	for _, existing := range result {
		if existing == extension {
			return result
		}
	}
	return append(result, extension)
}
