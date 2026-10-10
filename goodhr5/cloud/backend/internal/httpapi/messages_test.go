// 文件作用：验证消息账号隔离、受限会话、纯文本标题列表、稳定幂等及首次已读72小时清理边界。
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// messageTestAuth 为两个独立账号和一个受限插件建立合成会话。
func messageTestAuth(t *testing.T) *AuthService {
	t.Helper()
	store := NewMemoryAuthStore()
	for token, session := range map[string]Session{"owner": {Email: "owner@example.test"}, "other": {Email: "other@example.test"}, "plugin": {Email: "owner@example.test", Kind: extensionSessionKind}} {
		if err := store.SaveSession(token, session, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	return &AuthService{store: store}
}

// messageRequest 调用真实HTTP处理器，不使用任何真实账号资料。
func messageRequest(handler http.HandlerFunc, method, path, token, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler(response, request)
	return response
}

// messageFixture 生成合法的稳定业务消息。
func messageFixture(key string) MessageInput {
	return MessageInput{Title: "简历已导入", Body: "这份下载的简历已经记进GoodHR了。", Type: "resume_import", ActionURL: "/admin/resumes", IdempotencyKey: key}
}

// TestMessageHTTPPrivacy 验证消息推送不能指定其他收件人，列表无正文且跨账号读取统一404。
func TestMessageHTTPPrivacy(t *testing.T) {
	store := NewMemoryMessageStore()
	service := NewMessageService(messageTestAuth(t), store)
	payload, _ := json.Marshal(messageFixture("http-one"))
	response := messageRequest(service.PushCurrent, "POST", "/api/messages/push", "owner", string(payload))
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	var pushed struct {
		Message UserMessage `json:"message"`
		Created bool        `json:"created"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &pushed); err != nil {
		t.Fatal(err)
	}
	if !pushed.Created || pushed.Message.ID == "" {
		t.Fatal(response.Body.String())
	}
	response = messageRequest(service.Collection, "GET", "/api/messages?page=1&page_size=20", "owner", "")
	if response.Code != 200 || strings.Contains(response.Body.String(), `"body"`) || strings.Contains(response.Body.String(), "已经记进") {
		t.Fatal(response.Body.String())
	}
	response = messageRequest(service.Collection, "GET", "/api/messages", "other", "")
	if response.Code != 200 || strings.Contains(response.Body.String(), pushed.Message.ID) {
		t.Fatal(response.Body.String())
	}
	response = messageRequest(service.Read, "POST", "/api/messages/"+pushed.Message.ID+"/read", "other", "")
	if response.Code != 404 {
		t.Fatal(response.Body.String())
	}
	summary, _ := store.Summary("owner@example.test")
	if summary.UnreadCount != 1 {
		t.Fatal(summary)
	}
	response = messageRequest(service.Read, "POST", "/api/messages/"+pushed.Message.ID+"/read", "owner", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"body"`) {
		t.Fatal(response.Body.String())
	}
	summary, _ = store.Summary("owner@example.test")
	if summary.UnreadCount != 0 || summary.Version != 2 {
		t.Fatal(summary)
	}
	for _, token := range []string{"", "plugin"} {
		response = messageRequest(service.Summary, "GET", "/api/messages/summary", token, "")
		if response.Code != 401 && response.Code != 403 {
			t.Fatal(response.Body.String())
		}
	}
	for _, body := range []string{`{"title":"标题","body":"正文","idempotency_key":"scope","user_id":"someone-else"}`, `{"title":"标题","body":"正文","idempotency_key":"scope","email":"other@example.test"}`, `{"title":"标题","body":"正文","idempotency_key":"scope"} {}`} {
		response = messageRequest(service.PushCurrent, "POST", "/api/messages/push", "owner", body)
		if response.Code != 400 {
			t.Fatal(response.Body.String())
		}
	}
	for _, path := range []string{"https://evil.example/", "//evil.example/", "/\\evil.example/", "javascript:alert(1)"} {
		input := messageFixture("bad-url")
		input.ActionURL = path
		body, _ := json.Marshal(input)
		response = messageRequest(service.PushCurrent, "POST", "/api/messages/push", "owner", string(body))
		if response.Code != 400 {
			t.Fatalf("accepted %s: %s", path, response.Body.String())
		}
	}
}

// TestMessageServerRoutes 验证服务组装和真实路由复用账号隔离、推送、红点、列表、展开与设置接口。
func TestMessageServerRoutes(t *testing.T) {
	for _, name := range []string{"GOODHR_PG_DSN", "GOODHR_REDIS_ADDR", "GOODHR_SMTP_HOST"} {
		t.Setenv(name, "")
	}
	server := mustNewServer(t)
	for token, session := range map[string]Session{"message-route-owner": {Email: "message-route-owner@example.test"}, "message-route-plugin": {Email: "message-route-owner@example.test", Kind: extensionSessionKind}} {
		if err := server.auth.store.SaveSession(token, session, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	routes := server.Routes()
	payload, _ := json.Marshal(messageFixture("server-routing-one"))
	response := messageRequest(routes.ServeHTTP, "POST", "/api/messages/push", "message-route-owner", string(payload))
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	var result struct {
		Message UserMessage `json:"message"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Message.ID == "" {
		t.Fatal(response.Body.String(), err)
	}
	for _, path := range []string{"/api/messages/summary", "/api/messages?page=1&page_size=20", "/api/resume-library/settings"} {
		response = messageRequest(routes.ServeHTTP, "GET", path, "message-route-owner", "")
		if response.Code != 200 {
			t.Fatal(path, response.Body.String())
		}
		response = messageRequest(routes.ServeHTTP, "GET", path, "message-route-plugin", "")
		if response.Code != 403 {
			t.Fatal(path, response.Body.String())
		}
	}
	response = messageRequest(routes.ServeHTTP, "POST", "/api/messages/"+result.Message.ID+"/read", "message-route-owner", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"unread_count":0`) {
		t.Fatal(response.Body.String())
	}
	response = messageRequest(routes.ServeHTTP, "PATCH", "/api/resume-library/settings", "message-route-owner", `{"auto_import_downloads":false}`)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"auto_import_downloads":false`) {
		t.Fatal(response.Body.String())
	}
}

// TestMessageIdempotencyAndConcurrentRead 验证并发相同请求只新增一次，重复展开只扣一次未读。
func TestMessageIdempotencyAndConcurrentRead(t *testing.T) {
	store := NewMemoryMessageStore()
	verifyMessageIdempotency(t, store)
}

// verifyMessageIdempotency 复用内存和数据库的并发幂等与权限断言。
func verifyMessageIdempotency(t *testing.T, store MessageStore) {
	t.Helper()
	input := messageFixture("concurrent-stable")
	const count = 32
	var wg sync.WaitGroup
	results := make(chan MessagePushResult, count)
	failures := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := store.Push("owner@example.test", input)
			if err != nil {
				failures <- err
				return
			}
			results <- result
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	id := ""
	created := 0
	for result := range results {
		if id == "" {
			id = result.Message.ID
		}
		if result.Message.ID != id {
			t.Fatal("duplicate message")
		}
		if result.Created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("created=%d", created)
	}
	summary, err := store.Summary("owner@example.test")
	if err != nil || summary != (MessageSummary{1, 1}) {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
	changed := input
	changed.Body = "不同内容"
	if _, err = store.Push("owner@example.test", changed); !errors.Is(err, ErrMessageIdempotency) {
		t.Fatalf("changed content accepted: %v", err)
	}
	if _, _, err = store.Read("other@example.test", id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-account body read: %v", err)
	}
	failures = make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			message, _, err := store.Read("owner@example.test", id)
			if err != nil {
				failures <- err
				return
			}
			if message.ReadAt == nil {
				failures <- errors.New("no read timestamp")
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	summary, err = store.Summary("owner@example.test")
	if err != nil || summary != (MessageSummary{0, 2}) {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
	other, err := store.Push("other@example.test", input)
	if err != nil || !other.Created || other.Message.ID == id {
		t.Fatalf("account-scoped key=%+v err=%v", other, err)
	}
}

// TestMessageCleanupSeventyTwoHours 验证边界以前保留、恰满72小时删除，未读和永久幂等凭据保留。
func TestMessageCleanupSeventyTwoHours(t *testing.T) {
	store := NewMemoryMessageStore()
	now := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	service := NewMessageService(nil, store)
	old, err := store.Push("owner@example.test", messageFixture("read-old"))
	if err != nil {
		t.Fatal(err)
	}
	unread, err := store.Push("owner@example.test", messageFixture("keep-unread"))
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Hour)
	message, _, err := store.Read("owner@example.test", old.Message.ID)
	if err != nil {
		t.Fatal(err)
	}
	readAt := *message.ReadAt
	now = now.Add(20 * time.Hour)
	message, _, _ = store.Read("owner@example.test", old.Message.ID)
	if !message.ReadAt.Equal(readAt) {
		t.Fatal("repeat read extends retention")
	}
	if err = service.CleanupRead(readAt.Add(72*time.Hour - time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	list, _ := store.List("owner@example.test", 1, 20)
	if list.Total != 2 {
		t.Fatal(list)
	}
	if err = service.CleanupRead(readAt.Add(72 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	list, _ = store.List("owner@example.test", 1, 20)
	if list.Total != 1 || list.Messages[0].ID != unread.Message.ID {
		t.Fatal(list)
	}
	summary, _ := store.Summary("owner@example.test")
	if summary != (MessageSummary{1, 4}) {
		t.Fatal(summary)
	}
	repeat, err := store.Push("owner@example.test", messageFixture("read-old"))
	if err != nil || repeat.Created || repeat.Message.ID != old.Message.ID {
		t.Fatalf("cleanup lost receipt: %+v %v", repeat, err)
	}
}
