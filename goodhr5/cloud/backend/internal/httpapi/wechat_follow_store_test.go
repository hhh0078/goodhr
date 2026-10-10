// 本文件验证公众号绑定码、关注时序、奖励幂等及用户公开字段，全部使用内存数据和模拟 HTTP 请求。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// wechatStoreFixture 为内存与隔离 PostgreSQL 复用同一组存储行为断言。
type wechatStoreFixture struct {
	store WechatFollowStore
	appID string
	now   time.Time
	seed  func(email string, subscription Subscription)
	read  func(email string) Subscription
}

// newMemoryWechatFixture 创建固定时间的会员与公众号存储，避免测试依赖真实时钟。
func newMemoryWechatFixture(t *testing.T) wechatStoreFixture {
	t.Helper()
	now := time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC)
	subscriptions := NewMemorySubscriptionStore()
	subscriptions.now = func() time.Time { return now }
	return wechatStoreFixture{
		store: NewMemoryWechatFollowStore(subscriptions), appID: "wx-unit-test", now: now,
		seed: func(email string, subscription Subscription) { subscriptions.items[email] = subscription },
		read: func(email string) Subscription {
			item, err := subscriptions.UserSubscription(email)
			if err != nil {
				t.Fatal(err)
			}
			return item
		},
	}
}

// wechatTestCodeHash 生成测试码摘要，不向存储传递原始绑定码。
func wechatTestCodeHash(code string) string {
	return wechatOpenIDFingerprint("binding-code:" + code)
}

// createWechatTestCode 为指定邮箱生成十分钟有效的模拟绑定码。
func createWechatTestCode(t *testing.T, fixture wechatStoreFixture, email, code string) string {
	t.Helper()
	hash := wechatTestCodeHash(code)
	if err := fixture.store.CreateBinding(context.Background(), email, hash, fixture.now.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	return hash
}

// recordWechatTestFollow 保存模拟关注事件，调用方显式给出时间以覆盖乱序边界。
func recordWechatTestFollow(t *testing.T, fixture wechatStoreFixture, openID string, followed bool, eventAt time.Time) {
	t.Helper()
	if err := fixture.store.RecordFollow(context.Background(), fixture.appID, openID, followed, eventAt); err != nil {
		t.Fatal(err)
	}
}

// assertWechatSubscription 检查会员等级和精确到期时间，而不只比较有效状态。
func assertWechatSubscription(t *testing.T, actual Subscription, memberType string, expiresAt time.Time) {
	t.Helper()
	if actual.MemberType != memberType || !actual.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("subscription=%+v, want type=%s expiry=%s", actual, memberType, expiresAt)
	}
}

// runWechatStoreContract 覆盖两种存储必须共同满足的业务边界，每组使用独立实例或数据命名空间。
func runWechatStoreContract(t *testing.T, newFixture func(t *testing.T) wechatStoreFixture) {
	t.Helper()
	t.Run("unknown-follow-and-mistyped-code-do-not-consume", func(t *testing.T) {
		fixture := newFixture(t)
		code := createWechatTestCode(t, fixture, "unknown@example.com", "unknown")
		if _, err := fixture.store.BindCode(context.Background(), fixture.appID, "openid-unknown", code, fixture.now); !errors.Is(err, ErrWechatFollowRequired) {
			t.Fatalf("unknown follow err=%v", err)
		}
		if _, err := fixture.store.BindCode(context.Background(), fixture.appID, "openid-unknown", wechatTestCodeHash("wrong"), fixture.now); !errors.Is(err, ErrWechatBindingExpired) {
			t.Fatalf("wrong code err=%v", err)
		}
		recordWechatTestFollow(t, fixture, "openid-unknown", true, fixture.now)
		result, err := fixture.store.BindCode(context.Background(), fixture.appID, "openid-unknown", code, fixture.now)
		if err != nil || !result.GiftGranted {
			t.Fatalf("valid retry result=%+v err=%v", result, err)
		}
	})
	t.Run("new-code-invalidates-old-and-expiry-boundary", func(t *testing.T) {
		fixture := newFixture(t)
		old := createWechatTestCode(t, fixture, "expiry@example.com", "old")
		code := createWechatTestCode(t, fixture, "expiry@example.com", "new")
		recordWechatTestFollow(t, fixture, "openid-expiry", true, fixture.now)
		for _, input := range []struct {
			hash string
			now  time.Time
		}{{old, fixture.now}, {code, fixture.now.Add(10 * time.Minute)}} {
			if _, err := fixture.store.BindCode(context.Background(), fixture.appID, "openid-expiry", input.hash, input.now); !errors.Is(err, ErrWechatBindingExpired) {
				t.Fatalf("expired code err=%v", err)
			}
		}
		result, err := fixture.store.BindCode(context.Background(), fixture.appID, "openid-expiry", code, fixture.now)
		if err != nil || !result.GiftGranted {
			t.Fatalf("latest code result=%+v err=%v", result, err)
		}
	})
	t.Run("repeated-completion-only-succeeds-for-original-wechat", func(t *testing.T) {
		fixture := newFixture(t)
		code := createWechatTestCode(t, fixture, "repeat@example.com", "repeat")
		recordWechatTestFollow(t, fixture, "openid-repeat", true, fixture.now)
		first, err := fixture.store.BindCode(context.Background(), fixture.appID, "openid-repeat", code, fixture.now)
		if err != nil || !first.GiftGranted {
			t.Fatalf("first result=%+v err=%v", first, err)
		}
		for i := 0; i < 3; i++ {
			replay, replayErr := fixture.store.BindCode(context.Background(), fixture.appID, "openid-repeat", code, fixture.now.Add(time.Minute))
			if replayErr != nil || !replay.AlreadyBound || replay.GiftGranted {
				t.Fatalf("replay result=%+v err=%v", replay, replayErr)
			}
			assertWechatSubscription(t, replay.Subscription, first.Subscription.MemberType, first.Subscription.ExpiresAt)
		}
		if _, err = fixture.store.BindCode(context.Background(), fixture.appID, "someone-else", code, fixture.now); !errors.Is(err, ErrWechatBindingExpired) {
			t.Fatalf("completion identity mismatch err=%v", err)
		}
	})
	t.Run("active-paid-tier-preserved-otherwise-five-days-max", func(t *testing.T) {
		fixture := newFixture(t)
		cases := []struct {
			name, tier string
			offset     time.Duration
			wantTier   string
			wantOffset time.Duration
		}{
			{"active-plus", memberTypePlus, 30 * 24 * time.Hour, memberTypePlus, 32 * 24 * time.Hour},
			{"active-max", memberTypeMax, 90 * 24 * time.Hour, memberTypeMax, 92 * 24 * time.Hour},
			{"free-with-future-expiry", memberTypeFree, 90 * 24 * time.Hour, memberTypeMax, 2 * 24 * time.Hour},
			{"expired-plus", memberTypePlus, -time.Hour, memberTypeMax, 2 * 24 * time.Hour},
			{"expired-max", memberTypeMax, -time.Hour, memberTypeMax, 2 * 24 * time.Hour},
			{"expiry-exactly-now", memberTypePlus, 0, memberTypeMax, 2 * 24 * time.Hour},
		}
		for _, item := range cases {
			email, openID := item.name+"@example.com", "openid-"+item.name
			fixture.seed(email, Subscription{MemberType: item.tier, ExpiresAt: fixture.now.Add(item.offset)})
			code := createWechatTestCode(t, fixture, email, item.name)
			recordWechatTestFollow(t, fixture, openID, true, fixture.now)
			result, err := fixture.store.BindCode(context.Background(), fixture.appID, openID, code, fixture.now)
			if err != nil || !result.GiftGranted {
				t.Fatalf("%s result=%+v err=%v", item.name, result, err)
			}
			assertWechatSubscription(t, result.Subscription, item.wantTier, fixture.now.Add(item.wantOffset))
		}
	})
	t.Run("unsubscribe-rejoin-change-email-or-wechat-never-reward-twice", func(t *testing.T) {
		fixture := newFixture(t)
		code := createWechatTestCode(t, fixture, "first@example.com", "first")
		recordWechatTestFollow(t, fixture, "openid-shared", true, fixture.now)
		first, err := fixture.store.BindCode(context.Background(), fixture.appID, "openid-shared", code, fixture.now)
		if err != nil || !first.GiftGranted {
			t.Fatalf("first result=%+v err=%v", first, err)
		}
		recordWechatTestFollow(t, fixture, "openid-shared", false, fixture.now.Add(time.Second))
		status, err := fixture.store.BindingStatus(context.Background(), "first@example.com")
		if err != nil || status.Bound || status.Followed || status.OpenID != "" || !status.GiftGranted {
			t.Fatalf("unsubscribe status=%+v err=%v", status, err)
		}
		recordWechatTestFollow(t, fixture, "openid-shared", true, fixture.now.Add(2*time.Second))
		if _, err = fixture.store.BindCode(context.Background(), fixture.appID, "openid-shared", code, fixture.now.Add(3*time.Second)); !errors.Is(err, ErrWechatBindingExpired) {
			t.Fatalf("old consumed code must not rebind: %v", err)
		}
		newCode := createWechatTestCode(t, fixture, "second@example.com", "second")
		second, err := fixture.store.BindCode(context.Background(), fixture.appID, "openid-shared", newCode, fixture.now.Add(3*time.Second))
		if err != nil || second.GiftGranted {
			t.Fatalf("changed email result=%+v err=%v", second, err)
		}
		recordWechatTestFollow(t, fixture, "openid-other", true, fixture.now.Add(4*time.Second))
		firstCode := createWechatTestCode(t, fixture, "first@example.com", "first-again")
		changedWechat, err := fixture.store.BindCode(context.Background(), fixture.appID, "openid-other", firstCode, fixture.now.Add(5*time.Second))
		if err != nil || changedWechat.GiftGranted {
			t.Fatalf("changed wechat result=%+v err=%v", changedWechat, err)
		}
		assertWechatSubscription(t, fixture.read("first@example.com"), first.Subscription.MemberType, first.Subscription.ExpiresAt)
	})
	t.Run("bound-wechat-conflict-does-not-consume-new-code", func(t *testing.T) {
		fixture := newFixture(t)
		firstCode := createWechatTestCode(t, fixture, "owner@example.com", "owner")
		otherCode := createWechatTestCode(t, fixture, "other@example.com", "other")
		recordWechatTestFollow(t, fixture, "openid-owned", true, fixture.now)
		if _, err := fixture.store.BindCode(context.Background(), fixture.appID, "openid-owned", firstCode, fixture.now); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.store.BindCode(context.Background(), fixture.appID, "openid-owned", otherCode, fixture.now); !errors.Is(err, ErrWechatOpenIDConflict) {
			t.Fatalf("openid conflict err=%v", err)
		}
		recordWechatTestFollow(t, fixture, "openid-owned", false, fixture.now.Add(time.Second))
		recordWechatTestFollow(t, fixture, "openid-owned", true, fixture.now.Add(2*time.Second))
		result, err := fixture.store.BindCode(context.Background(), fixture.appID, "openid-owned", otherCode, fixture.now.Add(3*time.Second))
		if err != nil || result.GiftGranted {
			t.Fatalf("conflict retry result=%+v err=%v", result, err)
		}
	})
	t.Run("delayed-unsubscribe-cannot-clear-new-binding", func(t *testing.T) {
		fixture := newFixture(t)
		code := createWechatTestCode(t, fixture, "events@example.com", "events")
		recordWechatTestFollow(t, fixture, "openid-events", true, fixture.now.Add(3*time.Second))
		if _, err := fixture.store.BindCode(context.Background(), fixture.appID, "openid-events", code, fixture.now.Add(4*time.Second)); err != nil {
			t.Fatal(err)
		}
		recordWechatTestFollow(t, fixture, "openid-events", false, fixture.now.Add(2*time.Second))
		status, err := fixture.store.BindingStatus(context.Background(), "events@example.com")
		if err != nil || !status.Bound || !status.Followed {
			t.Fatalf("late unsubscribe status=%+v err=%v", status, err)
		}
		recordWechatTestFollow(t, fixture, "openid-events", false, fixture.now.Add(5*time.Second))
		recordWechatTestFollow(t, fixture, "openid-events", true, fixture.now.Add(5*time.Second))
		status, err = fixture.store.BindingStatus(context.Background(), "events@example.com")
		if err != nil || status.Bound || status.Followed {
			t.Fatalf("same-second conflict status=%+v err=%v", status, err)
		}
	})
	t.Run("concurrent-repeated-callbacks-grant-once", func(t *testing.T) {
		fixture := newFixture(t)
		fixture.seed("concurrent@example.com", Subscription{MemberType: memberTypePlus, ExpiresAt: fixture.now.Add(30 * 24 * time.Hour)})
		code := createWechatTestCode(t, fixture, "concurrent@example.com", "concurrent")
		recordWechatTestFollow(t, fixture, "openid-concurrent", true, fixture.now)
		type outcome struct {
			result WechatBindResult
			err    error
		}
		outcomes := make(chan outcome, 24)
		var pending sync.WaitGroup
		for i := 0; i < cap(outcomes); i++ {
			pending.Add(1)
			go func() {
				defer pending.Done()
				result, err := fixture.store.BindCode(context.Background(), fixture.appID, "openid-concurrent", code, fixture.now)
				outcomes <- outcome{result: result, err: err}
			}()
		}
		pending.Wait()
		close(outcomes)
		granted := 0
		for item := range outcomes {
			if item.err != nil {
				t.Fatal(item.err)
			}
			if item.result.GiftGranted {
				granted++
			}
		}
		if granted != 1 {
			t.Fatalf("gift count=%d, want 1", granted)
		}
		assertWechatSubscription(t, fixture.read("concurrent@example.com"), memberTypePlus, fixture.now.Add(32*24*time.Hour))
	})
	t.Run("new-code-for-bound-user-and-other-wechat-conflict", func(t *testing.T) {
		fixture := newFixture(t)
		code := createWechatTestCode(t, fixture, "bound@example.com", "bound-first")
		recordWechatTestFollow(t, fixture, "openid-current", true, fixture.now)
		first, err := fixture.store.BindCode(context.Background(), fixture.appID, "openid-current", code, fixture.now)
		if err != nil || !first.GiftGranted {
			t.Fatalf("first result=%+v err=%v", first, err)
		}
		code = createWechatTestCode(t, fixture, "bound@example.com", "bound-new")
		recordWechatTestFollow(t, fixture, "openid-not-current", true, fixture.now)
		if _, err = fixture.store.BindCode(context.Background(), fixture.appID, "openid-not-current", code, fixture.now); !errors.Is(err, ErrWechatOpenIDConflict) {
			t.Fatalf("different current wechat err=%v", err)
		}
		result, err := fixture.store.BindCode(context.Background(), fixture.appID, "openid-current", code, fixture.now)
		if err != nil || !result.AlreadyBound || result.GiftGranted {
			t.Fatalf("same binding result=%+v err=%v", result, err)
		}
		assertWechatSubscription(t, result.Subscription, first.Subscription.MemberType, first.Subscription.ExpiresAt)
	})
	t.Run("parallel-unsubscribe-and-bind-have-no-deadlock-or-stale-binding", func(t *testing.T) {
		fixture := newFixture(t)
		for i := 0; i < 8; i++ {
			email, openID := fmt.Sprintf("event-race-%d@example.com", i), fmt.Sprintf("openid-event-race-%d", i)
			code := createWechatTestCode(t, fixture, email, email)
			recordWechatTestFollow(t, fixture, openID, true, fixture.now)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			bindDone, followDone := make(chan error, 1), make(chan error, 1)
			go func() {
				_, err := fixture.store.BindCode(ctx, fixture.appID, openID, code, fixture.now)
				bindDone <- err
			}()
			go func() {
				followDone <- fixture.store.RecordFollow(ctx, fixture.appID, openID, false, fixture.now.Add(time.Second))
			}()
			bindErr, followErr := <-bindDone, <-followDone
			cancel()
			if bindErr != nil && !errors.Is(bindErr, ErrWechatFollowRequired) {
				t.Fatalf("bind error=%v", bindErr)
			}
			if followErr != nil {
				t.Fatalf("unsubscribe error=%v", followErr)
			}
			status, err := fixture.store.BindingStatus(context.Background(), email)
			if err != nil || status.Bound || status.Followed || status.OpenID != "" {
				t.Fatalf("event race status=%+v err=%v", status, err)
			}
		}
	})
	t.Run("cancelled-context-and-invalid-input-do-not-write", func(t *testing.T) {
		fixture := newFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := fixture.store.CreateBinding(ctx, "cancelled@example.com", wechatTestCodeHash("cancelled"), fixture.now.Add(time.Minute)); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled create err=%v", err)
		}
		if err := fixture.store.CreateBinding(context.Background(), "invalid", "raw-code", fixture.now); err == nil {
			t.Fatal("invalid email/code was accepted")
		}
		if _, err := fixture.store.BindCode(context.Background(), fixture.appID, "openid", "raw-code", fixture.now); !errors.Is(err, ErrWechatBindingExpired) {
			t.Fatalf("raw code err=%v", err)
		}
	})
}

// TestMemoryWechatFollowStoreContract 验证内存实现与生产存储共享的完整行为要求。
func TestMemoryWechatFollowStoreContract(t *testing.T) {
	runWechatStoreContract(t, newMemoryWechatFixture)
}

// failingWechatStatusStore 模拟返回不可信绑定状态和错误的存储，验证公开信息采用失败关闭。
type failingWechatStatusStore struct{ WechatFollowStore }

// BindingStatus 返回错误和错误的成功状态，调用方必须丢弃状态而不是误报已绑定。
func (failingWechatStatusStore) BindingStatus(context.Context, string) (WechatBindingStatus, error) {
	return WechatBindingStatus{Bound: true, GiftGranted: true, OpenID: "private-openid"}, errors.New("temporary storage failure")
}

// TestWechatAuthAndAdminPublicFields 验证登录信息与管理员列表只公开布尔字段，取消关注后历史奖励仍为真。
func TestWechatAuthAndAdminPublicFields(t *testing.T) {
	encodedStatus, err := json.Marshal(WechatBindingStatus{Bound: true, OpenID: "private-openid"})
	if err != nil || strings.Contains(string(encodedStatus), "private-openid") {
		t.Fatalf("internal status leaked OpenID: %s err=%v", encodedStatus, err)
	}
	server := mustNewServer(t)
	subscriptions, ok := server.auth.subscriptions.(*MemorySubscriptionStore)
	if !ok {
		t.Fatal("memory subscriptions required")
	}
	store := NewMemoryWechatFollowStore(subscriptions)
	server.auth.wechatFollowStore = store
	routes := server.Routes()
	email := "public-wechat@example.com"
	token := loginForTest(t, routes, email)
	adminToken := loginForTest(t, routes, "1224299352@qq.com")
	now := time.Now()
	hash := wechatTestCodeHash("public")
	if err := store.CreateBinding(context.Background(), email, hash, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordFollow(context.Background(), "wx-public", "private-openid", true, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindCode(context.Background(), "wx-public", "private-openid", hash, now); err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct{ path, token string }{{"/api/auth/me", token}, {"/api/admin/users", adminToken}} {
		req := httptest.NewRequest(http.MethodGet, input.path, nil)
		req.Header.Set("Authorization", "Bearer "+input.token)
		resp := httptest.NewRecorder()
		routes.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("%s code=%d body=%s", input.path, resp.Code, resp.Body.String())
		}
		if strings.Contains(resp.Body.String(), "private-openid") {
			t.Fatal("OpenID leaked in public response")
		}
		var payload map[string]any
		if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if input.path == "/api/auth/me" {
			user := payload["user"].(map[string]any)
			if user["has_openid"] != true || user["wechat_follow_gift_granted"] != true {
				t.Fatalf("user=%v", user)
			}
		} else {
			found := false
			for _, item := range payload["users"].([]any) {
				user := item.(map[string]any)
				if user["email"] == email {
					found = user["has_openid"] == true
				}
			}
			if !found {
				t.Fatalf("bound admin user missing: %v", payload)
			}
		}
	}
	if err := store.RecordFollow(context.Background(), "wx-public", "private-openid", false, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	user := server.auth.publicUser(email)
	if user["has_openid"] != false || user["wechat_follow_gift_granted"] != true {
		t.Fatalf("after unsubscribe user=%v", user)
	}
	server.auth.wechatFollowStore = failingWechatStatusStore{}
	user = server.auth.publicUser(email)
	if user["has_openid"] != false || user["wechat_follow_gift_granted"] != false {
		t.Fatalf("storage failure user=%v", user)
	}
}

// TestWechatFollowIdentityNamespaces 验证不同公众号的同字符串 OpenID 不会错误共享领取资格。
func TestWechatFollowIdentityNamespaces(t *testing.T) {
	fixture := newMemoryWechatFixture(t)
	for i := 0; i < 2; i++ {
		appID, email := fmt.Sprintf("wx-app-%d", i), fmt.Sprintf("namespace-%d@example.com", i)
		hash := createWechatTestCode(t, fixture, email, email)
		if err := fixture.store.RecordFollow(context.Background(), appID, "same-openid-text", true, fixture.now); err != nil {
			t.Fatal(err)
		}
		result, err := fixture.store.BindCode(context.Background(), appID, "same-openid-text", hash, fixture.now)
		if err != nil || !result.GiftGranted {
			t.Fatalf("app=%s result=%+v err=%v", appID, result, err)
		}
	}
}

// runWechatCompactStoreContract 验证短码有限池回收、冲突回滚、消息时序和动态奖励的双存储一致性。
func runWechatCompactStoreContract(t *testing.T, factory func(*testing.T) wechatStoreFixture) {
	for _, expired := range []bool{false, true} {
		name := "active-cross-collision-no-deadlock"
		if expired {
			name = "expired-cross-recycle-no-deadlock"
		}
		t.Run(name, func(t *testing.T) {
			fixture := factory(t)
			for round := 0; round < 20; round++ {
				emailA, emailB := fmt.Sprintf("cross-a-%d@example.com", round), fmt.Sprintf("cross-b-%d@example.com", round)
				hashA, hashB := wechatTestCodeHash(emailA), wechatTestCodeHash(emailB)
				for _, pair := range []struct{ email, hash string }{{emailA, hashA}, {emailB, hashB}} {
					if err := fixture.store.CreateBinding(context.Background(), pair.email, pair.hash, fixture.now.Add(wechatBindingTTL)); err != nil {
						t.Fatal(err)
					}
				}
				now := fixture.now
				if expired {
					now = now.Add(wechatBindingTTL)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				start := make(chan struct{})
				results := make(chan error, 2)
				for _, pair := range []struct{ email, hash string }{{emailA, hashB}, {emailB, hashA}} {
					go func(email, hash string) {
						<-start
						results <- fixture.store.CreateBinding(ctx, email, hash, now.Add(wechatBindingTTL))
					}(pair.email, pair.hash)
				}
				close(start)
				for i := 0; i < 2; i++ {
					err := <-results
					if expired && err != nil {
						cancel()
						t.Fatalf("expired cross round=%d: %v", round, err)
					}
					if !expired && !errors.Is(err, ErrWechatBindingCodeConflict) {
						cancel()
						t.Fatalf("active cross round=%d: %v", round, err)
					}
				}
				cancel()
				if !expired {
					for _, pair := range []struct{ email, hash string }{{emailA, hashA}, {emailB, hashB}} {
						recordWechatTestFollow(t, fixture, pair.email, true, now)
						if _, err := fixture.store.BindCode(context.Background(), fixture.appID, pair.email, pair.hash, now); err != nil {
							t.Fatalf("original invalidated round=%d: %v", round, err)
						}
					}
				}
			}
		})
	}
	t.Run("collision-keeps-original-and-reserves-completed-invalidated", func(t *testing.T) {
		fixture := factory(t)
		ctx := context.Background()
		old := createWechatTestCode(t, fixture, "original@example.com", "original")
		taken := createWechatTestCode(t, fixture, "taken@example.com", "taken")
		if err := fixture.store.CreateBinding(ctx, "original@example.com", taken, fixture.now.Add(wechatBindingTTL)); !errors.Is(err, ErrWechatBindingCodeConflict) {
			t.Fatalf("collision=%v", err)
		}
		recordWechatTestFollow(t, fixture, "original-openid", true, fixture.now)
		if _, err := fixture.store.BindCode(ctx, fixture.appID, "original-openid", old, fixture.now); err != nil {
			t.Fatalf("collision invalidated original: %v", err)
		}
		if err := fixture.store.CreateBinding(ctx, "third@example.com", old, fixture.now.Add(wechatBindingTTL)); !errors.Is(err, ErrWechatBindingCodeConflict) {
			t.Fatalf("completed code reused early: %v", err)
		}
		createWechatTestCode(t, fixture, "taken@example.com", "replacement")
		if err := fixture.store.CreateBinding(ctx, "third@example.com", taken, fixture.now.Add(wechatBindingTTL)); !errors.Is(err, ErrWechatBindingCodeConflict) {
			t.Fatalf("invalidated code reused early: %v", err)
		}
	})
	t.Run("expired-code-reuse-rejects-late-message-allows-same-second", func(t *testing.T) {
		fixture := factory(t)
		ctx := context.Background()
		start := fixture.now.Add(450 * time.Millisecond)
		fixture.seed("next@example.com", Subscription{MemberType: memberTypeFree})
		hash := wechatTestCodeHash("reusable")
		if err := fixture.store.CreateBinding(ctx, "first@example.com", hash, start.Add(wechatBindingTTL)); err != nil {
			t.Fatal(err)
		}
		reusedAt := start.Add(wechatBindingTTL)
		if err := fixture.store.CreateBinding(ctx, "next@example.com", hash, reusedAt.Add(wechatBindingTTL)); err != nil {
			t.Fatalf("expired collision: %v", err)
		}
		recordWechatTestFollow(t, fixture, "next-openid", true, reusedAt)
		if _, err := fixture.store.BindCode(ctx, fixture.appID, "next-openid", hash, reusedAt, WechatBindOptions{RewardDays: 7, MessageTime: reusedAt.Add(-time.Second)}); !errors.Is(err, ErrWechatBindingExpired) {
			t.Fatalf("late message accepted: %v", err)
		}
		status, err := fixture.store.BindingStatus(ctx, "next@example.com")
		if err != nil || status.Bound || status.GiftGranted {
			t.Fatalf("late message wrote: %+v %v", status, err)
		}
		result, err := fixture.store.BindCode(ctx, fixture.appID, "next-openid", hash, reusedAt, WechatBindOptions{RewardDays: 7, MessageTime: reusedAt.Truncate(time.Second)})
		if err != nil || !result.GiftGranted {
			t.Fatalf("same second rejected: %+v %v", result, err)
		}
		assertWechatSubscription(t, result.Subscription, memberTypeMax, reusedAt.Add(7*24*time.Hour))
		status, err = fixture.store.BindingStatus(ctx, "next@example.com")
		if err != nil || status.GiftDays != 7 {
			t.Fatalf("gift history missing: %+v %v", status, err)
		}
		first, err := fixture.store.BindingStatus(ctx, "first@example.com")
		if err != nil || first.Bound || first.GiftGranted {
			t.Fatalf("reused code bound old user: %+v %v", first, err)
		}
	})
	t.Run("concurrent-code-create-single-winner", func(t *testing.T) {
		fixture := factory(t)
		ctx := context.Background()
		hash := wechatTestCodeHash("shared-short-code")
		results := make(chan error, 24)
		var wait sync.WaitGroup
		for i := 0; i < 24; i++ {
			wait.Add(1)
			go func(i int) {
				defer wait.Done()
				results <- fixture.store.CreateBinding(ctx, fmt.Sprintf("creator-%d@example.com", i), hash, fixture.now.Add(wechatBindingTTL))
			}(i)
		}
		wait.Wait()
		close(results)
		winners := 0
		for err := range results {
			if err == nil {
				winners++
			} else if !errors.Is(err, ErrWechatBindingCodeConflict) {
				t.Fatal(err)
			}
		}
		if winners != 1 {
			t.Fatalf("code winners=%d", winners)
		}
	})
	t.Run("limited-mode-only-allows-own-completed-replay", func(t *testing.T) {
		fixture := factory(t)
		ctx := context.Background()
		hash := createWechatTestCode(t, fixture, "limited@example.com", "limited")
		recordWechatTestFollow(t, fixture, "limited-openid", true, fixture.now)
		if _, err := fixture.store.BindCode(ctx, fixture.appID, "limited-openid", hash, fixture.now, WechatBindOptions{ReplayOnly: true}); !errors.Is(err, ErrWechatBindingRateLimited) {
			t.Fatalf("limited new bind=%v", err)
		}
		first, err := fixture.store.BindCode(ctx, fixture.appID, "limited-openid", hash, fixture.now, WechatBindOptions{RewardDays: 7})
		if err != nil || !first.GiftGranted {
			t.Fatalf("first=%+v %v", first, err)
		}
		replay, err := fixture.store.BindCode(ctx, fixture.appID, "limited-openid", hash, fixture.now, WechatBindOptions{RewardDays: 2, ReplayOnly: true})
		if err != nil || !replay.AlreadyBound || replay.GiftGranted || !replay.Subscription.ExpiresAt.Equal(first.Subscription.ExpiresAt) {
			t.Fatalf("own replay=%+v %v", replay, err)
		}
		if _, err = fixture.store.BindCode(ctx, fixture.appID, "other-openid", hash, fixture.now, WechatBindOptions{ReplayOnly: true}); !errors.Is(err, ErrWechatBindingExpired) {
			t.Fatalf("other replay=%v", err)
		}
		if _, err = fixture.store.BindCode(ctx, fixture.appID, "limited-openid", hash, fixture.now.Add(wechatBindingTTL), WechatBindOptions{ReplayOnly: true}); !errors.Is(err, ErrWechatBindingExpired) {
			t.Fatalf("expired replay=%v", err)
		}
		status, err := fixture.store.BindingStatus(ctx, "limited@example.com")
		if err != nil || status.GiftDays != 7 {
			t.Fatalf("history changed: %+v %v", status, err)
		}
	})
	t.Run("dynamic-days-preserves-paid-time-and-rejects-out-of-range", func(t *testing.T) {
		fixture := factory(t)
		ctx := context.Background()
		for _, days := range []int{2, 7} {
			email := fmt.Sprintf("days-%d@example.com", days)
			fixture.seed(email, Subscription{MemberType: memberTypePlus, ExpiresAt: fixture.now.Add(30 * 24 * time.Hour)})
			hash := createWechatTestCode(t, fixture, email, email)
			openID := fmt.Sprintf("days-openid-%d", days)
			recordWechatTestFollow(t, fixture, openID, true, fixture.now)
			for _, invalid := range []int{-1, 366} {
				if _, err := fixture.store.BindCode(ctx, fixture.appID, openID, hash, fixture.now, WechatBindOptions{RewardDays: invalid}); err == nil {
					t.Fatalf("days=%d accepted", invalid)
				}
			}
			result, err := fixture.store.BindCode(ctx, fixture.appID, openID, hash, fixture.now, WechatBindOptions{RewardDays: days})
			if err != nil || !result.GiftGranted {
				t.Fatalf("days=%d result=%+v %v", days, result, err)
			}
			assertWechatSubscription(t, result.Subscription, memberTypePlus, fixture.now.Add(time.Duration(30+days)*24*time.Hour))
			status, err := fixture.store.BindingStatus(ctx, email)
			if err != nil || status.GiftDays != days {
				t.Fatalf("gift days=%+v %v", status, err)
			}
		}
	})
}

// TestMemoryWechatCompactStoreContract 在固定时钟内存环境验证所有短码存储边界。
func TestMemoryWechatCompactStoreContract(t *testing.T) {
	runWechatCompactStoreContract(t, newMemoryWechatFixture)
}
