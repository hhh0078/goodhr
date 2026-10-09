// 本文件提供公众号短码的原子缓存占位、邮箱生成限频与微信错码限流，生产使用Redis，开发使用内存。
package httpapi

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const wechatBindingAttemptLimit = 5

// wechatBindingCache 将短码占位与固定十分钟窗口的限流封装为可测试的原子操作。
type wechatBindingCache interface {
	ReserveCode(ctx context.Context, hash, owner string, ttl time.Duration) (bool, error)
	ReleaseCode(ctx context.Context, hash, owner string) error
	TakeGeneration(ctx context.Context, email string) (bool, error)
	ReserveAttempt(ctx context.Context, appID, openID string) (string, error)
	FinishAttempt(ctx context.Context, appID, openID, ticket string, failed bool) error
}

// wechatCacheReservation 记录本次请求的随机所有者，避免旧请求删除后来创建的占位。
type wechatCacheReservation struct {
	owner     string
	expiresAt time.Time
}

// wechatCacheCounter 记录限流窗口和已占用次数，正确请求只归还同一窗口内的预留次数。
type wechatCacheCounter struct {
	owner     string
	count     int
	expiresAt time.Time
}

// memoryWechatBindingCache 使用锁模拟Redis的原子占位和计数，供开发及确定时钟测试使用。
type memoryWechatBindingCache struct {
	mu          sync.Mutex
	now         func() time.Time
	codes       map[string]wechatCacheReservation
	generations map[string]wechatCacheCounter
	attempts    map[string]wechatCacheCounter
}

// newMemoryWechatBindingCache 创建独立缓存，时钟闭包可以跟随服务测试时钟调整。
func newMemoryWechatBindingCache(now func() time.Time) *memoryWechatBindingCache {
	return &memoryWechatBindingCache{now: now, codes: make(map[string]wechatCacheReservation), generations: make(map[string]wechatCacheCounter), attempts: make(map[string]wechatCacheCounter)}
}

// ReserveCode 只有码不存在或已过期时才原子占位，现有有效所有者不会被覆盖。
func (c *memoryWechatBindingCache) ReserveCode(ctx context.Context, hash, owner string, ttl time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	now := c.now()
	if current, exists := c.codes[hash]; exists && current.expiresAt.After(now) {
		return false, nil
	}
	c.codes[hash] = wechatCacheReservation{owner: owner, expiresAt: now.Add(ttl)}
	return true, nil
}

// ReleaseCode 仅清理当前请求拥有的占位，防止迟到的清理删除新请求的码。
func (c *memoryWechatBindingCache) ReleaseCode(ctx context.Context, hash, owner string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if current, exists := c.codes[hash]; exists && current.owner == owner {
		delete(c.codes, hash)
	}
	return nil
}

// TakeGeneration 每个邮箱在十分钟内最多占用五次生成机会，碰撞重试不额外计数。
func (c *memoryWechatBindingCache) TakeGeneration(ctx context.Context, email string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	now := c.now()
	key := wechatBindingCodeHash(email)
	current := c.generations[key]
	if !current.expiresAt.After(now) {
		current = wechatCacheCounter{expiresAt: now.Add(wechatBindingTTL)}
	}
	if current.count >= wechatBindingAttemptLimit {
		return false, nil
	}
	current.count++
	c.generations[key] = current
	return true, nil
}

// ReserveAttempt 在执行存储前预留错误次数，避免并发猜码绕过五次限制。
func (c *memoryWechatBindingCache) ReserveAttempt(ctx context.Context, appID, openID string) (string, error) {
	owner, err := newWechatMessageNonce()
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return "", err
	}
	now := c.now()
	key := wechatFollowKey(appID, openID)
	current := c.attempts[key]
	if !current.expiresAt.After(now) {
		current = wechatCacheCounter{owner: owner, expiresAt: now.Add(wechatBindingTTL)}
	}
	if current.count >= wechatBindingAttemptLimit {
		return "", nil
	}
	current.count++
	c.attempts[key] = current
	return current.owner, nil
}

// FinishAttempt 保留真正错码的次数，正确或暂时业务失败仅归还原窗口的预留次数。
func (c *memoryWechatBindingCache) FinishAttempt(ctx context.Context, appID, openID, ticket string, failed bool) error {
	if failed || ticket == "" {
		return ctx.Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	key := wechatFollowKey(appID, openID)
	current := c.attempts[key]
	if current.owner == ticket && current.expiresAt.After(c.now()) && current.count > 0 {
		current.count--
		c.attempts[key] = current
	}
	return nil
}

// redisWechatBindingCache 共享现有认证Redis客户端，连接失败时不悄悄退回进程内缓存。
type redisWechatBindingCache struct{ client *redis.Client }

var wechatCodeReleaseScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then return redis.call('DEL', KEYS[1]) end
return 0`)

var wechatGenerationScript = redis.NewScript(`
local count = tonumber(redis.call('GET', KEYS[1]) or '0')
if count >= tonumber(ARGV[1]) then return 0 end
count = redis.call('INCR', KEYS[1])
if count == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[2]) end
return 1`)

var wechatAttemptReserveScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then
  redis.call('HSET', KEYS[1], 'owner', ARGV[1], 'count', 0)
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
local count = tonumber(redis.call('HGET', KEYS[1], 'count'))
if count >= tonumber(ARGV[3]) then return '' end
redis.call('HINCRBY', KEYS[1], 'count', 1)
return redis.call('HGET', KEYS[1], 'owner')`)

var wechatAttemptFinishScript = redis.NewScript(`
if redis.call('HGET', KEYS[1], 'owner') == ARGV[1] then
  local count = tonumber(redis.call('HGET', KEYS[1], 'count') or '0')
  if count > 0 then return redis.call('HINCRBY', KEYS[1], 'count', -1) end
end
return 0`)

// wechatCacheKey 仅将不可逆身份摘要放进Redis键，避免邮箱和原始OpenID出现在缓存键中。
func wechatCacheKey(kind, identity string) string {
	return "goodhr:wechat-binding:v1:" + kind + ":" + wechatBindingCodeHash(identity)
}

// ReserveCode 使用带TTL的SET NX原子占位，防止多个进程同时分配相同短码。
func (c *redisWechatBindingCache) ReserveCode(ctx context.Context, hash, owner string, ttl time.Duration) (bool, error) {
	return c.client.SetNX(ctx, wechatCacheKey("code", hash), owner, ttl).Result()
}

// ReleaseCode 使用所有者比较删除，旧请求永远不能清理后来请求的占位。
func (c *redisWechatBindingCache) ReleaseCode(ctx context.Context, hash, owner string) error {
	return wechatCodeReleaseScript.Run(ctx, c.client, []string{wechatCacheKey("code", hash)}, owner).Err()
}

// TakeGeneration 用单个Lua脚本完成计数和首次TTL设置，防止并发超过邮箱配额。
func (c *redisWechatBindingCache) TakeGeneration(ctx context.Context, email string) (bool, error) {
	value, err := wechatGenerationScript.Run(ctx, c.client, []string{wechatCacheKey("generation", email)}, wechatBindingAttemptLimit, wechatBindingTTL.Milliseconds()).Int()
	return value == 1, err
}

// ReserveAttempt 原子预留错码机会，并返回窗口所有者防止跨过期窗口错误扣减。
func (c *redisWechatBindingCache) ReserveAttempt(ctx context.Context, appID, openID string) (string, error) {
	owner, err := newWechatMessageNonce()
	if err != nil {
		return "", err
	}
	return wechatAttemptReserveScript.Run(ctx, c.client, []string{wechatCacheKey("attempt", wechatFollowKey(appID, openID))}, owner, wechatBindingTTL.Milliseconds(), wechatBindingAttemptLimit).Text()
}

// FinishAttempt 仅归还同一窗口的正确请求机会，错误码则保留计数直到十分钟窗口过期。
func (c *redisWechatBindingCache) FinishAttempt(ctx context.Context, appID, openID, ticket string, failed bool) error {
	if failed || ticket == "" {
		return ctx.Err()
	}
	return wechatAttemptFinishScript.Run(ctx, c.client, []string{wechatCacheKey("attempt", wechatFollowKey(appID, openID))}, ticket).Err()
}
