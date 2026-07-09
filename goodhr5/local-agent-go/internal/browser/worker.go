// Package browser 负责管理 Node Browser Worker 和浏览器 API 转发。
package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
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
	Managed bool   `json:"managed"`
}

// WorkerManager 管理 Node Browser Worker 进程。
type WorkerManager struct {
	runtime *runtime.Manager
	mu      sync.Mutex
	cmd     *exec.Cmd
	done    chan error
	logFile *os.File
	logPath string
	baseURL string
	// agentBaseURL 是 Go 本地程序地址，供 Node Worker 回调本地能力。
	agentBaseURL string
	// attachedPID 记录复用到的旧 Worker 进程 ID。
	attachedPID int
}

// NewWorkerManager 创建 Node Worker 管理器。
// runtimeManager 为运行组件管理器。
func NewWorkerManager(runtimeManager *runtime.Manager) *WorkerManager {
	return &WorkerManager{runtime: runtimeManager, baseURL: "http://127.0.0.1:9101"}
}

// SetAgentBaseURL 设置 Go 本地程序回调地址。
// baseURL 为本地程序 HTTP 基础地址。
func (m *WorkerManager) SetAgentBaseURL(baseURL string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.agentBaseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

// Start 启动 Node Browser Worker。
// 如果 Worker 已经运行，则直接返回当前状态。
func (m *WorkerManager) Start(ctx context.Context) (WorkerStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.isRunningLocked() {
		log.Printf("[Node Worker] 复用当前管理的 Worker base_url=%s", m.baseURL)
		return m.statusLocked(), nil
	}
	if status, ok := m.probeExistingWorkerLocked(ctx); ok {
		log.Printf("[Node Worker] 复用已存在 Worker base_url=%s pid=%d", status.BaseURL, status.PID)
		return status, nil
	}
	if err := m.selectAvailableBaseURLLocked(ctx); err != nil {
		return WorkerStatus{}, err
	}
	log.Printf("[Node Worker] 准备启动 Worker base_url=%s", m.baseURL)
	if m.runtime == nil {
		return WorkerStatus{}, fmt.Errorf("本地程序缺少运行组件管理器")
	}
	status, err := m.runtime.Ensure()
	if err != nil {
		return WorkerStatus{}, err
	}
	if !status.WorkerInstalled {
		return WorkerStatus{}, fmt.Errorf("本地程序缺少浏览器控制组件，请重新安装本地程序")
	}
	cmd := exec.Command(status.NodePath, status.WorkerEntry)
	hideCommandWindow(cmd)
	cmd.Env = append(os.Environ(),
		"GOODHR_WORKER_ADDR="+workerAddrFromBaseURL(m.baseURL),
		"GOODHR_CLOAKBROWSER_PATH="+status.CloakBrowserPath,
		"CLOAKBROWSER_BINARY_PATH="+status.CloakBrowserPath,
	)
	if m.agentBaseURL != "" {
		cmd.Env = append(cmd.Env, "GOODHR_AGENT_BASE_URL="+m.agentBaseURL)
	}
	logFile, logPath, err := openWorkerLog(status.RuntimeDir)
	if err != nil {
		return WorkerStatus{}, err
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return WorkerStatus{}, fmt.Errorf("启动 Node Browser Worker 失败：%w", err)
	}
	log.Printf("[Node Worker] 进程已启动 pid=%d node=%s entry=%s log=%s", cmd.Process.Pid, status.NodePath, status.WorkerEntry, logPath)
	m.cmd = cmd
	m.attachedPID = 0
	m.logFile = logFile
	m.logPath = logPath
	m.done = make(chan error, 1)
	go func() {
		m.done <- cmd.Wait()
	}()
	if err := m.waitForReadyLocked(ctx, 8*time.Second); err != nil {
		log.Printf("[Node Worker] 等待就绪失败 pid=%d err=%v", cmd.Process.Pid, err)
		_ = killProcessTree(cmd.Process.Pid)
		m.cmd = nil
		m.done = nil
		m.closeLogLocked()
		return WorkerStatus{}, err
	}
	log.Printf("[Node Worker] 已就绪 base_url=%s pid=%d", m.baseURL, cmd.Process.Pid)
	return m.statusLocked(), nil
}

// openWorkerLog 打开 Node Worker 日志文件。
// runtimeDir 为运行组件目录。
func openWorkerLog(runtimeDir string) (*os.File, string, error) {
	logDir := filepath.Join(runtimeDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, "", fmt.Errorf("创建 Worker 日志目录失败：%w", err)
	}
	logPath := filepath.Join(logDir, "browser-worker.log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, "", fmt.Errorf("打开 Worker 日志失败：%w", err)
	}
	_, _ = fmt.Fprintf(file, "\n[%s] 启动 Node Browser Worker\n", time.Now().Format(time.RFC3339))
	return file, logPath, nil
}

// Stop 停止 Node Browser Worker。
// 如果 Worker 未运行，则返回当前状态。
func (m *WorkerManager) Stop() WorkerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.isRunningLocked() {
		m.cmd = nil
		m.attachedPID = 0
		return m.statusLocked()
	}
	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Signal(os.Interrupt)
		select {
		case <-m.done:
		case <-time.After(3 * time.Second):
			_ = killProcessTree(m.cmd.Process.Pid)
			select {
			case <-m.done:
			case <-time.After(2 * time.Second):
			}
		}
	} else if m.attachedPID > 0 {
		_ = killProcessTree(m.attachedPID)
		time.Sleep(200 * time.Millisecond)
	}
	m.cmd = nil
	m.done = nil
	m.attachedPID = 0
	m.closeLogLocked()
	return m.statusLocked()
}

// closeLogLocked 关闭 Worker 日志文件。
// 调用前必须持有锁。
func (m *WorkerManager) closeLogLocked() {
	if m.logFile != nil {
		_ = m.logFile.Close()
	}
	m.logFile = nil
	m.logPath = ""
}

// killProcessTree 强制结束 Worker 进程树。
// pid 为 Worker 主进程 ID。
func killProcessTree(pid int) error {
	if pid <= 0 {
		return nil
	}
	if goruntime.GOOS == "windows" {
		return exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
	}
	children := childPIDs(pid)
	for _, child := range children {
		_ = killProcessTree(child)
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

// childPIDs 读取当前进程的子进程 ID。
// pid 为父进程 ID。
func childPIDs(pid int) []int {
	if pid <= 0 || goruntime.GOOS == "windows" {
		return []int{}
	}
	out, err := exec.Command("pgrep", "-P", strconv.Itoa(pid)).Output()
	if err != nil {
		return []int{}
	}
	result := []int{}
	for _, item := range strings.Fields(string(out)) {
		if parsed, err := strconv.Atoi(item); err == nil {
			result = append(result, parsed)
		}
	}
	return result
}

// Status 返回 Node Browser Worker 当前状态。
// 返回值用于健康检查和前端展示。
func (m *WorkerManager) Status() WorkerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.isRunningLocked() {
		if status, ok := m.probeExistingWorkerLocked(context.Background()); ok {
			return status
		}
	}
	return m.statusLocked()
}

// Call 调用 Node Worker API。
// path 为 Worker 路由，payload 为请求体，返回 Worker 原始 JSON。
func (m *WorkerManager) Call(ctx context.Context, path string, payload any) (map[string]any, error) {
	result, err := m.call(ctx, http.MethodPost, path, payload)
	if err == nil || !isRestartableCallError(err) {
		return result, err
	}
	if _, startErr := m.Restart(ctx); startErr != nil {
		return nil, startErr
	}
	return m.call(ctx, http.MethodPost, path, payload)
}

// CallGet 调用 Node Worker GET API。
// path 为 Worker 路由，返回 Worker 原始 JSON。
func (m *WorkerManager) CallGet(ctx context.Context, path string) (map[string]any, error) {
	result, err := m.call(ctx, http.MethodGet, path, nil)
	if err == nil || !isRestartableCallError(err) {
		return result, err
	}
	if _, startErr := m.Restart(ctx); startErr != nil {
		return nil, startErr
	}
	return m.call(ctx, http.MethodGet, path, nil)
}

// Restart 重启 Node Browser Worker。
// ctx 为请求上下文，返回重启后的 Worker 状态。
func (m *WorkerManager) Restart(ctx context.Context) (WorkerStatus, error) {
	m.Stop()
	return m.Start(ctx)
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
		msg := "Worker 请求失败"
		if value, ok := result["msg"].(string); ok && strings.TrimSpace(value) != "" {
			msg = value
		}
		return result, fmt.Errorf("%s", msg)
	}
	return result, nil
}

// normalizeCallError 将 Worker 网络错误转换为中文提示。
// err 为原始请求错误。
func normalizeCallError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	text := err.Error()
	if strings.Contains(text, "connection refused") || strings.Contains(text, "connect:") {
		return fmt.Errorf("Node Browser Worker 未启动")
	}
	return fmt.Errorf("调用 Node Browser Worker 失败")
}

// isRestartableCallError 判断 Worker 调用错误是否适合自动重启。
// err 为调用错误。
func isRestartableCallError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	text := err.Error()
	return strings.Contains(text, "Worker 未启动") || strings.Contains(text, "调用 Node Browser Worker 失败")
}

// isRunningLocked 判断 Worker 进程是否还在运行。
// 调用前必须持有锁。
func (m *WorkerManager) isRunningLocked() bool {
	if m.cmd == nil || m.cmd.Process == nil {
		if m.attachedPID > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			if _, ok := m.probeWorker(ctx); ok {
				return true
			}
			m.attachedPID = 0
		}
		return false
	}
	select {
	case <-m.done:
		m.cmd = nil
		m.done = nil
		m.attachedPID = 0
		m.closeLogLocked()
		return false
	default:
	}
	return true
}

// statusLocked 返回当前 Worker 状态。
// 调用前必须持有锁。
func (m *WorkerManager) statusLocked() WorkerStatus {
	status := WorkerStatus{Running: m.isRunningLocked(), BaseURL: m.baseURL, Managed: m.cmd != nil && m.cmd.Process != nil}
	if status.Running && m.cmd != nil && m.cmd.Process != nil {
		status.PID = m.cmd.Process.Pid
	} else if status.Running && m.attachedPID > 0 {
		status.PID = m.attachedPID
	}
	return status
}

// probeExistingWorkerLocked 探测并复用已经存在的 GoodHR Node Worker。
// ctx 为请求上下文，返回值表示 Worker 状态和是否可复用。
func (m *WorkerManager) probeExistingWorkerLocked(ctx context.Context) (WorkerStatus, bool) {
	if health, ok := probeWorkerAt(ctx, m.baseURL); ok {
		if !m.workerHealthReusable(health) {
			log.Printf("[Node Worker] 跳过不兼容的旧 Worker base_url=%s", m.baseURL)
			return WorkerStatus{}, false
		}
		m.attachedPID = intFromAny(health["pid"])
		return WorkerStatus{Running: true, PID: m.attachedPID, BaseURL: m.baseURL, Managed: false}, true
	}
	for port := 9101; port <= 9109; port++ {
		baseURL := "http://127.0.0.1:" + strconv.Itoa(port)
		if baseURL == m.baseURL {
			continue
		}
		health, ok := probeWorkerAt(ctx, baseURL)
		if !ok {
			continue
		}
		if !m.workerHealthReusable(health) {
			log.Printf("[Node Worker] 跳过不兼容的旧 Worker base_url=%s", baseURL)
			continue
		}
		m.baseURL = baseURL
		m.attachedPID = intFromAny(health["pid"])
		return WorkerStatus{Running: true, PID: m.attachedPID, BaseURL: m.baseURL, Managed: false}, true
	}
	m.attachedPID = 0
	return WorkerStatus{}, false
}

// probeWorker 请求 Worker 健康检查接口，确认端口上运行的是 GoodHR Worker。
// ctx 为请求上下文，返回健康检查数据和是否可复用。
func (m *WorkerManager) probeWorker(ctx context.Context) (map[string]any, bool) {
	return probeWorkerAt(ctx, m.baseURL)
}

// probeWorkerAt 请求指定 Worker 健康检查接口。
// ctx 为请求上下文，baseURL 为 Worker 基础地址。
func probeWorkerAt(ctx context.Context, baseURL string) (map[string]any, bool) {
	client := http.Client{Timeout: 500 * time.Millisecond}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
	if err != nil {
		return nil, false
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, false
	}
	data, _ := body["data"].(map[string]any)
	if data["worker"] != "node" {
		return nil, false
	}
	return data, true
}

// selectAvailableBaseURLLocked 选择可用 Worker 端口。
// ctx 为请求上下文，调用前必须持有锁。
func (m *WorkerManager) selectAvailableBaseURLLocked(ctx context.Context) error {
	for port := 9101; port <= 9109; port++ {
		baseURL := "http://127.0.0.1:" + strconv.Itoa(port)
		if health, ok := probeWorkerAt(ctx, baseURL); ok {
			if !m.workerHealthReusable(health) {
				continue
			}
			m.baseURL = baseURL
			return nil
		}
		ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
		if err == nil {
			_ = ln.Close()
			m.baseURL = baseURL
			return nil
		}
	}
	return fmt.Errorf("Node Browser Worker 没有可用端口：9101-9109")
}

// workerHealthReusable 判断已有 Worker 是否兼容当前本地程序。
// health 为 Worker 健康检查数据。
func (m *WorkerManager) workerHealthReusable(health map[string]any) bool {
	if m.agentBaseURL == "" {
		return true
	}
	return boolFromAny(health["agent_notify"])
}

// workerAddrFromBaseURL 从 Worker 基础地址提取监听地址。
// baseURL 为 http://host:port 格式。
func workerAddrFromBaseURL(baseURL string) string {
	baseURL = strings.TrimPrefix(strings.TrimSpace(baseURL), "http://")
	baseURL = strings.TrimPrefix(baseURL, "https://")
	if baseURL == "" {
		return "127.0.0.1:9101"
	}
	return baseURL
}

// intFromAny 将 JSON 数字转换为 int。
// value 为任意 JSON 字段，无法转换时返回 0。
func intFromAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case float64:
		return int(v)
	case json.Number:
		parsed, _ := strconv.Atoi(v.String())
		return parsed
	default:
		return 0
	}
}

// boolFromAny 将任意值转换为布尔值。
// value 为任意 JSON 字段。
func boolFromAny(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}

// waitForReadyLocked 等待 Worker HTTP 服务可访问。
// 调用前必须持有锁，timeout 为最大等待时间。
func (m *WorkerManager) waitForReadyLocked(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := http.Client{Timeout: 500 * time.Millisecond}
	for time.Now().Before(deadline) {
		select {
		case err := <-m.done:
			return m.workerExitError(err)
		default:
		}
		if baseURL, ok := m.findReadyWorkerLocked(ctx, &client); ok {
			m.baseURL = baseURL
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("等待 Node Browser Worker 启动被取消")
		case <-time.After(120 * time.Millisecond):
		}
	}
	return fmt.Errorf("Node Browser Worker 启动超时")
}

// findReadyWorkerLocked 查找已经就绪的 GoodHR Worker。
// ctx 为请求上下文，client 为复用的 HTTP 客户端。
func (m *WorkerManager) findReadyWorkerLocked(ctx context.Context, client *http.Client) (string, bool) {
	baseURLs := []string{m.baseURL}
	for port := 9101; port <= 9109; port++ {
		baseURL := "http://127.0.0.1:" + strconv.Itoa(port)
		if baseURL != m.baseURL {
			baseURLs = append(baseURLs, baseURL)
		}
	}
	for _, baseURL := range baseURLs {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		var body map[string]any
		decodeErr := json.NewDecoder(resp.Body).Decode(&body)
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 || decodeErr != nil {
			continue
		}
		data, _ := body["data"].(map[string]any)
		if data["worker"] == "node" && m.workerHealthReusable(data) {
			return baseURL, true
		}
	}
	return "", false
}

// workerExitError 返回 Worker 启动期退出的可读错误。
// err 为 cmd.Wait 返回值，可能为空。
func (m *WorkerManager) workerExitError(err error) error {
	if m.logFile != nil {
		_ = m.logFile.Sync()
	}
	logText := recentLogTail(m.logPath, 2000)
	if err == nil {
		if logText != "" {
			return fmt.Errorf("Node Browser Worker 已正常退出，最近日志：%s", logText)
		}
		return fmt.Errorf("Node Browser Worker 已正常退出")
	}
	if logText != "" {
		return fmt.Errorf("Node Browser Worker 已退出：%v，最近日志：%s", err, logText)
	}
	return fmt.Errorf("Node Browser Worker 已退出：%v", err)
}

// recentLogTail 读取日志末尾摘要。
// path 为日志路径，limit 为最大字符数。
func recentLogTail(path string, limit int) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return ""
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return ""
	}
	if limit <= 0 {
		limit = 2000
	}
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[len(runes)-limit:])
	}
	return text
}
