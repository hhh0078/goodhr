// Package auto_reply 本文件负责在正式候选人确认后，以元素截图优先的方式尽力同步自托管头像。
package auto_reply

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

const currentAutoReplyAvatarSelectorKey = "message.current_avatar"

type avatarScreenshotBrowser interface {
	Screenshot(context.Context, contract.ScreenshotRequest) (contract.ScreenshotResult, error)
}

// syncCandidateAvatarBestEffort 在候选人编号确认后同步头像，失败只记警告且不阻断回复流程。
// 返回值表示本轮已经处理过头像，避免同一候选人在正式入库前后重复上传。
func (f *Flow) syncCandidateAvatarBestEffort(ctx context.Context, prepared shared.PreparedTask, snapshot model.AutoReplyConversationSnapshot, candidate *cloud.AutoReplyStoredCandidate) bool {
	if candidate == nil || strings.TrimSpace(candidate.ID) == "" {
		return false
	}
	if isSelfHostedCandidateAvatar(candidate.AvatarURL) {
		return true
	}
	startedAt := time.Now()
	if err := f.syncCandidateAvatar(ctx, prepared, snapshot, candidate.ID); err != nil {
		f.log(prepared.Request.TaskID, "sync_candidate_avatar", "warning", startedAt, err)
		return true
	}
	f.log(prepared.Request.TaskID, "sync_candidate_avatar", "success", startedAt, nil)
	return true
}

// syncCandidateAvatar 优先截图当前会话头像，再用页面已读取的 HTTPS 地址安全下载作为后备。
func (f *Flow) syncCandidateAvatar(ctx context.Context, prepared shared.PreparedTask, snapshot model.AutoReplyConversationSnapshot, candidateID string) error {
	temporaryDir, err := os.MkdirTemp("", "goodhr-candidate-avatar-*")
	if err != nil {
		return fmt.Errorf("准备候选人头像临时目录失败")
	}
	defer os.RemoveAll(temporaryDir)

	imageData, filePath, captureErr := f.captureCurrentCandidateAvatar(ctx, prepared.Platform, temporaryDir)
	if captureErr != nil {
		imageData, err = downloadSafeCandidateAvatar(ctx, snapshot.AvatarURL)
		if err != nil {
			return fmt.Errorf("候选人头像截图失败：%v；安全下载也没成功：%w", captureErr, err)
		}
		filePath = filepath.Join(temporaryDir, "avatar-download")
		if err = os.WriteFile(filePath, imageData, 0o600); err != nil {
			return fmt.Errorf("保存候选人头像临时文件失败")
		}
	}
	return f.uploadCandidateAvatar(ctx, credentials(prepared), candidateID, imageData, filePath)
}

// captureCurrentCandidateAvatar 使用平台配置的当前头像选择器执行标准元素截图，不读取或注入页面脚本。
func (f *Flow) captureCurrentCandidateAvatar(ctx context.Context, cfg model.Config, directory string) ([]byte, string, error) {
	selector, configured := cfg.Selectors[currentAutoReplyAvatarSelectorKey]
	if !configured {
		return nil, "", fmt.Errorf("%s没有配置当前候选人头像选择器", cfg.Name)
	}
	browser, ok := f.Browser.(avatarScreenshotBrowser)
	if !ok {
		return nil, "", fmt.Errorf("浏览器截图能力还没准备好")
	}
	result, err := browser.Screenshot(ctx, contract.ScreenshotRequest{
		Target: &selector, Directory: directory, Filename: "candidate-avatar.png",
	})
	if err != nil {
		return nil, "", err
	}
	path := strings.TrimSpace(result.Path)
	if path == "" {
		path = filepath.Join(directory, "candidate-avatar.png")
	}
	if !pathWithinAvatarDirectory(path, directory) {
		return nil, "", fmt.Errorf("浏览器返回的候选人头像路径不安全")
	}
	imageData, err := readCandidateAvatarFile(path)
	if err != nil {
		return nil, "", err
	}
	return imageData, path, nil
}

// uploadCandidateAvatar 优先使用 Base64；仅云端明确返回4xx协议错误时才用 multipart 后备，响应未知时禁止重复上传。
func (f *Flow) uploadCandidateAvatar(ctx context.Context, credentials cloud.AgentCredentials, candidateID string, imageData []byte, filePath string) error {
	if _, err := f.Cloud.UploadAutoReplyAvatarBase64(ctx, credentials, candidateID, imageData); err == nil {
		return nil
	} else if !candidateAvatarAllowsFileFallback(err) {
		return fmt.Errorf("Base64 上传结果暂时不能确认，我先不重复上传：%w", err)
	} else if _, fileErr := f.Cloud.UploadAutoReplyAvatarFile(ctx, credentials, candidateID, filePath); fileErr != nil {
		return fmt.Errorf("Base64 上传失败：%v；文件重试也没成功：%w", err, fileErr)
	}
	return nil
}

// candidateAvatarAllowsFileFallback 只在云端明确拒绝 Base64 请求格式时允许改用文件上传。
func candidateAvatarAllowsFileFallback(err error) bool {
	var apiErr *cloud.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode >= http.StatusBadRequest && apiErr.StatusCode < http.StatusInternalServerError
}

// readCandidateAvatarFile 读取普通头像文件，并严格限制为1MB以内。
func readCandidateAvatarFile(path string) ([]byte, error) {
	file, err := os.Open(strings.TrimSpace(path))
	if err != nil {
		return nil, fmt.Errorf("打开候选人头像截图失败")
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		return nil, fmt.Errorf("候选人头像截图不是普通文件")
	}
	if stat.Size() <= 0 || stat.Size() > cloud.AutoReplyMaxAvatarBytes {
		return nil, fmt.Errorf("候选人头像截图必须在1MB以内")
	}
	imageData, err := io.ReadAll(io.LimitReader(file, cloud.AutoReplyMaxAvatarBytes+1))
	if err != nil || int64(len(imageData)) != stat.Size() {
		return nil, fmt.Errorf("候选人头像截图没有读完整")
	}
	return imageData, nil
}

// downloadSafeCandidateAvatar 仅下载页面已经读到的公开 HTTPS 图片，并在本地完成地址和图片校验。
func downloadSafeCandidateAvatar(ctx context.Context, rawURL string) ([]byte, error) {
	target, err := parseSafeCandidateAvatarURL(rawURL)
	if err != nil {
		return nil, err
	}
	client := newSafeCandidateAvatarHTTPClient()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("创建候选人头像下载请求失败")
	}
	request.Header.Set("Accept", "image/png,image/jpeg;q=0.9")
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("下载候选人头像失败：%w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载候选人头像返回状态%d", response.StatusCode)
	}
	if response.ContentLength > cloud.AutoReplyMaxAvatarBytes {
		return nil, fmt.Errorf("候选人头像不能超过1MB")
	}
	imageData, err := io.ReadAll(io.LimitReader(response.Body, cloud.AutoReplyMaxAvatarBytes+1))
	if err != nil || int64(len(imageData)) > cloud.AutoReplyMaxAvatarBytes {
		return nil, fmt.Errorf("候选人头像下载内容超过1MB")
	}
	if err = validateDownloadedCandidateAvatar(imageData); err != nil {
		return nil, err
	}
	return imageData, nil
}

// newSafeCandidateAvatarHTTPClient 创建禁用代理、逐次校验 DNS 和重定向地址的头像下载客户端。
func newSafeCandidateAvatarHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 15 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, fmt.Errorf("候选人头像地址端口不正确")
			}
			ips, err := resolvePublicCandidateAvatarIPs(ctx, host)
			if err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		},
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		IdleConnTimeout:       15 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("候选人头像重定向次数过多")
			}
			_, err := parseSafeCandidateAvatarURL(request.URL.String())
			return err
		},
	}
}

// parseSafeCandidateAvatarURL 只接受没有账号信息且主机不是本机的 HTTPS 地址。
func parseSafeCandidateAvatarURL(rawURL string) (*url.URL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" || len(rawURL) > 2048 {
		return nil, fmt.Errorf("页面没有可安全下载的候选人头像地址")
	}
	target, err := url.Parse(rawURL)
	if err != nil || target.Scheme != "https" || target.Hostname() == "" || target.User != nil {
		return nil, fmt.Errorf("候选人头像只允许公开 HTTPS 地址")
	}
	host := strings.TrimSuffix(strings.ToLower(target.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return nil, fmt.Errorf("候选人头像地址不能指向本机")
	}
	if ip := net.ParseIP(host); ip != nil && !isPublicCandidateAvatarIP(ip) {
		return nil, fmt.Errorf("候选人头像地址不能指向内网")
	}
	return target, nil
}

// resolvePublicCandidateAvatarIPs 解析主机并拒绝任何私网、回环或保留地址，防止 DNS 重绑定。
func resolvePublicCandidateAvatarIPs(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(strings.TrimSpace(host)); ip != nil {
		if !isPublicCandidateAvatarIP(ip) {
			return nil, fmt.Errorf("候选人头像地址不能指向内网")
		}
		return []net.IP{ip}, nil
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 {
		return nil, fmt.Errorf("候选人头像域名暂时没解析出来")
	}
	result := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		if !isPublicCandidateAvatarIP(address.IP) {
			return nil, fmt.Errorf("候选人头像域名解析到了内网地址")
		}
		result = append(result, address.IP)
	}
	return result, nil
}

// isPublicCandidateAvatarIP 判断地址是否为可访问的公开单播 IP，并排除常见保留网段。
func isPublicCandidateAvatarIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	for _, cidr := range []string{
		"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32",
	} {
		_, network, _ := net.ParseCIDR(cidr)
		if network.Contains(ip) {
			return false
		}
	}
	return true
}

// validateDownloadedCandidateAvatar 验证下载内容是真实 PNG/JPEG 且宽高不超过2048像素。
func validateDownloadedCandidateAvatar(imageData []byte) error {
	if len(imageData) == 0 || int64(len(imageData)) > cloud.AutoReplyMaxAvatarBytes {
		return fmt.Errorf("候选人头像必须在1MB以内")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(imageData))
	if err != nil || (format != "png" && format != "jpeg") {
		return fmt.Errorf("下载内容不是真实的 PNG 或 JPEG 头像")
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > 2048 || config.Height > 2048 {
		return fmt.Errorf("候选人头像宽高不能超过2048像素")
	}
	return nil
}

// pathWithinAvatarDirectory 确认 Worker 返回的截图文件仍在本次临时目录内。
func pathWithinAvatarDirectory(path string, directory string) bool {
	absolutePath, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return false
	}
	absoluteDirectory, err := filepath.Abs(strings.TrimSpace(directory))
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(absoluteDirectory, absolutePath)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// isSelfHostedCandidateAvatar 判断云端候选人是否已经持有 GoodHR 自托管头像路径。
func isSelfHostedCandidateAvatar(avatarURL string) bool {
	const prefix = "/api/public/candidate-avatars/"
	avatarURL = strings.TrimSpace(avatarURL)
	if !strings.HasPrefix(avatarURL, prefix) {
		return false
	}
	filename := strings.TrimPrefix(avatarURL, prefix)
	if len(filename) != 68 || !strings.HasSuffix(filename, ".png") {
		return false
	}
	for _, char := range filename[:64] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
