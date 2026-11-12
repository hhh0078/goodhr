// Package cloudws 负责本地程序到云端 /api/agents/ws 的 WebSocket 长连接客户端：
// 接收云端下发的 position.start 命令，快速确认后转交任务启动逻辑，并自动重连。
package cloudws

import (
	"context"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// StartCommandType 云端下发岗位启动命令的消息类型，与云端 AgentWSHub 保持一致。
const StartCommandType = "position.start"

// Message 是本地程序与云端之间的统一消息协议，字段含义与云端 AgentWSMessage 相同。
type Message struct {
	MessageID  string         `json:"message_id"`
	ReplyTo    string         `json:"reply_to,omitempty"`
	Type       string         `json:"type"`
	PositionID string         `json:"position_id,omitempty"`
	Attempt    int            `json:"attempt,omitempty"`
	OK         bool           `json:"ok"`
	Error      string         `json:"error,omitempty"`
	Payload    map[string]any `json:"payload,omitempty"`
}

// StartLauncher 表示一个岗位启动入口：同步做快速校验，接受后自行在后台执行任务。
// accepted=false 时 message 会作为拒绝原因回复云端。
type StartLauncher func(positionID, taskType string) (accepted bool, message string)

// TokenProvider 每次建连时提供最新的云端会话令牌；返回空表示当前没有可用令牌。
type TokenProvider func() string

// Client 维护到云端的 WebSocket 长连接。
type Client struct {
	cloudBaseURL string
	token        TokenProvider
	launch       StartLauncher
	mu           sync.Mutex
	conn         *websocket.Conn
	connected    bool
}

// NewClient 创建云端 WS 客户端。cloudBaseURL 形如 https://goodhr.58it.cn。
func NewClient(cloudBaseURL string, token TokenProvider, launch StartLauncher) *Client {
	return &Client{
		cloudBaseURL: strings.TrimRight(cloudBaseURL, "/"),
		token:        token,
		launch:       launch,
	}
}

// Connected 返回当前是否已连接云端。
func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected && c.conn != nil
}

// Run 阻塞运行连接循环：断线后按退避间隔自动重连，直到 ctx 取消。
func (c *Client) Run(ctx context.Context) {
	backoff := 5 * time.Second
	const maxBackoff = 2 * time.Minute
	for ctx.Err() == nil {
		token := strings.TrimSpace(c.token())
		if token == "" {
			// 控制台还没把云端登录态同步过来，安静等待下一轮。
			if err := sleepContext(ctx, backoff); err != nil {
				return
			}
			continue
		}
		if err := c.serve(ctx, token); err != nil && ctx.Err() == nil {
			log.Printf("[云端WS客户端] 连接中断 err=%v，%v 后重连", err, backoff)
		}
		if err := sleepContext(ctx, backoff); err != nil {
			return
		}
		// 连接稳定一段时间后恢复较短的重连间隔。
		if c.Connected() {
			backoff = 5 * time.Second
		} else if backoff < maxBackoff {
			backoff *= 2
		}
	}
}

// serve 建立一次连接并处理消息，连接关闭或出错时返回。
func (c *Client) serve(ctx context.Context, token string) error {
	wsURL := c.cloudBaseURL + "/api/agents/ws?token=" + url.QueryEscape(token)
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.mu.Unlock()
	log.Printf("[云端WS客户端] 已连接云端命令通道 %s", c.cloudBaseURL)
	defer func() {
		c.mu.Lock()
		c.connected = false
		c.conn = nil
		c.mu.Unlock()
		_ = conn.Close()
		log.Printf("[云端WS客户端] 云端命令通道已断开")
	}()
	go pingLoop(ctx, conn)
	for {
		var msg Message
		if err := conn.ReadJSON(&msg); err != nil {
			return err
		}
		// 只处理云端命令，回复类消息由云端侧记录。
		if msg.ReplyTo != "" {
			continue
		}
		c.handle(ctx, conn, msg)
	}
}

// pingLoop 周期发送 ping 保持连接活跃，ctx 取消后退出。
func pingLoop(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				return
			}
		}
	}
}

// handle 分发云端命令并立即回复确认。
func (c *Client) handle(ctx context.Context, conn *websocket.Conn, msg Message) {
	switch msg.Type {
	case StartCommandType:
		positionID, _ := msg.Payload["position_id"].(string)
		taskType, _ := msg.Payload["task_type"].(string)
		positionID = strings.TrimSpace(positionID)
		accepted, message := false, ""
		if positionID == "" {
			message = "命令缺少 position_id，我这边没法启动岗位"
		} else if c.launch != nil {
			accepted, message = c.launch(positionID, taskType)
		} else {
			message = "本地程序还没准备好任务启动入口"
		}
		reply := Message{
			MessageID:  newMessageID(),
			ReplyTo:    msg.MessageID,
			Type:       msg.Type + ".ack",
			PositionID: positionID,
			OK:         accepted,
			Error:      "",
			Payload:    map[string]any{"message": message},
		}
		if !accepted && message != "" {
			reply.Error = message
		}
		_ = conn.WriteJSON(reply)
	default:
		// 未知命令只回复不支持，不影响连接。
		_ = conn.WriteJSON(Message{
			MessageID: newMessageID(),
			ReplyTo:   msg.MessageID,
			Type:      msg.Type + ".ack",
			OK:        false,
			Error:     "本地程序暂不支持该命令: " + msg.Type,
		})
	}
}

func newMessageID() string {
	return "ws-" + strings.ReplaceAll(time.Now().UTC().Format("20060102150405.000000000"), ".", "")
}

// sleepContext 等待一段时间，ctx 取消时立即返回。
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
