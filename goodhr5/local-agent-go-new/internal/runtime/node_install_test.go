// Package runtime 文件作用：验证 Node 无目录移动安装、重启兼容与失败保留旧版本。
package runtime

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestMain 让复制为 Node 的测试程序响应版本检查，其他调用继续正常执行测试。
func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "--version" && filepath.Base(os.Args[0]) == nodeBinaryName() {
		if version := os.Getenv("GOODHR_TEST_NODE_VERSION"); version != "" {
			fmt.Println(version)
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

// TestInstallNodeWithoutMovingDirectory 验证旧目录保留、重复安装跳过、重启和目录迁移后仍使用新 Node。
func TestInstallNodeWithoutMovingDirectory(t *testing.T) {
	t.Setenv("GOODHR_TEST_NODE_VERSION", "v22.19.0")
	parent := t.TempDir()
	runtimeDir := filepath.Join(parent, "runtime data")
	legacyPath := writeLegacyTestNode(t, runtimeDir, []byte("old-node"))
	manager := &Manager{runtimeDir: runtimeDir}
	asset, downloads := serveNodeTestArchive(t, makeNodeTestArchive(t, nodeTestExecutable(t)))
	if err := manager.installAsset(context.Background(), "node_runtime", "Node 运行环境", "node", asset); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(legacyPath); err != nil || string(content) != "old-node" {
		t.Fatalf("旧目录被改动：content=%q err=%v", content, err)
	}
	recorded := manager.loadVersions()["node_runtime"].ExecutablePath
	if filepath.IsAbs(recorded) || !strings.HasPrefix(recorded, "node-runtime-") {
		t.Fatalf("未记录独立安装目录的相对路径：%q", recorded)
	}
	if err := manager.CheckNode(); err != nil {
		t.Fatalf("新 Node 不可用：%v", err)
	}
	if _, err := os.Stat(filepath.Join(runtimeDir, "node.backup")); !os.IsNotExist(err) {
		t.Fatalf("不应创建旧目录备份：%v", err)
	}
	if err := manager.installAsset(context.Background(), "node_runtime", "Node 运行环境", "node", asset); err != nil {
		t.Fatal(err)
	}
	if downloads.Load() != 1 {
		t.Fatal("有效的同版本组件不应重复下载")
	}
	restarted := &Manager{runtimeDir: runtimeDir}
	if restarted.NodePath() != manager.NodePath() || restarted.CheckNode() != nil {
		t.Fatal("重启后没有复用记录的 Node 路径")
	}
	relocated := filepath.Join(parent, "runtime relocated")
	if err := os.Rename(runtimeDir, relocated); err != nil {
		t.Fatal(err)
	}
	restarted.runtimeDir = relocated
	if err := restarted.CheckNode(); err != nil {
		t.Fatalf("整体迁移后相对路径失效：%v", err)
	}
}

// TestInstallNodeTarGZ 验证 Mac 常见的 tar.gz 包保留 bin 结构与可执行权限且可直接运行。
func TestInstallNodeTarGZ(t *testing.T) {
	t.Setenv("GOODHR_TEST_NODE_VERSION", "v22.19.0")
	executable := nodeTestExecutable(t)
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	writer := tar.NewWriter(gzipWriter)
	for _, entry := range []struct {
		name    string
		content []byte
	}{
		{name: "node-v22-test/LICENSE", content: []byte("fixture")},
		{name: "node-v22-test/bin/" + nodeBinaryName(), content: executable},
	} {
		if err := writer.WriteHeader(&tar.Header{Name: entry.name, Mode: 0o755, Size: int64(len(entry.content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(entry.content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	manager := &Manager{runtimeDir: t.TempDir()}
	asset, _ := serveNodeTestArchive(t, archive.Bytes())
	if err := manager.installAsset(context.Background(), "node_runtime", "Node 运行环境", "node", asset); err != nil {
		t.Fatal(err)
	}
	if err := manager.CheckNode(); err != nil {
		t.Fatalf("tar.gz 安装后 Node 不可用：%v", err)
	}
	if !strings.HasSuffix(filepath.ToSlash(manager.NodePath()), "/bin/"+nodeBinaryName()) {
		t.Fatalf("bin 结构未保留：%s", manager.NodePath())
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(manager.NodePath())
		if err != nil || info.Mode().Perm()&0o111 == 0 {
			t.Fatalf("Unix 可执行权限丢失：err=%v", err)
		}
	}
}

// TestStartInstallFailurePreservesProgress 验证失败时保留组件和已完成进度，避免错误显示为 0%。
func TestStartInstallFailurePreservesProgress(t *testing.T) {
	runtimeDir := t.TempDir()
	writeLegacyTestNode(t, runtimeDir, []byte("broken legacy"))
	manager := &Manager{runtimeDir: runtimeDir}
	asset, _ := serveNodeTestArchive(t, makeNodeTestArchive(t, nil))
	if _, err := manager.StartInstall(Manifest{NodeRuntime: map[string]Asset{platformKey(): asset}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		progress := manager.InstallProgress()
		if !progress.Running {
			if progress.Stage != "failed" || progress.Component != "node_runtime" || progress.Percent < 75 {
				t.Fatalf("失败后丢失组件与进度：%+v", progress)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("后台安装未结束")
}

// TestInstallNodeFailurePreservesPreviousVersion 验证损坏包、旧 Node 和启动失败均不切换安装记录。
func TestInstallNodeFailurePreservesPreviousVersion(t *testing.T) {
	executable := nodeTestExecutable(t)
	validArchive := makeNodeTestArchive(t, executable)
	for _, test := range []struct {
		name    string
		archive []byte
		version string
		badSHA  bool
	}{
		{name: "旧版本", archive: validArchive, version: "v18.0.0"},
		{name: "版本输出不正确", archive: validArchive, version: "unknown"},
		{name: "无法执行", archive: makeNodeTestArchive(t, []byte("not an executable")), version: "v22.19.0"},
		{name: "缺少Node", archive: makeNodeTestArchive(t, nil), version: "v22.19.0"},
		{name: "损坏压缩包", archive: []byte("broken zip"), version: "v22.19.0"},
		{name: "校验失败", archive: validArchive, version: "v22.19.0", badSHA: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("GOODHR_TEST_NODE_VERSION", test.version)
			runtimeDir := t.TempDir()
			legacyPath := writeLegacyTestNode(t, runtimeDir, executable)
			manager := &Manager{runtimeDir: runtimeDir}
			oldAsset := Asset{Version: "previous", SHA256: strings.Repeat("a", 64)}
			if err := manager.saveVersion("node_runtime", oldAsset, filepath.ToSlash(filepath.Join("node", nodeBinaryName())), ""); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(manager.statePath())
			if err != nil {
				t.Fatal(err)
			}
			asset, _ := serveNodeTestArchive(t, test.archive)
			if test.badSHA {
				asset.SHA256 = strings.Repeat("0", 64)
			}
			if err := manager.installAsset(context.Background(), "node_runtime", "Node 运行环境", "node", asset); err == nil {
				t.Fatal("损坏或不可用的 Node 不应安装成功")
			}
			after, err := os.ReadFile(manager.statePath())
			if err != nil || !bytes.Equal(before, after) || manager.NodePath() != legacyPath {
				t.Fatalf("失败后旧安装记录未保留：err=%v", err)
			}
			remaining, err := filepath.Glob(filepath.Join(runtimeDir, "node-runtime-*"))
			if err != nil || len(remaining) != 0 {
				t.Fatalf("失败目录未清理：%v err=%v", remaining, err)
			}
		})
	}
}

// TestInstallNodeStateFailurePreservesLegacy 验证无法保存安装记录时不会丢失旧 Node。
func TestInstallNodeStateFailurePreservesLegacy(t *testing.T) {
	t.Setenv("GOODHR_TEST_NODE_VERSION", "v22.19.0")
	runtimeDir := t.TempDir()
	legacyPath := writeLegacyTestNode(t, runtimeDir, nodeTestExecutable(t))
	manager := &Manager{runtimeDir: runtimeDir}
	if err := os.Mkdir(manager.statePath(), 0o755); err != nil {
		t.Fatal(err)
	}
	asset, _ := serveNodeTestArchive(t, makeNodeTestArchive(t, nodeTestExecutable(t)))
	if err := manager.installAsset(context.Background(), "node_runtime", "Node 运行环境", "node", asset); err == nil {
		t.Fatal("安装记录保存失败不应报告成功")
	}
	if manager.NodePath() != legacyPath || manager.CheckNode() != nil {
		t.Fatal("保存失败后旧 Node 不可用")
	}
	remaining, _ := filepath.Glob(filepath.Join(runtimeDir, "node-runtime-*"))
	if len(remaining) != 0 {
		t.Fatalf("保存失败后留下未登记目录：%v", remaining)
	}
}

// TestRecordedNodePathFallback 验证旧记录、失效路径和越界路径回退到旧安装目录。
func TestRecordedNodePathFallback(t *testing.T) {
	runtimeDir := t.TempDir()
	legacyPath := writeLegacyTestNode(t, runtimeDir, []byte("legacy"))
	manager := &Manager{runtimeDir: runtimeDir}
	for _, path := range []string{"", "missing/node.exe", "../outside/node.exe", legacyPath} {
		if err := manager.saveVersion("node_runtime", Asset{Version: "previous"}, path, ""); err != nil {
			t.Fatal(err)
		}
		if got := manager.NodePath(); got != legacyPath {
			t.Fatalf("记录 %q 应回退旧目录，实际 %q", path, got)
		}
	}
	if err := os.WriteFile(manager.statePath(), []byte("null"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := manager.saveVersion("node_runtime", Asset{Version: "repaired"}, "", ""); err != nil {
		t.Fatalf("空安装记录无法修复：%v", err)
	}
}

// TestInstallNodeDuringStatusPolling 验证持续读取状态不会阻挡版本记录切换或读到半份记录。
func TestInstallNodeDuringStatusPolling(t *testing.T) {
	t.Setenv("GOODHR_TEST_NODE_VERSION", "v22.19.0")
	runtimeDir := t.TempDir()
	writeLegacyTestNode(t, runtimeDir, nodeTestExecutable(t))
	manager := &Manager{runtimeDir: runtimeDir}
	if err := manager.saveVersion("node_runtime", Asset{Version: "previous"}, "", ""); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	invalid := make(chan struct{}, 1)
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				if _, exists := manager.loadVersions()["node_runtime"]; !exists {
					invalid <- struct{}{}
					return
				}
				_ = manager.NodePath()
			}
		}
	}()
	defer func() { close(stop); <-done }()
	asset, _ := serveNodeTestArchive(t, makeNodeTestArchive(t, nodeTestExecutable(t)))
	if err := manager.installAsset(context.Background(), "node_runtime", "Node 运行环境", "node", asset); err != nil {
		t.Fatalf("轮询期间安装失败：%v", err)
	}
	select {
	case <-invalid:
		t.Fatal("状态轮询读到了不完整的安装记录")
	default:
	}
}

// TestInstallRealNodeSmoke 使用显式指定的真实 Node 做原地安装与启动冒烟，不下载额外依赖。
func TestInstallRealNodeSmoke(t *testing.T) {
	path := os.Getenv("GOODHR_TEST_REAL_NODE")
	if path == "" {
		t.Skip("设置 GOODHR_TEST_REAL_NODE 后验证真实 Node 安装")
	}
	executable, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	manager := &Manager{runtimeDir: t.TempDir()}
	asset, _ := serveNodeTestArchive(t, makeNodeTestArchive(t, executable))
	if err := manager.installAsset(context.Background(), "node_runtime", "Node 运行环境", "node", asset); err != nil {
		t.Fatal(err)
	}
	output, err := commandCombinedOutput(context.Background(), nil, manager.NodePath(), "-e", "process.stdout.write('goodhr-runtime-ok')")
	if err != nil || string(output) != "goodhr-runtime-ok" {
		t.Fatalf("真实 Node 安装后执行失败：output=%q err=%v", output, err)
	}
}

// nodeTestExecutable 读取当前平台的测试程序，作为能够响应版本检查的 Node 替身。
func nodeTestExecutable(t *testing.T) []byte {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

// writeLegacyTestNode 写入旧固定目录中的 Node 测试文件。
func writeLegacyTestNode(t *testing.T, runtimeDir string, content []byte) string {
	t.Helper()
	path := filepath.Join(runtimeDir, "node", nodeBinaryName())
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// makeNodeTestArchive 构造带包装目录的 Node 压缩包，空内容用于模拟缺少可执行文件。
func makeNodeTestArchive(t *testing.T, executable []byte) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	entries := map[string][]byte{"node-v22-test/LICENSE": []byte("fixture")}
	if executable != nil {
		entries["node-v22-test/bin/"+nodeBinaryName()] = executable
	}
	for name, content := range entries {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0o755)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

// serveNodeTestArchive 提供本地 HTTPS 测试下载并统计请求次数，测试结束后恢复默认传输。
func serveNodeTestArchive(t *testing.T, archive []byte) (Asset, *atomic.Int32) {
	t.Helper()
	var downloads atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		_, _ = w.Write(archive)
	}))
	t.Cleanup(server.Close)
	previous := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	digest := sha256.Sum256(archive)
	suffix := ".zip"
	if len(archive) >= 2 && archive[0] == 0x1f && archive[1] == 0x8b {
		suffix = ".tar.gz"
	}
	return Asset{Version: "22.19.0", URL: server.URL + "/node" + suffix, SHA256: hex.EncodeToString(digest[:])}, &downloads
}
