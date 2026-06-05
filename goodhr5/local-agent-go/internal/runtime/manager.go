// Package runtime 负责管理 Node Worker 和 CloakBrowser 运行组件。
package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"goodhr5/local-agent-go/internal/config"
)

// Status 表示本地运行组件状态。
type Status struct {
	NodeInstalled         bool   `json:"node_installed"`
	NodePath              string `json:"node_path"`
	WorkerInstalled       bool   `json:"worker_installed"`
	WorkerEntry           string `json:"worker_entry"`
	CloakBrowserInstalled bool   `json:"cloakbrowser_installed"`
	CloakBrowserPath      string `json:"cloakbrowser_path"`
	RuntimeDir            string `json:"runtime_dir"`
}

// Manager 管理本地运行组件路径和安装状态。
type Manager struct {
	cfg *config.Config
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
	browserPath := m.CloakBrowserPath()
	return Status{
		NodeInstalled:         fileExists(nodePath),
		NodePath:              nodePath,
		WorkerInstalled:       fileExists(workerEntry),
		WorkerEntry:           workerEntry,
		CloakBrowserInstalled: fileExists(browserPath),
		CloakBrowserPath:      browserPath,
		RuntimeDir:            m.cfg.RuntimeDir,
	}
}

// Ensure 检查运行组件是否已安装。
// 当前阶段只检查状态，后续会接入 OSS manifest 下载和 sha256 校验。
func (m *Manager) Ensure() (Status, error) {
	status := m.Status()
	if !status.NodeInstalled {
		return status, errors.New("Node 运行组件未安装，请先下载运行组件")
	}
	if !status.WorkerInstalled {
		return status, errors.New("Node Browser Worker 未安装，请先下载运行组件")
	}
	if !status.CloakBrowserInstalled {
		return status, errors.New("CloakBrowser 未安装，请先下载浏览器组件")
	}
	return status, nil
}

// NodePath 返回 Node 可执行文件路径。
// Windows 返回 node.exe，其他系统返回 node。
func (m *Manager) NodePath() string {
	name := "node"
	if runtime.GOOS == "windows" {
		name = "node.exe"
	}
	return filepath.Join(m.cfg.RuntimeDir, "node", name)
}

// WorkerEntry 返回 Node Worker 入口文件路径。
// 入口文件由 worker-node 构建后放入运行目录。
func (m *Manager) WorkerEntry() string {
	return filepath.Join(m.cfg.RuntimeDir, "browser-worker", "index.js")
}

// CloakBrowserPath 返回当前系统 CloakBrowser 可执行文件路径。
// 路径规则与 Python 版本保持兼容。
func (m *Manager) CloakBrowserPath() string {
	root := filepath.Join(m.cfg.RuntimeDir, "cloakbrowser")
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(root, "Chromium.app", "Contents", "MacOS", "Chromium")
	case "windows":
		return filepath.Join(root, "chrome.exe")
	default:
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
