// Package runtime 文件作用：验证 Mac 早启动字体修正保留资源、重复执行幂等且不影响其他系统。
package runtime

import (
	"context"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	browserprocess "goodhr5/local-agent-go-new/internal/browser/process"
)

// TestEnsureCamoufoxFontRendering 验证平铺和 .app 资源、原配置及权限保留和重复修正。
func TestEnsureCamoufoxFontRendering(t *testing.T) {
	for _, layout := range []string{"flat", "app"} {
		t.Run(layout, func(t *testing.T) {
			root := t.TempDir()
			resources := root
			if layout == "app" {
				resources = filepath.Join(root, "Camoufox.app", "Contents", "Resources")
				if err := os.MkdirAll(resources, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(resources, "camoufox.cfg")
			original := "// Camoufox AutoConfig\ndefaultPref(\"gfx.bundled-fonts.activate\", 1);"
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			originalInfo, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if err := ensureCamoufoxFontRendering(root, "darwin"); err != nil {
					t.Fatal(err)
				}
			}
			updated, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(updated) != original+camoufoxMacFontRenderingConfig {
				t.Fatal("原有启动设置被修改，或字体修正被重复添加")
			}
			for _, pref := range []string{"layers.gpu-process.enabled", "layers.gpu-process.force-enabled"} {
				if !strings.Contains(string(updated), `lockPref("`+pref+`", false);`) {
					t.Fatalf("缺少早启动锁定设置：%s", pref)
				}
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != originalInfo.Mode().Perm() {
				t.Fatal("原配置文件权限没有保留", err)
			}
			entries, err := os.ReadDir(resources)
			if err != nil || len(entries) != 1 {
				t.Fatal("修正后留下了临时文件", err)
			}
			// 组件重新安装会还原上游配置，下一次启动必须重新修正。
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := ensureCamoufoxFontRendering(root, "darwin"); err != nil {
				t.Fatal(err)
			}
			updated, err = os.ReadFile(path)
			if err != nil || string(updated) != original+camoufoxMacFontRenderingConfig {
				t.Fatal("组件更新后没有恢复字体修正", err)
			}
		})
	}
}

// TestEnsureCamoufoxFontRenderingInvalidConfig 验证目录为空、配置缺失及空配置时不制造新配置。
func TestEnsureCamoufoxFontRenderingInvalidConfig(t *testing.T) {
	if err := ensureCamoufoxFontRendering(" \t", "darwin"); err == nil {
		t.Fatal("空目录应该返回错误")
	}
	root := t.TempDir()
	path := filepath.Join(root, "camoufox.cfg")
	if err := ensureCamoufoxFontRendering(root, "darwin"); err == nil || !strings.Contains(err.Error(), "camoufox.cfg") {
		t.Fatal("缺少启动配置应该返回明确错误", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("缺少配置时不应该创建无头的 AutoConfig 文件", err)
	}
	if err := os.WriteFile(path, []byte(" \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureCamoufoxFontRendering(root, "darwin"); err == nil {
		t.Fatal("空配置应该返回错误")
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != " \n" {
		t.Fatal("空配置不应该被修改", err)
	}
}

// TestEnsureCamoufoxFontRenderingOtherPlatforms 验证 Windows 和 Linux 原样保留，且不读取无效目录。
func TestEnsureCamoufoxFontRenderingOtherPlatforms(t *testing.T) {
	for _, platform := range []string{"windows", "linux"} {
		t.Run(platform, func(t *testing.T) {
			for _, root := range []string{"", filepath.Join(t.TempDir(), "missing")} {
				if err := ensureCamoufoxFontRendering(root, platform); err != nil {
					t.Fatal("其他系统不应该读取或检查启动配置", err)
				}
			}
			root := t.TempDir()
			path := filepath.Join(root, "camoufox.cfg")
			original := "// original\ndefaultPref(\"layers.gpu-process.enabled\", true);\n"
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := ensureCamoufoxFontRendering(root, platform); err != nil {
				t.Fatal(err)
			}
			updated, err := os.ReadFile(path)
			if err != nil || string(updated) != original {
				t.Fatal("其他系统的启动配置被修改", err)
			}
		})
	}
}

// fontRenderingWorkerHealth 记录 Worker 启动入口是否被调用，不启动真实进程。
type fontRenderingWorkerHealth struct{ calls int }

// Health 标记 Worker 已被尝试启动，并返回健康状态以避免测试拉起子进程。
func (h *fontRenderingWorkerHealth) Health(context.Context) error {
	h.calls++
	return nil
}

// TestEnsureWorkerFontRenderingFailure 验证字体修正失败在启动入口返回错误，且不会启动 Worker。
func TestEnsureWorkerFontRenderingFailure(t *testing.T) {
	if goruntime.GOOS != "darwin" {
		t.Skip("仅 macOS 需要在 Worker 启动前修正字体")
	}
	t.Setenv("GOODHR_TEST_NODE_VERSION", "v22.19.0")
	t.Setenv("CAMOUFOX_BINARY_PATH", "")
	for _, configState := range []string{"missing", "empty"} {
		t.Run(configState, func(t *testing.T) {
			root := t.TempDir()
			nodePath := writeLegacyTestNode(t, root, nodeTestExecutable(t))
			workerRoot := filepath.Join(root, "worker")
			entryPath := filepath.Join(workerRoot, "dist", "main.js")
			dependencyPath := filepath.Join(workerRoot, "node_modules", "camoufox-js", "package.json")
			binaryPath := filepath.Join(root, "camoufox", "Camoufox.app", "Contents", "MacOS", "camoufox")
			for _, path := range []string{entryPath, dependencyPath, binaryPath} {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			resources := filepath.Join(root, "camoufox", "Camoufox.app", "Contents", "Resources")
			if err := os.MkdirAll(resources, 0o755); err != nil {
				t.Fatal(err)
			}
			if configState == "empty" {
				if err := os.WriteFile(filepath.Join(resources, "camoufox.cfg"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			health := &fontRenderingWorkerHealth{}
			manager := &Manager{
				nodePath: nodePath, entryPath: entryPath, runtimeDir: root,
				worker: browserprocess.New(nodePath, entryPath, 0, health),
			}
			err := manager.EnsureWorker(context.Background())
			if err == nil || !strings.Contains(err.Error(), "中文显示设置还没准备好") || !strings.Contains(err.Error(), "camoufox.cfg") {
				t.Fatal("字体修正失败应该从 Worker 启动入口返回明确错误", err)
			}
			if health.calls != 0 {
				t.Fatal("字体修正失败后仍调用了 Worker 启动入口")
			}
		})
	}
}
