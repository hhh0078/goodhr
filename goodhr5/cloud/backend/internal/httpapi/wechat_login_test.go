// 本文件用真实签名回调和内存HTTP服务验证公众号免密注册、原账号复用、协议确认与一次性登录凭据。
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// createWechatLoginForTest 只创建当前测试页面的挑战，不绕过正式公开接口。
func createWechatLoginForTest(t *testing.T, s *Server) (string, string) {
	t.Helper()
	r := wechatTestRequest(s.Routes(), http.MethodPost, "/api/auth/wechat/create", "", "{}")
	if r.Code != http.StatusOK {
		t.Fatalf("生成登录码失败：%s", r.Body.String())
	}
	var data struct {
		Ticket string `json:"ticket"`
		Code   string `json:"login_code"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if !validWechatLoginTicket(data.Ticket) || len(data.Code) != 8 {
		t.Fatalf("登录凭据和公开码没有分离：%+v", data)
	}
	return data.Ticket, data.Code
}

// pollWechatLoginForTest 让拥有随机凭据的页面通过正式接口领取结果。
func pollWechatLoginForTest(t *testing.T, s *Server, ticket string, agreement bool) map[string]any {
	t.Helper()
	body, _ := json.Marshal(wechatLoginPollRequest{Ticket: ticket, AgreementAccepted: agreement})
	r := wechatTestRequest(s.Routes(), http.MethodPost, "/api/auth/wechat/poll", "", string(body))
	if r.Code != http.StatusOK {
		t.Fatalf("轮询失败：%d %s", r.Code, r.Body.String())
	}
	var data map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	return data
}

// TestWechatLoginSignedCallback 验证明文和安全模式都必须经过微信签名确认，协议和页面独占凭据缺一不可。
func TestWechatLoginSignedCallback(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		t.Run(fmt.Sprint(encrypted), func(t *testing.T) {
			s, config := newWechatOfficialTestServer(t, "compat")
			ticket, code := createWechatLoginForTest(t, s)
			if got := pollWechatLoginForTest(t, s, ticket, true); got["status"] != "waiting" {
				t.Fatal(got)
			}
			message := wechatTestMessage(config, s.wechatOfficial.now(), "login-new-openid", "text", "验证码 "+code)
			query, body := wechatTestCallbackRequest(t, config, s.wechatOfficial.now(), message, encrypted)
			query.Set("signature", "forged")
			query.Set("msg_signature", "forged")
			r := wechatTestRequest(s.Routes(), http.MethodPost, "/api/wechat-official/callback?"+query.Encode(), "", body)
			if r.Code == http.StatusOK {
				t.Fatal("伪造签名被接受")
			}
			if got := pollWechatLoginForTest(t, s, ticket, true); got["status"] != "waiting" {
				t.Fatal(got)
			}
			wechatTestCallback(t, s, config, message, encrypted)
			if got := pollWechatLoginForTest(t, s, ticket, false); got["status"] != "agreement_required" || got["access_token"] != nil {
				t.Fatal(got)
			}
			wrong := strings.Repeat("a", 64)
			if wrong == ticket {
				wrong = strings.Repeat("b", 64)
			}
			if got := pollWechatLoginForTest(t, s, wrong, true); got["status"] != "expired" {
				t.Fatal(got)
			}
			got := pollWechatLoginForTest(t, s, ticket, true)
			token, _ := got["access_token"].(string)
			if token == "" || got["requires_password_setup"] != false {
				t.Fatal(got)
			}
			user := got["user"].(map[string]any)
			email := user["email"].(string)
			if !isWechatAccount(email) || user["display_name"] != "微信用户" {
				t.Fatal(user)
			}
			me := wechatTestRequest(s.Routes(), http.MethodGet, "/api/auth/me", token, "")
			if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"requires_password_setup":false`) {
				t.Fatal(me.Body.String())
			}
			if got := pollWechatLoginForTest(t, s, ticket, true); got["access_token"] != nil || got["status"] != "expired" {
				t.Fatal("登录凭据重复消费", got)
			}
			wechatTestCallback(t, s, config, message, encrypted)
			ticket2, code2 := createWechatLoginForTest(t, s)
			message.Content = "验证码 " + code2
			wechatTestCallback(t, s, config, message, encrypted)
			again := pollWechatLoginForTest(t, s, ticket2, false)
			if again["user"].(map[string]any)["email"] != email {
				t.Fatal("重复登录创建了新账号")
			}
		})
	}
}

// TestWechatLoginKeepsBoundEmailAccount 验证已经绑定公众号的邮箱用户继续使用原账号和数据，不强制补设密码。
func TestWechatLoginKeepsBoundEmailAccount(t *testing.T) {
	s, config := newWechatOfficialTestServer(t, "safe")
	email := "original@example.com"
	openID := "original-openid"
	store := s.wechatOfficial.store.(*MemoryWechatFollowStore)
	now := s.wechatOfficial.now()
	if err := store.RecordFollow(context.Background(), config.AppID, openID, true, now); err != nil {
		t.Fatal(err)
	}
	hash := wechatBindingCodeHash("1234")
	if err := store.CreateBinding(context.Background(), email, hash, now.Add(wechatBindingTTL)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindCode(context.Background(), config.AppID, openID, hash, now); err != nil {
		t.Fatal(err)
	}
	ticket, code := createWechatLoginForTest(t, s)
	wechatTestCallback(t, s, config, wechatTestMessage(config, now, openID, "text", "验证码"+code), true)
	got := pollWechatLoginForTest(t, s, ticket, true)
	if got["user"].(map[string]any)["email"] != email {
		t.Fatal("未复用邮箱原账号", got)
	}
	token := got["access_token"].(string)
	me := wechatTestRequest(s.Routes(), http.MethodGet, "/api/auth/me", token, "")
	if !strings.Contains(me.Body.String(), `"requires_password_setup":false`) {
		t.Fatal(me.Body.String())
	}
	if err := store.RecordFollow(context.Background(), config.AppID, openID, false, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	resolved, err := store.ResolveWechatLogin(context.Background(), config.AppID, openID)
	if err != nil || resolved != email {
		t.Fatalf("取消关注后丢失原账号：%q %v", resolved, err)
	}
}

// TestWechatLoginExpiryAndEmailIsolation 验证旧消息、过期码、另一微信覆盖和内部账号走邮箱入口均被拒绝。
func TestWechatLoginExpiryAndEmailIsolation(t *testing.T) {
	s, config := newWechatOfficialTestServer(t, "safe")
	ticket, code := createWechatLoginForTest(t, s)
	now := s.wechatOfficial.now()
	message := wechatTestMessage(config, now.Add(-time.Minute), "first-openid", "text", "验证码 "+code)
	wechatTestCallback(t, s, config, message, true)
	if got := pollWechatLoginForTest(t, s, ticket, true); got["status"] != "waiting" {
		t.Fatal("旧消息认领了新码", got)
	}
	message.CreateTime = now.Unix()
	wechatTestCallback(t, s, config, message, true)
	message.FromUserName = "other-openid"
	wechatTestCallback(t, s, config, message, true)
	got := pollWechatLoginForTest(t, s, ticket, true)
	email := got["user"].(map[string]any)["email"].(string)
	resolved, _ := s.wechatOfficial.store.(WechatLoginAccountStore).ResolveWechatLogin(context.Background(), config.AppID, "first-openid")
	if resolved != email {
		t.Fatal("登录页被另一微信覆盖")
	}
	for _, path := range []string{"/api/auth/send-code", "/api/auth/login", "/api/auth/password-login", "/api/auth/password/reset-code", "/api/auth/password/reset"} {
		body := fmt.Sprintf(`{"email":%q,"code":"1234","password":"12","agreement_accepted":true}`, email)
		r := wechatTestRequest(s.Routes(), http.MethodPost, path, "", body)
		if r.Code == http.StatusOK {
			t.Fatalf("内部账号能够从邮箱入口进入：%s", path)
		}
	}
	ticket, _ = createWechatLoginForTest(t, s)
	s.wechatOfficial.now = func() time.Time { return now.Add(wechatLoginTTL + time.Second) }
	if got := pollWechatLoginForTest(t, s, ticket, true); got["status"] != "expired" {
		t.Fatal(got)
	}
}

// TestWechatLoginIndependentOfFollowReward 验证关注奖励关闭时仍可登录，单独关闭登录时不再生成或确认挑战。
func TestWechatLoginIndependentOfFollowReward(t *testing.T) {
	s, config := newWechatOfficialTestServer(t, "safe")
	config.Enabled = false
	saveWechatOfficialTestConfig(t, s, config)
	ticket, code := createWechatLoginForTest(t, s)
	wechatTestCallback(t, s, config, wechatTestMessage(config, s.wechatOfficial.now(), "reward-off-login", "text", "验证码 "+code), true)
	if result := pollWechatLoginForTest(t, s, ticket, true); result["access_token"] == nil {
		t.Fatal(result)
	}
	disabled := false
	config.LoginEnabled = &disabled
	saveWechatOfficialTestConfig(t, s, config)
	r := wechatTestRequest(s.Routes(), http.MethodPost, "/api/auth/wechat/create", "", "{}")
	if r.Code != http.StatusServiceUnavailable {
		t.Fatal("登录开关关闭后仍能生成挑战", r.Body.String())
	}
}

// TestWechatLoginRefresh 验证原页面重新生成后旧凭据与旧短码一起失效，新码仍能完成登录。
func TestWechatLoginRefresh(t *testing.T) {
	s, config := newWechatOfficialTestServer(t, "safe")
	oldTicket, oldCode := createWechatLoginForTest(t, s)
	body, _ := json.Marshal(map[string]string{"previous_ticket": oldTicket})
	r := wechatTestRequest(s.Routes(), http.MethodPost, "/api/auth/wechat/create", "", string(body))
	if r.Code != http.StatusOK {
		t.Fatal(r.Body.String())
	}
	var next struct {
		Ticket string `json:"ticket"`
		Code   string `json:"login_code"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &next); err != nil {
		t.Fatal(err)
	}
	message := wechatTestMessage(config, s.wechatOfficial.now(), "refresh-openid", "text", "验证码 "+oldCode)
	wechatTestCallback(t, s, config, message, true)
	if got := pollWechatLoginForTest(t, s, oldTicket, true); got["status"] != "expired" {
		t.Fatal("旧页面凭据没有失效", got)
	}
	if got := pollWechatLoginForTest(t, s, next.Ticket, true); got["status"] != "waiting" {
		t.Fatal("旧消息认领了新登录码", got)
	}
	message.Content = "验证码 " + next.Code
	wechatTestCallback(t, s, config, message, true)
	if got := pollWechatLoginForTest(t, s, next.Ticket, true); got["access_token"] == nil {
		t.Fatal("新登录码不能登录", got)
	}
}

// TestWechatLoginDoesNotSendToInternalIdentity 验证免邮箱账号不会触发邮件，普通邮箱提醒保持原有行为。
func TestWechatLoginDoesNotSendToInternalIdentity(t *testing.T) {
	mailer := &recordingMailer{}
	service := &PositionExecutionService{mailer: mailer}
	for _, email := range []string{"wx-test@wechat.goodhr.invalid", "legacy@example.com"} {
		if err := sendAIBalanceNotice(mailer, email, AIBalanceNotice{ChangeUnits: 1}); err != nil {
			t.Fatal(err)
		}
		if err := service.sendPositionStatusNotice(Position{UserEmail: email}, "completed", "", 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	if len(mailer.balanceNotices) != 1 || mailer.balanceNotices[0].email != "legacy@example.com" || len(mailer.positionNotices) != 1 || mailer.positionNotices[0].email != "legacy@example.com" {
		t.Fatal("免邮箱过滤影响了普通邮箱提醒", mailer)
	}
	admin := &AdminEmailService{}
	recipients, _, err := admin.resolveRecipients(sendAdminEmailRequest{Emails: []string{"wx-test@wechat.goodhr.invalid", "legacy@example.com"}})
	if err != nil || len(recipients) != 1 || recipients[0] != "legacy@example.com" {
		t.Fatal("手工群发包含内部账号", recipients, err)
	}
}
