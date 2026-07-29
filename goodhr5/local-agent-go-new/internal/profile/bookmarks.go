// Package profile 负责为 GoodHR 浏览器 Profile 初始化招聘平台默认书签和书签栏显示配置。
package profile

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"time"
	"unicode/utf16"
)

const chromiumDefaultProfileDir = "Default"

var defaultBookmarks = []bookmarkSpec{
	{Name: "goodhr5.58it.cn", URL: "https://goodhr5.58it.cn/"},
	{Name: "BOSS直聘", URL: "https://www.zhipin.com/web/chat/recommend"},
	{Name: "猎聘猎头端", URL: "https://h.liepin.com/"},
	{Name: "猎聘", URL: "https://www.liepin.com/"},
	{Name: "智联招聘", URL: "https://rd6.zhaopin.com/app/recommend"},
}

// bookmarkSpec 描述一个需要固定到书签栏前面的默认入口。
type bookmarkSpec struct {
	Name string
	URL  string
}

// ensureProfileBookmarks 补齐 Profile 默认书签并让书签栏在所有页面显示。
func ensureProfileBookmarks(profileDir string) error {
	defaultDir := filepath.Join(profileDir, chromiumDefaultProfileDir)
	if err := os.MkdirAll(defaultDir, 0o755); err != nil {
		return fmt.Errorf("创建浏览器默认资料目录失败：%w", err)
	}
	if err := ensureBookmarksFile(defaultDir); err != nil {
		return err
	}
	if err := ensureBookmarkBarPreference(defaultDir); err != nil {
		return err
	}
	return nil
}

// ensureBookmarksFile 保留用户原有书签，并把缺少的 GoodHR 默认书签补到书签栏前面。
func ensureBookmarksFile(defaultDir string) error {
	path := filepath.Join(defaultDir, "Bookmarks")
	data := map[string]any{}
	if err := readProfileJSON(path, &data); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("读取浏览器书签失败：%w", err)
	}

	changed := ensureBookmarkRoots(data)
	roots := bookmarkMap(data, "roots")
	bar := bookmarkMap(roots, "bookmark_bar")
	children, ok := bar["children"].([]any)
	if !ok {
		children = []any{}
		changed = true
	}
	now := chromiumTime()
	nextID := maxBookmarkID(data) + 1
	byURL := make(map[string]map[string]any)
	for _, item := range children {
		node, nodeOK := item.(map[string]any)
		if nodeOK && bookmarkString(node["type"]) == "url" {
			byURL[bookmarkString(node["url"])] = node
		}
	}

	ordered := make([]any, 0, len(children)+len(defaultBookmarks))
	used := make(map[string]bool)
	for _, spec := range defaultBookmarks {
		node := byURL[spec.URL]
		if node == nil {
			node = map[string]any{
				"date_added":     now,
				"date_last_used": "0",
				"guid":           randomBookmarkGUID(),
				"id":             strconv.Itoa(nextID),
				"meta_info":      map[string]any{"power_bookmark_meta": ""},
				"type":           "url",
				"url":            spec.URL,
			}
			nextID++
			changed = true
		}
		if bookmarkString(node["name"]) != spec.Name ||
			bookmarkString(node["url"]) != spec.URL ||
			bookmarkString(node["type"]) != "url" {
			changed = true
		}
		node["name"] = spec.Name
		node["url"] = spec.URL
		node["type"] = "url"
		ordered = append(ordered, node)
		used[spec.URL] = true
	}
	for _, item := range children {
		node, nodeOK := item.(map[string]any)
		if nodeOK && used[bookmarkString(node["url"])] {
			continue
		}
		ordered = append(ordered, item)
	}
	if !reflect.DeepEqual(children, ordered) {
		changed = true
	}
	bar["children"] = ordered
	if bookmarkString(data["checksum"]) != bookmarkChecksum(data) {
		changed = true
	}
	if !changed {
		return nil
	}
	bar["date_modified"] = now
	data["checksum"] = bookmarkChecksum(data)
	if err := writeProfileJSON(path, data); err != nil {
		return fmt.Errorf("写入浏览器书签失败：%w", err)
	}
	return nil
}

// ensureBookmarkBarPreference 让 Chromium 在普通网页中也显示地址栏下方的书签栏。
func ensureBookmarkBarPreference(defaultDir string) error {
	path := filepath.Join(defaultDir, "Preferences")
	data := map[string]any{}
	if err := readProfileJSON(path, &data); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("读取浏览器偏好失败：%w", err)
	}
	bar := bookmarkMap(data, "bookmark_bar")
	if visible, ok := bar["show_on_all_tabs"].(bool); ok && visible {
		return nil
	}
	bar["show_on_all_tabs"] = true
	if err := writeProfileJSON(path, data); err != nil {
		return fmt.Errorf("写入浏览器书签栏偏好失败：%w", err)
	}
	return nil
}

// ensureBookmarkRoots 补齐 Chromium 书签文件必须具备的根节点。
func ensureBookmarkRoots(data map[string]any) bool {
	changed := false
	if bookmarkString(data["version"]) != "1" {
		data["version"] = json.Number("1")
		changed = true
	}
	roots := bookmarkMap(data, "roots")
	now := chromiumTime()
	if ensureBookmarkFolder(roots, "bookmark_bar", "1", "书签栏", now) {
		changed = true
	}
	if ensureBookmarkFolder(roots, "other", "2", "其他书签", now) {
		changed = true
	}
	if ensureBookmarkFolder(roots, "synced", "3", "移动设备书签", now) {
		changed = true
	}
	return changed
}

// ensureBookmarkFolder 补齐一个 Chromium 书签根文件夹，并返回是否发生修改。
func ensureBookmarkFolder(roots map[string]any, key string, id string, name string, now string) bool {
	changed := false
	folder, ok := roots[key].(map[string]any)
	if !ok {
		folder = map[string]any{}
		roots[key] = folder
		changed = true
	}
	defaults := map[string]any{
		"id":             id,
		"guid":           randomBookmarkGUID(),
		"type":           "folder",
		"name":           name,
		"date_added":     now,
		"date_last_used": "0",
		"date_modified":  "0",
	}
	for field, value := range defaults {
		if bookmarkString(folder[field]) == "" {
			folder[field] = value
			changed = true
		}
	}
	if _, childrenOK := folder["children"].([]any); !childrenOK {
		folder["children"] = []any{}
		changed = true
	}
	return changed
}

// maxBookmarkID 返回书签文件全部根节点中最大的数字编号。
func maxBookmarkID(data map[string]any) int {
	maxID := 3
	var visit func(map[string]any)
	visit = func(node map[string]any) {
		if id, err := strconv.Atoi(bookmarkString(node["id"])); err == nil && id > maxID {
			maxID = id
		}
		for _, child := range bookmarkArray(node, "children") {
			if object, ok := child.(map[string]any); ok {
				visit(object)
			}
		}
	}
	roots := bookmarkMap(data, "roots")
	for _, key := range []string{"bookmark_bar", "other", "synced"} {
		visit(bookmarkMap(roots, key))
	}
	return maxID
}

// bookmarkChecksum 按 Chromium 规则计算 Bookmarks 文件校验值。
func bookmarkChecksum(data map[string]any) string {
	hash := md5.New()
	roots := bookmarkMap(data, "roots")
	for _, key := range []string{"bookmark_bar", "other", "synced"} {
		writeBookmarkHash(hash, bookmarkMap(roots, key))
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

// writeBookmarkHash 把单个书签节点按 Chromium 字段顺序写入校验计算。
func writeBookmarkHash(hash interface{ Write([]byte) (int, error) }, node map[string]any) {
	_, _ = hash.Write([]byte(bookmarkString(node["id"])))
	_, _ = hash.Write(bookmarkUTF16LE(bookmarkString(node["name"])))
	_, _ = hash.Write([]byte(bookmarkString(node["type"])))
	if bookmarkString(node["type"]) == "url" {
		_, _ = hash.Write([]byte(bookmarkString(node["url"])))
		return
	}
	for _, child := range bookmarkArray(node, "children") {
		if object, ok := child.(map[string]any); ok {
			writeBookmarkHash(hash, object)
		}
	}
}

// bookmarkUTF16LE 把书签名称编码为 Chromium 校验使用的 UTF-16LE。
func bookmarkUTF16LE(value string) []byte {
	encoded := utf16.Encode([]rune(value))
	data := make([]byte, len(encoded)*2)
	for index, item := range encoded {
		binary.LittleEndian.PutUint16(data[index*2:], item)
	}
	return data
}

// chromiumTime 返回 Chromium 使用的 1601 年起始微秒时间戳。
func chromiumTime() string {
	now := time.Now()
	return fmt.Sprintf("%d", (now.Unix()+11644473600)*1000000+int64(now.Nanosecond()/1000))
}

// randomBookmarkGUID 生成 Chromium 书签节点使用的随机 UUID。
func randomBookmarkGUID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		sum := md5.Sum([]byte(fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())))
		data = sum
	}
	data[6] = (data[6] & 0x0f) | 0x40
	data[8] = (data[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", data[0:4], data[4:6], data[6:8], data[8:10], data[10:16])
}

// readProfileJSON 读取 Profile JSON 文件并保留数字的原始文本形式。
func readProfileJSON(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.UseNumber()
	return decoder.Decode(target)
}

// writeProfileJSON 把 Profile 配置写回紧凑 JSON 文件。
func writeProfileJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// bookmarkMap 读取 map 子节点，不存在时创建并挂回父对象。
func bookmarkMap(parent map[string]any, key string) map[string]any {
	if value, ok := parent[key].(map[string]any); ok {
		return value
	}
	value := map[string]any{}
	parent[key] = value
	return value
}

// bookmarkArray 读取数组子节点，不存在时返回空数组。
func bookmarkArray(parent map[string]any, key string) []any {
	if value, ok := parent[key].([]any); ok {
		return value
	}
	return nil
}

// bookmarkString 把书签 JSON 标量安全转换成字符串。
func bookmarkString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}
