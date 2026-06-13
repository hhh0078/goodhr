// Package runtime 负责管理 Node Worker 和 CloakBrowser 运行组件。
package runtime

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"goodhr5/local-agent-go/internal/config"
)

// Status 表示本地运行组件状态。
type Status struct {
	NodeInstalled         bool                          `json:"node_installed"`
	NodePath              string                        `json:"node_path"`
	WorkerInstalled       bool                          `json:"worker_installed"`
	WorkerEntry           string                        `json:"worker_entry"`
	WorkerDependencyPath  string                        `json:"worker_dependency_path"`
	CloakBrowserInstalled bool                          `json:"cloakbrowser_installed"`
	CloakBrowserPath      string                        `json:"cloakbrowser_path"`
	OCRInstalled          bool                          `json:"ocr_installed"`
	OCRPath               string                        `json:"ocr_path"`
	RuntimeDir            string                        `json:"runtime_dir"`
	InstallProgress       Progress                      `json:"install_progress"`
	InstalledVersions     map[string]InstalledComponent `json:"installed_versions"`
}

// Manager 管理本地运行组件路径和安装状态。
type Manager struct {
	cfg       *config.Config
	mu        sync.Mutex
	installMu sync.Mutex
	progress  Progress
}

// Progress 表示运行组件安装进度。
type Progress struct {
	Running   bool   `json:"running"`
	Component string `json:"component"`
	Stage     string `json:"stage"`
	Message   string `json:"message"`
	Percent   int    `json:"percent"`
	Received  int64  `json:"received"`
	Total     int64  `json:"total"`
	UpdatedAt string `json:"updated_at"`
}

// InstalledComponent 表示已安装运行组件版本。
type InstalledComponent struct {
	Version     string `json:"version"`
	URL         string `json:"url"`
	SHA256      string `json:"sha256"`
	InstalledAt string `json:"installed_at"`
}

// NewManager 创建运行组件管理器。
// cfg 为本地程序配置。
func NewManager(cfg *config.Config) *Manager {
	return &Manager{cfg: cfg}
}

// Status 返回当前运行组件安装状态。
// 返回值用于前端展示和初始化流程判断。
func (m *Manager) Status() Status {
	nodePath := m.NodePath()
	workerEntry := m.WorkerEntry()
	workerDependencyPath := m.WorkerDependencyPath()
	browserPath := m.CloakBrowserPath()
	ocrPath := m.OCRPath()
	return Status{
		NodeInstalled:         fileExists(nodePath),
		NodePath:              nodePath,
		WorkerInstalled:       fileExists(workerEntry) && fileExists(workerDependencyPath),
		WorkerEntry:           workerEntry,
		WorkerDependencyPath:  workerDependencyPath,
		CloakBrowserInstalled: fileExists(browserPath),
		CloakBrowserPath:      browserPath,
		OCRInstalled:          fileExists(ocrPath),
		OCRPath:               ocrPath,
		RuntimeDir:            m.cfg.RuntimeDir,
		InstallProgress:       m.Progress(),
		InstalledVersions:     m.loadVersions(),
	}
}

// OCRPath 返回 OCR 可执行文件路径。
// Windows 优先返回 exe，其他系统返回无后缀可执行文件。
func (m *Manager) OCRPath() string {
	names := []string{"RapidOCR-json", "RapidOCR_json", "rapidocr-json"}
	if runtime.GOOS == "windows" {
		names = []string{"RapidOCR-json.exe", "RapidOCR_json.exe", "rapidocr-json.exe"}
	}
	for _, name := range names {
		path := filepath.Join(m.cfg.RuntimeDir, "ocr", name)
		if fileExists(path) {
			return path
		}
	}
	if found := findFile(filepath.Join(m.cfg.RuntimeDir, "ocr"), names[0]); found != "" {
		return found
	}
	return filepath.Join(m.cfg.RuntimeDir, "ocr", names[0])
}

// Progress 返回当前运行组件安装进度。
// 返回值用于前端轮询展示。
func (m *Manager) Progress() Progress {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.progress
}

// setProgress 更新当前安装进度。
// progress 为新的安装进度。
func (m *Manager) setProgress(progress Progress) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if progress.UpdatedAt == "" {
		progress.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	m.progress = progress
}

// statePath 返回运行组件版本记录文件路径。
// 返回值位于运行组件目录。
func (m *Manager) statePath() string {
	return filepath.Join(m.cfg.RuntimeDir, "installed-components.json")
}

// loadVersions 读取已安装运行组件版本。
// 读取失败时返回空字典。
func (m *Manager) loadVersions() map[string]InstalledComponent {
	result := map[string]InstalledComponent{}
	raw, err := os.ReadFile(m.statePath())
	if err != nil {
		return result
	}
	_ = json.Unmarshal(raw, &result)
	return result
}

// saveVersion 保存单个运行组件版本记录。
// name 为组件名，asset 为安装资源配置。
func (m *Manager) saveVersion(name string, asset Asset) error {
	versions := m.loadVersions()
	versions[name] = InstalledComponent{
		Version:     asset.Version,
		URL:         asset.URL,
		SHA256:      asset.SHA256,
		InstalledAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	raw, err := json.MarshalIndent(versions, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.statePath(), raw, 0o644)
}

// Ensure 检查运行组件是否已安装。
// 当前阶段只检查状态，后续会接入 OSS manifest 下载和 sha256 校验。
func (m *Manager) Ensure() (Status, error) {
	status := m.Status()
	if !status.NodeInstalled {
		return status, errors.New("Node 运行组件未安装，请先下载运行组件")
	}
	if !status.CloakBrowserInstalled {
		return status, errors.New("CloakBrowser 未安装，请先下载浏览器组件")
	}
	return status, nil
}

// NodePath 返回 Node 可执行文件路径。
// Windows 返回 node.exe，其他系统返回 node。
func (m *Manager) NodePath() string {
	if found := m.bundledNodePath(); found != "" {
		return found
	}
	if found := systemNodePath(); found != "" {
		return found
	}
	return filepath.Join(m.cfg.RuntimeDir, "node", nodeBinaryName())
}

// WorkerEntry 返回内置浏览器控制组件入口文件路径。
// 优先使用本地程序自带 worker-node，兼容旧运行目录里的 browser-worker。
func (m *Manager) WorkerEntry() string {
	root := m.workerRoot()
	for _, name := range []string{"index.js", filepath.Join("dist", "index.js"), filepath.Join("src", "index.js")} {
		path := filepath.Join(root, name)
		if fileExists(path) {
			return path
		}
	}
	if found := findFile(root, "index.js"); found != "" {
		return found
	}
	return filepath.Join(root, "index.js")
}

// WorkerDependencyPath 返回浏览器控制组件依赖 cloakbrowser 的 package.json 路径。
// 返回值用于诊断内置组件是否完整。
func (m *Manager) WorkerDependencyPath() string {
	return filepath.Join(m.workerRoot(), "node_modules", "cloakbrowser", "package.json")
}

// CloakBrowserPath 返回当前系统 CloakBrowser 可执行文件路径。
// 路径规则与 Python 版本保持兼容。
func (m *Manager) CloakBrowserPath() string {
	root := filepath.Join(m.cfg.RuntimeDir, "cloakbrowser")
	switch runtime.GOOS {
	case "darwin":
		if found := findFile(root, "Chromium"); found != "" && strings.Contains(found, "Chromium.app") {
			return found
		}
		return filepath.Join(root, "Chromium.app", "Contents", "MacOS", "Chromium")
	case "windows":
		for _, name := range []string{"chrome.exe", "chromium.exe"} {
			if found := findFile(root, name); found != "" {
				return found
			}
		}
		return filepath.Join(root, "chrome.exe")
	default:
		for _, name := range []string{"chrome", "chromium"} {
			if found := findFile(root, name); found != "" {
				return found
			}
		}
		return filepath.Join(root, "chrome")
	}
}

// fileExists 判断文件是否存在。
// path 为空时返回 false。
func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// findFile 在目录中递归查找指定文件名。
// root 为搜索目录，name 为文件名，找不到时返回空字符串。
func findFile(root string, name string) string {
	if root == "" || name == "" {
		return ""
	}
	var found string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" || d.IsDir() {
			return nil
		}
		if d.Name() == name {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// bundledNodePath 返回 GoodHR 自带 Node 可执行文件路径。
// 找不到时返回空字符串，调用方可继续尝试系统 Node。
func (m *Manager) bundledNodePath() string {
	root := filepath.Join(m.cfg.RuntimeDir, "node")
	return findFile(root, nodeBinaryName())
}

// systemNodePath 返回用户系统 PATH 中的 Node 可执行文件路径。
// 找不到时返回空字符串。
func systemNodePath() string {
	path, err := exec.LookPath(nodeBinaryName())
	if err != nil {
		return ""
	}
	return path
}

// nodeBinaryName 返回当前系统 Node 可执行文件名。
func nodeBinaryName() string {
	if runtime.GOOS == "windows" {
		return "node.exe"
	}
	return "node"
}

// workerRoot 返回浏览器控制组件目录。
// 正式包优先把 worker-node 放在本地程序旁边；开发环境兼容仓库目录。
func (m *Manager) workerRoot() string {
	candidates := []string{}
	if value := strings.TrimSpace(os.Getenv("GOODHR_WORKER_DIR")); value != "" {
		candidates = append(candidates, value)
	}
	if execPath, err := os.Executable(); err == nil {
		execDir := filepath.Dir(execPath)
		candidates = append(candidates,
			filepath.Join(execDir, "worker-node"),
			filepath.Join(execDir, "resources", "worker-node"),
		)
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "worker-node"),
			filepath.Join(wd, "goodhr5", "local-agent-go", "worker-node"),
		)
	}
	candidates = append(candidates, filepath.Join(m.cfg.RuntimeDir, "browser-worker"))
	for _, candidate := range candidates {
		if fileExists(filepath.Join(candidate, "src", "index.js")) ||
			fileExists(filepath.Join(candidate, "index.js")) ||
			fileExists(filepath.Join(candidate, "dist", "index.js")) {
			return candidate
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return filepath.Join(m.cfg.RuntimeDir, "browser-worker")
}
