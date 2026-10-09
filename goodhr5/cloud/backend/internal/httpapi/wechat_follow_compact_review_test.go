// 本文件通过真实本地HTTP路由独立复核四位公众号绑定码、动态奖励、碰撞回收和回调限流，不访问生产或真实微信账号。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// wechatCompactReviewCode 为登录会话申请四位码，并验证有效期和动态奖励元数据。
func wechatCompactReviewCode(t *testing.T, server *Server, token string, rewardDays int) string {
	t.Helper()
	response := wechatTestRequest(server.Routes(), http.MethodPost, "/api/wechat-official/binding", token, `{"email":"must-not-be-trusted@example.com"}`)
	var payload struct {
		Code      string `json:"binding_code"`
		ExpiresAt string `json:"expires_at"`
		Status    struct {
			RewardDays int `json:"reward_days"`
		} `json:"status"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	value, err := strconv.Atoi(payload.Code)
	if response.Code != http.StatusOK || err != nil || len(payload.Code) != 4 || value < 1000 || value > 9999 || payload.ExpiresAt != server.wechatOfficial.now().Add(wechatBindingTTL).Format(time.RFC3339) || payload.Status.RewardDays != rewardDays {
		t.Fatalf("四位码或元数据不符：status=%d, body=%s", response.Code, response.Body.String())
	}
	return payload.Code
}

// wechatCompactReviewSaveReward 通过超管接口保存同组注册和关注天数，验证原始配置可以正常写入。
func wechatCompactReviewSaveReward(t *testing.T, server *Server, raw string) {
	t.Helper()
	token := wechatTestSession(t, server, "wechat-admin@example.com")
	body, err := json.Marshal(map[string]string{"config_value": raw})
	if err != nil {
		t.Fatal(err)
	}
	response := wechatTestRequest(server.Routes(), http.MethodPut, "/api/admin/system/configs/system.onboarding_config", token, string(body))
	if response.Code != http.StatusOK {
		t.Fatalf("动态奖励配置未保存：status=%d, body=%s", response.Code, response.Body.String())
	}
}

// wechatCompactReviewSeedSubscription 只初始化本地会员夹具，随后所有绑定和奖励均走真实回调。
func wechatCompactReviewSeedSubscription(t *testing.T, server *Server, email string, subscription Subscription) {
	t.Helper()
	store, ok := server.wechatOfficial.store.(*MemoryWechatFollowStore)
	if !ok {
		t.Fatal("独立回归必须使用本地内存存储")
	}
	store.subscriptions.grantMu.Lock()
	defer store.subscriptions.grantMu.Unlock()
	store.subscriptions.items[email] = subscription
}

// wechatCompactReviewSubscription 读取实际HTTP会员状态，验证绑定对共享会员数据的最终影响。
func wechatCompactReviewSubscription(t *testing.T, server *Server, token string) Subscription {
	t.Helper()
	response := wechatTestRequest(server.Routes(), http.MethodGet, "/api/subscription/status", token, "")
	var payload struct {
		Subscription Subscription `json:"subscription"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK {
		t.Fatalf("会员状态读取失败：status=%d, body=%s", response.Code, response.Body.String())
	}
	return payload.Subscription
}

// wechatCompactReviewStatus 通过本人状态接口验证绑定归属以及历史奖励不会跟随新配置改变。
func wechatCompactReviewStatus(t *testing.T, server *Server, token string, bound bool, rewardDays, giftDays int) {
	t.Helper()
	response := wechatTestRequest(server.Routes(), http.MethodGet, "/api/wechat-official/status", token, "")
	var payload struct {
		Bound       bool `json:"bound"`
		GiftGranted bool `json:"gift_granted"`
		RewardDays  int  `json:"reward_days"`
		GiftDays    int  `json:"gift_days"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || payload.Bound != bound || payload.RewardDays != rewardDays || payload.GiftDays != giftDays || payload.GiftGranted != (giftDays > 0) {
		t.Fatalf("本人状态不符：status=%d, body=%s", response.Code, response.Body.String())
	}
}

// wechatCompactReviewText 发送独立加密且签名有效的文本回调，解密回复后返回完整业务文案。
func wechatCompactReviewText(t *testing.T, server *Server, config WechatOfficialConfig, openID, code string, messageAt time.Time) string {
	t.Helper()
	response := wechatTestCallback(t, server, config, wechatTestMessage(config, messageAt, openID, "text", code), true)
	return wechatTestReply(t, response, config, true, openID)
}

// wechatCompactReviewFollow 通过真实加密关注事件更新微信状态，避免直接写关注数据掩盖接口问题。
func wechatCompactReviewFollow(t *testing.T, server *Server, config WechatOfficialConfig, openID, event string) {
	t.Helper()
	wechatTestCallback(t, server, config, wechatTestMessage(config, server.wechatOfficial.now(), openID, "event", event), true)
}

// TestWechatFollowCompactHTTPDefaultReward 验证旧配置缺失新字段时真正赠送两天，并保留有效付费会员的类型与剩余天数。
func TestWechatFollowCompactHTTPDefaultReward(t *testing.T) {
	for _, test := range []struct {
		name       string
		memberType string
		offset     time.Duration
		wantType   string
	}{
		{name: "免费用户", memberType: "free", offset: -24 * time.Hour, wantType: memberTypeMax},
		{name: "已过期Plus", memberType: memberTypePlus, offset: -24 * time.Hour, wantType: memberTypeMax},
		{name: "有效Plus", memberType: memberTypePlus, offset: 12 * 24 * time.Hour, wantType: memberTypePlus},
		{name: "有效Max", memberType: memberTypeMax, offset: 12 * 24 * time.Hour, wantType: memberTypeMax},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, config := newWechatOfficialTestServer(t, "safe")
			wechatCompactReviewSaveReward(t, server, `{"trial_days":17,"retained":{"enabled":true}}`)
			email := "default-reward@example.com"
			token := wechatTestSession(t, server, email)
			now := server.wechatOfficial.now()
			wechatCompactReviewSeedSubscription(t, server, email, Subscription{MemberType: test.memberType, ExpiresAt: now.Add(test.offset)})
			code := wechatCompactReviewCode(t, server, token, 2)
			wechatCompactReviewFollow(t, server, config, "default-reward-openid", "subscribe")
			reply := wechatCompactReviewText(t, server, config, "default-reward-openid", code, now)
			if !strings.Contains(reply, "2 天会员已到账") {
				t.Fatalf("默认奖励回复不符：%s", reply)
			}
			base := now
			if test.offset > 0 {
				base = now.Add(test.offset)
			}
			got := wechatCompactReviewSubscription(t, server, token)
			if got.MemberType != test.wantType || !got.ExpiresAt.Equal(base.Add(2*24*time.Hour)) {
				t.Fatalf("已有会员或默认两天被改变：got=%+v, base=%s", got, base)
			}
			wechatCompactReviewStatus(t, server, token, true, 2, 2)
		})
	}
}

// TestWechatFollowCompactHTTPRewardAtRedemption 验证兑换时读取新天数，而配置变化、重放和取消后重绑都不会重复赠送。
func TestWechatFollowCompactHTTPRewardAtRedemption(t *testing.T) {
	server, config := newWechatOfficialTestServer(t, "safe")
	wechatCompactReviewSaveReward(t, server, `{"trial_days":17,"wechat_follow_reward_days":2}`)
	email := "dynamic-reward@example.com"
	token := wechatTestSession(t, server, email)
	now := server.wechatOfficial.now()
	initial := Subscription{MemberType: memberTypePlus, ExpiresAt: now.Add(12 * 24 * time.Hour)}
	wechatCompactReviewSeedSubscription(t, server, email, initial)
	code := wechatCompactReviewCode(t, server, token, 2)
	wechatCompactReviewSaveReward(t, server, `{"trial_days":17,"wechat_follow_reward_days":7}`)
	wechatCompactReviewFollow(t, server, config, "dynamic-openid", "subscribe")
	if reply := wechatCompactReviewText(t, server, config, "dynamic-openid", code, now); !strings.Contains(reply, "7 天会员已到账") {
		t.Fatalf("兑换未读取当前奖励：%s", reply)
	}
	want := Subscription{MemberType: memberTypePlus, ExpiresAt: initial.ExpiresAt.Add(7 * 24 * time.Hour)}
	if got := wechatCompactReviewSubscription(t, server, token); got != want {
		t.Fatalf("动态奖励未保留Plus：got=%+v, want=%+v", got, want)
	}
	wechatCompactReviewSaveReward(t, server, `{"trial_days":17,"wechat_follow_reward_days":3}`)
	if reply := wechatCompactReviewText(t, server, config, "dynamic-openid", code, now); !strings.Contains(reply, "不重复赠送") {
		t.Fatalf("成功回调重放未返回幂等结果：%s", reply)
	}
	wechatCompactReviewStatus(t, server, token, true, 3, 7)
	wechatCompactReviewFollow(t, server, config, "dynamic-openid", "unsubscribe")
	wechatCompactReviewStatus(t, server, token, false, 3, 7)
	now = now.Add(time.Second)
	server.wechatOfficial.now = func() time.Time { return now }
	wechatCompactReviewFollow(t, server, config, "dynamic-openid", "subscribe")
	freshCode := wechatCompactReviewCode(t, server, token, 3)
	if reply := wechatCompactReviewText(t, server, config, "dynamic-openid", freshCode, now); !strings.Contains(reply, "不重复赠送") {
		t.Fatalf("重新绑定重复奖励：%s", reply)
	}
	if got := wechatCompactReviewSubscription(t, server, token); got != want {
		t.Fatalf("配置变化或重新绑定追加了奖励：got=%+v, want=%+v", got, want)
	}
	wechatCompactReviewStatus(t, server, token, true, 3, 7)
}

// TestWechatFollowCompactHTTPGiftIdentityUniqueness 验证换网站账号或换微信也不能绕过两种身份的一次性奖励记录。
func TestWechatFollowCompactHTTPGiftIdentityUniqueness(t *testing.T) {
	server, config := newWechatOfficialTestServer(t, "safe")
	ownerEmail, otherEmail := "gift-owner@example.com", "gift-other@example.com"
	owner := wechatTestSession(t, server, ownerEmail)
	other := wechatTestSession(t, server, otherEmail)
	now := server.wechatOfficial.now()
	initial := Subscription{MemberType: "free", ExpiresAt: now.Add(-24 * time.Hour)}
	wechatCompactReviewSeedSubscription(t, server, ownerEmail, initial)
	wechatCompactReviewSeedSubscription(t, server, otherEmail, initial)
	code := wechatCompactReviewCode(t, server, owner, 2)
	wechatCompactReviewFollow(t, server, config, "gift-first-openid", "subscribe")
	wechatCompactReviewText(t, server, config, "gift-first-openid", code, now)
	firstGift := wechatCompactReviewSubscription(t, server, owner)
	wechatCompactReviewSaveReward(t, server, `{"trial_days":17,"wechat_follow_reward_days":7}`)
	wechatCompactReviewFollow(t, server, config, "gift-first-openid", "unsubscribe")
	now = now.Add(time.Second)
	server.wechatOfficial.now = func() time.Time { return now }
	wechatCompactReviewFollow(t, server, config, "gift-first-openid", "subscribe")
	otherCode := wechatCompactReviewCode(t, server, other, 7)
	if reply := wechatCompactReviewText(t, server, config, "gift-first-openid", otherCode, now); !strings.Contains(reply, "不重复赠送") {
		t.Fatalf("已赠送微信换网站账号后再次获赠：%s", reply)
	}
	if got := wechatCompactReviewSubscription(t, server, other); got != initial {
		t.Fatalf("已领取微信污染了新账号原有会员：got=%+v, want=%+v", got, initial)
	}
	wechatCompactReviewStatus(t, server, other, true, 7, 0)
	wechatCompactReviewFollow(t, server, config, "gift-second-openid", "subscribe")
	ownerCode := wechatCompactReviewCode(t, server, owner, 7)
	if reply := wechatCompactReviewText(t, server, config, "gift-second-openid", ownerCode, now); !strings.Contains(reply, "不重复赠送") {
		t.Fatalf("已赠送账号换微信后再次获赠：%s", reply)
	}
	if got := wechatCompactReviewSubscription(t, server, owner); got != firstGift {
		t.Fatalf("已领取账号追加了新配置奖励：got=%+v, want=%+v", got, firstGift)
	}
	wechatCompactReviewStatus(t, server, owner, true, 7, 2)
}

// TestWechatFollowCompactHTTPCollisionRetries 验证缓存碰撞和缓存丢失后的持久层碰撞都会重试，其他用户有效码不受影响。
func TestWechatFollowCompactHTTPCollisionRetries(t *testing.T) {
	for _, resetCache := range []bool{false, true} {
		t.Run(fmt.Sprintf("缓存丢失=%t", resetCache), func(t *testing.T) {
			server, config := newWechatOfficialTestServer(t, "safe")
			ownerToken := wechatTestSession(t, server, "collision-owner@example.com")
			requesterToken := wechatTestSession(t, server, "collision-requester@example.com")
			server.wechatOfficial.newCode = func() (string, error) { return "1800", nil }
			ownerCode := wechatCompactReviewCode(t, server, ownerToken, 2)
			if resetCache {
				server.wechatOfficial.cache = newMemoryWechatBindingCache(func() time.Time { return server.wechatOfficial.now() })
			}
			calls := 0
			server.wechatOfficial.newCode = func() (string, error) {
				calls++
				if calls == 1 {
					return "1800", nil
				}
				return "1801", nil
			}
			requesterCode := wechatCompactReviewCode(t, server, requesterToken, 2)
			if calls != 2 || requesterCode != "1801" {
				t.Fatalf("碰撞未使用下一候选：calls=%d, code=%s", calls, requesterCode)
			}
			for _, binding := range []struct{ token, openID, code string }{
				{ownerToken, "collision-owner-openid", ownerCode},
				{requesterToken, "collision-requester-openid", requesterCode},
			} {
				wechatCompactReviewFollow(t, server, config, binding.openID, "subscribe")
				wechatCompactReviewText(t, server, config, binding.openID, binding.code, server.wechatOfficial.now())
				wechatCompactReviewStatus(t, server, binding.token, true, 2, 2)
			}
		})
	}
}

// TestWechatFollowCompactHTTPExhaustionKeepsOldCode 验证候选碰撞耗尽返回可重试错误，申请人的旧码和占用者的旧码都仍可兑换。
func TestWechatFollowCompactHTTPExhaustionKeepsOldCode(t *testing.T) {
	server, config := newWechatOfficialTestServer(t, "safe")
	owner := wechatTestSession(t, server, "exhaust-owner@example.com")
	requester := wechatTestSession(t, server, "exhaust-requester@example.com")
	server.wechatOfficial.newCode = func() (string, error) { return "1900", nil }
	ownerCode := wechatCompactReviewCode(t, server, owner, 2)
	server.wechatOfficial.newCode = func() (string, error) { return "1901", nil }
	requesterCode := wechatCompactReviewCode(t, server, requester, 2)
	calls := 0
	server.wechatOfficial.newCode = func() (string, error) { calls++; return "1900", nil }
	response := wechatTestRequest(server.Routes(), http.MethodPost, "/api/wechat-official/binding", requester, "")
	if response.Code != http.StatusServiceUnavailable || calls != 64 {
		t.Fatalf("碰撞耗尽缺少有界重试：status=%d, calls=%d, body=%s", response.Code, calls, response.Body.String())
	}
	for _, binding := range []struct{ token, openID, code string }{
		{owner, "exhaust-owner-openid", ownerCode},
		{requester, "exhaust-requester-openid", requesterCode},
	} {
		wechatCompactReviewFollow(t, server, config, binding.openID, "subscribe")
		wechatCompactReviewText(t, server, config, binding.openID, binding.code, server.wechatOfficial.now())
		wechatCompactReviewStatus(t, server, binding.token, true, 2, 2)
	}
}

// TestWechatFollowCompactHTTPExpiredCodeAndDelayedMessage 验证过期槽位可回收，但旧消息不能绑定新持有人，同秒的新消息可以正常兑换。
func TestWechatFollowCompactHTTPExpiredCodeAndDelayedMessage(t *testing.T) {
	server, config := newWechatOfficialTestServer(t, "safe")
	oldToken := wechatTestSession(t, server, "recycled-old@example.com")
	newToken := wechatTestSession(t, server, "recycled-new@example.com")
	now := server.wechatOfficial.now()
	oldMessageAt := now
	server.wechatOfficial.newCode = func() (string, error) { return "2700", nil }
	oldCode := wechatCompactReviewCode(t, server, oldToken, 2)
	wechatCompactReviewFollow(t, server, config, "recycled-old-openid", "subscribe")
	now = now.Add(wechatBindingTTL + time.Second + 987*time.Millisecond)
	server.wechatOfficial.now = func() time.Time { return now }
	newCode := wechatCompactReviewCode(t, server, newToken, 2)
	if newCode != oldCode {
		t.Fatal("过期槽位未能回收给新申请人")
	}
	if reply := wechatCompactReviewText(t, server, config, "recycled-old-openid", oldCode, oldMessageAt); !strings.Contains(reply, "过期") {
		t.Fatalf("迟到旧消息没有被拒绝：%s", reply)
	}
	wechatCompactReviewStatus(t, server, oldToken, false, 2, 0)
	wechatCompactReviewStatus(t, server, newToken, false, 2, 0)
	wechatCompactReviewFollow(t, server, config, "recycled-new-openid", "subscribe")
	if reply := wechatCompactReviewText(t, server, config, "recycled-new-openid", newCode, time.Unix(now.Unix(), 0)); !strings.Contains(reply, "2 天会员已到账") {
		t.Fatalf("同秒正常消息被纳秒时间误拒绝：%s", reply)
	}
	wechatCompactReviewStatus(t, server, newToken, true, 2, 2)
}

// TestWechatFollowCompactHTTPInvalidatedCodeReservation 验证旧码作废后仍占用到原有效期，不能立刻重新分配给其他账号。
func TestWechatFollowCompactHTTPInvalidatedCodeReservation(t *testing.T) {
	server, config := newWechatOfficialTestServer(t, "safe")
	owner := wechatTestSession(t, server, "invalidated-owner@example.com")
	other := wechatTestSession(t, server, "invalidated-other@example.com")
	server.wechatOfficial.newCode = func() (string, error) { return "3300", nil }
	oldCode := wechatCompactReviewCode(t, server, owner, 2)
	server.wechatOfficial.newCode = func() (string, error) { return "3301", nil }
	freshCode := wechatCompactReviewCode(t, server, owner, 2)
	calls := 0
	server.wechatOfficial.newCode = func() (string, error) {
		calls++
		if calls == 1 {
			return "3300", nil
		}
		return "3302", nil
	}
	if got := wechatCompactReviewCode(t, server, other, 2); calls != 2 || got != "3302" {
		t.Fatalf("已作废旧码被提前重新分配：calls=%d, code=%s", calls, got)
	}
	wechatCompactReviewFollow(t, server, config, "invalidated-owner-openid", "subscribe")
	if reply := wechatCompactReviewText(t, server, config, "invalidated-owner-openid", oldCode, server.wechatOfficial.now()); !strings.Contains(reply, "过期") {
		t.Fatalf("已作废旧码仍能使用：%s", reply)
	}
	wechatCompactReviewText(t, server, config, "invalidated-owner-openid", freshCode, server.wechatOfficial.now())
	wechatCompactReviewStatus(t, server, owner, true, 2, 2)
	wechatCompactReviewStatus(t, server, other, false, 2, 0)
}

// TestWechatFollowCompactHTTPLegacyEightCharacterCode 验证部署前生成的旧八位码仍能通过新回调兑换。
func TestWechatFollowCompactHTTPLegacyEightCharacterCode(t *testing.T) {
	server, config := newWechatOfficialTestServer(t, "safe")
	email := "legacy-code@example.com"
	token := wechatTestSession(t, server, email)
	const code = "23ABCDEF"
	if err := server.wechatOfficial.store.CreateBinding(context.Background(), email, wechatBindingCodeHash(code), server.wechatOfficial.now().Add(wechatBindingTTL)); err != nil {
		t.Fatal(err)
	}
	wechatCompactReviewFollow(t, server, config, "legacy-code-openid", "subscribe")
	if reply := wechatCompactReviewText(t, server, config, "legacy-code-openid", strings.ToLower(code), server.wechatOfficial.now()); !strings.Contains(reply, "2 天会员已到账") {
		t.Fatalf("旧八位码不兼容：%s", reply)
	}
	wechatCompactReviewStatus(t, server, token, true, 2, 2)
}

// TestWechatFollowCompactHTTPGenerationLimit 验证同邮箱五次生成后返回限频，且最后一个有效码仍可使用。
func TestWechatFollowCompactHTTPGenerationLimit(t *testing.T) {
	server, config := newWechatOfficialTestServer(t, "safe")
	token := wechatTestSession(t, server, "generation-limited@example.com")
	value := 4000
	server.wechatOfficial.newCode = func() (string, error) { value++; return strconv.Itoa(value), nil }
	var lastCode string
	for i := 0; i < 5; i++ {
		lastCode = wechatCompactReviewCode(t, server, token, 2)
	}
	response := wechatTestRequest(server.Routes(), http.MethodPost, "/api/wechat-official/binding", token, "")
	if response.Code != http.StatusTooManyRequests || value != 4005 {
		t.Fatalf("生成限频未阻止第六次分配：status=%d, generated=%d", response.Code, value)
	}
	wechatCompactReviewFollow(t, server, config, "generation-limited-openid", "subscribe")
	wechatCompactReviewText(t, server, config, "generation-limited-openid", lastCode, server.wechatOfficial.now())
	wechatCompactReviewStatus(t, server, token, true, 2, 2)
	now := server.wechatOfficial.now().Add(wechatBindingTTL + time.Second)
	server.wechatOfficial.now = func() time.Time { return now }
	wechatCompactReviewCode(t, server, token, 2)
}

// TestWechatFollowCompactHTTPWrongCodeLimit 验证五次错误后禁止新兑换，已完成同微信回调重放仍幂等成功。
func TestWechatFollowCompactHTTPWrongCodeLimit(t *testing.T) {
	for _, completeFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("已完成=%t", completeFirst), func(t *testing.T) {
			server, config := newWechatOfficialTestServer(t, "safe")
			token := wechatTestSession(t, server, "wrong-code-limited@example.com")
			server.wechatOfficial.newCode = func() (string, error) { return "5100", nil }
			code := wechatCompactReviewCode(t, server, token, 2)
			wechatCompactReviewFollow(t, server, config, "wrong-code-openid", "subscribe")
			if completeFirst {
				wechatCompactReviewText(t, server, config, "wrong-code-openid", code, server.wechatOfficial.now())
			}
			for i := 0; i < 5; i++ {
				if reply := wechatCompactReviewText(t, server, config, "wrong-code-openid", "5999", server.wechatOfficial.now()); !strings.Contains(reply, "过期") {
					t.Fatalf("错码回调异常：attempt=%d, reply=%s", i+1, reply)
				}
			}
			reply := wechatCompactReviewText(t, server, config, "wrong-code-openid", code, server.wechatOfficial.now())
			if completeFirst {
				if !strings.Contains(reply, "不重复赠送") {
					t.Fatalf("限流破坏同微信成功重放：%s", reply)
				}
				wechatCompactReviewStatus(t, server, token, true, 2, 2)
			} else {
				if !strings.Contains(reply, "十分钟") {
					t.Fatalf("五次错码后仍能新兑换：%s", reply)
				}
				wechatCompactReviewStatus(t, server, token, false, 2, 0)
			}
		})
	}
}

// TestWechatFollowCompactHTTPFollowErrorsDoNotCount 验证缺少关注的业务错误不占用错码次数，随后关注仍可使用原有效码。
func TestWechatFollowCompactHTTPFollowErrorsDoNotCount(t *testing.T) {
	server, config := newWechatOfficialTestServer(t, "safe")
	token := wechatTestSession(t, server, "not-followed@example.com")
	code := wechatCompactReviewCode(t, server, token, 2)
	for i := 0; i < 6; i++ {
		if reply := wechatCompactReviewText(t, server, config, "not-followed-openid", code, server.wechatOfficial.now()); !strings.Contains(reply, "关注记录") {
			t.Fatalf("未关注业务错误被计入错码：attempt=%d, reply=%s", i+1, reply)
		}
	}
	wechatCompactReviewFollow(t, server, config, "not-followed-openid", "subscribe")
	wechatCompactReviewText(t, server, config, "not-followed-openid", code, server.wechatOfficial.now())
	wechatCompactReviewStatus(t, server, token, true, 2, 2)
}

// wechatCompactReviewFailCache 模拟已配置缓存不可用，不创建网络连接或真实Redis数据。
type wechatCompactReviewFailCache struct {
	wechatBindingCache
	generationError bool
	attemptError    bool
}

// TakeGeneration 在指定夹具中返回缓存故障，其他操作继续使用真实内存缓存实现。
func (c *wechatCompactReviewFailCache) TakeGeneration(ctx context.Context, email string) (bool, error) {
	if c.generationError {
		return false, errors.New("测试缓存暂时不可用")
	}
	return c.wechatBindingCache.TakeGeneration(ctx, email)
}

// ReserveAttempt 在指定夹具中返回缓存故障，验证回调不会越过错码限流继续兑换。
func (c *wechatCompactReviewFailCache) ReserveAttempt(ctx context.Context, appID, openID string) (string, error) {
	if c.attemptError {
		return "", errors.New("测试缓存暂时不可用")
	}
	return c.wechatBindingCache.ReserveAttempt(ctx, appID, openID)
}

// TestWechatFollowCompactHTTPCacheFailure 验证缓存故障时接口停止生成或兑换，不退化成无保护的绑定成功。
func TestWechatFollowCompactHTTPCacheFailure(t *testing.T) {
	server, config := newWechatOfficialTestServer(t, "safe")
	token := wechatTestSession(t, server, "cache-failure@example.com")
	code := wechatCompactReviewCode(t, server, token, 2)
	wechatCompactReviewFollow(t, server, config, "cache-failure-openid", "subscribe")
	cache := &wechatCompactReviewFailCache{wechatBindingCache: server.wechatOfficial.cache, generationError: true, attemptError: true}
	server.wechatOfficial.cache = cache
	response := wechatTestRequest(server.Routes(), http.MethodPost, "/api/wechat-official/binding", token, "")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("缓存故障仍生成绑定码：status=%d", response.Code)
	}
	query, body := wechatTestCallbackRequest(t, config, server.wechatOfficial.now(), wechatTestMessage(config, server.wechatOfficial.now(), "cache-failure-openid", "text", code), true)
	response = wechatTestRequest(server.Routes(), http.MethodPost, "/api/wechat-official/callback?"+query.Encode(), "", body)
	if response.Code < 500 {
		t.Fatalf("缓存故障仍确认兑换：status=%d, body=%s", response.Code, response.Body.String())
	}
	wechatCompactReviewStatus(t, server, token, false, 2, 0)
	cache.generationError, cache.attemptError = false, false
	wechatCompactReviewText(t, server, config, "cache-failure-openid", code, server.wechatOfficial.now())
	wechatCompactReviewStatus(t, server, token, true, 2, 2)
}
