// 本文件验证短码缓存的原子并发、过期回收及窗口所有者，Redis集成仅允许专用本机测试端口。
package httpapi

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// wechatCacheFixture 封装缓存、隔离身份与主动过期能力，复用内存和真实Redis断言。
type wechatCacheFixture struct {
	cache    wechatBindingCache
	prefix   string
	expire   func()
	checkTTL func()
}

// newMemoryWechatCacheFixture 使用可推进时钟验证精确十分钟边界。
func newMemoryWechatCacheFixture(t *testing.T) wechatCacheFixture {
	t.Helper()
	now := time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)
	cache := newMemoryWechatBindingCache(func() time.Time { return now })
	return wechatCacheFixture{cache: cache, prefix: "memory", expire: func() { now = now.Add(wechatBindingTTL) }, checkTTL: func() {}}
}

// openWechatTestRedis 强制限定独立Redis地址与数据库，防止测试接触已有本机或生产缓存。
func openWechatTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("GOODHR_WECHAT_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("GOODHR_WECHAT_TEST_REDIS_ADDR is not configured")
	}
	if addr != "127.0.0.1:56389" {
		t.Fatal("wechat Redis tests require isolated 127.0.0.1:56389")
	}
	client := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// newRedisWechatCacheFixture 使用独立随机键验证真实Lua，并只主动过期本测试产生的键。
func newRedisWechatCacheFixture(t *testing.T) wechatCacheFixture {
	t.Helper()
	client := openWechatTestRedis(t)
	cache := &redisWechatBindingCache{client: client}
	nonce, err := newWechatMessageNonce()
	if err != nil {
		t.Fatal(err)
	}
	var keys sync.Map
	tracked := &trackedWechatBindingCache{wechatBindingCache: cache, keys: &keys}
	listKeys := func() []string {
		var result []string
		keys.Range(func(key, value any) bool { result = append(result, key.(string)); return true })
		return result
	}
	t.Cleanup(func() {
		if values := listKeys(); len(values) > 0 {
			_ = client.Del(context.Background(), values...).Err()
		}
	})
	return wechatCacheFixture{cache: tracked, prefix: nonce, expire: func() {
		for _, key := range listKeys() {
			if err := client.PExpire(context.Background(), key, time.Millisecond).Err(); err != nil {
				t.Fatal(err)
			}
		}
		deadline := time.Now().Add(time.Second)
		for {
			count, err := client.Exists(context.Background(), listKeys()...).Result()
			if err != nil {
				t.Fatal(err)
			}
			if count == 0 {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("Redis test keys did not expire")
			}
			time.Sleep(time.Millisecond)
		}
	}, checkTTL: func() {
		for _, key := range listKeys() {
			ttl, err := client.PTTL(context.Background(), key).Result()
			if err != nil || ttl <= 0 || ttl > wechatBindingTTL {
				t.Fatalf("invalid Redis TTL=%v err=%v", ttl, err)
			}
		}
	}}
}

// trackedWechatBindingCache 只记录本fixture访问的Redis键，清理和主动过期不影响其他测试。
type trackedWechatBindingCache struct {
	wechatBindingCache
	keys *sync.Map
}

// ReserveCode 记录码占位键后委托真实Redis原子操作。
func (c *trackedWechatBindingCache) ReserveCode(ctx context.Context, hash, owner string, ttl time.Duration) (bool, error) {
	c.keys.Store(wechatCacheKey("code", hash), true)
	return c.wechatBindingCache.ReserveCode(ctx, hash, owner, ttl)
}

// TakeGeneration 记录邮箱限流键后委托真实Redis计数。
func (c *trackedWechatBindingCache) TakeGeneration(ctx context.Context, email string) (bool, error) {
	c.keys.Store(wechatCacheKey("generation", email), true)
	return c.wechatBindingCache.TakeGeneration(ctx, email)
}

// ReserveAttempt 记录微信错误窗口键后委托真实Redis预留。
func (c *trackedWechatBindingCache) ReserveAttempt(ctx context.Context, appID, openID string) (string, error) {
	c.keys.Store(wechatCacheKey("attempt", wechatFollowKey(appID, openID)), true)
	return c.wechatBindingCache.ReserveAttempt(ctx, appID, openID)
}

// runWechatBindingCacheContract 检查双实现的占位、限频与并发错码限制。
func runWechatBindingCacheContract(t *testing.T, factory func(*testing.T) wechatCacheFixture) {
	t.Run("code-reservation-atomic-owner-and-expiry", func(t *testing.T) {
		fixture := factory(t)
		ctx := context.Background()
		hash := wechatBindingCodeHash(fixture.prefix + "code")
		owners := make(chan string, 24)
		errs := make(chan error, 24)
		var wait sync.WaitGroup
		for i := 0; i < 24; i++ {
			wait.Add(1)
			go func(i int) {
				defer wait.Done()
				owner := fmt.Sprintf("owner-%d", i)
				ok, err := fixture.cache.ReserveCode(ctx, hash, owner, wechatBindingTTL)
				if err != nil {
					errs <- err
				}
				if ok {
					owners <- owner
				}
			}(i)
		}
		wait.Wait()
		close(owners)
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		var winner string
		count := 0
		for owner := range owners {
			winner = owner
			count++
		}
		if count != 1 {
			t.Fatalf("reservation winners=%d", count)
		}
		fixture.checkTTL()
		if err := fixture.cache.ReleaseCode(ctx, hash, "not-owner"); err != nil {
			t.Fatal(err)
		}
		if ok, err := fixture.cache.ReserveCode(ctx, hash, "other", wechatBindingTTL); err != nil || ok {
			t.Fatalf("wrong owner cleared reservation: %v %v", ok, err)
		}
		fixture.expire()
		if ok, err := fixture.cache.ReserveCode(ctx, hash, "next", wechatBindingTTL); err != nil || !ok {
			t.Fatalf("expired reservation retained: %v %v", ok, err)
		}
		if err := fixture.cache.ReleaseCode(ctx, hash, winner); err != nil {
			t.Fatal(err)
		}
		if ok, err := fixture.cache.ReserveCode(ctx, hash, "last", wechatBindingTTL); err != nil || ok {
			t.Fatalf("old request cleared new reservation: %v %v", ok, err)
		}
		if err := fixture.cache.ReleaseCode(ctx, hash, "next"); err != nil {
			t.Fatal(err)
		}
		if ok, err := fixture.cache.ReserveCode(ctx, hash, "last", wechatBindingTTL); err != nil || !ok {
			t.Fatalf("owner release failed: %v %v", ok, err)
		}
	})
	t.Run("generation-concurrency-exactly-five-and-expiry", func(t *testing.T) {
		fixture := factory(t)
		ctx := context.Background()
		email := fixture.prefix + "@example.com"
		result := make(chan bool, 40)
		errs := make(chan error, 40)
		var wait sync.WaitGroup
		for i := 0; i < 40; i++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				ok, err := fixture.cache.TakeGeneration(ctx, email)
				result <- ok
				if err != nil {
					errs <- err
				}
			}()
		}
		wait.Wait()
		close(result)
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		count := 0
		for ok := range result {
			if ok {
				count++
			}
		}
		if count != 5 {
			t.Fatalf("generation allowed=%d", count)
		}
		fixture.checkTTL()
		fixture.expire()
		if ok, err := fixture.cache.TakeGeneration(ctx, email); err != nil || !ok {
			t.Fatalf("generation window stuck: %v %v", ok, err)
		}
	})
	t.Run("attempt-concurrency-correct-refund-new-window-protected", func(t *testing.T) {
		fixture := factory(t)
		ctx := context.Background()
		appID, openID := fixture.prefix, "private-openid"
		tickets := make(chan string, 40)
		errs := make(chan error, 40)
		var wait sync.WaitGroup
		for i := 0; i < 40; i++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				ticket, err := fixture.cache.ReserveAttempt(ctx, appID, openID)
				if err != nil {
					errs <- err
				}
				if ticket != "" {
					tickets <- ticket
				}
			}()
		}
		wait.Wait()
		close(tickets)
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		count := 0
		old := ""
		for ticket := range tickets {
			old = ticket
			count++
		}
		if count != 5 {
			t.Fatalf("attempt allowed=%d", count)
		}
		fixture.checkTTL()
		if err := fixture.cache.FinishAttempt(ctx, appID, openID, old, false); err != nil {
			t.Fatal(err)
		}
		refunded, err := fixture.cache.ReserveAttempt(ctx, appID, openID)
		if err != nil || refunded == "" {
			t.Fatalf("correct request counted as error: %v", err)
		}
		if err = fixture.cache.FinishAttempt(ctx, appID, openID, refunded, true); err != nil {
			t.Fatal(err)
		}
		if ticket, err := fixture.cache.ReserveAttempt(ctx, appID, openID); err != nil || ticket != "" {
			t.Fatalf("wrong request not counted: %q %v", ticket, err)
		}
		fixture.expire()
		for i := 0; i < 5; i++ {
			ticket, err := fixture.cache.ReserveAttempt(ctx, appID, openID)
			if err != nil || ticket == "" || ticket == old {
				t.Fatalf("new window invalid: %q %v", ticket, err)
			}
		}
		if err := fixture.cache.FinishAttempt(ctx, appID, openID, old, false); err != nil {
			t.Fatal(err)
		}
		if ticket, err := fixture.cache.ReserveAttempt(ctx, appID, openID); err != nil || ticket != "" {
			t.Fatalf("old refund changed new window: %q %v", ticket, err)
		}
		if ticket, err := fixture.cache.ReserveAttempt(ctx, appID, "different-openid"); err != nil || ticket == "" {
			t.Fatalf("one user limited another: %v", err)
		}
	})
}

// TestMemoryWechatBindingCacheContract 用内存锁验证原子行为及精确过期边界。
func TestMemoryWechatBindingCacheContract(t *testing.T) {
	runWechatBindingCacheContract(t, newMemoryWechatCacheFixture)
}

// TestRedisWechatBindingCacheContract 用独立Redis验证SET NX、Lua计数与TTL，没有配置时跳过。
func TestRedisWechatBindingCacheContract(t *testing.T) {
	runWechatBindingCacheContract(t, newRedisWechatCacheFixture)
}

// TestWechatBindingNonceAndCodeShape 验证四位数字码范围和独立256位nonce格式。
func TestWechatBindingNonceAndCodeShape(t *testing.T) {
	for i := 0; i < 256; i++ {
		code, err := newWechatBindingCode()
		if err != nil || len(code) != 4 || code[0] == '0' || strings.Trim(code, "0123456789") != "" {
			t.Fatalf("invalid short code=%q err=%v", code, err)
		}
		nonce, err := newWechatMessageNonce()
		if err != nil || len(nonce) != 64 || nonce == code {
			t.Fatalf("weak nonce=%q err=%v", nonce, err)
		}
	}
}

// TestWechatBindingCacheRedisSelectionAndFailure 验证生产复用认证客户端，缓存不可用时不会退回进程内缓存。
func TestWechatBindingCacheRedisSelectionAndFailure(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 25 * time.Millisecond, ReadTimeout: 25 * time.Millisecond, WriteTimeout: 25 * time.Millisecond, MaxRetries: -1})
	defer client.Close()
	service := NewWechatOfficialService(&AuthService{store: &RedisAuthStore{client: client}}, NewMemoryWechatFollowStore(nil), NewMemorySystemConfigStore())
	cache, ok := service.cache.(*redisWechatBindingCache)
	if !ok || cache.client != client {
		t.Fatal("service did not reuse auth Redis client")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if allowed, err := cache.TakeGeneration(ctx, "failure@example.com"); err == nil || allowed {
		t.Fatalf("Redis failure silently accepted: %v %v", allowed, err)
	}
}
