// 文件作用：缓存消息红点状态，使用Redis版本比较防止并发旧快照覆盖新计数，断连时降级到数据库和短期内存缓存。
package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const messageSummaryTTL = 60 * time.Second

// MessageSummaryCache 表示只缓存计数与版本的外部缓存，不存正文。
type MessageSummaryCache interface {
	Get(userEmail string) (MessageSummary, error)
	Put(userEmail string, summary MessageSummary) (MessageSummary, error)
}

// RedisMessageSummaryCache 以匿名账号键缓存轻量状态。
type RedisMessageSummaryCache struct{ client *redis.Client }

// NewRedisMessageSummaryCache 创建Redis缓存，连接错误由业务存储降级处理。
func NewRedisMessageSummaryCache(addr, password string, db int) *RedisMessageSummaryCache {
	return &RedisMessageSummaryCache{client: redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db, DialTimeout: 500 * time.Millisecond, ReadTimeout: 500 * time.Millisecond, WriteTimeout: 500 * time.Millisecond, MaxRetries: -1})}
}

// messageSummaryKey 对账号取摘要，避免Redis键名暴露邮箱。
func messageSummaryKey(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return "goodhr:messages:summary:v1:" + hex.EncodeToString(sum[:])
}

// decodeMessageSummary 校验缓存数据，坏缓存重新查询数据库。
func decodeMessageSummary(raw string) (MessageSummary, error) {
	var result MessageSummary
	err := json.Unmarshal([]byte(raw), &result)
	if err == nil && (result.UnreadCount < 0 || result.Version < 0) {
		err = errors.New("invalid message cache")
	}
	return result, err
}

// Get 读取Redis轻量计数，未命中返回ErrNotFound。
func (s *RedisMessageSummaryCache) Get(email string) (MessageSummary, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	raw, err := s.client.Get(ctx, messageSummaryKey(email)).Result()
	if errors.Is(err, redis.Nil) {
		err = ErrNotFound
	}
	if err != nil {
		return MessageSummary{}, err
	}
	return decodeMessageSummary(raw)
}

// Put 原子比较递增版本，旧数据库查询不能覆盖刚刚完成的新增或已读状态。
func (s *RedisMessageSummaryCache) Put(email string, summary MessageSummary) (MessageSummary, error) {
	raw, err := json.Marshal(summary)
	if err != nil {
		return MessageSummary{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := s.client.Eval(ctx, `local old=redis.call('GET',KEYS[1]); if old then local ok,value=pcall(cjson.decode,old); if ok and value.version and tonumber(value.version)>tonumber(ARGV[2]) then return old end end; redis.call('SET',KEYS[1],ARGV[1],'EX',ARGV[3]); return ARGV[1]`, []string{messageSummaryKey(email)}, string(raw), summary.Version, int(messageSummaryTTL/time.Second)).Text()
	if err != nil {
		return MessageSummary{}, err
	}
	return decodeMessageSummary(result)
}

type messageSummarySnapshot struct {
	summary MessageSummary
	expires time.Time
	pending bool
}

// CachedMessageStore 对同一账号合并缓存未命中的数据库查询，并即时刷新所有本进程消息变化。
type CachedMessageStore struct {
	persistent MessageStore
	cache      MessageSummaryCache
	locks      [64]sync.Mutex
	mu         sync.Mutex
	snapshots  map[string]messageSummarySnapshot
	now        func() time.Time
}

// NewCachedMessageStore 包装持久存储，Redis未配置时也避免每次红点轮询查数据库。
func NewCachedMessageStore(persistent MessageStore, cache MessageSummaryCache) *CachedMessageStore {
	return &CachedMessageStore{persistent: persistent, cache: cache, snapshots: map[string]messageSummarySnapshot{}, now: time.Now}
}

// userLock 以固定数量锁分片串行化同账号请求，不为每个账号永久分配锁。
func (s *CachedMessageStore) userLock(email string) *sync.Mutex {
	h := fnv.New32a()
	_, _ = h.Write([]byte(email))
	return &s.locks[h.Sum32()%uint32(len(s.locks))]
}

// snapshot 读取本进程最新缓存，版本不会因迟到查询倒退。
func (s *CachedMessageStore) snapshot(email string) (messageSummarySnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, found := s.snapshots[email]
	return value, found
}

// remember 保留最新版本并清除过期账号，限制常驻内存开销。
func (s *CachedMessageStore) remember(email string, summary MessageSummary, pending bool) MessageSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	old, found := s.snapshots[email]
	if found && old.summary.Version > summary.Version {
		return old.summary
	}
	if len(s.snapshots) >= 4096 {
		for key, value := range s.snapshots {
			if !value.expires.After(now) && !value.pending {
				delete(s.snapshots, key)
			}
		}
	}
	s.snapshots[email] = messageSummarySnapshot{summary: summary, expires: now.Add(messageSummaryTTL), pending: pending}
	return summary
}

// publish 将事务完成后的状态同步Redis；失败保留待重发快照，恢复后先修正缓存再使用它。
func (s *CachedMessageStore) publish(email string, summary MessageSummary) MessageSummary {
	if old, found := s.snapshot(email); found && old.summary.Version > summary.Version {
		summary = old.summary
	}
	if s.cache != nil {
		saved, err := s.cache.Put(email, summary)
		if err == nil {
			return s.remember(email, saved, false)
		}
		return s.remember(email, summary, true)
	}
	return s.remember(email, summary, false)
}

// Push 复用持久推送并立即发布新未读计数，幂等重试也会修复失败缓存。
func (s *CachedMessageStore) Push(email string, input MessageInput) (MessagePushResult, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	lock := s.userLock(email)
	lock.Lock()
	defer lock.Unlock()
	result, err := s.persistent.Push(email, input)
	if err == nil {
		result.Summary = s.publish(email, result.Summary)
	}
	return result, err
}

// Summary 优先Redis，故障时复用60秒本地缓存，并在恢复时补发未同步的最新状态。
func (s *CachedMessageStore) Summary(email string) (MessageSummary, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	lock := s.userLock(email)
	lock.Lock()
	defer lock.Unlock()
	local, found := s.snapshot(email)
	cacheUnavailable := s.cache == nil
	if s.cache != nil {
		if found && local.pending {
			if saved, err := s.cache.Put(email, local.summary); err == nil {
				return s.remember(email, saved, false), nil
			}
		}
		if remote, err := s.cache.Get(email); err == nil {
			if found && local.summary.Version > remote.Version {
				return s.publish(email, local.summary), nil
			}
			return s.remember(email, remote, false), nil
		} else {
			cacheUnavailable = !errors.Is(err, ErrNotFound)
		}
	}
	if cacheUnavailable && found && local.expires.After(s.now()) {
		return local.summary, nil
	}
	summary, err := s.persistent.Summary(email)
	if err != nil {
		return MessageSummary{}, err
	}
	return s.publish(email, summary), nil
}

// List 按需直接读取数据库标题列表，消息正文始终省略。
func (s *CachedMessageStore) List(email string, page, size int) (MessageList, error) {
	return s.persistent.List(strings.ToLower(strings.TrimSpace(email)), page, size)
}

// Read 首次展开后发布新版本，重复展开不会重复减少数量。
func (s *CachedMessageStore) Read(email, id string) (UserMessage, MessageSummary, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	lock := s.userLock(email)
	lock.Lock()
	defer lock.Unlock()
	message, summary, err := s.persistent.Read(email, id)
	if err == nil {
		summary = s.publish(email, summary)
	}
	return message, summary, err
}

// CleanupRead 清理后逐账号刷新缓存，版本比较保护并发推送和展开结果。
func (s *CachedMessageStore) CleanupRead(cutoff time.Time) ([]MessageUserSummary, error) {
	changed, err := s.persistent.CleanupRead(cutoff)
	if err != nil {
		return nil, err
	}
	for index, item := range changed {
		lock := s.userLock(item.UserEmail)
		lock.Lock()
		changed[index].Summary = s.publish(item.UserEmail, item.Summary)
		lock.Unlock()
	}
	return changed, nil
}
