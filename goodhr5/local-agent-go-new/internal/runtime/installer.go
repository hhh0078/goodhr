// Package runtime 文件作用：下载、校验并安装 Node、Camoufox 和 OCR 运行组件。
package runtime

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
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"
)

// StartInstall 校验清单并在后台安装当前平台的运行组件。
func (m *Manager) StartInstall(manifest Manifest) (Status, error) {
	if !m.installMu.TryLock() {
		return m.Status(), fmt.Errorf("运行组件正在更新中，请等它忙完这一轮")
	}
	if !manifestHasAssets(manifest) {
		m.installMu.Unlock()
		return m.Status(), fmt.Errorf("运行组件下载配置为空")
	}
	m.setInstallProgress(InstallProgress{
		Running: true, Stage: "queued", Message: "运行组件更新已开始", Percent: 1,
	})
	go func() {
		defer m.installMu.Unlock()
		if err := m.install(context.Background(), manifest); err != nil {
			m.setInstallProgress(InstallProgress{
				Running: false, Stage: "failed", Message: err.Error(),
			})
			return
		}
		m.configureWorkerEnvironment()
		m.setInstallProgress(InstallProgress{
			Running: false, Stage: "installed", Message: "运行组件安装完成", Percent: 100,
		})
	}()
	return m.Status(), nil
}

// install 按 Node、Camoufox、OCR 的顺序安装当前平台资源。
func (m *Manager) install(ctx context.Context, manifest Manifest) error {
	platform := platformKey()
	steps := []struct {
		component string
		label     string
		target    string
		asset     Asset
		optional  bool
	}{
		{component: "node_runtime", label: "Node 运行环境", target: "node", asset: manifest.NodeRuntime[platform]},
		{component: "camoufox", label: "Camoufox 浏览器", target: "camoufox", asset: manifest.BrowserAsset(platform)},
		{component: "ocr", label: "OCR 组件", target: "ocr", asset: manifest.OCR[platform], optional: true},
	}
	for index, step := range steps {
		if step.component == "node_runtime" && m.CheckNode() == nil {
			m.setInstallProgress(InstallProgress{
				Running: true, Component: step.component, Stage: "skipped",
				Message: "本机 Node.js 已可用，跳过重复下载", Percent: (index + 1) * 30,
			})
			continue
		}
		if strings.TrimSpace(step.asset.URL) == "" {
			if step.optional {
				continue
			}
			return fmt.Errorf("%s没有当前系统 %s 的下载地址", step.label, platform)
		}
		if err := m.installAsset(ctx, step.component, step.label, step.target, step.asset); err != nil {
			return err
		}
	}
	// GeoIP 数据库按单文件补充到 Camoufox 目录，缺失或失败不影响组件安装结果。
	m.installGeoIPDatabase(ctx, manifest)
	return nil
}

// installAsset 下载、SHA256 校验、安全解压并替换一个组件目录。
func (m *Manager) installAsset(ctx context.Context, component string, label string, targetName string, asset Asset) error {
	if current, ok := m.loadVersions()[component]; ok &&
		strings.TrimSpace(current.Version) == strings.TrimSpace(asset.Version) &&
		m.componentInstalled(component) {
		m.setInstallProgress(InstallProgress{
			Running: true, Component: component, Stage: "skipped",
			Message: label + "已经是当前版本", Percent: 95,
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
		Message: "正在下载" + label, Percent: 5,
	})
	if err := m.downloadAsset(ctx, component, label, asset.URL, archivePath); err != nil {
		return err
	}
	defer os.Remove(archivePath)
	m.setInstallProgress(InstallProgress{
		Running: true, Component: component, Stage: "verify",
		Message: "正在校验" + label, Percent: 65,
	})
	if err := verifySHA256(archivePath, asset.SHA256); err != nil {
		return fmt.Errorf("%s校验失败：%w", label, err)
	}
	m.setInstallProgress(InstallProgress{
		Running: true, Component: component, Stage: "extract",
		Message: "正在解压" + label, Percent: 75,
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
	if component == "camoufox" {
		m.writeCamoufoxVersionFile(targetDir, asset.Version)
	}
	if err = m.saveVersion(component, asset); err != nil {
		return fmt.Errorf("保存%s版本记录失败：%w", label, err)
	}
	m.setInstallProgress(InstallProgress{
		Running: true, Component: component, Stage: "installed",
		Message: label + "安装完成", Percent: 95,
	})
	return nil
}

// downloadAsset 下载单个运行组件并保存到临时文件后原子替换。
func (m *Manager) downloadAsset(ctx context.Context, component string, label string, sourceURL string, targetPath string) error {
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
			percent := 10
			if total > 0 {
				percent = min(60, 10+int(received*50/total))
			}
			m.setInstallProgress(InstallProgress{
				Running: true, Component: component, Stage: "download",
				Message: "正在下载" + label, Percent: percent, Received: received, Total: total,
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

// componentInstalled 判断一个组件的关键文件是否已经存在。
func (m *Manager) componentInstalled(component string) bool {
	switch component {
	case "node_runtime":
		return m.CheckNode() == nil
	case "camoufox", "cloakbrowser":
		return fileExists(m.CamoufoxPath())
	case "geoip":
		return fileExists(m.geoIPDatabasePath())
	case "ocr":
		return m.OCRInstalled()
	default:
		return false
	}
}

// installGeoIPDatabase 在 Camoufox 目录缺失 GeoIP 数据库时，按清单可选配置下载补充。
// 该数据库供代理场景自动匹配时区、语言和经纬度；由本安装器分发后 camoufox-js 不再访问 GitHub。
// 缺失配置或下载失败只跳过，不阻断组件安装，Worker 启动时会自动降级为不启用 GeoIP。
func (m *Manager) installGeoIPDatabase(ctx context.Context, manifest Manifest) {
	asset, ok := manifest.GeoIP[platformKey()]
	if !ok || strings.TrimSpace(asset.URL) == "" {
		return
	}
	targetPath := m.geoIPDatabasePath()
	if fileExists(targetPath) {
		return
	}
	if err := validateAssetURL(asset.URL); err != nil {
		return
	}
	if err := validateSHA256(asset.SHA256); err != nil {
		return
	}
	downloadsDir := filepath.Join(m.runtimeDir, "downloads")
	if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
		return
	}
	downloadPath := filepath.Join(downloadsDir, "GeoLite2-City.mmdb")
	m.setInstallProgress(InstallProgress{
		Running: true, Component: "geoip", Stage: "download",
		Message: "正在下载 GeoIP 数据库", Percent: 92,
	})
	if err := m.downloadAsset(ctx, "geoip", "GeoIP 数据库", asset.URL, downloadPath); err != nil {
		return
	}
	if err := verifySHA256(downloadPath, asset.SHA256); err != nil {
		_ = os.Remove(downloadPath)
		return
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		_ = os.Remove(downloadPath)
		return
	}
	_ = os.Remove(targetPath)
	if err := os.Rename(downloadPath, targetPath); err != nil {
		_ = os.Remove(downloadPath)
		return
	}
	m.setInstallProgress(InstallProgress{
		Running: true, Component: "geoip", Stage: "installed",
		Message: "GeoIP 数据库已就绪", Percent: 95,
	})
}

// geoIPDatabasePath 返回 camoufox-js 约定的 GeoIP 数据库落盘路径，即 Camoufox 组件根目录。
func (m *Manager) geoIPDatabasePath() string {
	return filepath.Join(m.runtimeDir, "camoufox", "GeoLite2-City.mmdb")
}

// manifestHasAssets 判断清单是否至少配置了一个下载资源。
func manifestHasAssets(manifest Manifest) bool {
	for _, group := range []map[string]Asset{manifest.NodeRuntime, manifest.Camoufox, manifest.CloakBrowser, manifest.GeoIP, manifest.OCR} {
		for _, asset := range group {
			if strings.TrimSpace(asset.URL) != "" {
				return true
			}
		}
	}
	return false
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

// validateAssetURL 校验组件下载地址必须使用 HTTPS。
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

// writeCamoufoxVersionFile 按 camoufox-js 约定在组件根目录写入 version.json，缺失时才写入。
func (m *Manager) writeCamoufoxVersionFile(targetDir string, version string) {
	content := camoufoxVersionJSON(version)
	if content == "" {
		return
	}
	versionPath := filepath.Join(targetDir, "version.json")
	if fileExists(versionPath) {
		return
	}
	if err := os.WriteFile(versionPath, []byte(content), 0o644); err != nil {
		// 写入失败只影响 camoufox-js 的版本校验提示，不阻断浏览器使用。
		return
	}
}

// camoufoxVersionJSON 把清单版本号转换为 camoufox-js version.json 内容，无法解析时返回空字符串。
func camoufoxVersionJSON(version string) string {
	trimmed := strings.TrimSpace(version)
	if trimmed == "" {
		return ""
	}
	release := ""
	if index := strings.Index(trimmed, "-"); index >= 0 {
		release = trimmed[index+1:]
		trimmed = trimmed[:index]
	}
	if release == "" {
		release = "stable"
	}
	payload, err := json.Marshal(map[string]string{"version": trimmed, "release": release})
	if err != nil {
		return ""
	}
	return string(payload)
}
