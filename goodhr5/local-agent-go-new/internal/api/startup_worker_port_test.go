// Package api 文件作用：验证浏览器换包前必须释放已知 Worker 回环端口，不能把 HTTP 不健康的残留进程当成已退出。
package api

import (
	"context"
	"net"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/config"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/runtime"
)

// TestStartupSyncUnhealthyListenerKeepsGate 验证没有健康 HTTP 响应的未知监听仍会挡住换包，且不会被停止或下载新组件。
func TestStartupSyncUnhealthyListenerKeepsGate(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	address := listener.Addr().(*net.TCPAddr)
	server := &Server{
		cfg:     config.Config{WorkerHost: address.IP.String(), WorkerPort: address.Port},
		runtime: runtime.New("", "", t.TempDir(), "", nil), cloud: cloud.New("http://127.0.0.1"),
	}
	syncer := newStartupCamoufoxSync(server)
	events := []string{}
	fakeRuntime := &startupRuntimeFixture{pending: true, stop: func() error {
		events = append(events, "worker")
		return nil
	}}
	syncer.runtime = fakeRuntime
	syncer.browser = &startupBrowserFixture{healthy: false, events: &events}
	err = fakeRuntime.SyncStartupCamoufox(context.Background(), runtime.Manifest{}, syncer.beforeInstall)
	if err == nil || !strings.Contains(err.Error(), "端口还没释放") || !fakeRuntime.StartupCamoufoxSyncPending() || strings.Join(events, ",") != "health,worker,health" {
		t.Fatalf("不健康的残留监听越过了更新门闩：events=%v err=%v", events, err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := checkStartupWorkerPortReleased(context.Background(), address.IP.String(), address.Port); err != nil {
		t.Fatalf("监听释放后应允许更新：%v", err)
	}
	probe, err := net.Listen("tcp", address.String())
	if err != nil {
		t.Fatalf("检查结束后仍占用 Worker 端口：%v", err)
	}
	_ = probe.Close()
}

// TestStartupWorkerPortRejectsNonLocalAddress 验证端口检查只接受配置中的有效回环地址，不探测远程主机。
func TestStartupWorkerPortRejectsNonLocalAddress(t *testing.T) {
	for _, test := range []struct {
		host string
		port int
	}{{"192.0.2.1", 39881}, {"0.0.0.0", 39881}, {"127.0.0.1", 0}, {"127.0.0.1", 65536}} {
		if err := checkStartupWorkerPortReleased(context.Background(), test.host, test.port); err == nil {
			t.Fatalf("不应探测非本机有效 Worker 地址：%+v", test)
		}
	}
}
