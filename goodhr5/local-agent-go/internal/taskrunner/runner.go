// Package taskrunner 负责管理 Go 版本本地任务启动、停止和运行锁。
package taskrunner

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/localdb"
)

// Runner 是本地任务运行器。
type Runner struct {
	db      *localdb.DB
	mu      sync.Mutex
	running map[string]struct{}
}

// StartOptions 表示本地任务启动参数。
type StartOptions struct {
	CloudAPIBase string
	Token        string
}

// New 创建本地任务运行器。
// db 为本地 SQLite 数据库。
func New(db *localdb.DB) *Runner {
	return &Runner{db: db, running: map[string]struct{}{}}
}

// Start 启动本地任务运行器。
// ctx 为请求上下文，taskID 为任务 ID，options 为启动参数。
func (r *Runner) Start(ctx context.Context, taskID string, options StartOptions) (map[string]any, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("任务 ID 不能为空")
	}
	r.mu.Lock()
	if _, ok := r.running[taskID]; ok {
		r.mu.Unlock()
		return nil, fmt.Errorf("任务正在运行")
	}
	r.running[taskID] = struct{}{}
	r.mu.Unlock()

	task, err := r.db.GetTask(taskID)
	if err != nil {
		r.clear(taskID)
		return nil, err
	}
	client := cloudapi.New(options.CloudAPIBase)
	subscription, err := client.FetchSubscription(ctx, options.Token)
	if err != nil {
		r.failStart(taskID, "会员校验失败："+err.Error())
		return nil, err
	}
	if !boolFromMap(subscription, "active") {
		msg := "会员已到期，请先订阅后再开始任务"
		r.failStart(taskID, msg)
		return nil, fmt.Errorf("%s", msg)
	}
	platformID := strings.ToLower(strings.TrimSpace(task.PlatformID))
	if platformID == "" {
		platformID = "boss"
	}
	platformConfig, err := client.FetchPlatformConfig(ctx, platformID)
	if err != nil {
		r.failStart(taskID, "读取云端平台配置失败："+err.Error())
		return nil, err
	}
	if len(platformConfig) == 0 {
		msg := "云端平台配置为空，任务无法启动"
		r.failStart(taskID, msg)
		return nil, fmt.Errorf("%s", msg)
	}
	if _, err := r.db.AddTaskLog(taskID, "info", "已从云端读取平台配置：platform="+platformID); err != nil {
		r.clear(taskID)
		return nil, err
	}
	updated, err := r.db.UpdateTaskStatus(taskID, "running")
	if err != nil {
		r.clear(taskID)
		return nil, err
	}
	_, _ = r.db.AddTaskLog(taskID, "info", "本地任务运行器已启动")
	return map[string]any{
		"task":            updated,
		"subscription":    subscription,
		"platform_config": platformConfig,
		"running":         true,
	}, nil
}

// Stop 停止本地任务运行器。
// taskID 为任务 ID。
func (r *Runner) Stop(taskID string) (map[string]any, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("任务 ID 不能为空")
	}
	r.clear(taskID)
	task, err := r.db.UpdateTaskStatus(taskID, "stopped")
	if err != nil {
		return nil, err
	}
	_, _ = r.db.AddTaskLog(taskID, "info", "本地任务已停止")
	return map[string]any{"task": task, "running": false}, nil
}

// IsRunning 判断任务是否正在运行。
// taskID 为任务 ID。
func (r *Runner) IsRunning(taskID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.running[strings.TrimSpace(taskID)]
	return ok
}

// failStart 记录启动失败日志并清理运行锁。
// taskID 为任务 ID，msg 为失败原因。
func (r *Runner) failStart(taskID string, msg string) {
	_, _ = r.db.AddTaskLog(taskID, "error", msg)
	_, _ = r.db.UpdateTaskStatus(taskID, "failed")
	r.clear(taskID)
}

// clear 清理任务运行锁。
// taskID 为任务 ID。
func (r *Runner) clear(taskID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.running, taskID)
}

// boolFromMap 从 map 中读取布尔值。
// item 为原始字典，key 为字段名。
func boolFromMap(item map[string]any, key string) bool {
	if item == nil {
		return false
	}
	if value, ok := item[key].(bool); ok {
		return value
	}
	return false
}
