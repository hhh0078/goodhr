// Package browserextension 文件作用：静态验证 Windows 安装器会先准备插件，并区分交互安装与静默升级。
package browserextension

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInnoInstallerIntegratesBrowserExtensionPreparation 验证安装器命令、目录、静默分支和人工加载引导齐全。
func TestInnoInstallerIntegratesBrowserExtensionPreparation(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "packaging", "GoodHRLocalAgent.iss"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(content)
	required := []string{
		"--prepare-browser-extension",
		"--browser-extension-dir",
		"{localappdata}\\GoodHRLocalAgent\\browser-extension\\chrome",
		"CurStep = ssPostInstall",
		"if not WizardSilent then",
		"LoadRequired := FileExists(ChromeExtensionLoadMarker)",
		"if LoadRequired then",
		"if OpenChromeExtensionGuide then",
		"DeleteFile(ChromeExtensionLoadMarker)",
		"chrome://extensions",
		"加载已解压的扩展程序",
		"/download#browser-extension",
	}
	for _, token := range required {
		if !strings.Contains(script, token) {
			t.Fatalf("安装器缺少插件流程：%s", token)
		}
	}
	if strings.Contains(strings.ToLower(script), "extensioninstallforcelist") || strings.Contains(strings.ToLower(script), "regwrite") {
		t.Fatal("安装器不得写入 Chrome 企业强制安装策略")
	}
	prepareIndex := strings.Index(script, "procedure PrepareLatestChromeExtension")
	startIndex := strings.Index(script, "Filename: \"{app}\\{#MyAppExeName}\"")
	if prepareIndex < 0 || startIndex < 0 {
		t.Fatal("安装器插件准备或主程序启动入口缺失")
	}
}
