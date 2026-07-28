// Package process 管理 TypeScript Browser Worker 子进程、日志和健康状态。
package process

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// HealthChecker 定义 Worker 启动后的健康检查能力。
type HealthChecker interface {
	Health(ctx context.Context) error
}

// Manager 管理当前唯一 Worker 子进程。
type Manager struct {
	mu          sync.Mutex
	nodePath    string
	entryPath   string
	port        int
	logPath     string
	health      HealthChecker
	environment []string
	command     *exec.Cmd
	logFile     *os.File
	done        chan struct{}
}

// SetEnvironment 设置 Worker 子进程需要的额外环境变量。
func (m *Manager) SetEnvironment(values ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.environment = append([]string(nil), values...)
}

// SetExecutable 更新后续启动 Worker 使用的 Node.js 可执行文件。
func (m *Manager) SetExecutable(nodePath string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if nodePath != "" {
		m.nodePath = nodePath
	}
}

// New 创建 Worker 进程管理器。
func New(nodePath string, entryPath string, port int, logPath string, health HealthChecker) *Manager {
	return &Manager{
		nodePath:  nodePath,
		entryPath: entryPath,
		port:      port,
		logPath:   logPath,
		health:    health,
	}
}

// Start 启动 Worker，已经健康时直接复用。
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.health != nil {
		checkCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
		err := m.health.Health(checkCtx)
		cancel()
		if err == nil {
			return nil
		}
	}
	if _, err := os.Stat(m.entryPath); err != nil {
		return fmt.Errorf("Worker 入口不存在，请先编译 TypeScript：%w", err)
	}
	file, err := os.OpenFile(m.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("打开 Worker 日志失败：%w", err)
	}
	command := exec.Command(m.nodePath, m.entryPath)
	command.Env = append(os.Environ(), "GOODHR_WORKER_PORT="+strconv.Itoa(m.port))
	command.Env = append(command.Env, m.environment...)
	command.Stdout = io.MultiWriter(os.Stdout, file)
	command.Stderr = io.MultiWriter(os.Stderr, file)
	if err := command.Start(); err != nil {
		file.Close()
		return fmt.Errorf("启动 Browser Worker 失败：%w", err)
	}
	m.command = command
	m.logFile = file
	done := make(chan struct{})
	m.done = done
	go m.wait(command, file, done)
	if err := m.waitHealthy(ctx); err != nil {
		_ = command.Process.Kill()
		return err
	}
	return nil
}

// Stop 优雅停止 Worker 子进程。
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.command == nil || m.command.Process == nil {
		return nil
	}
	process := m.command.Process
	done := m.done
	_ = process.Signal(os.Interrupt)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = process.Kill()
		<-done
	}
	m.command = nil
	m.done = nil
	if m.logFile != nil {
		_ = m.logFile.Close()
		m.logFile = nil
	}
	return nil
}

// waitHealthy 等待 Worker 健康检查通过。
func (m *Manager) waitHealthy(ctx context.Context) error {
	if m.health == nil {
		return nil
	}
	deadline := time.NewTimer(12 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		checkCtx, cancel := context.WithTimeout(ctx, time.Second)
		err := m.health.Health(checkCtx)
		cancel()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("Browser Worker 启动超时")
		case <-ticker.C:
		}
	}
}

// wait 回收 Worker 进程并关闭对应日志。
func (m *Manager) wait(command *exec.Cmd, file *os.File, done chan struct{}) {
	_ = command.Wait()
	_ = file.Close()
	close(done)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.command == command {
		m.command = nil
		m.logFile = nil
		m.done = nil
	}
}
