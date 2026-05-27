// 本文件负责云端与 Local Agent 的 WebSocket 长连接、消息确认和重试调度。
package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const agentWSReplyTimeout = 90 * time.Second

// AgentWSMessage 定义云端和 Local Agent 之间的统一消息协议。
// 任务相关消息必须填写 TaskID，ReplyTo 用于标记回复的是哪条消息。
type AgentWSMessage struct {
	MessageID string         `json:"message_id"`
	ReplyTo   string         `json:"reply_to,omitempty"`
	Type      string         `json:"type"`
	TaskID    string         `json:"task_id,omitempty"`
	Attempt   int            `json:"attempt,omitempty"`
	OK        bool           `json:"ok"`
	Error     string         `json:"error,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
}

// AgentWSHub 保存每个云端用户当前在线的唯一 Local Agent 连接。
type AgentWSHub struct {
	auth     *AuthService
	mu       sync.Mutex
	clients  map[string]*AgentWSClient
	upgrader websocket.Upgrader
}

// NewAgentWSHub 创建 Agent WebSocket 连接管理器。
// auth 用于校验 Local Agent 建连时携带的云端 access_token。
func NewAgentWSHub(auth *AuthService) *AgentWSHub {
	return &AgentWSHub{
		auth:    auth,
		clients: map[string]*AgentWSClient{},
		upgrader: websocket.Upgrader{
			CheckOrigin: func(_ *http.Request) bool { return true },
		},
	}
}

// ServeWS 处理 Local Agent 主动连接云端的 WebSocket 请求。
// 同一用户只保留一个在线连接，新连接会替换旧连接。
func (h *AgentWSHub) ServeWS(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		token = bearerToken(r.Header.Get("Authorization"))
	}
	if token == "" {
		writeError(w, http.StatusUnauthorized, "missing token")
		return
	}
	session, err := h.auth.SessionFromToken(token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[云端WS] 升级连接失败 user=%s err=%v", session.Email, err)
		return
	}
	client := NewAgentWSClient(session.Email, conn, h)
	h.replaceClient(session.Email, client)
	log.Printf("[云端WS] 已连接 user=%s", session.Email)
	go client.writeLoop()
	client.readLoop()
}

// Status 返回当前登录用户的 Local Agent WebSocket 在线状态。
func (h *AgentWSHub) Status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, err := h.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"connected": h.IsOnline(session.Email),
	})
}

// IsOnline 判断指定用户是否有在线 Local Agent WebSocket。
// userEmail 为空时直接返回 false。
func (h *AgentWSHub) IsOnline(userEmail string) bool {
	if userEmail == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	client := h.clients[userEmail]
	return client != nil && !client.isClosed()
}

// SendCommand 向指定用户的 Local Agent 发送命令并等待回复。
// retries 为重试次数，每次超时后会带新的 attempt 再发送一次。
func (h *AgentWSHub) SendCommand(userEmail string, msg AgentWSMessage, retries int) (AgentWSMessage, error) {
	if msg.Type == "" {
		return AgentWSMessage{}, errors.New("message type is required")
	}
	if retries <= 0 {
		retries = 1
	}
	client := h.client(userEmail)
	if client == nil {
		return AgentWSMessage{}, errors.New("local agent websocket is not connected")
	}
	if msg.MessageID == "" {
		msg.MessageID = newWSMessageID()
	}
	// log.Printf("[云端WS] 准备发送命令 user=%s type=%s task=%s message_id=%s retries=%d 摘要=%s", userEmail, msg.Type, msg.TaskID, msg.MessageID, retries, wsPayloadSummary(msg.Payload))
	for attempt := 1; attempt <= retries; attempt++ {
		msg.Attempt = attempt
		reply, err := client.sendAndWait(msg, agentWSReplyTimeout)
		if err == nil {
			log.Printf("[云端WS] 收到命令回复 user=%s type=%s task=%s message_id=%s attempt=%d ok=%v error=%s 摘要=%s", userEmail, msg.Type, msg.TaskID, msg.MessageID, attempt, reply.OK, reply.Error, wsPayloadSummary(reply.Payload))
			if !reply.OK {
				return reply, fmt.Errorf("local agent returned error: %s", reply.Error)
			}
			return reply, nil
		}
		log.Printf("[云端WS] 命令重试 user=%s type=%s task=%s attempt=%d err=%v", userEmail, msg.Type, msg.TaskID, attempt, err)
	}
	return AgentWSMessage{}, fmt.Errorf("local agent websocket command timeout: %s", msg.Type)
}

func (h *AgentWSHub) replaceClient(userEmail string, client *AgentWSClient) {
	h.mu.Lock()
	old := h.clients[userEmail]
	h.clients[userEmail] = client
	h.mu.Unlock()
	if old != nil {
		old.close()
	}
}

func (h *AgentWSHub) removeClient(userEmail string, client *AgentWSClient) {
	h.mu.Lock()
	if h.clients[userEmail] == client {
		delete(h.clients, userEmail)
	}
	h.mu.Unlock()
}

func (h *AgentWSHub) client(userEmail string) *AgentWSClient {
	h.mu.Lock()
	defer h.mu.Unlock()
	client := h.clients[userEmail]
	if client == nil || client.isClosed() {
		return nil
	}
	return client
}

// AgentWSClient 表示一个用户当前在线的 Local Agent WebSocket 连接。
type AgentWSClient struct {
	userEmail string
	conn      *websocket.Conn
	hub       *AgentWSHub
	send      chan AgentWSMessage
	pending   map[string]chan AgentWSMessage
	mu        sync.Mutex
	closed    bool
}

// NewAgentWSClient 创建单个 Local Agent 连接对象。
// userEmail 用于保证一个云端用户只保留一条在线连接。
func NewAgentWSClient(userEmail string, conn *websocket.Conn, hub *AgentWSHub) *AgentWSClient {
	return &AgentWSClient{
		userEmail: userEmail,
		conn:      conn,
		hub:       hub,
		send:      make(chan AgentWSMessage, 16),
		pending:   map[string]chan AgentWSMessage{},
	}
}

func (c *AgentWSClient) readLoop() {
	defer c.close()
	for {
		var msg AgentWSMessage
		if err := c.conn.ReadJSON(&msg); err != nil {
			log.Printf("[云端WS] 读取连接关闭 user=%s err=%v", c.userEmail, err)
			return
		}
		log.Printf("[云端WS] 收到消息 user=%s type=%s task=%s message_id=%s reply_to=%s ok=%v error=%s 摘要=%s", c.userEmail, msg.Type, msg.TaskID, msg.MessageID, msg.ReplyTo, msg.OK, msg.Error, wsPayloadSummary(msg.Payload))
		if msg.ReplyTo != "" {
			c.resolvePending(msg)
			continue
		}
		if msg.MessageID != "" {
			c.queue(AgentWSMessage{
				MessageID: newWSMessageID(),
				ReplyTo:   msg.MessageID,
				Type:      msg.Type + ".ack",
				TaskID:    msg.TaskID,
				OK:        true,
			})
		}
	}
}

// writeLoop 持续把待发送消息写入 WebSocket。
// 连接写入失败时会关闭连接并清理在线状态。
func (c *AgentWSClient) writeLoop() {
	for msg := range c.send {
		log.Printf("[云端WS] 发送消息 user=%s type=%s task=%s message_id=%s reply_to=%s attempt=%d 摘要=%s", c.userEmail, msg.Type, msg.TaskID, msg.MessageID, msg.ReplyTo, msg.Attempt, wsPayloadSummary(msg.Payload))
		if err := c.conn.WriteJSON(msg); err != nil {
			log.Printf("[云端WS] 写入消息失败 user=%s err=%v", c.userEmail, err)
			c.close()
			return
		}
	}
}

func wsPayloadSummary(payload map[string]any) string {
	if len(payload) == 0 {
		return "-"
	}
	path, _ := payload["path"].(string)
	body, _ := payload["body"].(map[string]any)
	if path == "" || body == nil {
		return fmt.Sprintf("keys=%v", sortedMapKeys(payload))
	}
	parts := []string{fmt.Sprintf("path=%s", path)}
	if url, ok := body["url"].(string); ok && url != "" {
		parts = append(parts, fmt.Sprintf("url=%s", url))
	}
	if cardElement, ok := body["card_element"].(map[string]any); ok {
		if parentClasses := toLogStringSlice(cardElement["parent_classes"]); len(parentClasses) > 0 {
			parts = append(parts, fmt.Sprintf("card_parent_classes=%v", parentClasses))
		}
		if targetClasses := toLogStringSlice(cardElement["target_classes"]); len(targetClasses) > 0 {
			parts = append(parts, fmt.Sprintf("card_target_classes=%v", targetClasses))
		}
	}
	if userDataDir, ok := body["user_data_dir"].(string); ok && userDataDir != "" {
		parts = append(parts, fmt.Sprintf("user_data_dir=%s", userDataDir))
	}
	if cookies, ok := body["cookies"].([]any); ok {
		parts = append(parts, fmt.Sprintf("cookies=%d条", len(cookies)))
	}
	if _, ok := body["encrypted_data"]; ok {
		parts = append(parts, "encrypted_data=已传")
	}
	if encryptedKeys, ok := body["encrypted_keys"].(map[string]any); ok {
		parts = append(parts, fmt.Sprintf("encrypted_keys=%d个机器", len(encryptedKeys)))
	}
	if selectors, ok := body["selectors"].(map[string]any); ok {
		parts = append(parts, fmt.Sprintf("selectors=%v", sortedMapKeys(selectors)))
	}
	if fields, ok := body["fields"].([]any); ok {
		names := make([]string, 0, len(fields))
		for _, item := range fields {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			for key := range m {
				names = append(names, key)
			}
		}
		if len(names) > 0 {
			sort.Strings(names)
			parts = append(parts, fmt.Sprintf("fields=%v", names))
		}
	}
	if elementRef, ok := body["element_ref"].(string); ok && elementRef != "" {
		parts = append(parts, fmt.Sprintf("element_ref=%s", elementRef))
	}
	if maxScrolls, ok := body["max_scrolls"]; ok {
		parts = append(parts, fmt.Sprintf("max_scrolls=%v", maxScrolls))
	}
	return strings.Join(parts, ", ")
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func toLogStringSlice(value any) []string {
	switch items := value.(type) {
	case []string:
		return items
	case []any:
		result := make([]string, 0, len(items))
		for _, item := range items {
			if s, ok := item.(string); ok && s != "" {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}

// sendAndWait 发送一条命令并等待对应 reply_to 回复。
// timeout 用于避免 Local Agent 无响应时永久阻塞。
func (c *AgentWSClient) sendAndWait(msg AgentWSMessage, timeout time.Duration) (AgentWSMessage, error) {
	replyCh := make(chan AgentWSMessage, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return AgentWSMessage{}, errors.New("websocket is closed")
	}
	c.pending[msg.MessageID] = replyCh
	c.mu.Unlock()
	if !c.queue(msg) {
		c.removePending(msg.MessageID)
		return AgentWSMessage{}, errors.New("websocket send queue is closed")
	}
	select {
	case reply, ok := <-replyCh:
		if !ok {
			return AgentWSMessage{}, errors.New("websocket is closed")
		}
		return reply, nil
	case <-time.After(timeout):
		c.removePending(msg.MessageID)
		return AgentWSMessage{}, errors.New("reply timeout")
	}
}

// queue 将消息放入写队列。
// 返回 false 表示连接已关闭或发送队列已满。
func (c *AgentWSClient) queue(msg AgentWSMessage) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	select {
	case c.send <- msg:
		return true
	default:
		return false
	}
}

// resolvePending 将 Local Agent 回复分发给等待中的发送方。
// msg.ReplyTo 必须对应之前发送过的 message_id。
func (c *AgentWSClient) resolvePending(msg AgentWSMessage) {
	c.mu.Lock()
	replyCh := c.pending[msg.ReplyTo]
	delete(c.pending, msg.ReplyTo)
	c.mu.Unlock()
	if replyCh != nil {
		replyCh <- msg
	}
}

// removePending 删除指定消息的等待状态。
// 超时和发送失败时会调用该方法避免内存残留。
func (c *AgentWSClient) removePending(messageID string) {
	c.mu.Lock()
	delete(c.pending, messageID)
	c.mu.Unlock()
}

// isClosed 返回当前连接是否已经关闭。
func (c *AgentWSClient) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// close 关闭 WebSocket 连接并唤醒所有等待中的消息。
// 该方法是幂等的，多次调用只会执行一次清理。
func (c *AgentWSClient) close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	close(c.send)
	for id, replyCh := range c.pending {
		delete(c.pending, id)
		close(replyCh)
	}
	c.mu.Unlock()
	_ = c.conn.Close()
	c.hub.removeClient(c.userEmail, c)
	log.Printf("[云端WS] 已断开 user=%s", c.userEmail)
}

// newWSMessageID 生成云端 WebSocket 消息 ID。
func newWSMessageID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("msg_%d", time.Now().UnixNano())
	}
	return "msg_" + hex.EncodeToString(buf)
}
