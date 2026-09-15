// 文件作用说明：模拟用户第一次安装——按云端清单下载 Camoufox 浏览器并校验落盘结果。真实下载约 380MB，仅发布前校验镜像时使用。

package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSimulateFirstInstallCamoufox 走 StartInstall 完整链路：下载、SHA256 校验、解压并补写版本文件。
// 真实下载约 380MB，仅在设置 GOODHR_SIMULATE_FIRST_INSTALL=1 时运行，用于发布前校验镜像地址与校验值。
func TestSimulateFirstInstallCamoufox(t *testing.T) {
	if os.Getenv("GOODHR_SIMULATE_FIRST_INSTALL") != "1" {
		t.Skip("需要真实下载 380MB 镜像，设置 GOODHR_SIMULATE_FIRST_INSTALL=1 后运行")
	}
	runtimeDir, err := filepath.Abs(filepath.Join("..", "..", ".smoke-runtime"))
	if err != nil {
		t.Fatalf("解析安装目录失败：%v", err)
	}
	if err := os.RemoveAll(runtimeDir); err != nil {
		t.Fatalf("清理旧安装目录失败：%v", err)
	}
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatalf("创建安装目录失败：%v", err)
	}
	t.Cleanup(func() {
		// 保留安装目录供 Worker 验证步骤使用，如需清理请手动删除 .smoke-runtime。
		_ = os.RemoveAll(filepath.Join(runtimeDir, "downloads"))
	})
	manager := New("", "", runtimeDir, "", nil)
	manifest := Manifest{
		Camoufox: map[string]Asset{
			"darwin-arm64": {
				Version: "152.0.4-beta.30",
				URL:     "https://github.com/daijro/camoufox/releases/download/v152.0.4-beta.30/camoufox-152.0.4-beta.30-mac.arm64.zip",
				SHA256:  "3b43e766574f286a6a63296cf58b660b7a3120952086c869b4df4c9a71604bc3",
			},
		},
	}
	if _, err := manager.StartInstall(manifest); err != nil {
		t.Fatalf("StartInstall() error = %v", err)
	}
	deadline := time.Now().Add(30 * time.Minute)
	lastPercent := -1
	for {
		progress := manager.InstallProgress()
		if progress.Percent != lastPercent {
			t.Logf("进度 [%s] %s %d%% received=%d total=%d message=%s",
				progress.Component, progress.Stage, progress.Percent,
				progress.Received, progress.Total, progress.Message)
			lastPercent = progress.Percent
		}
		if !progress.Running {
			if progress.Stage != "installed" {
				t.Fatalf("安装未成功：stage=%s message=%s", progress.Stage, progress.Message)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("安装超时：stage=%s message=%s", progress.Stage, progress.Message)
		}
		time.Sleep(1 * time.Second)
	}

	binaryPath := manager.CamoufoxPath()
	if binaryPath == "" {
		t.Fatal("CamoufoxPath() 未找到浏览器可执行文件")
	}
	if _, err := os.Stat(binaryPath); err != nil {
		t.Fatalf("浏览器可执行文件不存在：%v", err)
	}
	versionContent, err := os.ReadFile(filepath.Join(runtimeDir, "camoufox", "version.json"))
	if err != nil {
		t.Fatalf("version.json 不存在：%v", err)
	}
	var version map[string]string
	if err := json.Unmarshal(versionContent, &version); err != nil {
		t.Fatalf("version.json 解析失败：%v", err)
	}
	if version["version"] != "152.0.4" || version["release"] != "beta.30" {
		t.Fatalf("version.json 内容不符：%s", versionContent)
	}
	// 把安装目录写给后续的 Worker 启动验证步骤使用（worker/camoufox-launch-verify.mjs 读取）。
	markerPath := filepath.Join("..", "..", "worker", ".smoke-runtime-dir.txt")
	if err := os.WriteFile(markerPath, []byte(runtimeDir), 0o644); err != nil {
		t.Fatalf("记录安装目录失败：%v", err)
	}
	t.Logf("首次安装模拟成功，runtimeDir=%s", runtimeDir)
}
