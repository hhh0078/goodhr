// Package runtime 检查并启动 Node、TypeScript Worker 和 Camoufox 所需运行组件。
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
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
	// versionsMu 避免 Windows 状态轮询读取版本文件时阻挡该文件的原子替换。
	versionsMu        sync.RWMutex
	progressMu        sync.Mutex
	progress          InstallProgress
	systemMu          sync.Mutex
	systemChecked     bool
	systemStatus      SystemRuntimeStatus
	nativeInputMu     sync.Mutex
	nativeInputStatus NativeInputSafetyStatus
}

// New 创建运行组件管理器。
func New(nodePath string, entryPath string, runtimeDir string, ocrPath string, worker *browserprocess.Manager) *Manager {
	manager := &Manager{
		nodePath: nodePath, entryPath: entryPath, runtimeDir: runtimeDir,
		ocrPath: ocrPath, worker: worker,
		progress: InstallProgress{Stage: "idle", Message: "等待安装"},
	}
	manager.ensureCamoufoxInterface()
	manager.configureWorkerEnvironment()
	return manager
}

// ensureCamoufoxInterface 在已有组件中补齐中文语言包、标签交互、书签栏与原生下载通道。
func (m *Manager) ensureCamoufoxInterface() {
	if !fileExists(m.CamoufoxPath()) {
		m.setNativeInputSafetyStatus(NativeInputSafetyStatus{ExpectedVersion: nativeInputSafetyVersion, InstalledVersion: "missing", Error: "浏览器尚未安装"})
		return
	}
	camoufoxDir := filepath.Join(m.runtimeDir, "camoufox")
	patchErr := EnsureCamoufoxMouseSafety(m.CamoufoxPath())
	if patchErr != nil {
		log.Printf("[运行组件] 浏览器鼠标防卡死修正没有完成 err=%v", patchErr)
	}
	if err := EnsureCamoufoxTabInteraction(camoufoxDir); err != nil {
		log.Printf("[运行组件] 浏览器标签交互修正没有完成 err=%v", err)
	}
	if err := EnsureCamoufoxBookmarkToolbar(camoufoxDir); err != nil {
		log.Printf("[运行组件] 浏览器书签栏策略修正没有完成 err=%v", err)
	}
	if err := EnsureCamoufoxBookmarkWidget(camoufoxDir); err != nil {
		log.Printf("[运行组件] 浏览器书签组件位置修正没有完成 err=%v", err)
	}
	if err := EnsureCamoufoxNativeDownloads(camoufoxDir); err != nil {
		log.Printf("[运行组件] 浏览器原生下载通道没有装上 err=%v", err)
	}
	if err := EnsureChineseLanguagePack(camoufoxDir); err != nil {
		log.Printf("[运行组件] 中文语言包没有装上，浏览器界面会保持英文 err=%v", err)
	}
	status := InspectCamoufoxMouseSafety(m.CamoufoxPath())
	if patchErr != nil {
		status.Ready = false
		status.Error = patchErr.Error()
	}
	m.setNativeInputSafetyStatus(status)
}

// setNativeInputSafetyStatus 保存资源检查结果，允许组件安装与状态查询并发运行。
func (m *Manager) setNativeInputSafetyStatus(status NativeInputSafetyStatus) {
	m.nativeInputMu.Lock()
	defer m.nativeInputMu.Unlock()
	m.nativeInputStatus = status
}

// inputSafetyStatus 返回 Worker 启动前资源检查的副本。
func (m *Manager) inputSafetyStatus() NativeInputSafetyStatus {
	m.nativeInputMu.Lock()
	defer m.nativeInputMu.Unlock()
	return m.nativeInputStatus
}

// CheckNode 检查当前平台 Node.js 文件能够执行且主版本符合 Worker 要求。
func (m *Manager) CheckNode() error {
	return checkNodeExecutable(context.Background(), m.NodePath())
}

// NodePath 优先使用已验证的新安装路径，并兼容旧打包目录和系统 Node.js。
func (m *Manager) NodePath() string {
	if recorded := m.loadVersions()["node_runtime"].ExecutablePath; recorded != "" {
		if path, err := safeJoin(m.runtimeDir, recorded); err == nil && fileExists(path) {
			return path
		}
	}
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
	if dependency := m.WorkerDependencyPath(); !fileExists(dependency) {
		return fmt.Errorf("Worker 缺少 Camoufox Node 依赖：%s", dependency)
	}
	return nil
}

// WorkerDependencyPath 返回 Node 解析 Worker 入口时会使用的 Camoufox 客户端包路径。
func (m *Manager) WorkerDependencyPath() string {
	current := filepath.Dir(m.entryPath)
	for {
		candidate := filepath.Join(current, "node_modules", "camoufox-js", "package.json")
		if fileExists(candidate) {
			return candidate
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return filepath.Join(filepath.Dir(filepath.Dir(m.entryPath)), "node_modules", "camoufox-js", "package.json")
}

// EnsureWorker 检查 Node 和入口后启动 Worker。
func (m *Manager) EnsureWorker(ctx context.Context) error {
	if err := m.CheckNode(); err != nil {
		return err
	}
	if err := m.CheckWorkerBuild(); err != nil {
		return err
	}
	if status := m.SystemRuntimeStatus(true); status.Required && !status.Ready {
		return fmt.Errorf("%s", status.Message)
	}
	if fileExists(m.CamoufoxPath()) {
		if err := EnsureBundledUBO(filepath.Join(m.runtimeDir, "camoufox")); err != nil {
			return fmt.Errorf("浏览器内置扩展没准备好，请重试：%w", err)
		}
	}
	return m.worker.Start(ctx)
}

// StopWorker 停止 Worker 子进程。
func (m *Manager) StopWorker() error {
	return m.worker.Stop()
}

// PrepareWorkerStop 在任务收尾前通知 Worker 管理器，本次退出由主程序主动触发。
func (m *Manager) PrepareWorkerStop(reason string) {
	m.worker.PrepareStop(reason)
}

// Status 返回控制台使用的运行组件路径、安装状态、版本记录和安装进度。
func (m *Manager) Status() Status {
	nodePath := m.NodePath()
	browserPath := m.CamoufoxPath()
	ocrPath := m.OCRPath()
	return Status{
		NodeInstalled:       m.CheckNode() == nil,
		NodePath:            nodePath,
		NodeWorkerInstalled: m.CheckWorkerBuild() == nil,
		WorkerEntry:         m.entryPath,
		WorkerDependency:    m.WorkerDependencyPath(),
		CamoufoxInstalled:   fileExists(browserPath),
		CamoufoxPath:        browserPath,
		OCRInstalled:        m.OCRInstalled(),
		OCRPath:             ocrPath,
		RuntimeDir:          m.runtimeDir,
		Platform:            platformKey(),
		InstalledVersions:   m.loadVersions(),
		InstallProgress:     m.InstallProgress(),
		SystemRuntime:       m.SystemRuntimeStatus(false),
		NativeInputSafety:   m.inputSafetyStatus(),
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

// CamoufoxPath 返回本地运行目录中的 Camoufox 浏览器可执行文件路径。
func (m *Manager) CamoufoxPath() string {
	if value := strings.TrimSpace(os.Getenv("CAMOUFOX_BINARY_PATH")); value != "" {
		return value
	}
	root := filepath.Join(m.runtimeDir, "camoufox")
	switch goruntime.GOOS {
	case "darwin":
		return firstExistingFile(
			filepath.Join(root, "Camoufox.app", "Contents", "MacOS", "camoufox"),
			findFile(root, "camoufox"),
		)
	case "windows":
		return firstExistingFile(findFile(root, "camoufox.exe"), findFile(root, "camoufox"))
	default:
		return firstExistingFile(findFile(root, "camoufox-bin"), findFile(root, "camoufox"))
	}
}

// OCRInstalled 检查 OCR 可执行文件；Windows 额外检查模型，macOS 使用系统 Vision。
func (m *Manager) OCRInstalled() bool {
	ocrPath := m.OCRPath()
	if !fileExists(ocrPath) {
		return false
	}
	if goruntime.GOOS == "darwin" {
		info, err := os.Stat(ocrPath)
		return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
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

// configureWorkerEnvironment 把本地运行目录中的 Camoufox 路径交给 Worker。
func (m *Manager) configureWorkerEnvironment() {
	if m.worker == nil {
		return
	}
	m.worker.SetExecutable(m.NodePath())
	browserPath := m.CamoufoxPath()
	camoufoxDir := filepath.Join(m.runtimeDir, "camoufox")
	environment := []string{
		"CAMOUFOX_INSTALL_DIR=" + camoufoxDir,
		"GOODHR_UBO_EXTENSION_PATH=" + bundledUBOPath(camoufoxDir, goruntime.GOOS),
	}
	if status, err := json.Marshal(m.inputSafetyStatus()); err == nil {
		environment = append(environment, "GOODHR_NATIVE_INPUT_SAFETY="+string(status))
	}
	if fileExists(browserPath) {
		environment = append(environment, "CAMOUFOX_BINARY_PATH="+browserPath)
	}
	// camoufox-js 依赖 INSTALL_DIR 解析启动文件和版本信息，必须指向组件根目录。
	m.worker.SetEnvironment(environment...)
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
	m.versionsMu.RLock()
	defer m.versionsMu.RUnlock()
	result := make(map[string]InstalledComponent)
	content, err := os.ReadFile(m.statePath())
	if err != nil {
		return result
	}
	if err := json.Unmarshal(content, &result); err != nil || result == nil {
		return make(map[string]InstalledComponent)
	}
	return result
}

// saveVersion 原子保存组件版本与相对可执行路径，失败时保留旧安装记录。
func (m *Manager) saveVersion(component string, asset Asset, executablePath string) error {
	versions := m.loadVersions()
	versions[component] = InstalledComponent{
		Version: asset.Version, URL: asset.URL, SHA256: asset.SHA256,
		InstalledAt:    time.Now().UTC().Format(time.RFC3339Nano),
		ExecutablePath: executablePath,
	}
	content, err := json.MarshalIndent(versions, "", "  ")
	if err != nil {
		return fmt.Errorf("编码运行组件版本记录失败：%w", err)
	}
	if err = os.MkdirAll(m.runtimeDir, 0o755); err != nil {
		return fmt.Errorf("创建运行目录失败：%w", err)
	}
	temporary, err := os.CreateTemp(m.runtimeDir, ".components-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err = temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	m.versionsMu.Lock()
	defer m.versionsMu.Unlock()
	return os.Rename(temporary.Name(), m.statePath())
}

// fileExists 判断路径是否为普通文件。
func fileExists(path string) bool {
	info, err := os.Stat(strings.TrimSpace(path))
	return err == nil && !info.IsDir()
}

// firstExistingFile 返回第一个存在的文件路径。
func firstExistingFile(paths ...string) string {
	for _, path := range paths {
		if fileExists(path) {
			return path
		}
	}
	if len(paths) > 0 {
		return paths[0]
	}
	return ""
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
