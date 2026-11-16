// Package runtime 文件作用：统一检查系统运行库与 Worker 原生组件，供组件页面和启动保护复用。
package runtime

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const visualCppDownloadURL = "https://aka.ms/vc14/vc_redist.x64.exe"

// SystemRuntimeStatus 返回系统运行库状态；refresh 为 true 时忽略缓存重新检查。
func (m *Manager) SystemRuntimeStatus(refresh bool) SystemRuntimeStatus {
	m.systemMu.Lock()
	defer m.systemMu.Unlock()
	if m.systemChecked && !refresh {
		return m.systemStatus
	}
	m.systemStatus = checkSystemRuntime(m.NodePath(), m.entryPath)
	m.systemChecked = true
	return m.systemStatus
}

// InvalidateSystemRuntimeStatus 在 Node 或 Worker 组件变化后清除旧检查结果。
func (m *Manager) InvalidateSystemRuntimeStatus() {
	m.systemMu.Lock()
	defer m.systemMu.Unlock()
	m.systemChecked = false
	m.systemStatus = SystemRuntimeStatus{}
}

// checkSystemRuntime 先检查系统库，再用已安装的 Node 实际加载 impit 原生组件。
func checkSystemRuntime(nodePath string, workerEntry string) SystemRuntimeStatus {
	status := platformSystemRuntimeStatus()
	if !status.Required || !status.Ready {
		return status
	}
	if !fileExists(nodePath) || !fileExists(workerEntry) {
		return status
	}
	workerRoot := filepath.Dir(filepath.Dir(workerEntry))
	impitEntry := filepath.Join(workerRoot, "node_modules", "impit", "index.wrapper.js")
	if !fileExists(impitEntry) {
		return SystemRuntimeStatus{
			Required: true, Code: "NATIVE_COMPONENT_MISSING", Name: status.Name,
			Message:     "浏览器原生组件文件不完整，请重新安装 GoodHR 后再试",
			DownloadURL: status.DownloadURL,
		}
	}
	checkCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	output, err := nativeModuleCheck(checkCtx, nodePath, impitEntry)
	if err == nil {
		return status
	}
	message := "Microsoft Visual C++ x64 运行库没有正常工作，请安装或修复后重新检查"
	if checkCtx.Err() != nil {
		message = "检查 Windows 运行库超时，请关闭安全软件拦截提示后重新检查"
	} else if strings.Contains(strings.ToLower(string(output)), "module could not be found") {
		message = "Windows 缺少原生组件依赖的运行库，请安装或修复 Microsoft Visual C++ x64 运行库"
	}
	return SystemRuntimeStatus{
		Required: true, Code: "NATIVE_COMPONENT_LOAD_FAILED", Name: status.Name,
		Message: message, DownloadURL: status.DownloadURL,
	}
}

// nativeModuleCheck 使用与 Worker 相同的 Node 实际加载 impit，不启动浏览器或监听端口。
func nativeModuleCheck(ctx context.Context, nodePath string, impitEntry string) ([]byte, error) {
	script := "require(process.argv[1])"
	return commandCombinedOutput(ctx, []string{"IMPIT_VERBOSE=1"}, nodePath, "-e", script, impitEntry)
}

// missingVisualCppLibraries 返回 System32 中缺少的核心 x64 Visual C++ 运行库文件。
func missingVisualCppLibraries(systemRoot string) []string {
	root := filepath.Join(systemRoot, "System32")
	required := []string{"msvcp140.dll", "vcruntime140.dll", "vcruntime140_1.dll"}
	missing := make([]string, 0, len(required))
	for _, name := range required {
		if !fileExists(filepath.Join(root, name)) {
			missing = append(missing, name)
		}
	}
	return missing
}

// defaultSystemRoot 返回 Windows 系统目录根路径，空值时使用标准目录名。
func defaultSystemRoot() string {
	if root := strings.TrimSpace(os.Getenv("SystemRoot")); root != "" {
		return root
	}
	if runtime.GOOS == "windows" {
		return `C:\Windows`
	}
	return ""
}
