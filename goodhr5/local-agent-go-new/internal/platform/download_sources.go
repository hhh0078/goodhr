// Package platform 文件作用：集中维护招聘下载来源域名，按真实主机名识别平台，Worker 不包含业务来源规则。
package platform

import (
	"embed"
	"encoding/json"
	"net/url"
	"strings"
)

//go:embed boss/download_sources.json liepin/download_sources.json zhaopin/download_sources.json wuyou/download_sources.json
var downloadSourceFiles embed.FS

// downloadAliases 从各平台目录加载随程序发布的来源别名，页面平台配置不来自云端。
func downloadAliases() []struct {
	Platform string   `json:"platform"`
	Domains  []string `json:"domains"`
} {
	result := []struct {
		Platform string   `json:"platform"`
		Domains  []string `json:"domains"`
	}{}
	for _, name := range []string{"boss", "liepin", "zhaopin", "wuyou"} {
		data, err := downloadSourceFiles.ReadFile(name + "/download_sources.json")
		if err != nil {
			continue
		}
		var aliases struct {
			Platform string   `json:"platform"`
			Domains  []string `json:"domains"`
		}
		if json.Unmarshal(data, &aliases) == nil {
			result = append(result, aliases)
		}
	}
	return result
}

// DownloadSource 只返回明确来自招聘平台的下载来源，外部 CDN 必须有可信网页来源。
func DownloadSource(pageURL, downloadURL string) (string, string) {
	for _, value := range []string{pageURL, downloadURL} {
		parsed, err := url.Parse(strings.TrimSpace(value))
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil {
			continue
		}
		host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
		for _, source := range downloadAliases() {
			for _, domain := range source.Domains {
				if host == domain || strings.HasSuffix(host, "."+domain) {
					parsed.RawQuery, parsed.Fragment, parsed.ForceQuery = "", "", false
					return source.Platform, parsed.String()
				}
			}
		}
	}
	return "", ""
}
