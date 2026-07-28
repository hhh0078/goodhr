// Package runtime 检查并启动 Node、TypeScript Worker 和 CloakBrowser 所需运行组件。
package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	browserprocess "goodhr5/local-agent-go-new/internal/browser/process"
)

// Manager 负责运行组件状态检查和 Worker 生命周期。
type Manager struct {
	nodePath  string
	worker    *browserprocess.Manager
	entryPath string
}

// New 创建运行组件管理器。
func New(nodePath string, entryPath string, worker *browserprocess.Manager) *Manager {
	return &Manager{nodePath: nodePath, entryPath: entryPath, worker: worker}
}

// CheckNode 检查 Node.js 二进制是否存在。
func (m *Manager) CheckNode() error {
	if _, err := exec.LookPath(m.nodePath); err != nil {
		return fmt.Errorf("Node.js 暂时没找到：%w", err)
	}
	return nil
}

// CheckWorkerBuild 检查编译后的 TypeScript Worker 入口。
func (m *Manager) CheckWorkerBuild() error {
	if _, err := os.Stat(m.entryPath); err != nil {
		return fmt.Errorf("Worker 还没编译好：%w", err)
	}
	return nil
}

// EnsureWorker 检查 Node 和入口后启动 Worker。
func (m *Manager) EnsureWorker(ctx context.Context) error {
	if err := m.CheckNode(); err != nil {
		return err
	}
	if err := m.CheckWorkerBuild(); err != nil {
		return err
	}
	return m.worker.Start(ctx)
}

// StopWorker 停止 Worker 子进程。
func (m *Manager) StopWorker() error {
	return m.worker.Stop()
}
