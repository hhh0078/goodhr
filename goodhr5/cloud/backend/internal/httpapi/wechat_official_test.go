// 本文件用本地内存存储验证公众号回调、登录会话隔离和私有配置脱敏，不访问真实微信账号或生产服务。
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// newWechatOfficialTestServer 复用项目初始化工具并强制使用内存依赖和固定回调时间。
func newWechatOfficialTestServer(t *testing.T, mode string) (*Server, WechatOfficialConfig) {
	t.Helper()
	for _, key := range []string{"GOODHR_PG_DSN", "GOODHR_REDIS_ADDR", "GOODHR_SMTP_HOST"} {
		t.Setenv(key, "")
	}
	t.Setenv("GOODHR_SUPER_ADMINS", "wechat-admin@example.com")
	server := mustNewServer(t)
	server.wechatOfficial.now = func() time.Time { return time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC) }
	config := WechatOfficialConfig{
		Enabled: true, AccountName: "测试公众号", AppID: wechatTestAppID,
		OriginalID: "gh_test_account", Token: "WechatTestToken123", EncodingAESKey: wechatTestAESKey(),
		QRImageURL: "https://example.com/test-qr.png", MessageMode: mode,
	}
	saveWechatOfficialTestConfig(t, server, config)
	return server, config
}

// saveWechatOfficialTestConfig 将完全虚构的配置存入本地测试依赖。
func saveWechatOfficialTestConfig(t *testing.T, server *Server, config WechatOfficialConfig) {
	t.Helper()
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.systemConfigs.Save(SystemConfig{ConfigKey: wechatOfficialConfigKey, ConfigValue: string(raw), Enabled: true}); err != nil {
		t.Fatal(err)
	}
}

// wechatTestSession 创建本地测试会话，不发送验证码或邮件。
func wechatTestSession(t *testing.T, server *Server, email string) string {
	t.Helper()
	token := "wechat-test-session-" + email
	if err := server.auth.store.SaveSession(token, Session{Email: email, CreatedAt: time.Now()}, time.Hour); err != nil {
		t.Fatal(err)
	}
	return token
}

// wechatTestRequest 调用内存 HTTP 路由并返回完整响应，供权限和脱敏断言使用。
func wechatTestRequest(handler http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// wechatTestQuery 创建经过独立签名的查询参数，可修改参数测试篡改边界。
func wechatTestQuery(config WechatOfficialConfig, timestamp time.Time) url.Values {
	query := url.Values{"timestamp": {strconv.FormatInt(timestamp.Unix(), 10)}, "nonce": {"test-nonce"}}
	query.Set("signature", wechatTestSignature(config.Token, query.Get("timestamp"), query.Get("nonce")))
	return query
}

// wechatTestMessage 创建正常关注事件或文本消息，使用虚构的微信身份。
func wechatTestMessage(config WechatOfficialConfig, now time.Time, openID, messageType, content string) wechatIncomingMessage {
	message := wechatIncomingMessage{ToUserName: config.OriginalID, FromUserName: openID, CreateTime: now.Unix(), MsgType: messageType}
	if messageType == "event" {
		message.Event = content
	} else {
		message.Content = content
	}
	return message
}

// wechatTestCallbackRequest 独立加密回调数据并生成签名，避免正向流程依赖被测加密实现。
func wechatTestCallbackRequest(t *testing.T, config WechatOfficialConfig, now time.Time, message wechatIncomingMessage, encrypted bool) (url.Values, string) {
	t.Helper()
	raw, err := xml.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	query := wechatTestQuery(config, now)
	if encrypted {
		cipherText := wechatTestEncryptFrame(t, wechatTestFrame(raw, config.AppID))
		query.Set("encrypt_type", "aes")
		query.Set("msg_signature", wechatTestSignature(config.Token, query.Get("timestamp"), query.Get("nonce"), cipherText))
		raw, err = xml.Marshal(wechatIncomingMessage{ToUserName: config.OriginalID, Encrypt: cipherText})
		if err != nil {
			t.Fatal(err)
		}
	}
	return query, string(raw)
}

// wechatTestCallback 调用真实回调路由并检查成功响应，所有数据只存在测试内存中。
func wechatTestCallback(t *testing.T, server *Server, config WechatOfficialConfig, message wechatIncomingMessage, encrypted bool) *httptest.ResponseRecorder {
	t.Helper()
	query, body := wechatTestCallbackRequest(t, config, server.wechatOfficial.now(), message, encrypted)
	response := wechatTestRequest(server.Routes(), http.MethodPost, "/api/wechat-official/callback?"+query.Encode(), "", body)
	if response.Code != http.StatusOK {
		t.Fatalf("回调失败：status=%d，body=%s", response.Code, response.Body.String())
	}
	return response
}

// wechatTestReply 解密并验证安全模式回复签名，同时核对微信与公众号收发方向。
func wechatTestReply(t *testing.T, response *httptest.ResponseRecorder, config WechatOfficialConfig, encrypted bool, openID string) string {
	t.Helper()
	raw := response.Body.Bytes()
	if encrypted {
		var envelope wechatEncryptedReply
		if err := xml.Unmarshal(raw, &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Encrypt == "" || envelope.MsgSignature != wechatTestSignature(config.Token, envelope.TimeStamp, envelope.Nonce, envelope.Encrypt) {
			t.Fatal("加密回复缺失密文或消息签名不匹配")
		}
		raw = wechatTestDecodeReply(t, envelope.Encrypt, config.AppID)
	}
	var reply wechatTextReply
	if err := xml.Unmarshal(raw, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.ToUserName != openID || reply.FromUserName != config.OriginalID || reply.MsgType != "text" || reply.CreateTime <= 0 {
		t.Fatalf("回复收发方向或内容类型不符：%+v", reply)
	}
	return reply.Content
}

// TestWechatOfficialGETVerification 验证明文 URL 校验与加密 echo 均可正确返回，错误签名不能通过。
func TestWechatOfficialGETVerification(t *testing.T) {
	server, config := newWechatOfficialTestServer(t, "safe")
	for _, encrypted := range []bool{false, true} {
		query := wechatTestQuery(config, server.wechatOfficial.now())
		query.Set("echostr", "test-echo")
		if encrypted {
			echo := wechatTestEncryptFrame(t, wechatTestFrame([]byte("test-echo"), config.AppID))
			query.Set("echostr", echo)
			query.Set("encrypt_type", "aes")
			query.Set("msg_signature", wechatTestSignature(config.Token, query.Get("timestamp"), query.Get("nonce"), echo))
		}
		response := wechatTestRequest(server.Routes(), http.MethodGet, "/api/wechat-official/callback?"+query.Encode(), "", "")
		if response.Code != http.StatusOK || response.Body.String() != "test-echo" || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("GET 校验失败：encrypted=%v，status=%d，body=%s", encrypted, response.Code, response.Body.String())
		}
		if encrypted {
			query.Set("msg_signature", strings.Repeat("0", 40))
		} else {
			query.Set("signature", strings.Repeat("0", 40))
		}
		response = wechatTestRequest(server.Routes(), http.MethodGet, "/api/wechat-official/callback?"+query.Encode(), "", "")
		if response.Code != http.StatusForbidden {
			t.Fatalf("错误 GET 签名被接受：status=%d", response.Code)
		}
	}
}

// TestWechatOfficialSafeRejectsPlaintextPOST 验证默认安全模式和空模式均拒绝签名正确的明文业务数据。
func TestWechatOfficialSafeRejectsPlaintextPOST(t *testing.T) {
	for _, mode := range []string{"safe", ""} {
		t.Run("mode_"+mode, func(t *testing.T) {
			server, config := newWechatOfficialTestServer(t, mode)
			message := wechatTestMessage(config, server.wechatOfficial.now(), "test-openid", "event", "subscribe")
			query, body := wechatTestCallbackRequest(t, config, server.wechatOfficial.now(), message, false)
			response := wechatTestRequest(server.Routes(), http.MethodPost, "/api/wechat-official/callback?"+query.Encode(), "", body)
			if response.Code != http.StatusForbidden {
				t.Fatalf("安全模式被降级为明文：status=%d，body=%s", response.Code, response.Body.String())
			}
		})
	}
}

// TestWechatOfficialPOSTReplies 验证明文兼容模式与安全模式均正确回复，密文正文用消息签名而非普通签名校验。
func TestWechatOfficialPOSTReplies(t *testing.T) {
	for _, mode := range []string{"plaintext", "compat", "safe"} {
		t.Run(mode, func(t *testing.T) {
			server, config := newWechatOfficialTestServer(t, mode)
			encrypted := mode == "safe"
			message := wechatTestMessage(config, server.wechatOfficial.now(), "test-openid", "event", "subscribe")
			query, body := wechatTestCallbackRequest(t, config, server.wechatOfficial.now(), message, encrypted)
			if encrypted {
				query.Set("signature", "intentionally-invalid-plain-signature")
			}
			response := wechatTestRequest(server.Routes(), http.MethodPost, "/api/wechat-official/callback?"+query.Encode(), "", body)
			if response.Code != http.StatusOK || !strings.Contains(wechatTestReply(t, response, config, encrypted, "test-openid"), "绑定码") {
				t.Fatalf("关注回复失败：status=%d，body=%s", response.Code, response.Body.String())
			}
			message.Event = "unsubscribe"
			message.CreateTime++
			response = wechatTestCallback(t, server, config, message, encrypted)
			if response.Body.String() != "success" {
				t.Fatalf("无被动回复事件没有返回 success：body=%s", response.Body.String())
			}
		})
	}
}

// wechatCallbackMutationSpy 记录回调是否调用任何写入方法，保证被拒绝的数据没有业务副作用。
type wechatCallbackMutationSpy struct {
	WechatFollowStore
	mutations int
}

// RecordFollow 记录测试写入次数，再委托正常内存实现处理。
func (s *wechatCallbackMutationSpy) RecordFollow(ctx context.Context, appID, openID string, followed bool, eventAt time.Time) error {
	s.mutations++
	return s.WechatFollowStore.RecordFollow(ctx, appID, openID, followed, eventAt)
}

// BindCode 记录测试写入次数，再委托正常内存实现处理。
func (s *wechatCallbackMutationSpy) BindCode(ctx context.Context, appID, openID, codeHash string, now time.Time) (WechatBindResult, error) {
	s.mutations++
	return s.WechatFollowStore.BindCode(ctx, appID, openID, codeHash, now)
}

// TestWechatOfficialCallbackRejectsTampering 验证签名、接收方、AppID、消息时间和超长消息拒绝后不写入关注或绑定状态。
func TestWechatOfficialCallbackRejectsTampering(t *testing.T) {
	server, config := newWechatOfficialTestServer(t, "safe")
	spy := &wechatCallbackMutationSpy{WechatFollowStore: server.wechatOfficial.store}
	server.wechatOfficial.store = spy
	now := server.wechatOfficial.now()
	for _, test := range []struct {
		name string
		code int
	}{
		{"missing_message_signature", http.StatusForbidden}, {"wrong_message_signature", http.StatusForbidden},
		{"wrong_nonce", http.StatusForbidden}, {"old_timestamp", http.StatusForbidden}, {"future_timestamp", http.StatusForbidden},
		{"invalid_timestamp", http.StatusForbidden}, {"oversized_nonce", http.StatusForbidden}, {"wrong_recipient", http.StatusBadRequest},
		{"empty_openid", http.StatusBadRequest}, {"oversized_openid", http.StatusBadRequest}, {"future_message_time", http.StatusBadRequest},
		{"zero_message_time", http.StatusBadRequest}, {"wrong_appid", http.StatusBadRequest}, {"invalid_ciphertext", http.StatusBadRequest},
		{"invalid_plain_xml", http.StatusBadRequest}, {"oversized_body", http.StatusRequestEntityTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			message := wechatTestMessage(config, now, "test-openid", "event", "subscribe")
			switch test.name {
			case "wrong_recipient":
				message.ToUserName = "gh_other_account"
			case "empty_openid":
				message.FromUserName = ""
			case "oversized_openid":
				message.FromUserName = strings.Repeat("x", 129)
			case "future_message_time":
				message.CreateTime = now.Add(10*time.Minute + time.Second).Unix()
			case "zero_message_time":
				message.CreateTime = 0
			}
			query, body := wechatTestCallbackRequest(t, config, now, message, true)
			switch test.name {
			case "missing_message_signature":
				query.Del("msg_signature")
			case "wrong_message_signature":
				query.Set("msg_signature", strings.Repeat("0", 40))
			case "wrong_nonce":
				query.Set("nonce", "different-nonce")
			case "old_timestamp", "future_timestamp", "invalid_timestamp", "oversized_nonce":
				if test.name == "old_timestamp" {
					query.Set("timestamp", strconv.FormatInt(now.Add(-10*time.Minute-time.Second).Unix(), 10))
				} else if test.name == "future_timestamp" {
					query.Set("timestamp", strconv.FormatInt(now.Add(10*time.Minute+time.Second).Unix(), 10))
				} else if test.name == "invalid_timestamp" {
					query.Set("timestamp", "not-a-number")
				} else {
					query.Set("nonce", strings.Repeat("x", 257))
				}
				var envelope wechatIncomingMessage
				if err := xml.Unmarshal([]byte(body), &envelope); err != nil {
					t.Fatal(err)
				}
				query.Set("msg_signature", wechatTestSignature(config.Token, query.Get("timestamp"), query.Get("nonce"), envelope.Encrypt))
			case "wrong_appid", "invalid_ciphertext", "invalid_plain_xml":
				plain, err := xml.Marshal(message)
				if err != nil {
					t.Fatal(err)
				}
				appID := "wx1111111111111111"
				if test.name == "invalid_plain_xml" {
					plain, appID = []byte("not xml"), config.AppID
				}
				cipherText := wechatTestEncryptFrame(t, wechatTestFrame(plain, appID))
				if test.name == "invalid_ciphertext" {
					cipherText = "%%%invalid%%%"
				}
				body = "<xml><Encrypt>" + cipherText + "</Encrypt></xml>"
				query.Set("msg_signature", wechatTestSignature(config.Token, query.Get("timestamp"), query.Get("nonce"), cipherText))
			case "oversized_body":
				body = strings.Repeat("x", wechatCallbackLimit+1)
			}
			response := wechatTestRequest(server.Routes(), http.MethodPost, "/api/wechat-official/callback?"+query.Encode(), "", body)
			if response.Code != test.code || spy.mutations != 0 {
				t.Fatalf("错误消息未被安全拒绝：status=%d，want=%d，mutations=%d，body=%s", response.Code, test.code, spy.mutations, response.Body.String())
			}
		})
	}
}

// wechatTestBindingCode 为指定会话申请绑定码并检查响应与固定有效期。
func wechatTestBindingCode(t *testing.T, server *Server, token string) string {
	t.Helper()
	response := wechatTestRequest(server.Routes(), http.MethodPost, "/api/wechat-official/binding", token, `{"email":"other-user@example.com"}`)
	var payload struct {
		Code      string `json:"binding_code"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || len(payload.Code) != 8 || payload.ExpiresAt != server.wechatOfficial.now().Add(wechatBindingTTL).Format(time.RFC3339) {
		t.Fatalf("绑定码响应不符：status=%d，body=%s", response.Code, response.Body.String())
	}
	return payload.Code
}

// TestWechatOfficialSessionIsolation 验证请求中的其他邮箱不能越权，旧码失效且两个网站账号分别绑定自己的微信。
func TestWechatOfficialSessionIsolation(t *testing.T) {
	server, config := newWechatOfficialTestServer(t, "safe")
	now := server.wechatOfficial.now()
	tokenA := wechatTestSession(t, server, "owner-a@example.com")
	tokenB := wechatTestSession(t, server, "owner-b@example.com")
	oldCodeA := wechatTestBindingCode(t, server, tokenA)
	codeA := wechatTestBindingCode(t, server, tokenA)
	codeB := wechatTestBindingCode(t, server, tokenB)
	for _, openID := range []string{"openid-a", "openid-b"} {
		wechatTestCallback(t, server, config, wechatTestMessage(config, now, openID, "event", "subscribe"), true)
	}
	oldReply := wechatTestCallback(t, server, config, wechatTestMessage(config, now, "openid-a", "text", oldCodeA), true)
	if !strings.Contains(wechatTestReply(t, oldReply, config, true, "openid-a"), "过期") {
		t.Fatal("重新申请后旧码仍可绑定")
	}
	response := wechatTestCallback(t, server, config, wechatTestMessage(config, now, "openid-a", "text", strings.ToLower(codeA)), true)
	if !strings.Contains(wechatTestReply(t, response, config, true, "openid-a"), "5 天会员已到账") {
		t.Fatal("当前账号没有成功完成首次绑定")
	}
	statusA, err := server.wechatOfficial.store.BindingStatus(context.Background(), "owner-a@example.com")
	if err != nil || !statusA.Bound || statusA.OpenID != "openid-a" || !statusA.GiftGranted {
		t.Fatalf("A 账号绑定不符：status=%+v，错误=%v", statusA, err)
	}
	statusB, err := server.wechatOfficial.store.BindingStatus(context.Background(), "owner-b@example.com")
	if err != nil || statusB.Bound || statusB.GiftGranted {
		t.Fatalf("A 的请求污染 B 账号：status=%+v，错误=%v", statusB, err)
	}
	response = wechatTestCallback(t, server, config, wechatTestMessage(config, now, "openid-a", "text", codeB), true)
	if !strings.Contains(wechatTestReply(t, response, config, true, "openid-a"), "另一个") {
		t.Fatal("同一个微信可以同时绑定另一个网站账号")
	}
	wechatTestCallback(t, server, config, wechatTestMessage(config, now, "openid-b", "text", codeB), true)
	response = wechatTestRequest(server.Routes(), http.MethodGet, "/api/wechat-official/status?email=owner-a@example.com", tokenB, "")
	for _, secret := range []string{config.Token, config.EncodingAESKey, "openid-a", "openid-b", "encoding_aes_key", "app_id"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("本人状态接口包含私有信息：%q", secret)
		}
	}
	statusB, err = server.wechatOfficial.store.BindingStatus(context.Background(), "owner-b@example.com")
	if err != nil || statusB.OpenID != "openid-b" {
		t.Fatalf("B 的绑定身份不符：status=%+v，错误=%v", statusB, err)
	}
	response = wechatTestCallback(t, server, config, wechatTestMessage(config, now, "openid-a", "text", codeA), true)
	if !strings.Contains(wechatTestReply(t, response, config, true, "openid-a"), "不重复赠送") {
		t.Fatal("成功消息重试没有返回幂等绑定结果")
	}
}

// TestWechatOfficialAuthenticationAndDisabledDefaults 验证匿名请求无权读取或生成绑定码，默认配置停用且不会意外发放奖励。
func TestWechatOfficialAuthenticationAndDisabledDefaults(t *testing.T) {
	server, config := newWechatOfficialTestServer(t, "safe")
	for _, path := range []string{"/api/wechat-official/status", "/api/wechat-official/binding"} {
		method := http.MethodGet
		if strings.HasSuffix(path, "binding") {
			method = http.MethodPost
		}
		if response := wechatTestRequest(server.Routes(), method, path, "", ""); response.Code != http.StatusUnauthorized {
			t.Fatalf("匿名请求被接受：path=%s，status=%d", path, response.Code)
		}
	}
	config.Enabled = false
	saveWechatOfficialTestConfig(t, server, config)
	token := wechatTestSession(t, server, "disabled@example.com")
	if response := wechatTestRequest(server.Routes(), http.MethodPost, "/api/wechat-official/binding", token, ""); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("停用配置仍可生成绑定码：status=%d", response.Code)
	}
	response := wechatTestCallback(t, server, config, wechatTestMessage(config, server.wechatOfficial.now(), "test-openid", "text", "23456789"), true)
	if response.Body.String() != "success" {
		t.Fatal("停用配置仍处理绑定业务")
	}
	loaded, err := loadWechatOfficialConfig(NewMemorySystemConfigStore())
	if err != nil || loaded.Enabled || loaded.configured() || loaded.EncodingAESKey != "" || loaded.MessageMode != "safe" {
		t.Fatalf("默认配置不是安全停用状态：config=%+v，错误=%v", loaded, err)
	}
}

// TestWechatOfficialPrivateConfigRedactionAndSave 验证超管 GET/PUT 都隐藏 AES，空密钥保存保留原值且未知字段不丢失。
func TestWechatOfficialPrivateConfigRedactionAndSave(t *testing.T) {
	server, config := newWechatOfficialTestServer(t, "safe")
	adminToken := wechatTestSession(t, server, "wechat-admin@example.com")
	userToken := wechatTestSession(t, server, "ordinary@example.com")
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		t.Fatal(err)
	}
	values["future_option"] = "keep-me"
	raw, err = json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.systemConfigs.Save(SystemConfig{ConfigKey: wechatOfficialConfigKey, ConfigValue: string(raw), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", userToken} {
		want := http.StatusForbidden
		if token == "" {
			want = http.StatusUnauthorized
		}
		response := wechatTestRequest(server.Routes(), http.MethodGet, "/api/admin/system/configs/", token, "")
		if response.Code != want || strings.Contains(response.Body.String(), config.EncodingAESKey) {
			t.Fatalf("非超管读取私有配置：status=%d，body=%s", response.Code, response.Body.String())
		}
	}
	response := wechatTestRequest(server.Routes(), http.MethodGet, "/api/admin/system/configs/", adminToken, "")
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), config.EncodingAESKey) {
		t.Fatalf("超管 GET 泄露 AES 或读取失败：status=%d", response.Code)
	}
	var listed struct {
		Configs []SystemConfig `json:"configs"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, record := range listed.Configs {
		if record.ConfigKey == wechatOfficialConfigKey {
			found = true
			var exposed map[string]any
			if err := json.Unmarshal([]byte(record.ConfigValue), &exposed); err != nil {
				t.Fatal(err)
			}
			if exposed["encoding_aes_key"] != "" || exposed["encoding_aes_key_configured"] != true || exposed["reward_days"] != float64(5) {
				t.Fatalf("配置脱敏展示不符：%+v", exposed)
			}
		}
	}
	if !found {
		t.Fatal("超管列表缺少公众号配置")
	}
	for _, replacement := range []string{"", "********"} {
		incoming, err := json.Marshal(map[string]any{"encoding_aes_key": replacement, "account_name": " 修改名称 ", "callback_url": "https://invalid.example/", "reward_days": 999})
		if err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(updateAdminSystemConfigRequest{ConfigValue: string(incoming)})
		if err != nil {
			t.Fatal(err)
		}
		response = wechatTestRequest(server.Routes(), http.MethodPut, "/api/admin/system/configs/"+wechatOfficialConfigKey, adminToken, string(body))
		if response.Code != http.StatusOK || strings.Contains(response.Body.String(), config.EncodingAESKey) {
			t.Fatalf("PUT 响应泄露 AES 或保存失败：status=%d，body=%s", response.Code, response.Body.String())
		}
		stored, err := server.systemConfigs.Get(wechatOfficialConfigKey)
		if err != nil {
			t.Fatal(err)
		}
		var saved map[string]any
		if err := json.Unmarshal([]byte(stored.ConfigValue), &saved); err != nil {
			t.Fatal(err)
		}
		if saved["encoding_aes_key"] != config.EncodingAESKey || saved["account_name"] != "修改名称" || saved["future_option"] != "keep-me" {
			t.Fatalf("脱敏配置保存破坏密钥或未知字段：%+v", saved)
		}
		if saved["callback_url"] != nil || saved["reward_days"] != nil || saved["encoding_aes_key_configured"] != nil {
			t.Fatal("仅展示字段被写入私有配置")
		}
	}
	for _, path := range []string{"/api/system/app-config", "/api/platforms/config/"} {
		response = wechatTestRequest(server.Routes(), http.MethodGet, path, "", "")
		if response.Code != http.StatusOK || bytes.Contains(response.Body.Bytes(), []byte(config.EncodingAESKey)) || strings.Contains(response.Body.String(), wechatOfficialConfigKey) {
			t.Fatalf("公开配置泄露公众号私有信息：path=%s，status=%d", path, response.Code)
		}
	}
}

// TestWechatOfficialConfigValidation 验证错密钥、错误标识和无效模式无法覆盖原配置，空旧对象合并不会崩溃。
func TestWechatOfficialConfigValidation(t *testing.T) {
	server, _ := newWechatOfficialTestServer(t, "safe")
	existing, err := server.systemConfigs.Get(wechatOfficialConfigKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, incoming := range []string{`null`, `[]`, `{"encoding_aes_key":123}`, `{"encoding_aes_key":"wrong"}`, `{"app_id":"wxWrong"}`, `{"original_id":"invalid"}`, `{"token":"bad-token"}`, `{"message_mode":"unknown"}`, `{"qr_image_url":"http://example.com/qr.png"}`, `{"qr_image_url":"https://user:pass@example.com/qr.png"}`} {
		if _, err := mergeWechatOfficialConfig(existing.ConfigValue, incoming); err == nil {
			t.Fatalf("错误配置被接受：%s", incoming)
		}
	}
	if _, err := mergeWechatOfficialConfig("null", `{"enabled":false}`); err != nil {
		t.Fatalf("历史空对象无法修复：%v", err)
	}
}

// TestWechatOfficialConfigCaseInsensitiveAESRedaction 验证历史或管理员 JSON 中大小写不同的 AES 字段也不能泄露密钥。
func TestWechatOfficialConfigCaseInsensitiveAESRedaction(t *testing.T) {
	for _, alias := range []string{"Encoding_AES_Key", "ENCODING_AES_KEY"} {
		t.Run(alias, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{alias: wechatTestAESKey(), "enabled": false})
			if err != nil {
				t.Fatal(err)
			}
			record := redactWechatOfficialRecord(SystemConfig{ConfigKey: wechatOfficialConfigKey, ConfigValue: string(raw), Enabled: true})
			if strings.Contains(record.ConfigValue, wechatTestAESKey()) {
				t.Fatalf("大小写不同的 AES 键未脱敏：%s", alias)
			}
			merged, err := mergeWechatOfficialConfig(string(raw), `{"account_name":"测试"}`)
			if err != nil {
				t.Fatal(err)
			}
			var values map[string]any
			if err := json.Unmarshal([]byte(merged), &values); err != nil {
				t.Fatal(err)
			}
			if values["encoding_aes_key"] != wechatTestAESKey() || values[alias] != nil {
				t.Fatal("AES 大小写别名未统一到标准字段或丢失密钥")
			}
		})
	}
}

// TestWechatOfficialConfigRejectsDuplicateCaseFields 验证同一已知字段的多个大小写拼法不能产生不确定的配置值。
func TestWechatOfficialConfigRejectsDuplicateCaseFields(t *testing.T) {
	for _, raw := range []string{
		`{"enabled":false,"Enabled":true}`,
		`{"encoding_aes_key":"","Encoding_AES_Key":""}`,
		`{"message_mode":"safe","MESSAGE_MODE":"plaintext"}`,
	} {
		if _, err := mergeWechatOfficialConfig(`{}`, raw); err == nil {
			t.Fatalf("新配置接受了重复大小写字段：%s", raw)
		}
		if _, err := mergeWechatOfficialConfig(raw, `{"account_name":"测试"}`); err == nil {
			t.Fatalf("历史重复大小写字段没有被拒绝：%s", raw)
		}
	}
}

// TestWechatOfficialBindingRequiresFollowAndValidTTL 验证文本不会自行确认关注，未关注失败不消费绑定码，而过期码不能绑定。
func TestWechatOfficialBindingRequiresFollowAndValidTTL(t *testing.T) {
	server, config := newWechatOfficialTestServer(t, "safe")
	token := wechatTestSession(t, server, "follow-required@example.com")
	code := wechatTestBindingCode(t, server, token)
	now := server.wechatOfficial.now()
	message := wechatTestMessage(config, now, "test-openid", "text", code)
	response := wechatTestCallback(t, server, config, message, true)
	if !strings.Contains(wechatTestReply(t, response, config, true, "test-openid"), "还没收到你的关注记录") {
		t.Fatal("单纯文本被当作已关注")
	}
	status, err := server.wechatOfficial.store.BindingStatus(context.Background(), "follow-required@example.com")
	if err != nil || status.Bound || status.GiftGranted {
		t.Fatalf("未关注状态被修改：%+v，错误=%v", status, err)
	}
	wechatTestCallback(t, server, config, wechatTestMessage(config, now, "test-openid", "event", "subscribe"), true)
	response = wechatTestCallback(t, server, config, message, true)
	if !strings.Contains(wechatTestReply(t, response, config, true, "test-openid"), "已到账") {
		t.Fatal("未关注失败错误地消费了绑定码")
	}
	expiredToken := wechatTestSession(t, server, "expired-code@example.com")
	expiredCode := wechatTestBindingCode(t, server, expiredToken)
	server.wechatOfficial.now = func() time.Time { return now.Add(wechatBindingTTL) }
	wechatTestCallback(t, server, config, wechatTestMessage(config, server.wechatOfficial.now(), "expired-openid", "event", "subscribe"), true)
	response = wechatTestCallback(t, server, config, wechatTestMessage(config, server.wechatOfficial.now(), "expired-openid", "text", expiredCode), true)
	if !strings.Contains(wechatTestReply(t, response, config, true, "expired-openid"), "过期") {
		t.Fatal("过期边界的绑定码仍被接受")
	}
}

// wechatCallbackFailingStore 模拟关注事件持久化失败，验证回调不会错误确认成功。
type wechatCallbackFailingStore struct{ WechatFollowStore }

// RecordFollow 返回固定测试错误，不连接任何数据库或外部接口。
func (s *wechatCallbackFailingStore) RecordFollow(context.Context, string, string, bool, time.Time) error {
	return errors.New("test-store-failure")
}

// TestWechatOfficialStoreFailureRequestsRetry 验证落库失败返回非 200，避免微信认为奖励已成功处理。
func TestWechatOfficialStoreFailureRequestsRetry(t *testing.T) {
	server, config := newWechatOfficialTestServer(t, "safe")
	server.wechatOfficial.store = &wechatCallbackFailingStore{WechatFollowStore: server.wechatOfficial.store}
	message := wechatTestMessage(config, server.wechatOfficial.now(), "test-openid", "event", "subscribe")
	query, body := wechatTestCallbackRequest(t, config, server.wechatOfficial.now(), message, true)
	response := wechatTestRequest(server.Routes(), http.MethodPost, "/api/wechat-official/callback?"+query.Encode(), "", body)
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "绑定好了") {
		t.Fatalf("落库失败仍被确认为成功：status=%d，body=%s", response.Code, response.Body.String())
	}
}

// TestWechatOfficialConfigPUTAuthorization 验证匿名及普通用户不能通过 PUT 修改公众号私有配置。
func TestWechatOfficialConfigPUTAuthorization(t *testing.T) {
	server, _ := newWechatOfficialTestServer(t, "safe")
	userToken := wechatTestSession(t, server, "ordinary@example.com")
	before, err := server.systemConfigs.Get(wechatOfficialConfigKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", userToken} {
		body, err := json.Marshal(updateAdminSystemConfigRequest{ConfigValue: `{"enabled":false}`})
		if err != nil {
			t.Fatal(err)
		}
		response := wechatTestRequest(server.Routes(), http.MethodPut, "/api/admin/system/configs/"+wechatOfficialConfigKey, token, string(body))
		want := http.StatusForbidden
		if token == "" {
			want = http.StatusUnauthorized
		}
		after, err := server.systemConfigs.Get(wechatOfficialConfigKey)
		if response.Code != want || err != nil || after.ConfigValue != before.ConfigValue {
			t.Fatalf("未授权配置更新未被拒绝：status=%d，错误=%v", response.Code, err)
		}
	}
}
