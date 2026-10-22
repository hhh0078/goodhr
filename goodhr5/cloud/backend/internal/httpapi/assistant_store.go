// 本文件提供 AI 会话及可回放事件存储；只有用户新消息可以延长两小时截止时间。
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const assistantTTL = 2 * time.Hour

// assistantEvent 表示一条先保存后推送的完整 JSON 事件。
type assistantEvent struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	RunID     string         `json:"run_id"`
	Type      string         `json:"type"`
	Data      map[string]any `json:"data"`
	CreatedAt time.Time      `json:"created_at"`
}

// assistantRun 保存一次用户消息的状态、幂等键和候选人版本引用。
type assistantRun struct {
	ID         string           `json:"id"`
	RequestID  string           `json:"request_id"`
	Content    string           `json:"content"`
	Status     string           `json:"status"`
	Events     []assistantEvent `json:"events"`
	References map[string]int64 `json:"references"`
	StartedAt  time.Time        `json:"started_at"`
}

// assistantRecord 按团队及账号隔离，不保存 AI Key 或登录凭证。
type assistantRecord struct {
	ID        string                   `json:"id"`
	Owner     string                   `json:"owner"`
	TenantID  string                   `json:"tenant_id"`
	ExpiresAt time.Time                `json:"expires_at"`
	ActiveRun string                   `json:"active_run"`
	Messages  []AIMsg                  `json:"messages"`
	Runs      map[string]*assistantRun `json:"runs"`
}

// assistantStore 提供原子修改，避免多标签页及多个服务实例重复启动付费任务。
type assistantStore interface {
	Get(context.Context, string) (assistantRecord, error)
	Create(context.Context, string, assistantRecord) (assistantRecord, error)
	Update(context.Context, string, func(*assistantRecord) error) (assistantRecord, error)
}

// memoryAssistantStore 仅用于没有持久数据库的开发环境和测试。
type memoryAssistantStore struct {
	mu      sync.Mutex
	records map[string]assistantRecord
	now     func() time.Time
}

// newAssistantStore 复用认证 Redis；生产没有 Redis 时由调用方关闭助手，避免假装持久化。
func newAssistantStore(auth AuthStore) assistantStore {
	if store, ok := auth.(*RedisAuthStore); ok {
		return &redisAssistantStore{client: store.client}
	}
	return &memoryAssistantStore{records: map[string]assistantRecord{}, now: time.Now}
}

// assistantID 生成不可预测的请求或会话编号，返回十六进制字符串。
func assistantID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("系统随机数不可用")
	}
	return hex.EncodeToString(b[:])
}

// assistantKey 对账号与团队散列命名，原始邮箱不会进入 Redis key。
func assistantKey(scope CandidateLibraryScope, id string) string {
	return "goodhr:assistant:" + attachmentDigest(scope.TenantID+":"+scope.UserEmail) + ":" + id
}

// assistantCopy 隔离嵌套对象，禁止调用方绕过存储锁直接修改事件。
func assistantCopy(item assistantRecord) assistantRecord {
	raw, _ := json.Marshal(item)
	var result assistantRecord
	_ = json.Unmarshal(raw, &result)
	return result
}

// Get 读取尚未超过截止时间的会话；读取不续期。
func (s *memoryAssistantStore) Get(_ context.Context, key string) (assistantRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.records[key]
	if !ok || !s.now().Before(item.ExpiresAt) {
		delete(s.records, key)
		return assistantRecord{}, ErrNotFound
	}
	return assistantCopy(item), nil
}

// Create 幂等创建会话，已有会话的内容和截止时间保持原样。
func (s *memoryAssistantStore) Create(_ context.Context, key string, item assistantRecord) (assistantRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.records[key]; ok && s.now().Before(old.ExpiresAt) {
		return assistantCopy(old), nil
	}
	s.records[key] = assistantCopy(item)
	return assistantCopy(item), nil
}

// Update 在锁内修改状态，截止时间只采用业务传入值，不因保存事件重新计算。
func (s *memoryAssistantStore) Update(_ context.Context, key string, change func(*assistantRecord) error) (assistantRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.records[key]
	if !ok || !s.now().Before(old.ExpiresAt) {
		return assistantRecord{}, ErrNotFound
	}
	item := assistantCopy(old)
	if err := change(&item); err != nil {
		return assistantRecord{}, err
	}
	raw, err := json.Marshal(item)
	if err != nil || len(raw) > 4<<20 {
		return assistantRecord{}, errors.New("会话已装满，请新开一个会话")
	}
	s.records[key] = item
	return assistantCopy(item), nil
}

// redisAssistantStore 将完整事件回执持久保留至用户最后消息后两小时。
type redisAssistantStore struct{ client *redis.Client }

// Get 读取会话；缺失和过期均返回不可恢复的旧任务状态。
func (s *redisAssistantStore) Get(ctx context.Context, key string) (assistantRecord, error) {
	raw, err := s.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return assistantRecord{}, ErrNotFound
	}
	var item assistantRecord
	if err != nil {
		return item, err
	}
	err = json.Unmarshal(raw, &item)
	if err == nil && !time.Now().Before(item.ExpiresAt) {
		err = ErrNotFound
	}
	return item, err
}

// Create 用 SET NX 原子创建，刷新或网络重试不会覆盖活动任务。
func (s *redisAssistantStore) Create(ctx context.Context, key string, item assistantRecord) (assistantRecord, error) {
	raw, err := json.Marshal(item)
	if err != nil {
		return item, err
	}
	_, err = s.client.SetNX(ctx, key, raw, time.Until(item.ExpiresAt)).Result()
	if err != nil {
		return item, err
	}
	return s.Get(ctx, key)
}

// Update 使用 WATCH 事务防并发覆盖，PEXPIREAT 保留固定绝对期限，心跳和事件不续期。
func (s *redisAssistantStore) Update(ctx context.Context, key string, change func(*assistantRecord) error) (assistantRecord, error) {
	var item assistantRecord
	for attempt := 0; attempt < 8; attempt++ {
		err := s.client.Watch(ctx, func(tx *redis.Tx) error {
			raw, err := tx.Get(ctx, key).Bytes()
			if errors.Is(err, redis.Nil) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
			// 每次WATCH重试必须创建新对象，不能把失败事务的Runs map合并进真实快照。
			var snapshot assistantRecord
			if err = json.Unmarshal(raw, &snapshot); err != nil {
				return err
			}
			item = snapshot
			if !time.Now().Before(item.ExpiresAt) {
				return ErrNotFound
			}
			if err = change(&item); err != nil {
				return err
			}
			raw, err = json.Marshal(item)
			if err != nil {
				return err
			}
			if len(raw) > 4<<20 {
				return errors.New("会话已装满，请新开一个会话")
			}
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Set(ctx, key, raw, 0)
				pipe.PExpireAt(ctx, key, item.ExpiresAt)
				return nil
			})
			return err
		}, key)
		if err == redis.TxFailedErr {
			continue
		}
		return item, err
	}
	return item, errors.New("会话正在更新，请稍后重试")
}
