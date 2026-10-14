// 本文件负责公开浏览器插件统一版本和分浏览器下载地址，复用后台系统配置维护与发布。
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

const browserExtensionConfigKey = "system.browser_extension"
const defaultBrowserExtensionConfig = `{"version":"1.0.2","note":"自动查重、独立备注、推荐进展和AI总结都补齐了。Chrome 与 Firefox 共用版本号，当前为开发包。","url_chrome":"/downloads/goodhr-extension-chrome-1.0.2.zip","url_firefox":"/downloads/goodhr-extension-firefox-1.0.2.zip","sha256_chrome":"20cc217154bd1fa17ec0442a1a669dd9cc84625cec0b0dab566e30836a7ffc4c","sha256_firefox":"16156c049370229e01262c30bd4fbc2fb747b8688b2dc11397ad1d048a10e998","firefox_signed":false}`

// BrowserExtensionRelease 定义默认插件版本、可选浏览器独立版本和两个安装包。
type BrowserExtensionRelease struct {
	Version        string `json:"version"`
	VersionChrome  string `json:"version_chrome,omitempty"`
	VersionFirefox string `json:"version_firefox,omitempty"`
	Note           string `json:"note"`
	URLChrome      string `json:"url_chrome"`
	URLFirefox     string `json:"url_firefox"`
	SHA256Chrome   string `json:"sha256_chrome"`
	SHA256Firefox  string `json:"sha256_firefox"`
	FirefoxSigned  bool   `json:"firefox_signed"`
}

// browserExtensionReleaseConfig 仅在配置内部保存旧本地程序的完整兼容快照，公开响应不暴露快照字段。
type browserExtensionReleaseConfig struct {
	BrowserExtensionRelease
	AgentCompatibility *BrowserExtensionRelease `json:"agent_compatibility,omitempty"`
}

// GetBrowserExtension 返回可公开的插件版本，不公开系统配置中的未知私有字段。
func (s *Server) GetBrowserExtension(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var config browserExtensionReleaseConfig
	cfg, err := s.systemConfigs.Get(browserExtensionConfigKey)
	if err == nil && cfg.Enabled {
		if json.Unmarshal([]byte(cfg.ConfigValue), &config) != nil {
			config = browserExtensionReleaseConfig{}
		}
	}
	release := config.BrowserExtensionRelease
	// 已发布的旧本地程序按一个版本严格校验两个浏览器包，过渡期只向其返回版本与包完全一致的旧快照。
	if !r.URL.Query().Has("browser") && r.UserAgent() == "Go-http-client/1.1" && config.AgentCompatibility != nil && validBrowserExtensionVersion(config.AgentCompatibility.Version) != "" {
		release = *config.AgentCompatibility
	}
	release.URLChrome = safeExtensionDownloadURL(release.URLChrome)
	normalizePublicFirefoxRelease(&release)
	release.VersionChrome = validBrowserExtensionVersion(release.VersionChrome)
	release.VersionFirefox = validBrowserExtensionVersion(release.VersionFirefox)
	switch browserExtensionRequestBrowser(r) {
	case "chrome":
		if release.VersionChrome != "" {
			release.Version = release.VersionChrome
		}
	case "firefox":
		if release.FirefoxSigned && release.VersionFirefox != "" {
			release.Version = release.VersionFirefox
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Add("Vary", "User-Agent")
	writeJSON(w, http.StatusOK, struct {
		OK      bool                    `json:"ok"`
		Release BrowserExtensionRelease `json:"release"`
	}{true, release})
}

// normalizePublicFirefoxRelease 仅公开已完成 Mozilla 签名、地址和摘要均有效的 Firefox 安装包。
func normalizePublicFirefoxRelease(release *BrowserExtensionRelease) {
	if release == nil {
		return
	}
	downloadURL := safeExtensionDownloadURL(release.URLFirefox)
	parsed, err := url.Parse(downloadURL)
	if !release.FirefoxSigned || err != nil || parsed.Scheme != "https" || !strings.HasSuffix(parsed.Path, ".xpi") || !isLowercaseSHA256(release.SHA256Firefox) {
		release.URLFirefox = ""
		release.SHA256Firefox = ""
		release.FirefoxSigned = false
		release.VersionFirefox = ""
		return
	}
	release.URLFirefox = downloadURL
}

// browserExtensionRequestBrowser 优先识别明确浏览器参数；旧 Firefox 插件依靠浏览器请求头保持独立发布版本。
func browserExtensionRequestBrowser(r *http.Request) string {
	if r == nil {
		return ""
	}
	value := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("browser")))
	if value == "chrome" || value == "firefox" {
		return value
	}
	if value == "" && strings.Contains(r.UserAgent(), "Firefox/") {
		return "firefox"
	}
	return ""
}

// validBrowserExtensionVersion 仅允许三段数字的独立正式版本，错误或缺失字段继续沿用原统一版本。
func validBrowserExtensionVersion(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 50 {
		return ""
	}
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return ""
	}
	for _, part := range parts {
		if part == "" {
			return ""
		}
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return ""
			}
		}
	}
	return value
}

// isLowercaseSHA256 验证摘要是 64 位小写十六进制，避免错误安装包被公开。
func isLowercaseSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
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
