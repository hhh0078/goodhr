// 文件作用：读取浏览器插件发布配置，并安全下载校验插件压缩包。
package browserextension

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type releaseResponse struct {
	OK      bool    `json:"ok"`
	Release release `json:"release"`
}

type release struct {
	Version      string `json:"version"`
	URLChrome    string `json:"url_chrome"`
	SHA256Chrome string `json:"sha256_chrome"`
}

// fetchRelease 从云端公开接口读取并校验 Chrome 插件发布信息。
func fetchRelease(ctx context.Context, client *http.Client, cloudBaseURL string) (release, string, error) {
	base, err := parseCloudBaseURL(cloudBaseURL)
	if err != nil {
		return release{}, "", err
	}
	endpoint := strings.TrimRight(base.String(), "/") + releasePath
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return release{}, "", fmt.Errorf("创建插件配置请求失败：%w", err)
	}
	response, err := doWithRedirectPolicy(client, request, true)
	if err != nil {
		return release{}, "", fmt.Errorf("读取插件配置失败：%w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return release{}, "", fmt.Errorf("读取插件配置失败，状态码：%d", response.StatusCode)
	}
	if response.ContentLength > maxReleaseBytes {
		return release{}, "", fmt.Errorf("插件配置响应超过大小限制")
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, maxReleaseBytes+1))
	if err != nil {
		return release{}, "", fmt.Errorf("读取插件配置失败：%w", err)
	}
	if int64(len(content)) > maxReleaseBytes {
		return release{}, "", fmt.Errorf("插件配置响应超过大小限制")
	}
	var payload releaseResponse
	if err = json.Unmarshal(content, &payload); err != nil {
		return release{}, "", fmt.Errorf("解析插件配置失败：%w", err)
	}
	payload.Release.Version = strings.TrimSpace(payload.Release.Version)
	payload.Release.URLChrome = strings.TrimSpace(payload.Release.URLChrome)
	payload.Release.SHA256Chrome = strings.ToLower(strings.TrimSpace(payload.Release.SHA256Chrome))
	if !payload.OK || payload.Release.Version == "" || payload.Release.URLChrome == "" {
		return release{}, "", fmt.Errorf("云端插件配置不完整")
	}
	if err = validateSHA256(payload.Release.SHA256Chrome); err != nil {
		return release{}, "", err
	}
	downloadURL, err := resolveDownloadURL(base, payload.Release.URLChrome)
	if err != nil {
		return release{}, "", err
	}
	return payload.Release, downloadURL, nil
}

// parseCloudBaseURL 校验云端基础地址并去掉查询和片段。
func parseCloudBaseURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return nil, fmt.Errorf("云端地址不正确")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed, nil
}

// resolveDownloadURL 只接受 HTTPS 绝对地址或同站根相对地址。
func resolveDownloadURL(base *url.URL, value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//") && !strings.ContainsAny(value, "\\\r\n") {
		reference, err := url.Parse(value)
		if err != nil {
			return "", fmt.Errorf("插件下载地址不正确")
		}
		resolved := base.ResolveReference(reference)
		if !strings.EqualFold(resolved.Host, base.Host) {
			return "", fmt.Errorf("插件相对下载地址不是同站地址")
		}
		return resolved.String(), nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return "", fmt.Errorf("插件下载地址必须是 HTTPS 或同站相对地址")
	}
	return parsed.String(), nil
}

// validateSHA256 校验云端必须提供完整十六进制 SHA256。
func validateSHA256(value string) error {
	if len(value) != sha256.Size*2 {
		return fmt.Errorf("云端插件配置缺少完整 SHA256")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("云端插件 SHA256 格式不正确")
	}
	return nil
}

// downloadArchive 限量下载插件压缩包，并在落盘前完成 SHA256 校验。
func downloadArchive(ctx context.Context, client *http.Client, downloadURL string, expectedSHA256 string, parent string, maxBytes int64) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("创建插件下载请求失败：%w", err)
	}
	response, err := doWithRedirectPolicy(client, request, false)
	if err != nil {
		return "", fmt.Errorf("下载浏览器插件失败：%w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("下载浏览器插件失败，状态码：%d", response.StatusCode)
	}
	if err = validateDownloadRedirect(downloadURL, response.Request.URL); err != nil {
		return "", err
	}
	if response.ContentLength > maxBytes {
		return "", fmt.Errorf("浏览器插件压缩包超过大小限制")
	}
	file, err := os.CreateTemp(parent, ".chrome-download-*.zip")
	if err != nil {
		return "", fmt.Errorf("创建插件下载文件失败：%w", err)
	}
	archivePath := file.Name()
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, maxBytes+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || written > maxBytes {
		_ = os.Remove(archivePath)
		if written > maxBytes {
			return "", fmt.Errorf("浏览器插件压缩包超过大小限制")
		}
		if copyErr != nil {
			return "", fmt.Errorf("保存浏览器插件失败：%w", copyErr)
		}
		return "", fmt.Errorf("关闭插件下载文件失败：%w", closeErr)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != strings.ToLower(strings.TrimSpace(expectedSHA256)) {
		_ = os.Remove(archivePath)
		return "", fmt.Errorf("浏览器插件 SHA256 不一致")
	}
	return archivePath, nil
}

// validateDownloadRedirect 防止 HTTPS 下载被重定向降级，开发环境相对 HTTP 地址也只能留在同站。
func validateDownloadRedirect(requested string, final *url.URL) error {
	original, err := url.Parse(requested)
	if err != nil || final == nil {
		return fmt.Errorf("插件下载重定向地址不正确")
	}
	if original.Scheme == "https" && final.Scheme != "https" {
		return fmt.Errorf("插件下载不允许从 HTTPS 降级")
	}
	if original.Scheme == "http" && (final.Scheme != "http" || !strings.EqualFold(original.Host, final.Host)) {
		return fmt.Errorf("开发环境插件下载不允许跳到站外")
	}
	return nil
}

// doWithRedirectPolicy 为单次请求复制客户端，并检查每一跳重定向，避免 HTTPS 中途降级。
func doWithRedirectPolicy(client *http.Client, request *http.Request, sameHost bool) (*http.Response, error) {
	redirectClient := *client
	previousCheck := client.CheckRedirect
	initialHost := request.URL.Host
	redirectClient.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) == 0 {
			return fmt.Errorf("插件请求的重定向来源为空")
		}
		if len(via) >= 10 {
			return fmt.Errorf("插件请求重定向次数过多")
		}
		previous := via[len(via)-1].URL
		if err := validateRedirectHop(previous, next.URL, initialHost, sameHost); err != nil {
			return err
		}
		if previousCheck != nil {
			return previousCheck(next, via)
		}
		return nil
	}
	return redirectClient.Do(request)
}

// validateRedirectHop 禁止 HTTPS 降级；开发 HTTP 只能留在同站，配置接口始终不能跳站。
func validateRedirectHop(previous *url.URL, next *url.URL, initialHost string, sameHost bool) error {
	if previous == nil || next == nil || next.User != nil {
		return fmt.Errorf("插件请求重定向地址不正确")
	}
	if previous.Scheme == "https" && next.Scheme != "https" {
		return fmt.Errorf("插件请求不允许从 HTTPS 降级")
	}
	if previous.Scheme == "http" && (next.Scheme != "http" || !strings.EqualFold(next.Host, initialHost)) {
		return fmt.Errorf("开发环境插件请求不允许跳到站外")
	}
	if sameHost && !strings.EqualFold(next.Host, initialHost) {
		return fmt.Errorf("插件配置接口不允许跳到站外")
	}
	return nil
}
