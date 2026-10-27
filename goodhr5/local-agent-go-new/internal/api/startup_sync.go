// Package api 文件作用：在有效网页绑定后单次后台同步 Windows x64 或 Mac 浏览器组件，退出时取消并等待，不保存登录凭证。
package api

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/runtime"
)

// startupSyncRuntime 定义启动同步与现有 Worker 停止能力，使取消和并发边界可以独立验证。
type startupSyncRuntime interface {
	StartupCamoufoxSyncPending() bool
	BeginStartupCamoufoxSyncCheck()
	FailStartupCamoufoxSync(error)
	SyncStartupCamoufox(context.Context, runtime.Manifest, func(context.Context) error) error
	StopWorker() error
}

// startupSyncCloud 复用已经鉴权的组件清单读取能力，不在同步器中保留 Token。
type startupSyncCloud interface {
	RuntimeManifest(context.Context, string) (runtime.Manifest, error)
}

// startupSyncBrowser 只使用已有浏览器健康检查和正常关闭入口。
type startupSyncBrowser interface {
	Health(context.Context) error
	StopBrowser(context.Context) (contract.BrowserStatus, error)
}

// startupSyncTasks 保护正在运行的任务，组件更新不能代替用户停止任务。
type startupSyncTasks interface {
	HasActive() bool
}

// startupCamoufoxSync 保存本次进程唯一的检查协程及退出状态，不持久化用户凭证。
type startupCamoufoxSync struct {
	runtime    startupSyncRuntime
	cloud      startupSyncCloud
	browser    startupSyncBrowser
	tasks      startupSyncTasks
	workerHost string
	workerPort int
	mu         sync.Mutex
	cancel     context.CancelFunc
	done       chan struct{}
	closing    bool
}

// newStartupCamoufoxSync 复用本地服务已有依赖；未配置运行组件时不启用额外任务。
func newStartupCamoufoxSync(server *Server) *startupCamoufoxSync {
	if server.runtime == nil || server.cloud == nil {
		return nil
	}
	syncer := &startupCamoufoxSync{runtime: server.runtime, cloud: server.cloud, workerHost: server.cfg.WorkerHost, workerPort: server.cfg.WorkerPort}
	if server.browser != nil {
		syncer.browser = server.browser
	}
	if server.runner != nil {
		syncer.tasks = server.runner
	}
	return syncer
}

// start 在绑定成功后启动后台检查；重复绑定共用当前协程，失败后允许下次绑定重试。
func (s *startupCamoufoxSync) start(token string) {
	token = strings.TrimSpace(token)
	if s == nil || token == "" {
		return
	}
	s.mu.Lock()
	if s.closing || s.done != nil || !s.runtime.StartupCamoufoxSyncPending() {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	done := make(chan struct{})
	s.cancel, s.done = cancel, done
	s.runtime.BeginStartupCamoufoxSyncCheck()
	s.mu.Unlock()
	go func() {
		defer func() {
			cancel()
			s.mu.Lock()
			s.cancel, s.done = nil, nil
			close(done)
			s.mu.Unlock()
		}()
		manifest, err := s.cloud.RuntimeManifest(ctx, token)
		if err == nil {
			err = s.runtime.SyncStartupCamoufox(ctx, manifest, s.beforeInstall)
		}
		if err != nil {
			// 云端错误即使意外包含原凭证，也不能进入组件状态或落入日志。
			safeError := strings.ReplaceAll(err.Error(), token, "[登录凭证已隐藏]")
			s.runtime.FailStartupCamoufoxSync(fmt.Errorf("浏览器更新还没完成，请刷新后台重试：%s", safeError))
		}
	}()
}

// beforeInstall 在真正换包前保护活动任务，并先关浏览器再停旧 Worker，确保新环境能够生效。
func (s *startupCamoufoxSync) beforeInstall(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.tasks != nil && s.tasks.HasActive() {
		return fmt.Errorf("任务还在运行，浏览器更新先等这一轮结束，再刷新后台重试")
	}
	if s.browser == nil {
		return nil
	}
	healthCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	healthy := s.browser.Health(healthCtx) == nil
	cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	if healthy {
		if _, err := s.browser.StopBrowser(ctx); err != nil {
			return fmt.Errorf("旧浏览器还没退出，请先关闭浏览器再刷新后台：%w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// 健康检查失败也可能仍持有旧 Worker；先停当前管理器的进程，不能让恢复后的旧环境绕过换包。
	if err := s.runtime.StopWorker(); err != nil {
		return fmt.Errorf("旧的浏览器操作组件还没退出，请重启小助手：%w", err)
	}
	// 旧进程可能来自上次异常退出，当前管理器没有它的句柄；仍响应时禁止复用旧环境。
	healthCtx, cancel = context.WithTimeout(ctx, 2*time.Second)
	stillRunning := s.browser.Health(healthCtx) == nil
	cancel()
	if stillRunning {
		return fmt.Errorf("旧的浏览器操作组件还在运行，请彻底退出旧程序后再重试")
	}
	return checkStartupWorkerPortReleased(ctx, s.workerHost, s.workerPort)
}

// checkStartupWorkerPortReleased 仅短暂绑定已知本机 Worker 端口确认无残留监听；发现未知进程时保持门闩，不结束其 PID。
func checkStartupWorkerPortReleased(ctx context.Context, host string, port int) error {
	// 未提供地址的单元测试依赖只验证同步合同；正式程序始终传入其实际回环地址与端口。
	if host == "" && port == 0 {
		return ctx.Err()
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() || port <= 0 || port > 65535 {
		return fmt.Errorf("浏览器操作组件的本机地址还没准备好，请重启小助手")
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("旧的浏览器操作组件端口还没释放，请彻底退出旧程序后再重试：%w", err)
	}
	if err := listener.Close(); err != nil {
		return fmt.Errorf("浏览器操作组件端口还没确认释放，请重启小助手：%w", err)
	}
	return ctx.Err()
}

// CancelStartupSync 阻止新的绑定检查，取消并等待当前检查结束后再继续主程序退出。
func (s *Server) CancelStartupSync(ctx context.Context) error {
	if s == nil || s.startupSync == nil {
		return nil
	}
	return s.startupSync.stop(ctx)
}

// stop 关闭同步入口并等待协程完成，避免退出安装期间旧协程继续登记组件。
func (s *startupCamoufoxSync) stop(ctx context.Context) error {
	s.mu.Lock()
	s.closing = true
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("等待浏览器更新检查退出超时：%w", ctx.Err())
	}
}
