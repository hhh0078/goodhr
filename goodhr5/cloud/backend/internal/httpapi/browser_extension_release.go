// 本文件负责公开浏览器插件统一版本和分浏览器下载地址，复用后台系统配置维护与发布。
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

const browserExtensionConfigKey = "system.browser_extension"
const defaultBrowserExtensionConfig = `{"version":"1.0.1","note":"补齐可见头像保存与完整简历入口；Chrome 与 Firefox 共用版本号。当前为开发安装包。","url_chrome":"/downloads/goodhr-extension-chrome-1.0.1.zip","url_firefox":"/downloads/goodhr-extension-firefox-1.0.1.zip","sha256_chrome":"855118f185cfa5e8aa3f45a1de4e663fb50deef496da31134709ebe6684fefb8","sha256_firefox":"a1d1eb60f68264c029f5821884e0d2ec36b0d2221a824f55c5fc148bc9d497f5","firefox_signed":false}`

// BrowserExtensionRelease 定义唯一插件版本和两个浏览器安装包。
type BrowserExtensionRelease struct {
	Version       string `json:"version"`
	Note          string `json:"note"`
	URLChrome     string `json:"url_chrome"`
	URLFirefox    string `json:"url_firefox"`
	SHA256Chrome  string `json:"sha256_chrome"`
	SHA256Firefox string `json:"sha256_firefox"`
	FirefoxSigned bool   `json:"firefox_signed"`
}

// GetBrowserExtension 返回可公开的插件版本，不公开系统配置中的未知私有字段。
func (s *Server) GetBrowserExtension(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var release BrowserExtensionRelease
	cfg, err := s.systemConfigs.Get(browserExtensionConfigKey)
	if err == nil && cfg.Enabled {
		if json.Unmarshal([]byte(cfg.ConfigValue), &release) != nil {
			release = BrowserExtensionRelease{}
		}
	}
	release.URLChrome = safeExtensionDownloadURL(release.URLChrome)
	release.URLFirefox = safeExtensionDownloadURL(release.URLFirefox)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, struct {
		OK      bool                    `json:"ok"`
		Release BrowserExtensionRelease `json:"release"`
	}{true, release})
}

// safeExtensionDownloadURL 仅允许本站绝对路径或 HTTPS 下载地址，拒绝脚本和协议相对地址。
func safeExtensionDownloadURL(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//") && !strings.ContainsAny(value, "\\\r\n") {
		return value
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return ""
	}
	return value
}
