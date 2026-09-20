// Package process 文件作用：验证 Worker 提前退出时立即返回真实启动阶段错误。
package process

import (
	"context"
	"errors"
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
