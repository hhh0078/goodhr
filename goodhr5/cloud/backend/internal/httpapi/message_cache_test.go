// 文件作用：验证消息红点缓存命中、并发合并、故障降级、恢复补发及真实Redis原子版本比较。
package httpapi

import (
	"context"
	"errors"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type countingMessageStore struct {
	MessageStore
	reads atomic.Int64
}

// Summary 记录实际持久层读取次数。
func (s *countingMessageStore) Summary(email string) (MessageSummary, error) {
	s.reads.Add(1)
	return s.MessageStore.Summary(email)
}

type fakeMessageSummaryCache struct {
	mu      sync.Mutex
	items   map[string]MessageSummary
	offline bool
}

// Get 模拟Redis未命中或故障，不访问任何真实服务。
func (s *fakeMessageSummaryCache) Get(email string) (MessageSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.offline {
		return MessageSummary{}, errors.New("cache offline")
	}
	value, found := s.items[email]
	if !found {
		return MessageSummary{}, ErrNotFound
	}
	return value, nil
}

// Put 模拟Redis原子版本比较，迟到的旧版本不会覆盖新版本。
func (s *fakeMessageSummaryCache) Put(email string, summary MessageSummary) (MessageSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.offline {
		return MessageSummary{}, errors.New("cache offline")
	}
	if old, found := s.items[email]; found && old.Version > summary.Version {
		return old, nil
	}
	s.items[email] = summary
	return summary, nil
}

// TestMessageCacheHitMutationAndRecovery 验证10秒轮询无需重复查库，读写即时刷新且缓存恢复不会显示旧红点。
func TestMessageCacheHitMutationAndRecovery(t *testing.T) {
	persistent := &countingMessageStore{MessageStore: NewMemoryMessageStore()}
	cache := &fakeMessageSummaryCache{items: map[string]MessageSummary{}}
	store := NewCachedMessageStore(persistent, cache)
	const email = "owner@example.test"
	var wg sync.WaitGroup
	failures := make(chan error, 64)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Summary(email)
			if err != nil {
				failures <- err
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	if persistent.reads.Load() != 1 {
		t.Fatalf("stampede reads=%d", persistent.reads.Load())
	}
	result, err := store.Push(email, messageFixture("cache-one"))
	if err != nil {
		t.Fatal(err)
	}
	summary, err := store.Summary(email)
	if err != nil || summary != (MessageSummary{1, 1}) || persistent.reads.Load() != 1 {
		t.Fatal(summary, err, persistent.reads.Load())
	}
	cache.mu.Lock()
	cache.offline = true
	cache.mu.Unlock()
	_, summary, err = store.Read(email, result.Message.ID)
	if err != nil || summary != (MessageSummary{0, 2}) {
		t.Fatal(summary, err)
	}
	for i := 0; i < 6; i++ {
		summary, err = store.Summary(email)
		if err != nil || summary.UnreadCount != 0 {
			t.Fatal(summary, err)
		}
	}
	if persistent.reads.Load() != 1 {
		t.Fatal("offline poll bypassed local cache")
	}
	cache.mu.Lock()
	cache.offline = false
	cache.mu.Unlock()
	summary, err = store.Summary(email)
	if err != nil || summary != (MessageSummary{0, 2}) {
		t.Fatal(summary, err)
	}
	remote, _ := cache.Get(email)
	if remote != summary {
		t.Fatal(remote, summary)
	}
	// 另一个进程在数据库读取完成后先发布新状态，本进程必须返回缓存中的最新版本。
	cache.mu.Lock()
	cache.items[email] = MessageSummary{3, 9}
	cache.mu.Unlock()
	summary = store.publish(email, MessageSummary{0, 3})
	if summary != (MessageSummary{3, 9}) {
		t.Fatal("old fetch overwrote new version", summary)
	}
}

// TestMessageCacheExpiryAndUnavailableFallback 验证Redis缺失或断连时数据库读取合并且60秒后刷新。
func TestMessageCacheExpiryAndUnavailableFallback(t *testing.T) {
	for _, external := range []bool{false, true} {
		persistent := &countingMessageStore{MessageStore: NewMemoryMessageStore()}
		var cache MessageSummaryCache
		if external {
			cache = &fakeMessageSummaryCache{items: map[string]MessageSummary{}, offline: true}
		}
		store := NewCachedMessageStore(persistent, cache)
		now := time.Now()
		store.now = func() time.Time { return now }
		for i := 0; i < 6; i++ {
			if _, err := store.Summary("offline@example.test"); err != nil {
				t.Fatal(err)
			}
			now = now.Add(10 * time.Second)
		}
		if persistent.reads.Load() != 1 {
			t.Fatalf("external=%v reads=%d", external, persistent.reads.Load())
		}
		if _, err := store.Summary("offline@example.test"); err != nil {
			t.Fatal(err)
		}
		if persistent.reads.Load() != 2 {
			t.Fatalf("expired cache did not refresh: %d", persistent.reads.Load())
		}
	}
}

// TestRedisMessageSummaryVersionComparison 验证两个真实Redis客户端的并发发布只保留最大版本，测试仅允许明确本机地址。
func TestRedisMessageSummaryVersionComparison(t *testing.T) {
	addr := os.Getenv("GOODHR_MESSAGE_REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("GOODHR_MESSAGE_REDIS_TEST_ADDR is not set")
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
		t.Fatal("message Redis test requires loopback address")
	}
	first := NewRedisMessageSummaryCache(addr, "", 0)
	second := NewRedisMessageSummaryCache(addr, "", 0)
	email := randomMailID() + "@message-cache.test"
	t.Cleanup(func() {
		first.client.Del(context.Background(), messageSummaryKey(email))
		first.client.Close()
		second.client.Close()
	})
	var wg sync.WaitGroup
	failures := make(chan error, 64)
	for index := 0; index < 64; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			cache := first
			if index%2 == 0 {
				cache = second
			}
			_, err := cache.Put(email, MessageSummary{int64(index), int64(index)})
			if err != nil {
				failures <- err
			}
		}(index)
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	summary, err := first.Get(email)
	if err != nil || summary != (MessageSummary{63, 63}) {
		t.Fatal(summary, err)
	}
	ttl, err := first.client.TTL(context.Background(), messageSummaryKey(email)).Result()
	if err != nil || ttl <= 0 || ttl > messageSummaryTTL {
		t.Fatal(ttl, err)
	}
	// 模拟坏缓存后覆盖：Lua不能因JSON损坏而阻断持久层降级。
	first.client.Set(context.Background(), messageSummaryKey(email), "not-json", time.Minute)
	if _, err = first.Get(email); err == nil {
		t.Fatal("bad cache accepted")
	}
	summary, err = second.Put(email, MessageSummary{0, 64})
	if err != nil || summary.Version != 64 {
		t.Fatal(summary, err)
	}
}
