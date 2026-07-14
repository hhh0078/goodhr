// Package browser 文件作用：按职责承载实验性 Go 浏览器控制库的拆分实现。
package browser

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/url"
	"strings"
	"sync"
)

type cdpClient struct {
	conn      *websocketConn
	mu        sync.Mutex
	nextID    int
	pending   map[int]chan cdpMessage
	closed    chan struct{}
	closeOnce sync.Once
}

type cdpMessage struct {
	ID     int            `json:"id,omitempty"`
	Method string         `json:"method,omitempty"`
	Params map[string]any `json:"params,omitempty"`
	Result map[string]any `json:"result,omitempty"`
	Error  *cdpError      `json:"error,omitempty"`
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type websocketConn struct {
	conn net.Conn
	mu   sync.Mutex
}

// Call 调用一个 CDP 方法并等待对应响应。
func (c *cdpClient) Call(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan cdpMessage, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	payload := map[string]any{"id": id, "method": method}
	if params != nil {
		payload["params"] = params
	}
	raw, _ := json.Marshal(payload)
	if err := c.conn.WriteText(raw); err != nil {
		c.removePending(id)
		return nil, err
	}
	select {
	case msg := <-ch:
		if msg.Error != nil {
			return nil, fmt.Errorf("%s", msg.Error.Message)
		}
		if msg.Result == nil {
			msg.Result = map[string]any{}
		}
		return msg.Result, nil
	case <-ctx.Done():
		c.removePending(id)
		return nil, ctx.Err()
	case <-c.closed:
		return nil, io.ErrClosedPipe
	}
}

func (c *GoController) evalLocked(ctx context.Context, expression string) (any, error) {
	page, err := c.ensurePageLocked(ctx)
	if err != nil {
		return nil, err
	}
	result, err := page.client.Call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  true,
	})
	if err != nil {
		return nil, err
	}
	if details, ok := result["exceptionDetails"]; ok {
		return nil, fmt.Errorf("页面执行失败：%v", details)
	}
	remote, _ := result["result"].(map[string]any)
	if value, ok := remote["value"]; ok {
		return value, nil
	}
	return nil, nil
}

func dialCDP(ctx context.Context, wsURL string) (*cdpClient, error) {
	conn, err := dialWebSocket(ctx, wsURL)
	if err != nil {
		return nil, err
	}
	client := &cdpClient{conn: conn, pending: make(map[int]chan cdpMessage), closed: make(chan struct{})}
	go client.readLoop()
	return client, nil
}

func (c *cdpClient) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return c.conn.Close()
}

func (c *cdpClient) removePending(id int) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *cdpClient) readLoop() {
	defer c.closeOnce.Do(func() { close(c.closed) })
	for {
		raw, err := c.conn.ReadText()
		if err != nil {
			return
		}
		var msg cdpMessage
		if err := json.Unmarshal(raw, &msg); err != nil || msg.ID == 0 {
			continue
		}
		c.mu.Lock()
		ch := c.pending[msg.ID]
		delete(c.pending, msg.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- msg
		}
	}
}

func dialWebSocket(ctx context.Context, rawURL string) (*websocketConn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		host += ":80"
	}
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, err
	}
	keyBytes := make([]byte, 16)
	_, _ = rand.Read(keyBytes)
	key := base64.StdEncoding.EncodeToString(keyBytes)
	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", path, u.Host, key)
	if _, err := conn.Write([]byte(req)); err != nil {
		_ = conn.Close()
		return nil, err
	}
	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if !strings.Contains(status, " 101 ") {
		_ = conn.Close()
		return nil, fmt.Errorf("WebSocket 握手失败：%s", strings.TrimSpace(status))
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		if strings.TrimSpace(line) == "" {
			break
		}
	}
	return &websocketConn{conn: conn}, nil
}

func (c *websocketConn) WriteText(payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var frame bytes.Buffer
	frame.WriteByte(0x81)
	length := len(payload)
	switch {
	case length < 126:
		frame.WriteByte(byte(length) | 0x80)
	case length <= math.MaxUint16:
		frame.WriteByte(126 | 0x80)
		_ = binary.Write(&frame, binary.BigEndian, uint16(length))
	default:
		frame.WriteByte(127 | 0x80)
		_ = binary.Write(&frame, binary.BigEndian, uint64(length))
	}
	mask := make([]byte, 4)
	_, _ = rand.Read(mask)
	frame.Write(mask)
	for i, b := range payload {
		frame.WriteByte(b ^ mask[i%4])
	}
	_, err := c.conn.Write(frame.Bytes())
	return err
}

func (c *websocketConn) ReadText() ([]byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(c.conn, header); err != nil {
		return nil, err
	}
	opcode := header[0] & 0x0f
	masked := header[1]&0x80 != 0
	length := uint64(header[1] & 0x7f)
	if length == 126 {
		var v uint16
		if err := binary.Read(c.conn, binary.BigEndian, &v); err != nil {
			return nil, err
		}
		length = uint64(v)
	} else if length == 127 {
		if err := binary.Read(c.conn, binary.BigEndian, &length); err != nil {
			return nil, err
		}
	}
	var mask []byte
	if masked {
		mask = make([]byte, 4)
		if _, err := io.ReadFull(c.conn, mask); err != nil {
			return nil, err
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(c.conn, payload); err != nil {
		return nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	switch opcode {
	case 0x1, 0x2:
		return payload, nil
	case 0x8:
		return nil, io.EOF
	case 0x9:
		_ = c.writeControl(0xA, payload)
		return c.ReadText()
	default:
		return c.ReadText()
	}
}

func (c *websocketConn) writeControl(opcode byte, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	frame := []byte{0x80 | opcode, byte(len(payload)) | 0x80}
	mask := make([]byte, 4)
	_, _ = rand.Read(mask)
	frame = append(frame, mask...)
	for i, b := range payload {
		frame = append(frame, b^mask[i%4])
	}
	_, err := c.conn.Write(frame)
	return err
}

func (c *websocketConn) Close() error {
	return c.conn.Close()
}
