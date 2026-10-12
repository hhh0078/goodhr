// Package browserextension 文件作用：静态验证 Windows 安装器只启动 GoodHR 页面，不尝试安装 Chrome 插件。
package browserextension

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInnoInstallerStartsAgentWithoutChromeExtensionPreparation 验证安装完成后直接启动 Agent，由 Agent 打开原有控制台页面。
func TestInnoInstallerStartsAgentWithoutChromeExtensionPreparation(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "packaging", "GoodHRLocalAgent.iss"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(content)
	required := []string{"Filename: \"{app}\\{#MyAppExeName}\"", "安装完成后立即运行 GoodHR 小助手", "Flags: nowait postinstall"}
	for _, token := range required {
		if !strings.Contains(script, token) {
			t.Fatalf("安装器缺少原有启动流程：%s", token)
		}
	}
	for _, forbidden := range []string{
		"--prepare-browser-extension", "--browser-extension-dir", "ChromeExtensionDir", "PrepareLatestChromeExtension",
		"OpenChromeExtensionGuide", "chrome://extensions", "browser-extension\\chrome", "ExtensionInstallForcelist",
	} {
		if strings.Contains(strings.ToLower(script), strings.ToLower(forbidden)) {
			t.Fatalf("安装器仍会尝试安装或引导加载 Chrome 插件：%s", forbidden)
		}
	}
}

// TestWindowsPackageDoesNotPassChromeGuideURL 验证打包脚本不再向安装器传入只服务于 Chrome 引导页的参数。
func TestWindowsPackageDoesNotPassChromeGuideURL(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "package-windows.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "MyPublicBaseURL") {
		t.Fatal("Windows 打包脚本仍会向安装器传入 Chrome 引导页地址")
	}
}
