// Package auto_reply 本文件在当前登录会话下恢复本机待办，切号取消请求，退出前等待落盘，不保存登录凭证。
package auto_reply

import (
	"context"
	"sync"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// Background 让通知、授权上传和推荐任务在没有新聊天消息时也能恢复。
type Background struct {
	mu                      sync.Mutex
	flushMu                 sync.Mutex
	owner, token, machineID string
	binding                 context.Context
	cancel                  context.CancelFunc
	wake                    chan struct{}
	validate                func(context.Context, string) (cloud.UserSession, error)
	outbox                  func(context.Context, cloud.AgentCredentials) error
	recommendations         func(context.Context, cloud.AgentCredentials) error
}

// NewBackground 复用既有客户端和报告处理器；Windows与Mac均仅使用Go上下文与本机SQLite。
func NewBackground(client *cloud.Client, responder *AIResponder, machineID string) *Background {
	if client == nil || responder == nil {
		return nil
	}
	return &Background{machineID: machineID, wake: make(chan struct{}, 1), validate: client.ValidateSession, outbox: client.RetryAutoReplyOutbox, recommendations: responder.ProcessPendingRecommendations}
}

// Bind 仅接受已验证的用户身份；新的登录会话会立即取消旧会话的收费请求和上传。
func (b *Background) Bind(owner, token string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	if b.owner == owner && b.token == token {
		b.mu.Unlock()
		return
	}
	if b.cancel != nil {
		b.cancel()
	}
	b.owner, b.token = owner, token
	b.binding, b.cancel = context.WithCancel(context.Background())
	b.mu.Unlock()
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

// Run 登录后每十秒检查到期待办；应用退出取消处理并由调用方等待返回后关闭数据库。
func (b *Background) Run(ctx context.Context) {
	if b == nil {
		return
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-b.wake:
		}
		b.flush(ctx)
	}
}

// flush 每轮重新校验账号和会话类型，失败保留原待办；凭证仅存于当前调用栈和内存。
func (b *Background) flush(parent context.Context) {
	b.flushMu.Lock()
	defer b.flushMu.Unlock()
	b.mu.Lock()
	owner, token, binding := b.owner, b.token, b.binding
	b.mu.Unlock()
	if owner == "" || token == "" || binding == nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 4*time.Minute)
	defer cancel()
	stop := context.AfterFunc(binding, cancel)
	defer stop()
	if binding.Err() != nil || ctx.Err() != nil {
		return
	}
	session, err := b.validate(ctx, token)
	if err != nil || session.UserID != owner || session.SessionKind == "browser_extension" || ctx.Err() != nil {
		return
	}
	credentials := cloud.AgentCredentials{Token: token, MachineID: b.machineID}
	// 通知失败不阻断独立报告；每个待办自身保存重试状态和退避时间。
	_ = b.outbox(ctx, credentials)
	if ctx.Err() == nil {
		_ = b.recommendations(ctx, credentials)
	}
}
