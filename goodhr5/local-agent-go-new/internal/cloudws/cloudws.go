// Package cloudws 负责本地程序到云端 /api/agents/ws 的 WebSocket 长连接客户端：
// 接收云端下发的 position.start 命令，快速确认后转交任务启动逻辑，并自动重连。
package cloudws

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// StartCommandType 云端下发岗位启动命令的消息类型，与云端 AgentWSHub 保持一致。
const StartCommandType = "position.start"

const (
	initialBackoff     = 5 * time.Second
	maxBackoff         = 2 * time.Minute
	tokenCheckInterval = 5 * time.Second
)

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
	errorCode    string
	httpStatus   int
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

// Diagnostic 返回最近一次连接错误的稳定分类与握手状态码，不包含地址、响应正文或登录凭据。
func (c *Client) Diagnostic() (string, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.errorCode, c.httpStatus
}

// Run 阻塞运行连接循环：断线后按退避间隔自动重连，直到 ctx 取消。
func (c *Client) Run(ctx context.Context) {
	backoff := initialBackoff
	for ctx.Err() == nil {
		token := strings.TrimSpace(c.token())
		if token == "" {
			// 控制台还没把云端登录态同步过来，安静等待下一轮。
			if err := sleepContext(ctx, initialBackoff); err != nil {
				return
			}
			continue
		}
		connected, err := c.serve(ctx, token)
		if connected {
			backoff = nextBackoff(backoff, true)
		}
		if err != nil && ctx.Err() == nil {
			log.Printf("[云端WS客户端] 连接中断 err=%v，%v 后重连", err, backoff)
		}
		changed, err := c.waitForRetry(ctx, token, backoff)
		if err != nil {
			return
		}
		if changed {
			backoff = nextBackoff(backoff, true)
		} else if !connected {
			backoff = nextBackoff(backoff, false)
		}
	}
}

// nextBackoff 在成功握手或更新会话后恢复短重试，连续失败的等待严格不超过两分钟。
func nextBackoff(previous time.Duration, connected bool) time.Duration {
	if connected {
		return initialBackoff
	}
	return min(previous*2, maxBackoff)
}

// waitForRetry 在退避期间复用当前凭证读取入口，账号更新后最迟五秒结束等待。
func (c *Client) waitForRetry(ctx context.Context, token string, delay time.Duration) (bool, error) {
	if strings.TrimSpace(c.token()) != token {
		return true, nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	ticker := time.NewTicker(tokenCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-timer.C:
			return strings.TrimSpace(c.token()) != token, nil
		case <-ticker.C:
			if strings.TrimSpace(c.token()) != token {
				return true, nil
			}
		}
	}
}

// serve 建立一次连接并处理消息，返回本次是否成功握手及断开原因，不在断开后读取在线状态判断成功。
func (c *Client) serve(ctx context.Context, token string) (bool, error) {
	wsURL, err := cloudWebSocketURL(c.cloudBaseURL)
	if err != nil {
		c.recordDialFailure(nil, "WS_URL_INVALID")
		return false, err
	}
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+token)
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, response, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		c.recordDialFailure(response, "WS_CONNECTION_FAILED")
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return false, err
	}
	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.errorCode, c.httpStatus = "", 0
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
	connectionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go c.pingLoop(connectionCtx, conn, token)
	for {
		var msg Message
		if err := conn.ReadJSON(&msg); err != nil {
			c.recordDialFailure(nil, "WS_CONNECTION_CLOSED")
			return true, err
		}
		// 只处理云端命令，回复类消息由云端侧记录。
		if msg.ReplyTo != "" {
			continue
		}
		c.handle(ctx, conn, msg)
	}
}

// recordDialFailure 只从握手响应识别固定协议错误，正文读取有界且不保存、不打印原文。
// response 为失败响应；fallback 为本地固定错误码，接口不接收或回显任意外部错误文本。
func (c *Client) recordDialFailure(response *http.Response, fallback string) {
	code, status := fallback, 0
	if response != nil {
		code, status = "WS_HTTP_REJECTED", response.StatusCode
		if response.Body != nil {
			body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
			switch {
			case strings.Contains(string(body), "'upgrade' token not found in 'Connection' header"):
				code = "WS_UPGRADE_CONNECTION_MISSING"
			case strings.Contains(string(body), "'websocket' token not found in 'Upgrade' header"):
				code = "WS_UPGRADE_HEADER_MISSING"
			case strings.Contains(string(body), "does not implement http.Hijacker"):
				code = "WS_SERVER_UPGRADE_UNSUPPORTED"
			}
		}
	}
	c.mu.Lock()
	c.errorCode, c.httpStatus = code, status
	c.mu.Unlock()
}

// cloudWebSocketURL 将云端 HTTP 地址转换为对应的 WS 地址，不把登录凭证放入查询参数。
// baseURL 为云端服务地址；返回命令通道地址，格式或协议不支持时返回不含原始地址的错误。
func cloudWebSocketURL(baseURL string) (string, error) {
	endpoint, err := url.Parse(baseURL)
	if err != nil || endpoint.Host == "" || endpoint.User != nil {
		return "", errors.New("云端命令通道地址不正确")
	}
	switch endpoint.Scheme {
	case "http":
		endpoint.Scheme = "ws"
	case "https":
		endpoint.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", errors.New("云端命令通道地址协议不支持")
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/api/agents/ws"
	endpoint.RawPath, endpoint.RawQuery, endpoint.Fragment = "", "", ""
	endpoint.ForceQuery = false
	return endpoint.String(), nil
}

// pingLoop 保持连接活跃，并在退出或凭证更新时关闭本次连接，让消息读取及时结束并重新认证。
func (c *Client) pingLoop(ctx context.Context, conn *websocket.Conn, token string) {
	defer conn.Close()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	tokenTicker := time.NewTicker(tokenCheckInterval)
	defer tokenTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tokenTicker.C:
			if strings.TrimSpace(c.token()) != token {
				return
			}
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
