// Package runtime 文件作用：用本机真实 Camoufox 样本验证原地安装、浏览器资源和 Worker 无界面启动，不访问招聘页面。
package runtime

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	browserclient "goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	browserprocess "goodhr5/local-agent-go-new/internal/browser/process"
)

// TestInstallRealCamoufoxSmoke 在显式提供浏览器 ZIP 和编译 Worker 时执行，普通单元测试不下载或启动真实浏览器。
func TestInstallRealCamoufoxSmoke(t *testing.T) {
	archivePath := os.Getenv("GOODHR_TEST_CAMOUFOX_ARCHIVE")
	workerEntry := os.Getenv("GOODHR_TEST_WORKER_ENTRY")
	if archivePath == "" || workerEntry == "" {
		t.Skip("设置 GOODHR_TEST_CAMOUFOX_ARCHIVE 和 GOODHR_TEST_WORKER_ENTRY 后运行真实浏览器样本")
	}
	t.Setenv("CAMOUFOX_BINARY_PATH", "")
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	asset, _ := serveNodeTestArchive(t, archive)
	asset.Version = "152.0.4-beta.30"
	root := t.TempDir()
	manager := &Manager{runtimeDir: root, entryPath: workerEntry}
	if err := manager.installAsset(context.Background(), "camoufox", "Camoufox", "camoufox", asset); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	client := browserclient.New(fmt.Sprintf("http://127.0.0.1:%d", port))
	manager.worker = browserprocess.New(manager.NodePath(), workerEntry, port, client)
	manager.ensureCamoufoxInterface()
	manager.configureWorkerEnvironment()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	t.Cleanup(func() { _ = manager.StopWorker() })
	if err := manager.EnsureWorker(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := client.RuntimeStatus(ctx)
	if err != nil || !status.Installed || status.BinaryPath != manager.CamoufoxPath() || status.CamoufoxVersion != asset.Version {
		t.Fatalf("Worker 解析组件失败：status=%+v err=%v", status, err)
	}
	headless, humanize := true, false
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_, _ = client.StopBrowser(stopCtx)
	})
	started, err := client.StartBrowser(ctx, contract.BrowserStartRequest{
		UserDataDir: filepath.Join(root, "isolated-profile"), DownloadsPath: filepath.Join(root, "isolated-downloads"),
		Headless: &headless, Humanize: &humanize, TimeoutMS: 60000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !started.Running {
		t.Fatalf("真实浏览器没有从新路径启动：%+v", started)
	}
	t.Logf("真实 Camoufox 安装与启动通过 version=%s root=%s binary=%s", status.CamoufoxVersion, manager.CamoufoxDir(), status.BinaryPath)
}
