// Package browserprofile 负责初始化本机 Chromium Profile 的默认书签和搜索引擎配置。
package browserprofile

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"

	_ "modernc.org/sqlite"
)

const (
	defaultProfileName = "default"
	chromiumProfileDir = "Default"
	bingGUID           = "485bf7d3-0215-45af-87dc-538868000003"
	prefHashSeed       = "61eff07de4f37ac1c6969c91034a447ef6cd394d"
)

var recruitBookmarks = []bookmarkSpec{
	{Name: "goodhr5.58it.cn", URL: "https://goodhr5.58it.cn/"},
	{Name: "BOSS直聘", URL: "https://www.zhipin.com/"},
	{Name: "猎聘猎头端", URL: "https://h.liepin.com/account/login"},
	{Name: "猎聘", URL: "https://www.liepin.com/"},
	{Name: "智联招聘", URL: "https://www.zhaopin.com/"},
}

// bookmarkSpec 描述需要固定到书签栏的招聘平台入口。
type bookmarkSpec struct {
	Name string
	URL  string
}

// EnsureDefaultsAsync 异步检查并修复浏览器 Profile 默认配置。
// profilesDir 为本地浏览器账号目录，函数立即返回，失败只写入本地日志。
func EnsureDefaultsAsync(profilesDir string) {
	go func() {
		if err := EnsureDefaults(profilesDir); err != nil {
			log.Printf("初始化浏览器默认配置失败：%v", err)
		}
	}()
}

// EnsureDefaults 检查并修复所有已知浏览器 Profile 默认配置。
// profilesDir 为本地浏览器账号目录，默认目录不存在时会创建基础 Profile。
func EnsureDefaults(profilesDir string) error {
	profilesDir = strings.TrimSpace(profilesDir)
	if profilesDir == "" {
		return nil
	}
	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		return fmt.Errorf("创建浏览器账号目录失败：%w", err)
	}
	for _, profileDir := range profileDirs(profilesDir) {
		if err := ensureProfileDefaults(profileDir); err != nil {
			log.Printf("初始化浏览器账号配置失败：profile=%s err=%v", filepath.Base(profileDir), err)
		}
	}
	return nil
}

// profileDirs 返回需要检查的 Profile 根目录列表。
// profilesDir 为本地浏览器账号目录，默认账号始终排在第一位。
func profileDirs(profilesDir string) []string {
	seen := map[string]bool{}
	defaultDir := filepath.Join(profilesDir, defaultProfileName)
	result := []string{defaultDir}
	seen[defaultDir] = true

	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		return result
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(profilesDir, entry.Name())
		if !seen[dir] {
			result = append(result, dir)
			seen[dir] = true
		}
	}
	return result
}

// ensureProfileDefaults 修复单个 Profile 的书签和搜索引擎默认值。
// profileDir 为 Chromium user data dir，例如 profiles/default。
func ensureProfileDefaults(profileDir string) error {
	defaultDir := filepath.Join(profileDir, chromiumProfileDir)
	if err := os.MkdirAll(defaultDir, 0o755); err != nil {
		return fmt.Errorf("创建浏览器默认资料目录失败：%w", err)
	}
	if err := ensureBookmarks(defaultDir); err != nil {
		return err
	}
	if err := ensureBingSearch(defaultDir); err != nil {
		return err
	}
	return nil
}

// ensureBookmarks 确保招聘平台书签存在并按固定顺序排在书签栏前面。
// defaultDir 为 Chromium 的 Default 资料目录。
func ensureBookmarks(defaultDir string) error {
	path := filepath.Join(defaultDir, "Bookmarks")
	data := map[string]any{}
	if err := readJSONFile(path, &data); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("读取浏览器书签失败：%w", err)
	}
	ensureBookmarkRoots(data)

	roots := mapValue(data, "roots")
	bar := mapValue(roots, "bookmark_bar")
	children := arrayValue(bar, "children")
	now := chromeTime()
	nextID := maxBookmarkID(data) + 1
	byURL := map[string]map[string]any{}
	for _, item := range children {
		if node, ok := item.(map[string]any); ok && stringValue(node["type"]) == "url" {
			byURL[stringValue(node["url"])] = node
		}
	}

	ordered := make([]any, 0, len(children)+len(recruitBookmarks))
	used := map[string]bool{}
	for _, spec := range recruitBookmarks {
		node := byURL[spec.URL]
		if node == nil {
			node = map[string]any{
				"date_added":     now,
				"date_last_used": "0",
				"guid":           randomGUID(),
				"id":             fmt.Sprintf("%d", nextID),
				"meta_info":      map[string]any{"power_bookmark_meta": ""},
				"type":           "url",
				"url":            spec.URL,
			}
			nextID++
		}
		node["name"] = spec.Name
		node["url"] = spec.URL
		node["type"] = "url"
		ordered = append(ordered, node)
		used[spec.URL] = true
	}
	for _, item := range children {
		node, ok := item.(map[string]any)
		if ok && used[stringValue(node["url"])] {
			continue
		}
		ordered = append(ordered, item)
	}
	bar["children"] = ordered
	bar["date_modified"] = now
	data["checksum"] = bookmarkChecksum(data)
	return writeJSONFile(path, data)
}

// ensureBookmarkRoots 补齐 Chromium 书签文件的基础根节点。
// data 为 Bookmarks JSON 根对象。
func ensureBookmarkRoots(data map[string]any) {
	data["version"] = json.Number("1")
	roots := mapValue(data, "roots")
	now := chromeTime()
	ensureBookmarkFolder(roots, "bookmark_bar", "1", "书签栏", now)
	ensureBookmarkFolder(roots, "other", "2", "其他书签", now)
	ensureBookmarkFolder(roots, "synced", "3", "移动设备书签", now)
}

// ensureBookmarkFolder 补齐一个 Chromium 书签根文件夹。
// roots 为 roots 节点，key 为根文件夹字段名。
func ensureBookmarkFolder(roots map[string]any, key string, id string, name string, now string) {
	folder := mapValue(roots, key)
	if stringValue(folder["id"]) == "" {
		folder["id"] = id
	}
	if stringValue(folder["guid"]) == "" {
		folder["guid"] = randomGUID()
	}
	if stringValue(folder["type"]) == "" {
		folder["type"] = "folder"
	}
	if stringValue(folder["name"]) == "" {
		folder["name"] = name
	}
	if stringValue(folder["date_added"]) == "" {
		folder["date_added"] = now
	}
	if _, ok := folder["children"].([]any); !ok {
		folder["children"] = []any{}
	}
}

// ensureBingSearch 确保默认搜索引擎配置为必应。
// defaultDir 为 Chromium 的 Default 资料目录。
func ensureBingSearch(defaultDir string) error {
	deviceID, err := machineDeviceID()
	if err != nil {
		log.Printf("跳过默认搜索引擎初始化：读取设备 ID 失败：%v", err)
		return nil
	}
	if err := ensureBingPreferences(defaultDir, deviceID); err != nil {
		return err
	}
	if err := ensureBingKeyword(defaultDir); err != nil {
		log.Printf("更新浏览器搜索引擎数据库失败，先不影响启动：%v", err)
	}
	return nil
}

// ensureBingPreferences 写入必应搜索引擎偏好和保护校验。
// defaultDir 为 Chromium 的 Default 资料目录，deviceID 为当前设备 ID。
func ensureBingPreferences(defaultDir string, deviceID string) error {
	prefsPath := filepath.Join(defaultDir, "Preferences")
	prefs := map[string]any{}
	if err := readJSONFile(prefsPath, &prefs); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("读取浏览器 Preferences 失败：%w", err)
	}
	defaultSearch := mapValue(prefs, "default_search_provider")
	defaultSearch["guid"] = bingGUID
	defaultSearch["reset_occurred"] = false
	mapValue(prefs, "default_search_provider_data")["mirrored_template_url_data"] = bingTemplateData()
	if err := writeJSONFile(prefsPath, prefs); err != nil {
		return fmt.Errorf("写入浏览器 Preferences 失败：%w", err)
	}

	securePath := filepath.Join(defaultDir, "Secure Preferences")
	secure := map[string]any{}
	if err := readJSONFile(securePath, &secure); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("读取浏览器 Secure Preferences 失败：%w", err)
	}
	mapValue(secure, "default_search_provider_data")["template_url_data"] = bingTemplateData()
	restampProtection(secure, deviceID)
	if err := writeJSONFile(securePath, secure); err != nil {
		return fmt.Errorf("写入浏览器 Secure Preferences 失败：%w", err)
	}
	return nil
}

// ensureBingKeyword 确保 Web Data keywords 表里存在必应记录。
// defaultDir 为 Chromium 的 Default 资料目录，数据库不存在时直接跳过。
func ensureBingKeyword(defaultDir string) error {
	webDataPath := filepath.Join(defaultDir, "Web Data")
	if _, err := os.Stat(webDataPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	db, err := sql.Open("sqlite", webDataPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(`PRAGMA busy_timeout = 1000`); err != nil {
		return err
	}
	var exists int
	if err := db.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name='keywords'`).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	_, err = db.Exec(`
		INSERT OR REPLACE INTO keywords (
			id, short_name, keyword, favicon_url, url, safe_for_autoreplace,
			originating_url, date_created, usage_count, input_encodings,
			suggest_url, prepopulate_id, created_by_policy, last_modified,
			sync_guid, alternate_urls, image_url, search_url_post_params,
			suggest_url_post_params, image_url_post_params, new_tab_url,
			last_visited, created_from_play_api, is_active, starter_pack_id,
			enforced_by_policy, featured_by_policy
		) VALUES (
			3, 'Microsoft Bing', 'bing.com', 'https://www.bing.com/sa/simg/bing_p_rr_teal_min.ico',
			'https://www.bing.com/search?q={searchTerms}', 1,
			'', 0, 0, 'UTF-8',
			'https://www.bing.com/osjson.aspx?query={searchTerms}&language={language}', 3, 0, 0,
			?, '[]', 'https://www.bing.com/images/detail/search?iss=sbiupload&FORM=CHROMI#enterInsights',
			'', '', 'imageBin={google:imageThumbnailBase64}', 'https://www.bing.com/chrome/newtab',
			0, 0, 0, 0, 0, 0
		)
	`, bingGUID)
	return err
}

// restampProtection 重算 Secure Preferences 里的普通保护校验。
// secure 为 Secure Preferences JSON，deviceID 为当前设备 ID。
func restampProtection(secure map[string]any, deviceID string) {
	protection := mapValue(secure, "protection")
	macs := mapValue(protection, "macs")
	mapValue(macs, "default_search_provider_data")["template_url_data"] = ""
	restampMACMap(secure, macs, "", deviceID)
	protection["super_mac"] = prefHMAC("", macs, deviceID)
}

// restampMACMap 递归重算 protection.macs 下的普通 HMAC，并删除加密哈希。
// root 为 Secure Preferences 根对象，macs 为当前 protection.macs 子树。
func restampMACMap(root map[string]any, macs map[string]any, prefix string, deviceID string) {
	for key, value := range macs {
		if strings.HasSuffix(key, "_encrypted_hash") {
			delete(macs, key)
			continue
		}
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		if child, ok := value.(map[string]any); ok {
			restampMACMap(root, child, path, deviceID)
			continue
		}
		macs[key] = prefHMAC(path, pathValue(root, path), deviceID)
	}
}

// prefHMAC 计算 Chromium Secure Preferences 使用的普通 HMAC。
// path 为受保护配置路径，value 为该路径对应值，deviceID 为当前设备 ID。
func prefHMAC(path string, value any, deviceID string) string {
	mac := hmac.New(sha256.New, []byte(prefHashSeed))
	mac.Write([]byte(deviceID))
	mac.Write([]byte(path))
	mac.Write([]byte(valueAsString(value)))
	return strings.ToUpper(hex.EncodeToString(mac.Sum(nil)))
}

// valueAsString 将偏好值转成 Chromium 参与 HMAC 的 JSON 字符串。
// value 为原始 JSON 值，空对象和空数组会被剔除。
func valueAsString(value any) string {
	if value == nil {
		return ""
	}
	if object, ok := value.(map[string]any); ok {
		cleaned := removeEmpty(object)
		if cleaned == nil {
			cleaned = map[string]any{}
		}
		value = cleaned
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

// removeEmpty 递归移除空对象和空数组，匹配 Chromium 的 HMAC 计算逻辑。
// value 为原始 JSON 值，返回清理后的值。
func removeEmpty(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		cleaned := map[string]any{}
		for key, child := range typed {
			child = removeEmpty(child)
			if child != nil {
				cleaned[key] = child
			}
		}
		if len(cleaned) == 0 {
			return nil
		}
		return cleaned
	case []any:
		cleaned := make([]any, 0, len(typed))
		for _, child := range typed {
			child = removeEmpty(child)
			if child != nil {
				cleaned = append(cleaned, child)
			}
		}
		if len(cleaned) == 0 {
			return nil
		}
		return cleaned
	default:
		return value
	}
}

// bingTemplateData 返回 Chromium 搜索引擎模板里的必应配置。
// 返回值用于 Preferences 和 Secure Preferences。
func bingTemplateData() map[string]any {
	return map[string]any{
		"alternate_urls":              []any{},
		"contextual_search_url":       "",
		"created_from_play_api":       false,
		"date_created":                "0",
		"doodle_url":                  "",
		"enforced_by_policy":          false,
		"favicon_url":                 "https://www.bing.com/sa/simg/bing_p_rr_teal_min.ico",
		"featured_by_policy":          false,
		"id":                          "3",
		"image_search_branding_label": "",
		"image_translate_source_language_param_key": "",
		"image_translate_target_language_param_key": "",
		"image_translate_url":                       "",
		"image_url":                                 "https://www.bing.com/images/detail/search?iss=sbiupload&FORM=CHROMI#enterInsights",
		"image_url_post_params":                     "imageBin={google:imageThumbnailBase64}",
		"input_encodings":                           []any{"UTF-8"},
		"is_active":                                 json.Number("0"),
		"keyword":                                   "bing.com",
		"last_modified":                             "0",
		"last_visited":                              "0",
		"logo_url":                                  "https://cdn.sapphire.microsoftapp.net/icons/bing_144.png",
		"new_tab_url":                               "https://www.bing.com/chrome/newtab",
		"originating_url":                           "",
		"policy_origin":                             json.Number("0"),
		"preconnect_to_search_url":                  false,
		"prefetch_likely_navigations":               false,
		"prepopulate_id":                            json.Number("3"),
		"safe_for_autoreplace":                      true,
		"search_intent_params":                      []any{},
		"search_url_post_params":                    "",
		"short_name":                                "Microsoft Bing",
		"starter_pack_id":                           json.Number("0"),
		"suggestions_url":                           "https://www.bing.com/osjson.aspx?query={searchTerms}&language={language}",
		"suggestions_url_post_params":               "",
		"synced_guid":                               bingGUID,
		"url":                                       "https://www.bing.com/search?q={searchTerms}",
	}
}

// machineDeviceID 读取 Chromium 计算保护校验时使用的设备 ID。
// macOS 使用 IOPlatformUUID，Windows 使用计算机名对应的 SID。
func machineDeviceID() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return darwinDeviceID()
	case "windows":
		return windowsDeviceID()
	default:
		return "", fmt.Errorf("当前系统暂不支持：%s", runtime.GOOS)
	}
}

// darwinDeviceID 读取 macOS 的 IOPlatformUUID。
// 返回值用于 Chromium Secure Preferences HMAC。
func darwinDeviceID() (string, error) {
	output, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(output), "\n") {
		if !strings.Contains(line, "IOPlatformUUID") {
			continue
		}
		parts := strings.Split(line, "\"")
		if len(parts) >= 4 {
			return strings.TrimSpace(parts[3]), nil
		}
	}
	return "", errors.New("没有找到 IOPlatformUUID")
}

// windowsDeviceID 读取 Windows 计算机名对应的 SID。
// 返回值用于 Chromium Secure Preferences HMAC。
func windowsDeviceID() (string, error) {
	commands := [][]string{
		{"powershell.exe", "-NoProfile", "-Command", `[System.Security.Principal.NTAccount]::new($env:COMPUTERNAME).Translate([System.Security.Principal.SecurityIdentifier]).Value`},
		{"powershell.exe", "-NoProfile", "-Command", `[System.Security.Principal.NTAccount]::new($env:COMPUTERNAME + '$').Translate([System.Security.Principal.SecurityIdentifier]).Value`},
		{"powershell", "-NoProfile", "-Command", `[System.Security.Principal.NTAccount]::new($env:COMPUTERNAME).Translate([System.Security.Principal.SecurityIdentifier]).Value`},
	}
	for _, args := range commands {
		output, err := exec.Command(args[0], args[1:]...).Output()
		if err == nil {
			value := strings.TrimSpace(string(output))
			if strings.HasPrefix(value, "S-") {
				return value, nil
			}
		}
	}
	return "", errors.New("没有读取到 Windows 计算机 SID")
}

// readJSONFile 读取 JSON 文件到目标对象。
// path 为 JSON 文件路径，target 为解码目标。
func readJSONFile(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.UseNumber()
	return decoder.Decode(target)
}

// writeJSONFile 将对象写入 JSON 文件。
// path 为目标文件路径，value 为需要写入的 JSON 对象。
func writeJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// mapValue 读取或创建 map 子节点。
// parent 为父对象，key 为子节点字段名。
func mapValue(parent map[string]any, key string) map[string]any {
	if value, ok := parent[key].(map[string]any); ok {
		return value
	}
	value := map[string]any{}
	parent[key] = value
	return value
}

// arrayValue 读取或创建数组子节点。
// parent 为父对象，key 为子节点字段名。
func arrayValue(parent map[string]any, key string) []any {
	if value, ok := parent[key].([]any); ok {
		return value
	}
	value := []any{}
	parent[key] = value
	return value
}

// stringValue 将任意 JSON 值转成字符串。
// value 为原始 JSON 值。
func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}

// pathValue 按点分路径读取 JSON 值。
// root 为根对象，path 为点分路径。
func pathValue(root map[string]any, path string) any {
	var current any = root
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[part]
	}
	return current
}

// maxBookmarkID 返回书签文件里最大的数字 ID。
// data 为 Bookmarks JSON 根对象。
func maxBookmarkID(data map[string]any) int {
	maxID := 0
	var walk func(any)
	walk = func(value any) {
		object, ok := value.(map[string]any)
		if !ok {
			return
		}
		if id := stringValue(object["id"]); id != "" {
			var parsed int
			if _, err := fmt.Sscanf(id, "%d", &parsed); err == nil && parsed > maxID {
				maxID = parsed
			}
		}
		for _, child := range existingArray(object, "children") {
			walk(child)
		}
	}
	for _, root := range []string{"bookmark_bar", "other", "synced"} {
		walk(pathValue(data, "roots."+root))
	}
	return maxID
}

// bookmarkChecksum 计算 Chromium 书签文件 checksum。
// data 为 Bookmarks JSON 根对象。
func bookmarkChecksum(data map[string]any) string {
	hash := md5.New()
	roots := mapValue(data, "roots")
	for _, key := range []string{"bookmark_bar", "other", "synced"} {
		writeBookmarkHash(hash, mapValue(roots, key))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// writeBookmarkHash 写入单个书签节点参与 checksum 的字段。
// hash 为 md5 对象，node 为书签节点。
func writeBookmarkHash(hash interface{ Write([]byte) (int, error) }, node map[string]any) {
	hash.Write([]byte(stringValue(node["id"])))
	hash.Write(utf16LE(stringValue(node["name"])))
	hash.Write([]byte(stringValue(node["type"])))
	if stringValue(node["type"]) == "url" {
		hash.Write([]byte(stringValue(node["url"])))
		return
	}
	for _, child := range existingArray(node, "children") {
		if object, ok := child.(map[string]any); ok {
			writeBookmarkHash(hash, object)
		}
	}
}

// existingArray 只读取数组子节点，不存在时不修改原对象。
// parent 为父对象，key 为数组字段名。
func existingArray(parent map[string]any, key string) []any {
	if value, ok := parent[key].([]any); ok {
		return value
	}
	return nil
}

// utf16LE 将字符串编码为 UTF-16LE 字节。
// value 为原始字符串。
func utf16LE(value string) []byte {
	encoded := utf16.Encode([]rune(value))
	data := make([]byte, len(encoded)*2)
	for index, item := range encoded {
		binary.LittleEndian.PutUint16(data[index*2:], item)
	}
	return data
}

// chromeTime 返回 Chromium 使用的 1601 起始微秒时间戳。
// 返回值用于书签创建和修改时间。
func chromeTime() string {
	return fmt.Sprintf("%d", (time.Now().Unix()+11644473600)*1000000+int64(time.Now().Nanosecond()/1000))
}

// randomGUID 生成一个满足书签文件使用的随机 GUID。
// 返回值格式为 xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx。
func randomGUID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		sum := md5.Sum([]byte(fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())))
		data = sum
	}
	data[6] = (data[6] & 0x0f) | 0x40
	data[8] = (data[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", data[0:4], data[4:6], data[6:8], data[8:10], data[10:16])
}
