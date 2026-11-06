// Package auto_reply 本文件验证待办恢复的登录隔离、切号取消和应用退出行为，全部使用合成凭证。
package auto_reply

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// TestBackgroundRequiresMatchingSession 防止未登录、身份不匹配和插件会话恢复其他账号待办。
func TestBackgroundRequiresMatchingSession(t *testing.T) {
	for _, item := range []struct {
		name, owner, actual, kind string
		want                      int32
	}{
		{"未登录", "", "", "", 0}, {"身份不符", "one", "two", "", 0}, {"插件会话", "one", "one", "browser_extension", 0}, {"正常登录", "one", "one", "", 2},
	} {
		t.Run(item.name, func(t *testing.T) {
			var calls atomic.Int32
			process := func(ctx context.Context, credentials cloud.AgentCredentials) error {
				calls.Add(1)
				if credentials.Token != "synthetic" {
					t.Error("未使用当前凭证")
				}
				return nil
			}
			b := &Background{wake: make(chan struct{}, 1), validate: func(context.Context, string) (cloud.UserSession, error) {
				return cloud.UserSession{UserID: item.actual, SessionKind: item.kind}, nil
			}, outbox: process, recommendations: process}
			b.Bind(item.owner, "synthetic")
			b.flush(context.Background())
			if calls.Load() != item.want {
				t.Fatalf("调用次数=%d，预期=%d", calls.Load(), item.want)
			}
		})
	}
}

// TestBackgroundSwitchCancelsActive 验证切号取消旧在途请求且不会继续发起旧账号报告。
func TestBackgroundSwitchCancelsActive(t *testing.T) {
	started, done := make(chan struct{}), make(chan struct{})
	var reports atomic.Int32
	b := &Background{wake: make(chan struct{}, 1), validate: func(context.Context, string) (cloud.UserSession, error) { return cloud.UserSession{UserID: "one"}, nil }, outbox: func(ctx context.Context, _ cloud.AgentCredentials) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}, recommendations: func(context.Context, cloud.AgentCredentials) error { reports.Add(1); return nil }}
	b.Bind("one", "first")
	go func() { defer close(done); b.flush(context.Background()) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("待办未启动")
	}
	b.Bind("two", "second")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("切号没有取消旧请求")
	}
	if reports.Load() != 0 {
		t.Fatal("切号后仍启动旧账号报告")
	}
}

// TestBackgroundShutdownCancelsAndJoins 验证没有聊天任务也能唤醒恢复，应用退出能等到处理器完全结束。
func TestBackgroundShutdownCancelsAndJoins(t *testing.T) {
	started, done := make(chan struct{}), make(chan struct{})
	b := &Background{wake: make(chan struct{}, 1), validate: func(context.Context, string) (cloud.UserSession, error) { return cloud.UserSession{UserID: "one"}, nil }, outbox: func(ctx context.Context, _ cloud.AgentCredentials) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}, recommendations: func(context.Context, cloud.AgentCredentials) error { return nil }}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.Bind("one", "synthetic")
	go func() { defer close(done); b.Run(ctx) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("没有新消息时未恢复待办")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("退出未等待处理器完成")
	}
}
