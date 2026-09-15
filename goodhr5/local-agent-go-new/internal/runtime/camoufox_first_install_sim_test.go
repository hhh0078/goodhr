// 文件作用说明：模拟用户第一次安装——按云端清单下载 Camoufox 浏览器与 GeoIP 数据库并校验落盘结果。（临时模拟测试，验证后删除）

package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSimulateFirstInstallCamoufoxAndGeoIP 走 StartInstall 完整链路：下载、校验、解压、补版本文件、补 GeoIP 数据库。
// 真实下载约 380MB，仅在设置 GOODHR_SIMULATE_FIRST_INSTALL=1 时运行，用于发布前校验镜像地址与校验值。
func TestSimulateFirstInstallCamoufoxAndGeoIP(t *testing.T) {
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
		GeoIP: map[string]Asset{
			"darwin-arm64": {
				Version: "2026.09.13",
				URL:     "https://github.com/P3TERX/GeoLite.mmdb/releases/download/2026.09.13/GeoLite2-City.mmdb",
				SHA256:  "04f2ac880ad5f25cd0aef185038156ffd8758577df13c9ed38e3f4e9068e35e3",
			},
		},
	}
	if _, err := manager.StartInstall(manifest); err != nil {
		t.Fatalf("StartInstall() error = %v", err)
	}
	deadline := time.Now().Add(20 * time.Minute)
	for {
		progress := manager.InstallProgress()
		if !progress.Running {
			if progress.Stage != "installed" {
				t.Fatalf("安装未成功：stage=%s message=%s", progress.Stage, progress.Message)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("安装超时：stage=%s message=%s", progress.Stage, progress.Message)
		}
		time.Sleep(2 * time.Second)
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
	mmdbPath := manager.geoIPDatabasePath()
	info, err := os.Stat(mmdbPath)
	if err != nil {
		t.Fatalf("GeoIP 数据库不存在：%v", err)
	}
	if info.Size() < 50*1024*1024 {
		t.Fatalf("GeoIP 数据库大小异常：%d", info.Size())
	}
	// 把安装目录写给后续的 Worker 启动验证步骤使用（worker/camoufox-launch-verify.mjs 读取）。
	markerPath := filepath.Join("..", "..", "worker", ".smoke-runtime-dir.txt")
	if err := os.WriteFile(markerPath, []byte(runtimeDir), 0o644); err != nil {
		t.Fatalf("记录安装目录失败：%v", err)
	}
	t.Logf("首次安装模拟成功，runtimeDir=%s", runtimeDir)
}
