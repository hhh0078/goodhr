// Package screening_sync 仅用当前已验证账号的内存凭证补传SQLite筛选快照，切号取消旧请求，不阻塞招聘流程。
package screening_sync

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/storage"
)

// Uploader 以单一后台循环发送完整快照，账号和Token只保存在进程内存。
type Uploader struct {
	store        *storage.Store
	cloud        *cloud.Client
	mu           sync.Mutex
	flushMu      sync.Mutex
	owner, token string
	generation   uint64
	binding      context.Context
	cancel       context.CancelFunc
	wake         chan struct{}
}

// New 复用本地存储与云端客户端，不创建新的网络协议或后台数据库。
func New(store *storage.Store, client *cloud.Client) *Uploader {
	return &Uploader{store: store, cloud: client, wake: make(chan struct{}, 1)}
}

// OwnerKey 由已验证云端身份生成本地归属键；正常账号优先使用不可变用户ID。
func OwnerKey(session cloud.UserSession) string {
	if id := strings.TrimSpace(session.UserID); id != "" {
		return id
	}
	if email := strings.ToLower(strings.TrimSpace(session.Email)); email != "" {
		return "email:" + email
	}
	return ""
}

// Bind 更新内存登录态并取消旧账号的在途请求；调用方只能传入云端验证过的身份。
func (u *Uploader) Bind(owner, token string) {
	if u == nil {
		return
	}
	u.mu.Lock()
	if owner == u.owner && token == u.token {
		u.mu.Unlock()
		return
	}
	if u.cancel != nil {
		u.cancel()
	}
	u.owner, u.token = owner, token
	u.generation++
	u.binding, u.cancel = context.WithCancel(context.Background())
	u.mu.Unlock()
	select {
	case u.wake <- struct{}{}:
	default:
	}
}

// Run 每三秒尝试补传，程序结束取消HTTP；不持久保存凭证，也不因断网结束招聘任务。
func (u *Uploader) Run(ctx context.Context) {
	if u == nil || u.store == nil || u.cloud == nil {
		return
	}
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-u.wake:
		}
		u.Flush(ctx)
	}
}

// Flush 发送有界批次，逐次核实当前账号；回执只确认发送版本，不能清除上传时刚产生的二轮结果。
func (u *Uploader) Flush(parent context.Context) {
	if u == nil || u.store == nil || u.cloud == nil {
		return
	}
	u.flushMu.Lock()
	defer u.flushMu.Unlock()
	u.mu.Lock()
	owner, token, generation, binding := u.owner, u.token, u.generation, u.binding
	u.mu.Unlock()
	if owner == "" || token == "" || binding == nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	stop := context.AfterFunc(binding, cancel)
	defer stop()
	items, err := u.store.PendingScreeningRecords(ctx, owner, 30)
	if err != nil || len(items) == 0 {
		return
	}
	current, err := u.cloud.ValidateSession(ctx, token)
	if err != nil || OwnerKey(current) != owner || current.SessionKind == "browser_extension" {
		return
	}
	for _, item := range items {
		u.mu.Lock()
		valid := u.generation == generation && u.owner == owner && u.token == token
		u.mu.Unlock()
		if !valid || ctx.Err() != nil {
			return
		}
		_, err = u.cloud.SaveScreeningRecord(ctx, token, item.PositionID, item.Snapshot)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			_ = u.store.DeferScreeningRecord(ctx, owner, item.Snapshot.AttemptID, item.Snapshot.Revision)
			// 错误正文可能含业务数据或凭证，普通日志只记录编号和待补传状态。
			log.Printf("flow=screening_sync attempt_id=%s revision=%d status=pending message=筛选记录暂未同步，稍后自动补传", item.Snapshot.AttemptID, item.Snapshot.Revision)
			continue
		}
		u.mu.Lock()
		valid = u.generation == generation && u.owner == owner
		u.mu.Unlock()
		if !valid {
			return
		}
		if err = u.store.AckScreeningRecord(ctx, owner, item.Snapshot.AttemptID, item.Snapshot.Revision); err != nil {
			return
		}
	}
}
