// Package cloudws 文件作用：验证云端地址协议转换，以及真实 WebSocket 握手、Bearer 认证和任务回执。
package cloudws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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
		return true, "合成岗位已接收"
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = client.serve(ctx, token)
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
