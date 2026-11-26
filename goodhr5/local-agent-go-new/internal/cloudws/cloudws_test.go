// Package cloudws 文件作用：验证云端地址协议转换，以及真实 WebSocket 握手、Bearer 认证和任务回执。
package cloudws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestCloudWebSocketURL 验证正式 HTTPS、测试 HTTP、路径前缀和无效地址，不泄露查询参数或身份信息。
func TestCloudWebSocketURL(t *testing.T) {
	for _, test := range []struct {
		input, want string
	}{
		{"https://goodhr.58it.cn", "wss://goodhr.58it.cn/api/agents/ws"},
		{"http://127.0.0.1:8084/", "ws://127.0.0.1:8084/api/agents/ws"},
		{"https://example.test/goodhr/?token=old-secret#section", "wss://example.test/goodhr/api/agents/ws"},
		{"wss://example.test", "wss://example.test/api/agents/ws"},
		{"ws://127.0.0.1:8084", "ws://127.0.0.1:8084/api/agents/ws"},
		{"https://user:private-secret@example.test", ""},
		{"file:///tmp/private-secret", ""},
		{"https://", ""},
		{"", ""},
	} {
		got, err := cloudWebSocketURL(test.input)
		if got != test.want || (err != nil) != (test.want == "") {
			t.Fatalf("地址转换不符合预期：got=%q want=%q err=%v", got, test.want, err)
		}
		if err != nil && strings.Contains(err.Error(), "private-secret") {
			t.Fatal("错误信息不应包含地址中的身份信息")
		}
	}
}

// TestClientServeWebSocketAuthentication 使用真实本机握手证明 HTTP 基址可建连、令牌只进请求头且岗位回执正常。
func TestClientServeWebSocketAuthentication(t *testing.T) {
	const token = "synthetic-token-for-header-only"
	acknowledged := make(chan Message, 1)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/goodhr/api/agents/ws" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("命令通道路径、查询参数或 Bearer 请求头不正确")
			http.Error(w, "unexpected websocket authentication", http.StatusUnauthorized)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		if err := conn.WriteJSON(Message{MessageID: "synthetic-command", Type: StartCommandType, Payload: map[string]any{"position_id": "synthetic-position", "task_type": "greeting"}}); err != nil {
			t.Error(err)
			return
		}
		var reply Message
		if err := conn.ReadJSON(&reply); err != nil {
			t.Error(err)
			return
		}
		acknowledged <- reply
	}))
	defer server.Close()
	var client *Client
	client = NewClient(server.URL+"/goodhr/", func() string { return token }, func(positionID, taskType string) (bool, string) {
		if !client.Connected() || positionID != "synthetic-position" || taskType != "greeting" {
			t.Error("握手后连接状态或岗位参数不正确")
			return false, "参数不正确"
		}
		if code, status := client.Diagnostic(); code != "" || status != 0 {
			t.Error("成功握手后应清除旧连接错误")
		}
		return true, "合成岗位已接收"
	})
	client.recordDialFailure(nil, "WS_CONNECTION_FAILED")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connected, _ := client.serve(ctx, token)
	if !connected {
		t.Fatal("成功握手必须保留本次连接成功事实，供退避恢复使用")
	}
	if client.Connected() {
		t.Fatal("连接关闭后不应仍然报告在线")
	}
	select {
	case reply := <-acknowledged:
		if !reply.OK || reply.ReplyTo != "synthetic-command" || reply.Type != StartCommandType+".ack" || reply.PositionID != "synthetic-position" {
			t.Fatalf("任务回执不正确：%+v", reply)
		}
	default:
		t.Fatal("真实 WebSocket 握手没有完成任务回执")
	}
}

// TestClientHandshakeDiagnostic 验证真实握手拒绝返回准确状态，外部正文中的凭据不会进入诊断接口。
func TestClientHandshakeDiagnostic(t *testing.T) {
	for _, test := range []struct {
		status     int
		body, code string
	}{
		{http.StatusBadRequest, "websocket: the client is not using the websocket protocol: 'upgrade' token not found in 'Connection' header private-secret", "WS_UPGRADE_CONNECTION_MISSING"},
		{http.StatusBadRequest, "websocket: 'websocket' token not found in 'Upgrade' header private-secret", "WS_UPGRADE_HEADER_MISSING"},
		{http.StatusUnauthorized, `{"error":"session is invalid or expired","token":"private-secret"}`, "WS_HTTP_REJECTED"},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(test.status)
			_, _ = w.Write([]byte(test.body))
		}))
		client := NewClient(server.URL, nil, nil)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		connected, err := client.serve(ctx, "private-secret")
		cancel()
		server.Close()
		code, status := client.Diagnostic()
		if connected || err == nil || code != test.code || status != test.status || strings.Contains(code, "private-secret") {
			t.Fatalf("握手诊断不符合预期：code=%s status=%d err=%v", code, status, err)
		}
	}
}

// TestSessionRejectionOnlyOnUnauthorized 验证真实握手仅在明确 401 时撤销该次凭证，权限或服务器故障保留重试。
func TestSessionRejectionOnlyOnUnauthorized(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
		client := NewClient(server.URL, nil, nil)
		var rejected string
		client.SetSessionHandlers(func(context.Context, string) { t.Error("拒绝握手不能恢复运行账号") }, func(token string) { rejected = token })
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		connected, err := client.serve(ctx, "synthetic-session")
		cancel()
		server.Close()
		if connected || err == nil || (rejected != "") != (status == http.StatusUnauthorized) {
			t.Fatal("握手失败错误地撤销了可重试会话，或未清理明确失效凭证")
		}
		if rejected != "" && rejected != "synthetic-session" {
			t.Fatal("撤销回调没有固定本次握手的身份")
		}
	}
}

// TestReconnectBackoff 验证连续失败严格封顶，成功握手或新凭证会恢复五秒重试。
func TestReconnectBackoff(t *testing.T) {
	backoff := initialBackoff
	for _, expected := range []time.Duration{10, 20, 40, 80, 120, 120} {
		backoff = nextBackoff(backoff, false)
		if backoff != expected*time.Second {
			t.Fatalf("连续失败退避=%s，预期=%ss", backoff, expected)
		}
	}
	if got := nextBackoff(backoff, true); got != 5*time.Second {
		t.Fatalf("重新握手后退避=%s，预期5s", got)
	}
}

// TestNewTokenWakesRetry 验证新凭证不沿用两分钟退避，凭证仅在内存中用于比较。
func TestNewTokenWakesRetry(t *testing.T) {
	t.Parallel()
	var current atomic.Value
	current.Store("old-synthetic-token")
	client := NewClient("https://example.test", func() string { return current.Load().(string) }, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	go func() {
		select {
		case <-ctx.Done():
		case <-time.After(50 * time.Millisecond):
			current.Store("new-synthetic-token")
		}
	}()
	changed, err := client.waitForRetry(ctx, "old-synthetic-token", maxBackoff)
	if err != nil || !changed {
		t.Fatalf("新凭证没有及时唤醒退避：changed=%v err=%v", changed, err)
	}
}

// TestConnectedTokenChangeReconnects 验证已连接时换号也会结束旧通道，退出取消不会卡在消息读取上。
func TestConnectedTokenChangeReconnects(t *testing.T) {
	t.Parallel()
	var current atomic.Value
	current.Store("old-synthetic-token")
	upgraded := make(chan struct{})
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		close(upgraded)
		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()
	client := NewClient(server.URL, func() string { return current.Load().(string) }, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	finished := make(chan bool, 1)
	go func() { connected, _ := client.serve(ctx, "old-synthetic-token"); finished <- connected }()
	select {
	case <-upgraded:
		current.Store("new-synthetic-token")
	case <-ctx.Done():
		t.Fatal("合成通道没有完成握手")
	}
	select {
	case connected := <-finished:
		if !connected || client.Connected() {
			t.Fatal("换号后旧连接状态不正确")
		}
	case <-ctx.Done():
		t.Fatal("换号后未及时结束旧连接")
	}
}
