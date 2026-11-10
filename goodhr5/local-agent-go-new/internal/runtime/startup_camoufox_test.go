// Package runtime 文件作用：验证 Windows x64 与 Mac 首次浏览器更新门闩、真实下载校验、失败保留和并发重试，不启动浏览器。
package runtime

import (
	"bytes"
	"context"
	"debug/macho"
	"debug/pe"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// startupTestMachO 构造只用于读取文件头的最小 Mac 启动文件，测试不会执行该文件。
func startupTestMachO(cpu macho.Cpu) []byte {
	content := make([]byte, 32)
	binary.LittleEndian.PutUint32(content[0:], macho.Magic64)
	binary.LittleEndian.PutUint32(content[4:], uint32(cpu))
	binary.LittleEndian.PutUint32(content[12:], uint32(macho.TypeExec))
	return content
}

// startupTestCPU 返回当前 Mac 架构和另一个架构，用于验证错包不会被登记。
func startupTestCPU() (macho.Cpu, macho.Cpu) {
	if goruntime.GOARCH == "amd64" {
		return macho.CpuAmd64, macho.CpuArm64
	}
	return macho.CpuArm64, macho.CpuAmd64
}

// startupTestExecutable 为当前平台构造正确或错误架构的浏览器文件，Windows 使用 PE，Mac 使用 Mach-O。
func startupTestExecutable(wrongArchitecture bool) []byte {
	if goruntime.GOOS == "windows" {
		machine := uint16(pe.IMAGE_FILE_MACHINE_AMD64)
		if wrongArchitecture {
			machine = pe.IMAGE_FILE_MACHINE_I386
		}
		return startupTestPE(machine)
	}
	cpu, wrongCPU := startupTestCPU()
	if wrongArchitecture {
		cpu = wrongCPU
	}
	return startupTestMachO(cpu)
}

// startupTestManager 创建隔离目录与旧安装记录，在 Windows x64 和 Mac 双架构执行真实下载集成测试。
func startupTestManager(t *testing.T, previous Asset) (*Manager, string, []byte) {
	t.Helper()
	if !startupCamoufoxPlatformSupported(platformKey()) {
		t.Skip("当前系统不发布本地程序，仍运行选包和文件头单元测试")
	}
	t.Setenv("CAMOUFOX_BINARY_PATH", "")
	manager := &Manager{runtimeDir: filepath.Join(t.TempDir(), "runtime")}
	root := filepath.Join(manager.runtimeDir, "camoufox")
	writeComponentTestFiles(t, root, componentTestFiles("camoufox", string(startupTestExecutable(false))))
	path := manager.CamoufoxPath()
	relative, err := filepath.Rel(manager.runtimeDir, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.saveVersion("camoufox", previous, relative, "camoufox"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(manager.statePath())
	if err != nil {
		t.Fatal(err)
	}
	return manager, path, before
}

// TestStartupCamoufoxAssetSelection 验证 Windows 与 Apple 别名、精确架构优先及跨系统错包拒绝。
func TestStartupCamoufoxAssetSelection(t *testing.T) {
	asset := Asset{Version: "152-fixed", URL: "https://example.invalid/browser.zip", SHA256: strings.Repeat("a", 64)}
	for _, alias := range []string{"win-x64", "windows-x64", "win", "windows"} {
		manifest := Manifest{Camoufox: map[string]Asset{alias: asset, "mac": {Version: "wrong"}}}
		if got, err := startupCamoufoxAsset(manifest, "win-x64"); err != nil || got != asset {
			t.Fatalf("Windows 别名 %s 未选中：%+v %v", alias, got, err)
		}
	}
	if _, err := startupCamoufoxAsset(Manifest{Camoufox: map[string]Asset{"mac": asset}}, "win-x64"); err == nil {
		t.Fatal("Windows 不能借用 Mac 包")
	}
	for _, alias := range []string{"darwin-arm64", "mac-arm64", "macos-arm64", "mac", "macos", "darwin"} {
		manifest := Manifest{Camoufox: map[string]Asset{alias: asset}}
		if got, err := startupCamoufoxAsset(manifest, "darwin-arm64"); err != nil || got != asset {
			t.Fatalf("Apple 别名 %s 未选中：%+v %v", alias, got, err)
		}
		if _, err := startupCamoufoxAsset(manifest, "darwin-x64"); err == nil {
			t.Fatalf("Intel 错用了 %s 的 Apple 包", alias)
		}
	}
	manifest := Manifest{Camoufox: map[string]Asset{"darwin-x64": asset, "mac": {Version: "wrong"}}}
	if got, err := startupCamoufoxAsset(manifest, "darwin-x64"); err != nil || got != asset {
		t.Fatalf("Intel 专属包未选中：%+v %v", got, err)
	}
	for _, platform := range []string{"win-x64", "darwin-arm64"} {
		for _, invalid := range []Asset{{}, {Version: "fixed", URL: "http://example.invalid/browser.zip", SHA256: asset.SHA256}, {Version: "fixed", URL: asset.URL, SHA256: ""}} {
			if _, err := startupCamoufoxAsset(Manifest{Camoufox: map[string]Asset{platform: invalid, "win": asset, "mac": asset}}, platform); err == nil {
				t.Fatal("无效的精确架构记录不能通过或偷偷借用别名")
			}
		}
	}
	for _, platform := range []string{"linux-amd64", "windows-arm64", "windows-386", "darwin-386", ""} {
		if startupCamoufoxPlatformSupported(platform) {
			t.Fatalf("未发布的平台 %s 不能启用启动同步", platform)
		}
		if _, err := startupCamoufoxAsset(Manifest{Camoufox: map[string]Asset{platform: asset}}, platform); err == nil {
			t.Fatalf("未发布的平台 %s 不能选包", platform)
		}
	}
}

// TestStartupCamoufoxArchitecture 验证读取真实 Mach-O 文件头时拒绝错架构和非 Mac 程序。
func TestStartupCamoufoxArchitecture(t *testing.T) {
	for _, test := range []struct {
		platform string
		cpu      macho.Cpu
	}{{"darwin-arm64", macho.CpuArm64}, {"darwin-x64", macho.CpuAmd64}} {
		path := filepath.Join(t.TempDir(), "browser")
		if err := os.WriteFile(path, startupTestMachO(test.cpu), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := checkStartupCamoufoxArchitecture(path, test.platform); err != nil {
			t.Fatal(err)
		}
		other := "darwin-arm64"
		if test.platform == other {
			other = "darwin-x64"
		}
		if err := checkStartupCamoufoxArchitecture(path, other); err == nil {
			t.Fatal("错架构的启动文件不应通过")
		}
	}
}

// TestStartupCamoufoxSyncUpgrade 验证同版本换摘要、缺摘要和失效启动文件都会下载，同包不重下也不关浏览器。
func TestStartupCamoufoxSyncUpgrade(t *testing.T) {
	for _, scenario := range []string{"changed sha", "missing sha", "same asset", "fresh install", "broken executable", "wrong installed architecture"} {
		t.Run(scenario, func(t *testing.T) {
			asset, downloads := serveNodeTestArchive(t, makeComponentTestArchive(t, componentTestFiles("camoufox", string(startupTestExecutable(false)))))
			asset.Version = "152-fixed"
			previous := asset
			if scenario == "changed sha" {
				previous.SHA256 = strings.Repeat("b", 64)
			} else if scenario == "missing sha" {
				previous.SHA256 = ""
			}
			manager, oldPath, _ := startupTestManager(t, previous)
			profile := filepath.Join(filepath.Dir(manager.runtimeDir), "profiles", "login.cookie")
			writeComponentTestFiles(t, filepath.Dir(profile), map[string][]byte{filepath.Base(profile): []byte("keep-login")})
			if scenario == "fresh install" {
				if err := os.RemoveAll(manager.runtimeDir); err != nil {
					t.Fatal(err)
				}
			} else if scenario == "broken executable" {
				if err := os.WriteFile(oldPath, []byte("corrupt"), 0o755); err != nil {
					t.Fatal(err)
				}
			} else if scenario == "wrong installed architecture" {
				if err := os.WriteFile(oldPath, startupTestExecutable(true), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			manager.EnableStartupCamoufoxSync()
			if !manager.StartupCamoufoxSyncPending() || manager.Status().CamoufoxInstalled || manager.EnsureWorker(context.Background()) == nil {
				t.Fatal("首次检查前旧浏览器不能被报告已就绪或启动 Worker")
			}
			var stops int
			manifest := Manifest{Camoufox: map[string]Asset{platformKey(): asset}}
			if err := manager.SyncStartupCamoufox(context.Background(), manifest, func(context.Context) error { stops++; return nil }); err != nil {
				t.Fatal(err)
			}
			want := int32(1)
			if scenario == "same asset" {
				want = 0
			}
			if downloads.Load() != want || int32(stops) != want || manager.StartupCamoufoxSyncPending() || !manager.Status().CamoufoxInstalled {
				t.Fatalf("更新未按目标放行：downloads=%d stops=%d pending=%v", downloads.Load(), stops, manager.StartupCamoufoxSyncPending())
			}
			if want > 0 && manager.CamoufoxPath() == oldPath {
				t.Fatal("没有切换到新安装目录")
			}
			if content, err := os.ReadFile(profile); err != nil || string(content) != "keep-login" {
				t.Fatalf("浏览器更新修改了登录资料：%v", err)
			}
			if scenario != "fresh install" && !fileExists(oldPath) {
				t.Fatal("浏览器更新不应删除旧组件")
			}
			manager.EnableStartupCamoufoxSync()
			if err := manager.SyncStartupCamoufox(context.Background(), Manifest{}, nil); err != nil || manager.StartupCamoufoxSyncPending() || downloads.Load() != want {
				t.Fatal("同一进程重复绑定或启用不应重新安装")
			}
			manager.FailStartupCamoufoxSync(errors.New("late result"))
			manager.BeginStartupCamoufoxSyncCheck()
			if progress := manager.InstallProgress(); progress.Stage != "installed" || progress.Running {
				t.Fatalf("完成后的旧错误或重复检查不应改写结果：%+v", progress)
			}
		})
	}
}

// TestStartupCamoufoxProgressAndDefault 验证默认管理器不受门闩影响，配置失败可重新进入检查且完成后不再被旧错误覆盖。
func TestStartupCamoufoxProgressAndDefault(t *testing.T) {
	plain := &Manager{}
	plain.BeginStartupCamoufoxSyncCheck()
	plain.FailStartupCamoufoxSync(errors.New("unused"))
	if plain.StartupCamoufoxSyncPending() || plain.InstallProgress().Stage != "" || plain.SyncStartupCamoufox(nil, Manifest{}, nil) != nil {
		t.Fatal("默认管理器不应隐式启用首次更新")
	}
	if !startupCamoufoxPlatformSupported(platformKey()) {
		plain.EnableStartupCamoufoxSync()
		if plain.StartupCamoufoxSyncPending() {
			t.Fatal("未发布的平台不能启用首次更新")
		}
		return
	}
	plain.EnableStartupCamoufoxSync()
	plain.FailStartupCamoufoxSync(errors.New("manifest offline"))
	if plain.InstallProgress().Stage != "failed" || !plain.StartupCamoufoxSyncPending() {
		t.Fatal("配置读取失败后应保留待重试状态")
	}
	plain.BeginStartupCamoufoxSyncCheck()
	if progress := plain.InstallProgress(); !progress.Running || progress.Stage != "queued" {
		t.Fatalf("重新检查没有清除旧失败进度：%+v", progress)
	}
}

// TestStartupCamoufoxFailurePreservesPrevious 验证回调失败、取消、坏摘要、错架构及坏启动文件均保留旧组件、登录资料与完整记录。
func TestStartupCamoufoxFailurePreservesPrevious(t *testing.T) {
	scenarios := []string{"stop failed", "cancelled", "bad sha", "wrong architecture", "missing executable", "invalid executable"}
	if goruntime.GOOS == "windows" {
		scenarios = append(scenarios, "dll")
	}
	for _, scenario := range scenarios {
		t.Run(scenario, func(t *testing.T) {
			executable := startupTestExecutable(scenario == "wrong architecture")
			if scenario == "invalid executable" {
				executable = []byte("broken browser executable")
			} else if scenario == "dll" {
				binary.LittleEndian.PutUint16(executable[0x96:], pe.IMAGE_FILE_EXECUTABLE_IMAGE|pe.IMAGE_FILE_DLL)
			}
			files := componentTestFiles("camoufox", string(executable))
			if scenario == "missing executable" {
				files = map[string][]byte{"readme.txt": []byte("no executable")}
			}
			asset, _ := serveNodeTestArchive(t, makeComponentTestArchive(t, files))
			if scenario == "bad sha" {
				asset.SHA256 = strings.Repeat("f", 64)
			}
			manager, oldPath, before := startupTestManager(t, Asset{Version: "previous"})
			oldContent, _ := os.ReadFile(oldPath)
			profile := filepath.Join(filepath.Dir(manager.runtimeDir), "profiles", "login.cookie")
			writeComponentTestFiles(t, filepath.Dir(profile), map[string][]byte{filepath.Base(profile): []byte("keep-login")})
			manager.EnableStartupCamoufoxSync()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			err := manager.SyncStartupCamoufox(ctx, Manifest{Camoufox: map[string]Asset{platformKey(): asset}}, func(context.Context) error {
				if scenario == "stop failed" {
					return errors.New("browser still closing")
				}
				if scenario == "cancelled" {
					cancel()
				}
				return nil
			})
			after, _ := os.ReadFile(manager.statePath())
			content, _ := os.ReadFile(oldPath)
			if err == nil || !bytes.Equal(before, after) || !bytes.Equal(oldContent, content) || !manager.StartupCamoufoxSyncPending() || manager.InstallProgress().Stage != "failed" {
				t.Fatalf("失败未保留旧组件或门闩：err=%v progress=%+v", err, manager.InstallProgress())
			}
			if content, err := os.ReadFile(profile); err != nil || string(content) != "keep-login" {
				t.Fatalf("更新失败修改了登录资料：%v", err)
			}
		})
	}
}

// TestStartupCamoufoxConcurrentAndManualRetry 验证重复绑定不并发下载、外部错误不覆盖安装进度、手动重试不能安装旧目标。
func TestStartupCamoufoxConcurrentAndManualRetry(t *testing.T) {
	asset, downloads := serveNodeTestArchive(t, makeComponentTestArchive(t, componentTestFiles("camoufox", string(startupTestExecutable(false)))))
	manager, _, _ := startupTestManager(t, Asset{Version: "previous"})
	manager.nodePath, _ = exec.LookPath("node")
	manager.EnableStartupCamoufoxSync()
	manifest := Manifest{Camoufox: map[string]Asset{platformKey(): asset}}
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		finished <- manager.SyncStartupCamoufox(context.Background(), manifest, func(context.Context) error {
			close(started)
			<-release
			return errors.New("test stop failed")
		})
	}()
	<-started
	if err := manager.SyncStartupCamoufox(context.Background(), manifest, nil); err == nil {
		t.Fatal("重复绑定必须合并或拒绝，不能再启动下载")
	}
	manager.FailStartupCamoufoxSync(errors.New("stale external error"))
	if !manager.InstallProgress().Running || manager.EnsureWorker(context.Background()) == nil {
		t.Fatal("并发错误不应覆盖安装进度，也不能启动 Worker")
	}
	close(release)
	if err := <-finished; err == nil {
		t.Fatal("关闭失败应保留待重试状态")
	}
	wrong := asset
	wrong.SHA256 = strings.Repeat("c", 64)
	if _, err := manager.StartInstall(Manifest{Camoufox: map[string]Asset{platformKey(): wrong}}); err == nil || downloads.Load() != 0 {
		t.Fatal("手动重试不能下载旧目标或假成功")
	}
	if _, err := manager.StartInstall(manifest); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for manager.StartupCamoufoxSyncPending() && manager.InstallProgress().Stage != "failed" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if manager.StartupCamoufoxSyncPending() || downloads.Load() != 1 {
		t.Fatalf("手动重试未正确放行：downloads=%d progress=%+v", downloads.Load(), manager.InstallProgress())
	}
}

// TestStartupCamoufoxCancelDownload 验证网络读取中取消可及时返回，且不会登记半个安装结果。
func TestStartupCamoufoxCancelDownload(t *testing.T) {
	manager, _, before := startupTestManager(t, Asset{Version: "previous"})
	started := make(chan struct{})
	var count atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	previous := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	defer func() { http.DefaultTransport = previous }()
	manager.EnableStartupCamoufoxSync()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		finished <- manager.SyncStartupCamoufox(ctx, Manifest{Camoufox: map[string]Asset{platformKey(): {Version: "fixed", URL: server.URL + "/browser.zip", SHA256: strings.Repeat("a", 64)}}}, func(context.Context) error { return nil })
	}()
	<-started
	cancel()
	select {
	case err := <-finished:
		after, _ := os.ReadFile(manager.statePath())
		if err == nil || !bytes.Equal(before, after) || !manager.StartupCamoufoxSyncPending() || count.Load() != 1 {
			t.Fatalf("取消后意外登记或放行：err=%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("下载取消未及时退出")
	}
}
