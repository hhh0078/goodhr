// 文件作用：读取固定同源临时 Firefox 发布清单，复用安全安装流程，并验证离线可继续使用的代码缓存。
package browserextension

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const temporaryReleasePath = "/zip/goodhr-resume-firefox-latest.json"
const temporaryChannel = "temporary"

// temporaryRelease 只接收官方临时通道的版本、同源包地址、摘要和固定扩展身份。
type temporaryRelease struct {
	Version     string `json:"version"`
	URL         string `json:"url"`
	SHA256      string `json:"sha256"`
	ExtensionID string `json:"extension_id"`
	Channel     string `json:"channel"`
}

// PrepareFirefoxTemporary 准备经过完整校验的临时插件；失败时只复用同通道且目录摘要仍相符的缓存。
func PrepareFirefoxTemporary(ctx context.Context, options Options) (Result, error) {
	if strings.TrimSpace(options.DestinationDir) == "" {
		return Result{}, fmt.Errorf("Firefox 插件稳定目录不能为空")
	}
	return prepareForBrowser(ctx, options, browserFirefoxTemporary)
}

// fetchTemporaryFirefoxRelease 限量读取固定同源 JSON，不借用正式签名版或旧 Agent 兼容发布信息。
func fetchTemporaryFirefoxRelease(ctx context.Context, client *http.Client, cloudBaseURL string) (release, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	base, err := parseCloudBaseURL(cloudBaseURL)
	if err != nil {
		return release{}, "", err
	}
	endpoint := base.ResolveReference(&url.URL{Path: temporaryReleasePath})
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return release{}, "", err
	}
	request.Header.Set("Cache-Control", "no-cache")
	strictClient, err := sameOriginDownloadClient(client, cloudBaseURL)
	if err != nil {
		return release{}, "", err
	}
	response, err := strictClient.Do(request)
	if err != nil {
		return release{}, "", fmt.Errorf("临时插件版本暂时没查到：%w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return release{}, "", fmt.Errorf("临时插件版本暂时没查到（%d）", response.StatusCode)
	}
	if response.ContentLength > maxReleaseBytes {
		return release{}, "", fmt.Errorf("临时插件清单超过大小限制")
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, maxReleaseBytes+1))
	if err != nil {
		return release{}, "", err
	}
	if int64(len(content)) > maxReleaseBytes {
		return release{}, "", fmt.Errorf("临时插件清单超过大小限制")
	}
	var published temporaryRelease
	if err = json.Unmarshal(content, &published); err != nil {
		return release{}, "", fmt.Errorf("临时插件清单格式不正确")
	}
	if !validTemporaryVersion(published.Version) || published.ExtensionID != expectedFirefoxID || published.Channel != temporaryChannel || published.SHA256 != strings.ToLower(published.SHA256) || validateSHA256(published.SHA256) != nil {
		return release{}, "", fmt.Errorf("临时插件版本、通道、身份或摘要不正确")
	}
	downloadURL, err := url.Parse(published.URL)
	if err != nil || published.URL == "" || strings.TrimSpace(published.URL) != published.URL || strings.ContainsAny(published.URL, "\\\r\n") || strings.HasPrefix(published.URL, "//") || downloadURL.User != nil || downloadURL.Fragment != "" {
		return release{}, "", fmt.Errorf("临时插件包地址不正确")
	}
	if !downloadURL.IsAbs() && !strings.HasPrefix(published.URL, "/") {
		return release{}, "", fmt.Errorf("临时插件包地址必须是同站根路径")
	}
	downloadURL = base.ResolveReference(downloadURL)
	if !sameOrigin(base, downloadURL) || !strings.EqualFold(path.Ext(downloadURL.Path), ".zip") && !strings.EqualFold(path.Ext(downloadURL.Path), ".xpi") {
		return release{}, "", fmt.Errorf("临时插件包必须是同源 ZIP 或 XPI")
	}
	return release{Version: published.Version, URLFirefox: downloadURL.String(), SHA256Firefox: published.SHA256}, downloadURL.String(), nil
}

// sameOrigin 按协议和完整主机严格匹配发布源，不允许带凭证的地址。
func sameOrigin(expected, actual *url.URL) bool {
	return expected != nil && actual != nil && actual.User == nil && expected.Scheme == actual.Scheme && strings.EqualFold(expected.Host, actual.Host)
}

// sameOriginDownloadClient 为临时通道复制客户端，每次重定向都要求保持官方同源。
func sameOriginDownloadClient(client *http.Client, cloudBaseURL string) (*http.Client, error) {
	base, err := parseCloudBaseURL(cloudBaseURL)
	if err != nil {
		return nil, err
	}
	copyClient := *client
	previous := client.CheckRedirect
	copyClient.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 10 || !sameOrigin(base, next.URL) {
			return fmt.Errorf("临时插件下载不能跳到其它站点")
		}
		if previous != nil {
			return previous(next, via)
		}
		return nil
	}
	return &copyClient, nil
}

// validTemporaryVersion 只允许三段无前导零的数字版本，拒绝溢出及临时后缀。
func validTemporaryVersion(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || len(part) > 1 && part[0] == '0' {
			return false
		}
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return false
			}
		}
		if _, err := strconv.ParseUint(part, 10, 32); err != nil {
			return false
		}
	}
	return true
}

// olderTemporaryVersion 比较已校验的数字版本，防止暂时过期的发布清单把新代码降回旧版。
func olderTemporaryVersion(published, current string) bool {
	if !validTemporaryVersion(published) || !validTemporaryVersion(current) {
		return false
	}
	a, b := strings.Split(published, "."), strings.Split(current, ".")
	for i := range a {
		x, _ := strconv.ParseUint(a[i], 10, 32)
		y, _ := strconv.ParseUint(b[i], 10, 32)
		if x != y {
			return x < y
		}
	}
	return false
}

// validatedTemporaryCache 核对普通目录、临时收据、固定身份与全树摘要，不把签名旧版当临时缓存。
func validatedTemporaryCache(destination string) (Result, error) {
	info, err := os.Lstat(destination)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Result{}, fmt.Errorf("临时插件缓存目录不可用")
	}
	for _, name := range []string{"manifest.json", receiptFilename} {
		info, err = os.Lstat(filepath.Join(destination, name))
		if err != nil || !info.Mode().IsRegular() {
			return Result{}, fmt.Errorf("临时插件缓存文件不可用")
		}
	}
	manifest, err := readManifest(filepath.Join(destination, "manifest.json"))
	if err != nil {
		return Result{}, err
	}
	receipt, err := readReceipt(filepath.Join(destination, receiptFilename))
	if err != nil || receipt.Channel != temporaryChannel || receipt.Version != manifest.Version || !validTemporaryVersion(manifest.Version) {
		return Result{}, fmt.Errorf("临时插件缓存收据不匹配")
	}
	if err = validateFirefoxIdentity(destination, manifest); err != nil {
		return Result{}, err
	}
	if _, err = os.Lstat(filepath.Join(destination, signedFirefoxXPIFilename)); !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("临时插件缓存混入签名安装包")
	}
	tree, err := directoryTreeSHA256(destination)
	if err != nil || tree != receipt.TreeSHA256 {
		return Result{}, fmt.Errorf("临时插件缓存内容已经变化")
	}
	return Result{Version: manifest.Version, Directory: destination, AlreadyCurrent: true}, nil
}

// WithManagedFirefoxPath 只保留一个托管产品身份，保留其它自定义扩展和 Profile 书签扩展。
func WithManagedFirefoxPath(paths []string, managed string) []string {
	result := make([]string, 0, len(paths)+1)
	seen := make(map[string]bool)
	for _, current := range paths {
		if destinationKey(current) == destinationKey(managed) || destinationKey(current) == destinationKey(managed+".previous") {
			continue
		}
		manifest, err := readManifest(filepath.Join(current, "manifest.json"))
		if err == nil && manifest.BrowserSpecificSettings.Gecko.ID == expectedFirefoxID {
			continue
		}
		key := destinationKey(current)
		if !seen[key] {
			result = append(result, current)
			seen[key] = true
		}
	}
	return append([]string{managed}, result...)
}
