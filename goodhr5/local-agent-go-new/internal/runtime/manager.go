// Package runtime 检查并启动 Node、TypeScript Worker 和 CloakBrowser 所需运行组件。
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	browserprocess "goodhr5/local-agent-go-new/internal/browser/process"
)

// Manager 负责运行组件状态检查和 Worker 生命周期。
type Manager struct {
	nodePath   string
	worker     *browserprocess.Manager
	entryPath  string
	runtimeDir string
	ocrPath    string
	installMu  sync.Mutex
	progressMu sync.Mutex
	progress   InstallProgress
	settingsMu sync.RWMutex
	licenseKey string
}

// New 创建运行组件管理器。
func New(nodePath string, entryPath string, runtimeDir string, ocrPath string, worker *browserprocess.Manager) *Manager {
	manager := &Manager{
		nodePath: nodePath, entryPath: entryPath, runtimeDir: runtimeDir,
		ocrPath: ocrPath, worker: worker,
		progress: InstallProgress{Stage: "idle", Message: "等待安装"},
	}
	manager.loadPrivateSettings()
	manager.configureWorkerEnvironment()
	return manager
}

// CheckNode 检查 Node.js 二进制是否存在。
func (m *Manager) CheckNode() error {
	nodePath := m.NodePath()
	if !fileExists(nodePath) {
		return fmt.Errorf("Node.js 暂时没找到：%s", nodePath)
	}
	output, err := exec.Command(nodePath, "--version").Output()
	if err != nil {
		return fmt.Errorf("Node.js 版本读取失败：%w", err)
	}
	versionText := strings.TrimPrefix(strings.TrimSpace(string(output)), "v")
	majorText := strings.SplitN(versionText, ".", 2)[0]
	major, err := strconv.Atoi(majorText)
	if err != nil || major < 22 {
		return fmt.Errorf("Node.js 版本需要 22 或更高，当前是 %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// NodePath 返回当前可用的打包 Node.js 或系统 Node.js 路径。
func (m *Manager) NodePath() string {
	if bundled := findFile(filepath.Join(m.runtimeDir, "node"), nodeBinaryName()); bundled != "" {
		return bundled
	}
	if resolved, err := exec.LookPath(m.nodePath); err == nil {
		return resolved
	}
	if system, err := exec.LookPath(nodeBinaryName()); err == nil {
		return system
	}
	return m.nodePath
}

// CheckWorkerBuild 检查编译后的 TypeScript Worker 入口。
func (m *Manager) CheckWorkerBuild() error {
	if _, err := os.Stat(m.entryPath); err != nil {
		return fmt.Errorf("Worker 还没编译好：%w", err)
	}
	if !m.workerDependenciesReady() {
		return fmt.Errorf("Worker 生产依赖还没安装完整，请先安装运行组件")
	}
	return nil
}

// WorkerDependencyPath 返回 Node 解析 Worker 入口时会使用的 CloakBrowser 包路径。
func (m *Manager) WorkerDependencyPath() string {
	current := filepath.Dir(m.entryPath)
	for {
		candidate := filepath.Join(current, "node_modules", "cloakbrowser", "package.json")
		if fileExists(candidate) {
			return candidate
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return filepath.Join(filepath.Dir(filepath.Dir(m.entryPath)), "node_modules", "cloakbrowser", "package.json")
}

// EnsureWorker 检查 Node 和入口后启动 Worker。
func (m *Manager) EnsureWorker(ctx context.Context) error {
	if err := m.CheckNode(); err != nil {
		return err
	}
	if !m.CloakBrowserLicenseConfigured() {
		return fmt.Errorf("还没填写 CloakBrowser Key，请先去个人配置里补上")
	}
	if !fileExists(m.CloakBrowserPath()) {
		return fmt.Errorf("最新版 CloakBrowser 还没安装，请先完成运行组件安装")
	}
	if err := m.CheckWorkerBuild(); err != nil {
		return err
	}
	return m.worker.Start(ctx)
}

// StopWorker 停止 Worker 子进程。
func (m *Manager) StopWorker() error {
	return m.worker.Stop()
}

// Status 返回控制台使用的运行组件路径、安装状态、版本记录和安装进度。
func (m *Manager) Status() Status {
	nodePath := m.NodePath()
	cloakPath := m.CloakBrowserPath()
	ocrPath := m.OCRPath()
	wrapperInstalled := m.cloakBrowserWrapperReady()
	licenseConfigured := m.CloakBrowserLicenseConfigured()
	cloakCached := fileExists(cloakPath)
	cloakInstalled := wrapperInstalled && licenseConfigured && cloakCached
	versions := m.loadVersions()
	cloakVersion := ""
	if installed, ok := versions["cloakbrowser"]; ok && fileExists(installed.Path) {
		cloakVersion = installed.Version
	} else {
		delete(versions, "cloakbrowser")
	}
	return Status{
		Platform:              platformKey(),
		NodeInstalled:         m.CheckNode() == nil,
		NodePath:              nodePath,
		NodeWorkerInstalled:   m.CheckWorkerBuild() == nil,
		WorkerEntry:           m.entryPath,
		WorkerDependency:      m.WorkerDependencyPath(),
		CloakBrowserInstalled: cloakInstalled,
		CloakBrowserCached:    cloakCached,
		CloakBrowserWrapper:   wrapperInstalled,
		CloakBrowserLicensed:  licenseConfigured,
		CloakBrowserPath:      cloakPath,
		CloakBrowserVersion:   cloakVersion,
		OCRInstalled:          m.OCRInstalled(),
		OCRPath:               ocrPath,
		RuntimeDir:            m.runtimeDir,
		InstalledVersions:     versions,
		InstallProgress:       m.InstallProgress(),
	}
}

// OCRPath 返回当前平台可用的 OCR 可执行文件路径。
func (m *Manager) OCRPath() string {
	if fileExists(m.ocrPath) {
		return m.ocrPath
	}
	root := filepath.Join(m.runtimeDir, "ocr")
	names := []string{"RapidOCR-json", "RapidOCR_json", "rapidocr-json"}
	if goruntime.GOOS == "windows" {
		names = []string{"RapidOCR-json.exe", "RapidOCR_json.exe", "rapidocr-json.exe"}
	}
	for _, name := range names {
		if found := findFile(root, name); found != "" {
			return found
		}
	}
	return m.ocrPath
}

// CloakBrowserPath 返回 GoodHR 私有缓存中的官方 CloakBrowser 增强浏览器路径。
func (m *Manager) CloakBrowserPath() string {
	if installed, ok := m.loadVersions()["cloakbrowser"]; ok && fileExists(installed.Path) {
		return installed.Path
	}
	root := filepath.Join(m.runtimeDir, "cloakbrowser")
	patterns := []string{}
	switch goruntime.GOOS {
	case "darwin":
		patterns = append(patterns, filepath.Join(root, "chromium-*-pro", "Chromium.app", "Contents", "MacOS", "Chromium"))
	case "windows":
		patterns = append(patterns, filepath.Join(root, "chromium-*-pro", "chrome.exe"))
	default:
		patterns = append(patterns, filepath.Join(root, "chromium-*-pro", "chrome"))
	}
	candidates := make([]string, 0)
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		for _, match := range matches {
			if fileExists(match) {
				candidates = append(candidates, match)
			}
		}
	}
	sort.Strings(candidates)
	if len(candidates) > 0 {
		return candidates[len(candidates)-1]
	}
	return ""
}

// OCRInstalled 检查 OCR 可执行文件和基础检测模型是否完整。
func (m *Manager) OCRInstalled() bool {
	ocrPath := m.OCRPath()
	if !fileExists(ocrPath) {
		return false
	}
	modelRoot := filepath.Join(filepath.Dir(ocrPath), "models")
	return fileExists(filepath.Join(modelRoot, "ch_PP-OCRv3_det_infer.onnx"))
}

// InstallProgress 返回当前组件安装进度。
func (m *Manager) InstallProgress() InstallProgress {
	m.progressMu.Lock()
	defer m.progressMu.Unlock()
	return m.progress
}

// setInstallProgress 保存当前组件安装进度。
func (m *Manager) setInstallProgress(progress InstallProgress) {
	m.progressMu.Lock()
	defer m.progressMu.Unlock()
	progress.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	m.progress = progress
}

// configureWorkerEnvironment 把 Key、官方缓存目录和稳定通道交给 Worker，并禁用启动时后台下载。
func (m *Manager) configureWorkerEnvironment() {
	if m.worker == nil {
		return
	}
	m.worker.SetExecutable(m.NodePath())
	m.worker.SetEnvironment(
		"CLOAKBROWSER_LICENSE_KEY="+m.cloakBrowserLicenseKey(),
		"CLOAKBROWSER_CACHE_DIR="+filepath.Join(m.runtimeDir, "cloakbrowser"),
		"CLOAKBROWSER_RELEASE_CHANNEL=stable",
		"CLOAKBROWSER_AUTO_UPDATE=false",
		"CLOAKBROWSER_BINARY_PATH=",
		"CLOAKBROWSER_DOWNLOAD_URL=",
		"CLOAKBROWSER_VERSION=",
		"CLOAKBROWSER_SKIP_CHECKSUM=false",
	)
}

// nodeBinaryName 返回当前平台的 Node.js 可执行文件名。
func nodeBinaryName() string {
	if goruntime.GOOS == "windows" {
		return "node.exe"
	}
	return "node"
}

// statePath 返回运行组件安装版本记录路径。
func (m *Manager) statePath() string {
	return filepath.Join(m.runtimeDir, "installed-components.json")
}

// loadVersions 读取已安装运行组件版本记录。
func (m *Manager) loadVersions() map[string]InstalledComponent {
	result := make(map[string]InstalledComponent)
	content, err := os.ReadFile(m.statePath())
	if err != nil {
		return result
	}
	_ = json.Unmarshal(content, &result)
	return result
}

// saveVersion 保存单个运行组件版本记录。
func (m *Manager) saveVersion(component string, asset Asset) error {
	versions := m.loadVersions()
	versions[component] = InstalledComponent{
		Version: asset.Version, URL: asset.URL, SHA256: asset.SHA256,
		InstalledAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	return m.writeVersions(versions)
}

// saveCloakBrowserVersion 保存官方安装后确认的 Chromium 版本和真实路径。
func (m *Manager) saveCloakBrowserVersion(version string, path string) error {
	versions := m.loadVersions()
	versions["cloakbrowser"] = InstalledComponent{
		Version: strings.TrimSpace(version), Path: filepath.Clean(path),
		InstalledAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	return m.writeVersions(versions)
}

// writeVersions 写入当前运行组件版本记录。
func (m *Manager) writeVersions(versions map[string]InstalledComponent) error {
	content, err := json.MarshalIndent(versions, "", "  ")
	if err != nil {
		return fmt.Errorf("编码运行组件版本记录失败：%w", err)
	}
	if err = os.MkdirAll(m.runtimeDir, 0o755); err != nil {
		return fmt.Errorf("创建运行目录失败：%w", err)
	}
	return os.WriteFile(m.statePath(), content, 0o644)
}

// fileExists 判断路径是否为普通文件。
func fileExists(path string) bool {
	info, err := os.Stat(strings.TrimSpace(path))
	return err == nil && !info.IsDir()
}

// findFile 在指定目录内递归查找文件名。
func findFile(root string, name string) string {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(name) == "" {
		return ""
	}
	found := ""
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if !entry.IsDir() && entry.Name() == name {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}
