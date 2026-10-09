// Package process 文件作用：验证 Worker 提前退出时立即返回真实启动阶段错误。
package process

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type unavailableHealth struct{}

// Health 模拟尚未监听成功的 Worker 健康接口。
func (unavailableHealth) Health(context.Context) error { return errors.New("not ready") }

// TestWaitHealthyReturnsWhenProcessExited 验证进程退出后不再错误等待十二秒。
func TestWaitHealthyReturnsWhenProcessExited(t *testing.T) {
	manager := &Manager{health: unavailableHealth{}}
	done := make(chan struct{})
	close(done)
	started := time.Now()
	err := manager.waitHealthy(context.Background(), done)
	if err == nil || !strings.Contains(err.Error(), "立即退出") {
		t.Fatalf("没有返回提前退出原因：%v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("提前退出仍等待过久：%s", elapsed)
	}
}

// TestManagerRetainedWorkerHelper 为进程保留验证提供独立测试进程，不打开真实用户浏览器。
func TestManagerRetainedWorkerHelper(t *testing.T) {
	if os.Getenv("GOODHR_RETAIN_WORKER_HELPER") != "true" {
		return
	}
	time.Sleep(time.Minute)
	os.Exit(0)
}

// TestStartRetainsAliveUnhealthyWorker 验证下一次启动时健康检查失败只报错，不停止旧 Worker 或创建第二个进程。
func TestStartRetainsAliveUnhealthyWorker(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestManagerRetainedWorkerHelper$")
	command.Env = append(os.Environ(), "GOODHR_RETAIN_WORKER_HELPER=true")
	configureProcess(command)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = command.Wait(); close(done) }()
	defer func() { _ = command.Process.Kill(); <-done }()
	manager := &Manager{health: unavailableHealth{}, command: command, done: done}
	err = manager.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "窗口保留") {
		t.Fatalf("健康检查失败没有说明保留窗口：%v", err)
	}
	if manager.command != command {
		t.Fatal("存活 Worker 被替换了")
	}
	select {
	case <-done:
		t.Fatal("健康检查失败停止了旧 Worker")
	default:
	}
}
