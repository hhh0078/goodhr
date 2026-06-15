// Package runtime 负责下载、校验和解压本地运行组件。
package runtime

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Manifest 是本地运行组件下载清单。
type Manifest struct {
	NodeRuntime  map[string]Asset `json:"node_runtime"`
	CloakBrowser map[string]Asset `json:"cloakbrowser"`
	OCR          map[string]Asset `json:"ocr"`
}

// Asset 是单个运行组件资源。
type Asset struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Note    string `json:"note,omitempty"`
}

// InstallResult 表示运行组件安装结果。
type InstallResult struct {
	Platform  string   `json:"platform"`
	Installed []string `json:"installed"`
	Skipped   []string `json:"skipped"`
	Status    Status   `json:"status"`
}

// StartInstall 在后台启动运行组件安装。
// manifest 为前端从 system.onboarding_config 整理后的运行组件配置。
func (m *Manager) StartInstall(manifest Manifest) (Status, error) {
	if !m.installMu.TryLock() {
		return m.Status(), fmt.Errorf("运行组件正在更新中，请等待完成")
	}
	if !manifestHasRuntimeAssets(manifest) {
		m.installMu.Unlock()
		return m.Status(), fmt.Errorf("运行组件下载配置为空，请先在系统配置里填写运行组件下载地址")
	}
	m.setProgress(Progress{Running: true, Stage: "queued", Message: "运行组件更新已开始", Percent: 1})
	go func() {
		defer m.installMu.Unlock()
		_, _ = m.installLocked(context.Background(), manifest)
	}()
	return m.Status(), nil
}

// Install 根据运行组件配置安装运行组件。
// ctx 为请求上下文，manifest 为运行组件下载配置。
func (m *Manager) Install(ctx context.Context, manifest Manifest) (InstallResult, error) {
	if !m.installMu.TryLock() {
		return InstallResult{}, fmt.Errorf("运行组件正在更新中，请等待完成")
	}
	defer m.installMu.Unlock()
	if !manifestHasRuntimeAssets(manifest) {
		return InstallResult{}, fmt.Errorf("运行组件下载配置为空，请先在系统配置里填写运行组件下载地址")
	}
	return m.installLocked(ctx, manifest)
}

// installLocked 根据传入配置安装运行组件。
// 调用前必须持有安装锁，ctx 为安装上下文。
func (m *Manager) installLocked(ctx context.Context, manifest Manifest) (InstallResult, error) {
	m.setProgress(Progress{Running: true, Stage: "manifest", Message: "正在读取运行组件配置", Percent: 1})
	defer func() {
		progress := m.Progress()
		if progress.Running {
			progress.Running = false
			progress.Stage = "idle"
			progress.Message = "运行组件安装结束"
			progress.Percent = 100
			m.setProgress(progress)
		}
	}()
	platform := platformKey()
	installed := []string{}
	skipped := []string{}
	if m.bundledNodePath() == "" && systemNodePath() != "" {
		m.setProgress(Progress{Running: true, Component: "node_runtime", Stage: "skipped", Message: "已检测到系统 Node，跳过下载", Percent: 20})
		skipped = append(skipped, "node_runtime")
	} else {
		if didInstall, err := m.installAsset(ctx, manifest.NodeRuntime[platform], "node", "Node 运行组件", "node_runtime"); err != nil {
			m.setProgress(Progress{Running: false, Component: "node_runtime", Stage: "failed", Message: err.Error()})
			return InstallResult{}, err
		} else if didInstall {
			installed = append(installed, "node_runtime")
		} else {
			skipped = append(skipped, "node_runtime")
		}
	}
	if didInstall, err := m.installAsset(ctx, manifest.CloakBrowser[platform], "cloakbrowser", "CloakBrowser", "cloakbrowser"); err != nil {
		m.setProgress(Progress{Running: false, Component: "cloakbrowser", Stage: "failed", Message: err.Error()})
		return InstallResult{}, err
	} else if didInstall {
		installed = append(installed, "cloakbrowser")
	} else {
		skipped = append(skipped, "cloakbrowser")
	}
	if asset := manifest.OCR[platform]; strings.TrimSpace(asset.URL) != "" {
		if didInstall, err := m.installAsset(ctx, asset, "ocr", "OCR 组件", "ocr"); err != nil {
			m.setProgress(Progress{Running: false, Component: "ocr", Stage: "failed", Message: err.Error()})
			return InstallResult{}, err
		} else if didInstall {
			installed = append(installed, "ocr")
		} else {
			skipped = append(skipped, "ocr")
		}
	}
	return InstallResult{Platform: platform, Installed: installed, Skipped: skipped, Status: m.Status()}, nil
}

// manifestHasRuntimeAssets 判断配置里是否至少包含一个运行组件下载地址。
// manifest 为前端整理后的运行组件配置。
func manifestHasRuntimeAssets(manifest Manifest) bool {
	for _, group := range []map[string]Asset{manifest.NodeRuntime, manifest.CloakBrowser, manifest.OCR} {
		for _, asset := range group {
			if strings.TrimSpace(asset.URL) != "" {
				return true
			}
		}
	}
	return false
}

// InstallLocalWorker 从仓库源码安装 Node Browser Worker。
// sourceDir 为 worker-node 目录，主要用于本地开发阶段。
func (m *Manager) InstallLocalWorker(sourceDir string) (InstallResult, error) {
	if !m.installMu.TryLock() {
		return InstallResult{}, fmt.Errorf("运行组件正在更新中，请等待完成")
	}
	defer m.installMu.Unlock()
	sourceDir = strings.TrimSpace(sourceDir)
	if sourceDir == "" {
		return InstallResult{}, fmt.Errorf("Node Worker 源码目录不能为空")
	}
	info, err := os.Stat(sourceDir)
	if err != nil || !info.IsDir() {
		return InstallResult{}, fmt.Errorf("Node Worker 源码目录不存在：%s", sourceDir)
	}
	targetDir := filepath.Join(m.cfg.RuntimeDir, "browser-worker")
	if err := os.RemoveAll(targetDir); err != nil {
		return InstallResult{}, fmt.Errorf("清理旧 Node Worker 失败：%w", err)
	}
	if err := copyDir(sourceDir, targetDir); err != nil {
		return InstallResult{}, fmt.Errorf("安装 Node Worker 失败：%w", err)
	}
	_ = m.saveVersion("node_worker", Asset{Version: "local", URL: sourceDir})
	return InstallResult{Platform: platformKey(), Installed: []string{"node_worker"}, Status: m.Status()}, nil
}

// installAsset 下载并解压单个运行组件。
// ctx 为请求上下文，asset 为资源配置，targetName 为目标目录名，label 为中文组件名，component 为组件键名。
func (m *Manager) installAsset(ctx context.Context, asset Asset, targetName string, label string, component string) (bool, error) {
	if strings.TrimSpace(asset.URL) == "" {
		return false, fmt.Errorf("%s 下载地址为空", label)
	}
	if m.assetIsCurrent(component, asset) {
		m.setProgress(Progress{Running: true, Component: component, Stage: "skipped", Message: label + "已是最新版本，跳过下载", Percent: 95})
		return false, nil
	}
	m.setProgress(Progress{Running: true, Component: component, Stage: "download", Message: "正在下载" + label, Percent: 5})
	downloadsDir := filepath.Join(m.cfg.RuntimeDir, "downloads")
	if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
		return false, fmt.Errorf("创建下载目录失败：%w", err)
	}
	archivePath := filepath.Join(downloadsDir, archiveName(asset.URL, targetName))
	if err := downloadFile(ctx, asset.URL, archivePath, func(received int64, total int64) {
		percent := 10
		if total > 0 {
			percent = 10 + int(received*50/total)
		}
		m.setProgress(Progress{Running: true, Component: component, Stage: "download", Message: "正在下载" + label, Percent: percent, Received: received, Total: total})
	}); err != nil {
		return false, fmt.Errorf("下载%s失败：%w", label, err)
	}
	m.setProgress(Progress{Running: true, Component: component, Stage: "verify", Message: "正在校验" + label, Percent: 65})
	if err := verifySHA256(archivePath, asset.SHA256); err != nil {
		return false, fmt.Errorf("%s校验失败：%w", label, err)
	}
	m.setProgress(Progress{Running: true, Component: component, Stage: "extract", Message: "正在解压" + label, Percent: 75})
	targetDir := filepath.Join(m.cfg.RuntimeDir, targetName)
	if err := os.RemoveAll(targetDir); err != nil {
		return false, fmt.Errorf("清理旧%s失败：%w", label, err)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return false, fmt.Errorf("创建%s目录失败：%w", label, err)
	}
	if err := extractArchive(archivePath, targetDir); err != nil {
		return false, fmt.Errorf("解压%s失败：%w", label, err)
	}
	if err := m.saveVersion(component, asset); err != nil {
		return false, fmt.Errorf("保存%s版本记录失败：%w", label, err)
	}
	m.setProgress(Progress{Running: true, Component: component, Stage: "installed", Message: label + "安装完成", Percent: 95})
	return true, nil
}

// assetIsCurrent 判断组件文件和版本记录是否与清单一致。
// component 为组件键名，asset 为清单中的组件版本。
func (m *Manager) assetIsCurrent(component string, asset Asset) bool {
	if !m.componentFileExists(component) {
		return false
	}
	installed, ok := m.loadVersions()[component]
	if !ok {
		return false
	}
	if strings.TrimSpace(installed.Version) != strings.TrimSpace(asset.Version) {
		return false
	}
	expectedSHA := strings.TrimSpace(strings.ToLower(asset.SHA256))
	if expectedSHA != "" && strings.TrimSpace(strings.ToLower(installed.SHA256)) != expectedSHA {
		return false
	}
	return true
}

// componentFileExists 判断组件关键文件是否存在。
// component 为组件键名。
func (m *Manager) componentFileExists(component string) bool {
	switch component {
	case "node_runtime":
		return fileExists(m.bundledNodePath())
	case "node_worker":
		return fileExists(m.WorkerEntry()) && fileExists(m.WorkerDependencyPath())
	case "cloakbrowser":
		return fileExists(m.CloakBrowserPath())
	case "ocr":
		return m.ocrInstalled()
	default:
		return false
	}
}

// downloadFile 下载文件到指定路径。
// ctx 为请求上下文，url 为下载地址，targetPath 为保存路径，onProgress 为进度回调。
func downloadFile(ctx context.Context, url string, targetPath string, onProgress func(received int64, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("下载失败，状态码：%d", resp.StatusCode)
	}
	tmpPath := targetPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	reader := &progressReader{reader: resp.Body, total: resp.ContentLength, onProgress: onProgress}
	if _, err := io.Copy(out, reader); err != nil {
		_ = out.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, targetPath)
}

// progressReader 在读取下载内容时回调进度。
type progressReader struct {
	reader     io.Reader
	received   int64
	total      int64
	onProgress func(received int64, total int64)
}

// Read 读取下载内容并更新进度。
// p 为目标缓冲区。
func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.received += int64(n)
		if r.onProgress != nil {
			r.onProgress(r.received, r.total)
		}
	}
	return n, err
}

// verifySHA256 校验文件 sha256。
// expected 为空时跳过校验。
func verifySHA256(path string, expected string) error {
	expected = strings.TrimSpace(strings.ToLower(expected))
	if expected == "" {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != expected {
		return fmt.Errorf("sha256 不一致，期望 %s，实际 %s", expected, actual)
	}
	return nil
}

// extractArchive 解压 zip 或 tar.gz 压缩包。
// archivePath 为压缩包路径，targetDir 为目标目录。
func extractArchive(archivePath string, targetDir string) error {
	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(archivePath, targetDir)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractTarGZ(archivePath, targetDir)
	default:
		return fmt.Errorf("暂不支持的压缩包格式：%s", filepath.Base(archivePath))
	}
}

// extractZip 解压 zip 压缩包。
// archivePath 为压缩包路径，targetDir 为目标目录。
func extractZip(archivePath string, targetDir string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		targetPath, err := safeJoin(targetDir, file.Name)
		if err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		mode := file.FileInfo().Mode()
		if mode == 0 {
			mode = 0o644
		}
		dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			_ = src.Close()
			return err
		}
		_, copyErr := io.Copy(dst, src)
		_ = src.Close()
		_ = dst.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

// extractTarGZ 解压 tar.gz 压缩包。
// archivePath 为压缩包路径，targetDir 为目标目录。
func extractTarGZ(archivePath string, targetDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		targetPath, err := safeJoin(targetDir, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(header.Mode)
			if mode == 0 {
				mode = 0o644
			}
			dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(dst, reader)
			_ = dst.Close()
			if copyErr != nil {
				return copyErr
			}
		}
	}
	return nil
}

// safeJoin 安全拼接解压目标路径。
// targetDir 为目标目录，name 为压缩包内路径。
func safeJoin(targetDir string, name string) (string, error) {
	targetPath := filepath.Join(targetDir, filepath.Clean(name))
	cleanTarget := filepath.Clean(targetDir) + string(os.PathSeparator)
	if !strings.HasPrefix(filepath.Clean(targetPath)+string(os.PathSeparator), cleanTarget) {
		return "", fmt.Errorf("压缩包包含不安全路径：%s", name)
	}
	return targetPath, nil
}

// platformKey 返回当前系统对应的 manifest 平台键。
// 返回值示例：win-x64、darwin-arm64、linux-x64。
func platformKey() string {
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x64"
	}
	if runtime.GOOS == "windows" {
		return "win-" + arch
	}
	return runtime.GOOS + "-" + arch
}

// archiveName 根据 URL 生成下载文件名。
// rawURL 为下载地址，fallback 为兜底文件名。
func archiveName(rawURL string, fallback string) string {
	name := filepath.Base(strings.Split(rawURL, "?")[0])
	if name == "" || name == "." || name == "/" {
		return fallback + ".zip"
	}
	return name
}

// copyDir 递归复制目录。
// sourceDir 为源目录，targetDir 为目标目录。
func copyDir(sourceDir string, targetDir string) error {
	return filepath.WalkDir(sourceDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(targetDir, 0o755)
		}
		targetPath := filepath.Join(targetDir, rel)
		if entry.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			_ = src.Close()
			return err
		}
		dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			_ = src.Close()
			return err
		}
		_, copyErr := io.Copy(dst, src)
		_ = src.Close()
		closeErr := dst.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}
