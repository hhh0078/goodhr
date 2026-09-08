// Package runtime 文件作用：按顺序安装 Node、最新版 CloakBrowser 和可选 OCR 运行组件。
package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"
)

const runtimeInstallMaxAttempts = 3

// StartInstall 校验 Key 并在后台安装当前平台的运行组件。
func (m *Manager) StartInstall(manifest Manifest, licenseKey string) (Status, error) {
	if !m.installMu.TryLock() {
		return m.Status(), fmt.Errorf("运行组件正在更新中，请等它忙完这一轮")
	}
	if strings.TrimSpace(licenseKey) != "" {
		if err := m.SaveCloakBrowserLicenseKey(licenseKey); err != nil {
			m.installMu.Unlock()
			return m.Status(), err
		}
	}
	if !m.CloakBrowserLicenseConfigured() {
		m.installMu.Unlock()
		return m.Status(), fmt.Errorf("还没填写 CloakBrowser Key，请先去个人配置里补上")
	}
	m.setInstallProgress(InstallProgress{
		Running: true, Stage: "queued", Message: "运行组件安装已开始", Percent: 1,
	})
	go func() {
		defer m.installMu.Unlock()
		if err := m.install(context.Background(), manifest); err != nil {
			progress := m.InstallProgress()
			progress.Running = false
			progress.Stage = "failed"
			progress.Message = installErrorSummary(err)
			progress.Detail = err.Error()
			progress.CanRetry = true
			m.setInstallProgress(progress)
			return
		}
		m.configureWorkerEnvironment()
		m.setInstallProgress(InstallProgress{
			Running: false, Component: "cloakbrowser", Stage: "installed",
			Message: "运行组件安装完成", Percent: 100,
		})
	}()
	return m.Status(), nil
}

// install 按 Node、npm 依赖、Key 校验、官方 Chromium、试启动和 OCR 的顺序安装。
func (m *Manager) install(ctx context.Context, manifest Manifest) error {
	platform := platformKey()
	if m.CheckNode() == nil {
		m.setInstallProgress(InstallProgress{
			Running: true, Component: "node_runtime", Stage: "skipped",
			Message: "Node 运行环境已经可用", Percent: 24,
		})
	} else {
		asset := manifest.NodeRuntime[platform]
		if strings.TrimSpace(asset.URL) == "" {
			return fmt.Errorf("Node 运行环境没有当前系统 %s 的下载地址", platform)
		}
		if err := m.installAsset(ctx, "node_runtime", "Node 运行环境", "node", asset, 3, 24); err != nil {
			return err
		}
	}
	m.configureWorkerEnvironment()
	if m.worker != nil {
		if err := m.worker.Stop(); err != nil {
			return fmt.Errorf("停止旧浏览器操作程序失败：%w", err)
		}
	}
	if err := m.installWorkerDependencies(ctx); err != nil {
		return err
	}
	licenseKey := m.cloakBrowserLicenseKey()
	if err := m.validateOfficialLicense(ctx, licenseKey); err != nil {
		return err
	}
	info, err := m.installOfficialCloakBrowser(ctx, licenseKey)
	if err != nil {
		return err
	}
	if err = m.smokeTestCloakBrowser(ctx, licenseKey); err != nil {
		return err
	}
	if err = m.saveCloakBrowserVersion(info.Binary.Version, info.Binary.Path); err != nil {
		return err
	}
	if asset := manifest.OCR[platform]; strings.TrimSpace(asset.URL) != "" {
		if err = m.installAsset(ctx, "ocr", "OCR 组件", "ocr", asset, 96, 99); err != nil {
			return err
		}
	}
	return nil
}

// installAsset 下载、SHA256 校验、安全解压并替换一个 GoodHR 自有组件目录。
func (m *Manager) installAsset(ctx context.Context, component string, label string, targetName string, asset Asset, progressStart int, progressEnd int) error {
	if current, ok := m.loadVersions()[component]; ok &&
		strings.TrimSpace(current.Version) == strings.TrimSpace(asset.Version) &&
		m.componentInstalled(component) {
		m.setInstallProgress(InstallProgress{
			Running: true, Component: component, Stage: "skipped",
			Message: label + "已经是当前版本", Percent: progressEnd,
		})
		return nil
	}
	if err := validateAssetURL(asset.URL); err != nil {
		return fmt.Errorf("%s下载地址不正确：%w", label, err)
	}
	if err := validateSHA256(asset.SHA256); err != nil {
		return fmt.Errorf("%s校验值不正确：%w", label, err)
	}
	downloadsDir := filepath.Join(m.runtimeDir, "downloads")
	if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
		return fmt.Errorf("创建运行组件下载目录失败：%w", err)
	}
	archivePath := filepath.Join(downloadsDir, archiveName(asset.URL, targetName))
	m.setInstallProgress(InstallProgress{
		Running: true, Component: component, Stage: "download",
		Message: "正在下载" + label, Percent: progressStart,
	})
	verifyProgress := progressStart + max(1, (progressEnd-progressStart)*7/10)
	if err := m.downloadAsset(ctx, component, label, asset.URL, archivePath, progressStart, verifyProgress); err != nil {
		return err
	}
	defer os.Remove(archivePath)
	m.setInstallProgress(InstallProgress{
		Running: true, Component: component, Stage: "verify",
		Message: "正在校验" + label, Percent: verifyProgress,
	})
	if err := verifySHA256(archivePath, asset.SHA256); err != nil {
		return fmt.Errorf("%s校验失败：%w", label, err)
	}
	extractProgress := progressStart + max(1, (progressEnd-progressStart)*8/10)
	m.setInstallProgress(InstallProgress{
		Running: true, Component: component, Stage: "extract",
		Message: "正在解压" + label, Percent: extractProgress,
	})
	stagingDir, err := os.MkdirTemp(m.runtimeDir, "."+targetName+"-install-*")
	if err != nil {
		return fmt.Errorf("创建%s临时目录失败：%w", label, err)
	}
	defer os.RemoveAll(stagingDir)
	if err = extractArchive(archivePath, stagingDir); err != nil {
		return fmt.Errorf("解压%s失败：%w", label, err)
	}
	sourceDir := installRoot(stagingDir, component)
	targetDir := filepath.Join(m.runtimeDir, targetName)
	if err = replaceDirectory(sourceDir, targetDir); err != nil {
		return fmt.Errorf("安装%s失败：%w", label, err)
	}
	if err = m.saveVersion(component, asset); err != nil {
		return fmt.Errorf("保存%s版本记录失败：%w", label, err)
	}
	m.setInstallProgress(InstallProgress{
		Running: true, Component: component, Stage: "installed",
		Message: label + "安装完成", Percent: progressEnd,
	})
	return nil
}

// downloadAsset 下载单个 GoodHR 自有组件并实时报告字节进度。
func (m *Manager) downloadAsset(ctx context.Context, component string, label string, sourceURL string, targetPath string, progressStart int, progressEnd int) error {
	var err error
	for attempt := 1; attempt <= runtimeInstallMaxAttempts; attempt++ {
		err = m.downloadAssetOnce(ctx, component, label, sourceURL, targetPath, progressStart, progressEnd, attempt)
		if err == nil || !retryableInstallError(err.Error()) || attempt == runtimeInstallMaxAttempts {
			return err
		}
		if waitErr := m.waitForInstallRetry(ctx, component, "download", label+"下载刚才断了一下", progressStart, attempt+1, runtimeInstallMaxAttempts); waitErr != nil {
			return waitErr
		}
	}
	return err
}

// downloadAssetOnce 执行一次 GoodHR 自有组件下载并实时报告字节进度。
func (m *Manager) downloadAssetOnce(ctx context.Context, component string, label string, sourceURL string, targetPath string, progressStart int, progressEnd int, attempt int) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return fmt.Errorf("创建%s下载请求失败：%w", label, err)
	}
	client := &http.Client{Timeout: 30 * time.Minute}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("下载%s失败：%w", label, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("下载%s失败，状态码：%d", label, response.StatusCode)
	}
	tempPath := targetPath + ".tmp"
	file, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("创建%s下载文件失败：%w", label, err)
	}
	reader := &installProgressReader{
		reader: response.Body, total: response.ContentLength,
		onProgress: func(received int64, total int64) {
			percent := progressStart
			if total > 0 {
				percent += int(received * int64(progressEnd-progressStart) / total)
				percent = min(percent, progressEnd)
			}
			m.setInstallProgress(InstallProgress{
				Running: true, Component: component, Stage: "download",
				Message: "正在下载" + label, Percent: percent, Received: received, Total: total,
				Attempt: attempt, MaxAttempts: runtimeInstallMaxAttempts,
			})
		},
	}
	_, copyErr := io.Copy(file, reader)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(tempPath)
		if copyErr != nil {
			return fmt.Errorf("保存%s失败：%w", label, copyErr)
		}
		return fmt.Errorf("关闭%s下载文件失败：%w", label, closeErr)
	}
	_ = os.Remove(targetPath)
	if err = os.Rename(tempPath, targetPath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("完成%s下载失败：%w", label, err)
	}
	return nil
}

// waitForInstallRetry 展示下一次重试并等待短暂退避时间。
func (m *Manager) waitForInstallRetry(ctx context.Context, component string, stage string, message string, percent int, attempt int, maxAttempts int) error {
	delay := time.Duration((attempt-1)*2) * time.Second
	m.setInstallProgress(InstallProgress{
		Running: true, Component: component, Stage: stage, Percent: percent,
		Message: fmt.Sprintf("%s，%d 秒后自动重试（%d/%d）", message, int(delay.Seconds()), attempt, maxAttempts),
		Attempt: attempt, MaxAttempts: maxAttempts,
	})
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// retryableInstallError 判断错误是否属于网络或服务短暂波动。
func retryableInstallError(value string) bool {
	lower := strings.ToLower(value)
	for _, fragment := range []string{
		"could not determine latest pro version", "pro binary unavailable",
		"fetch failed", "econnreset", "econnrefused", "etimedout",
		"socket hang up", "network error", "unexpected eof", "connection reset",
		"connection refused", "timeout", "timed out", "tls handshake timeout",
		"http 429", "http 500", "http 502", "http 503", "http 504",
		"状态码：429", "状态码：500", "状态码：502", "状态码：503", "状态码：504",
	} {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}

// installErrorSummary 返回适合在进度标题中展示的单行错误摘要。
func installErrorSummary(err error) string {
	if err == nil {
		return "运行组件安装失败"
	}
	value := strings.TrimSpace(err.Error())
	if line, _, ok := strings.Cut(value, "\n"); ok {
		value = strings.TrimSpace(line)
	}
	if len([]rune(value)) > 180 {
		value = string([]rune(value)[:180]) + "..."
	}
	return value
}

// componentInstalled 判断一个 GoodHR 自有组件的关键文件是否已经存在。
func (m *Manager) componentInstalled(component string) bool {
	switch component {
	case "node_runtime":
		return m.CheckNode() == nil
	case "ocr":
		return m.OCRInstalled()
	default:
		return false
	}
}

// platformKey 返回运行组件清单使用的平台编号。
func platformKey() string {
	switch {
	case goruntime.GOOS == "windows" && goruntime.GOARCH == "amd64":
		return "win-x64"
	case goruntime.GOOS == "darwin" && goruntime.GOARCH == "arm64":
		return "darwin-arm64"
	case goruntime.GOOS == "darwin" && goruntime.GOARCH == "amd64":
		return "darwin-x64"
	default:
		return goruntime.GOOS + "-" + goruntime.GOARCH
	}
}

// validateAssetURL 校验 GoodHR 自有组件下载地址必须使用 HTTPS。
func validateAssetURL(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || parsed.Scheme != "https" || parsed.User != nil {
		return fmt.Errorf("只支持 HTTPS 地址")
	}
	return nil
}

// archiveName 根据下载地址保留支持的压缩包后缀。
func archiveName(sourceURL string, fallback string) string {
	parsed, _ := url.Parse(sourceURL)
	name := filepath.Base(parsed.Path)
	lower := strings.ToLower(name)
	for _, suffix := range []string{".tar.gz", ".tgz", ".zip"} {
		if strings.HasSuffix(lower, suffix) {
			return fallback + suffix
		}
	}
	return fallback + ".zip"
}

// verifySHA256 强制校验下载文件的 SHA256。
func verifySHA256(path string, expected string) error {
	expected = strings.ToLower(strings.TrimSpace(expected))
	if err := validateSHA256(expected); err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return err
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != expected {
		return fmt.Errorf("SHA256 不一致，期望 %s，实际 %s", expected, actual)
	}
	return nil
}

// validateSHA256 检查 SHA256 是否为完整的 64 位十六进制字符串。
func validateSHA256(value string) error {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != sha256.Size*2 {
		return fmt.Errorf("必须提供完整 SHA256")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("SHA256 必须是十六进制字符串")
	}
	return nil
}

// installProgressReader 在读取下载内容时回调累计进度。
type installProgressReader struct {
	reader     io.Reader
	received   int64
	total      int64
	onProgress func(int64, int64)
}

// Read 读取组件下载内容并报告进度。
func (r *installProgressReader) Read(buffer []byte) (int, error) {
	count, err := r.reader.Read(buffer)
	if count > 0 {
		r.received += int64(count)
		if r.onProgress != nil {
			r.onProgress(r.received, r.total)
		}
	}
	return count, err
}
