// Package taskrunner 负责管理 Go 版本本地任务启动、停止和运行锁。
package taskrunner

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"goodhr5/local-agent-go/internal/browser"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/localdb"
)

// BrowserWorker 表示任务运行器需要的浏览器 Worker 能力。
type BrowserWorker interface {
	Start(ctx context.Context) (browser.WorkerStatus, error)
	Call(ctx context.Context, path string, payload any) (map[string]any, error)
}

// Runner 是本地任务运行器。
type Runner struct {
	db      *localdb.DB
	worker  BrowserWorker
	mu      sync.Mutex
	running map[string]struct{}
}

// StartOptions 表示本地任务启动参数。
type StartOptions struct {
	CloudAPIBase string
	Token        string
}

// New 创建本地任务运行器。
// db 为本地 SQLite 数据库，worker 为浏览器 Worker 管理器。
func New(db *localdb.DB, worker BrowserWorker) *Runner {
	return &Runner{db: db, worker: worker, running: map[string]struct{}{}}
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
	scanResult, err := r.scanOnce(ctx, task, platformConfig)
	if err != nil {
		r.failStart(taskID, "本地任务扫描失败："+err.Error())
		return nil, err
	}
	updated, _ = r.db.UpdateTaskStatus(taskID, "completed")
	r.clear(taskID)
	return map[string]any{
		"task":            updated,
		"subscription":    subscription,
		"platform_config": platformConfig,
		"scan":            scanResult,
		"running":         false,
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

// scanOnce 执行一轮候选人扫描并保存到本地数据库。
// ctx 为请求上下文，task 为任务记录，platformConfig 为云端平台配置。
func (r *Runner) scanOnce(ctx context.Context, task localdb.Task, platformConfig cloudapi.PlatformConfig) (map[string]any, error) {
	if r.worker == nil {
		return nil, fmt.Errorf("浏览器 Worker 未配置")
	}
	if _, err := r.worker.Start(ctx); err != nil {
		return nil, err
	}
	if _, err := r.worker.Call(ctx, "/api/v1/browser/start", map[string]any{
		"humanize": true,
	}); err != nil {
		return nil, err
	}
	entryURL := platformEntryURL(platformConfig)
	if entryURL == "" {
		return nil, fmt.Errorf("云端平台配置缺少入口页面地址")
	}
	if _, err := r.worker.Call(ctx, "/api/v1/page/open", map[string]any{"url": entryURL}); err != nil {
		return nil, err
	}
	result, err := r.worker.Call(ctx, "/api/v1/boss/candidates/extract", map[string]any{
		"platform_config": platformConfig,
		"max_items":       30,
	})
	if err != nil {
		return nil, err
	}
	candidates := mapList(workerData(result, "candidates"))
	for _, candidate := range candidates {
		if _, err := r.db.SaveCandidate(task.ID, candidate); err != nil {
			return nil, err
		}
	}
	if len(candidates) > 0 {
		_, _ = r.db.IncrementTaskCounts(task.ID, len(candidates), 0, 0, 0)
		_, _ = r.db.AddTaskLog(task.ID, "info", fmt.Sprintf("已提取并保存 %d 个可见候选人", len(candidates)))
	} else {
		_, _ = r.db.AddTaskLog(task.ID, "warning", "当前页面未提取到可见候选人，请确认账号已登录且页面在推荐列表")
	}
	return map[string]any{"candidates_count": len(candidates), "entry_url": entryURL}, nil
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

// platformEntryURL 读取平台推荐页入口。
// platformConfig 为云端平台配置。
func platformEntryURL(platformConfig cloudapi.PlatformConfig) string {
	if url := stringFromMap(platformConfig, "url"); url != "" {
		return url
	}
	pages, ok := platformConfig["pages"].([]any)
	if !ok || len(pages) == 0 {
		return ""
	}
	first, ok := pages[0].(map[string]any)
	if !ok {
		return ""
	}
	return stringFromMap(first, "url")
}

// workerData 从 Worker 统一响应中读取 data 字段。
// result 为 Worker 返回体，key 为 data 内字段名。
func workerData(result map[string]any, key string) any {
	if result == nil {
		return nil
	}
	data, _ := result["data"].(map[string]any)
	if data == nil {
		return result[key]
	}
	return data[key]
}

// mapList 将任意值转换为 map 列表。
// value 为原始值。
func mapList(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return []map[string]any{}
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if candidate, ok := item.(map[string]any); ok {
			result = append(result, candidate)
		}
	}
	return result
}

// stringFromMap 从 map 中读取字符串。
// item 为原始字典，key 为字段名。
func stringFromMap(item map[string]any, key string) string {
	if item == nil {
		return ""
	}
	value, _ := item[key].(string)
	return strings.TrimSpace(value)
}
