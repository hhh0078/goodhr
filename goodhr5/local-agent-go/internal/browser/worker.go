// Package browser 负责管理 Node Browser Worker 和浏览器 API 转发。
package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"goodhr5/local-agent-go/internal/runtime"
)

// WorkerStatus 表示 Node Browser Worker 运行状态。
type WorkerStatus struct {
	Running bool   `json:"running"`
	PID     int    `json:"pid,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
}

// WorkerManager 管理 Node Browser Worker 进程。
type WorkerManager struct {
	runtime *runtime.Manager
	mu      sync.Mutex
	cmd     *exec.Cmd
	done    chan error
	baseURL string
}

// NewWorkerManager 创建 Node Worker 管理器。
// runtimeManager 为运行组件管理器。
func NewWorkerManager(runtimeManager *runtime.Manager) *WorkerManager {
	return &WorkerManager{runtime: runtimeManager, baseURL: "http://127.0.0.1:9101"}
}

// Start 启动 Node Browser Worker。
// 如果 Worker 已经运行，则直接返回当前状态。
func (m *WorkerManager) Start(ctx context.Context) (WorkerStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.isRunningLocked() {
		return m.statusLocked(), nil
	}
	status, err := m.runtime.Ensure()
	if err != nil {
		return WorkerStatus{}, err
	}
	cmd := exec.CommandContext(ctx, status.NodePath, status.WorkerEntry)
	cmd.Env = append(os.Environ(),
		"GOODHR_WORKER_ADDR=127.0.0.1:9101",
		"GOODHR_CLOAKBROWSER_PATH="+status.CloakBrowserPath,
		"CLOAKBROWSER_BINARY_PATH="+status.CloakBrowserPath,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return WorkerStatus{}, fmt.Errorf("启动 Node Browser Worker 失败：%w", err)
	}
	m.cmd = cmd
	m.done = make(chan error, 1)
	go func() {
		m.done <- cmd.Wait()
	}()
	if err := m.waitForReadyLocked(ctx, 8*time.Second); err != nil {
		_ = cmd.Process.Kill()
		m.cmd = nil
		m.done = nil
		return WorkerStatus{}, err
	}
	return m.statusLocked(), nil
}

// Stop 停止 Node Browser Worker。
// 如果 Worker 未运行，则返回当前状态。
func (m *WorkerManager) Stop() WorkerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.isRunningLocked() {
		m.cmd = nil
		return m.statusLocked()
	}
	if m.cmd.Process != nil {
		_ = m.cmd.Process.Signal(os.Interrupt)
		select {
		case <-m.done:
		case <-time.After(3 * time.Second):
			_ = m.cmd.Process.Kill()
			select {
			case <-m.done:
			case <-time.After(2 * time.Second):
			}
		}
	}
	m.cmd = nil
	m.done = nil
	return m.statusLocked()
}

// Status 返回 Node Browser Worker 当前状态。
// 返回值用于健康检查和前端展示。
func (m *WorkerManager) Status() WorkerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked()
}

// Call 调用 Node Worker API。
// path 为 Worker 路由，payload 为请求体，返回 Worker 原始 JSON。
func (m *WorkerManager) Call(ctx context.Context, path string, payload any) (map[string]any, error) {
	return m.call(ctx, http.MethodPost, path, payload)
}

// CallGet 调用 Node Worker GET API。
// path 为 Worker 路由，返回 Worker 原始 JSON。
func (m *WorkerManager) CallGet(ctx context.Context, path string) (map[string]any, error) {
	return m.call(ctx, http.MethodGet, path, nil)
}

// call 调用 Node Worker API。
// method 为 HTTP 方法，path 为 Worker 路由，payload 为请求体。
func (m *WorkerManager) call(ctx context.Context, method string, path string, payload any) (map[string]any, error) {
	if path == "" {
		return nil, fmt.Errorf("Worker 路径不能为空")
	}
	var reader *bytes.Reader
	if method == http.MethodGet {
		reader = bytes.NewReader(nil)
	} else {
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("请求参数编码失败：%w", err)
		}
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, m.baseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("创建 Worker 请求失败：%w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, normalizeCallError(err)
	}
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析 Worker 返回失败：%w", err)
	}
	if resp.StatusCode >= 400 {
		return result, fmt.Errorf("Worker 请求失败")
	}
	return result, nil
}

// normalizeCallError 将 Worker 网络错误转换为中文提示。
// err 为原始请求错误。
func normalizeCallError(err error) error {
	if err == nil {
		return nil
	}
	text := err.Error()
	if strings.Contains(text, "connection refused") || strings.Contains(text, "connect:") {
		return fmt.Errorf("Node Browser Worker 未启动")
	}
	return fmt.Errorf("调用 Node Browser Worker 失败")
}

// isRunningLocked 判断 Worker 进程是否还在运行。
// 调用前必须持有锁。
func (m *WorkerManager) isRunningLocked() bool {
	if m.cmd == nil || m.cmd.Process == nil {
		return false
	}
	return m.cmd.ProcessState == nil || !m.cmd.ProcessState.Exited()
}

// statusLocked 返回当前 Worker 状态。
// 调用前必须持有锁。
func (m *WorkerManager) statusLocked() WorkerStatus {
	status := WorkerStatus{Running: m.isRunningLocked(), BaseURL: m.baseURL}
	if status.Running && m.cmd != nil && m.cmd.Process != nil {
		status.PID = m.cmd.Process.Pid
	}
	return status
}

// waitForReadyLocked 等待 Worker HTTP 服务可访问。
// 调用前必须持有锁，timeout 为最大等待时间。
func (m *WorkerManager) waitForReadyLocked(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := http.Client{Timeout: 500 * time.Millisecond}
	for time.Now().Before(deadline) {
		select {
		case err := <-m.done:
			return fmt.Errorf("Node Browser Worker 已退出：%w", err)
		default:
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.baseURL+"/health", nil)
		if err == nil {
			resp, err := client.Do(req)
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 500 {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("等待 Node Browser Worker 启动被取消")
		case <-time.After(120 * time.Millisecond):
		}
	}
	return fmt.Errorf("Node Browser Worker 启动超时")
}
