// Package taskrunner 负责管理 Go 版本本地任务启动、停止和运行锁。
package taskrunner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"goodhr5/local-agent-go/internal/browser"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/localai"
	"goodhr5/local-agent-go/internal/localdb"
)

const defaultScanRounds = 3

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
	EnableGreet  bool
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
	scanResult, err := r.scanOnce(ctx, task, platformConfig, options)
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
func (r *Runner) scanOnce(ctx context.Context, task localdb.Task, platformConfig cloudapi.PlatformConfig, options StartOptions) (map[string]any, error) {
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
	seen := map[string]struct{}{}
	totalSaved := 0
	totalSkipped := 0
	totalGreeted := 0
	totalFailed := 0
	for round := 1; round <= defaultScanRounds; round++ {
		result, err := r.worker.Call(ctx, "/api/v1/boss/candidates/extract", map[string]any{
			"platform_config": platformConfig,
			"max_items":       30,
		})
		if err != nil {
			return nil, err
		}
		candidates := freshCandidates(mapList(workerData(result, "candidates")), seen)
		if len(candidates) == 0 {
			_, _ = r.db.AddTaskLog(task.ID, "info", fmt.Sprintf("第 %d 轮未发现新候选人", round))
			break
		}
		filtered, skipped := applyKeywordFilter(task, candidates)
		totalSkipped += skipped
		if taskMode(task) == "ai" && len(filtered) > 0 {
			scored, aiSkipped, err := r.scoreCandidates(ctx, task, filtered)
			if err != nil {
				return nil, err
			}
			filtered = scored
			totalSkipped += aiSkipped
		}
		if options.EnableGreet && len(filtered) > 0 {
			greeted, failed := r.greetCandidates(ctx, task, platformConfig, filtered, totalGreeted)
			totalGreeted += greeted
			totalFailed += failed
		}
		for _, candidate := range filtered {
			if _, err := r.db.SaveCandidate(task.ID, candidate); err != nil {
				return nil, err
			}
		}
		totalSaved += len(filtered)
		_, _ = r.db.AddTaskLog(task.ID, "info", fmt.Sprintf("第 %d 轮保存 %d 个新候选人", round, len(filtered)))
		if round < defaultScanRounds {
			_, _ = r.worker.Call(ctx, "/api/v1/page/scroll", map[string]any{"distance": 720})
		}
	}
	if totalSaved > 0 || totalSkipped > 0 {
		_, _ = r.db.IncrementTaskCounts(task.ID, totalSaved, totalGreeted, totalSkipped, totalFailed)
		_, _ = r.db.AddTaskLog(task.ID, "info", fmt.Sprintf("本次扫描保存 %d 个候选人，跳过 %d 个，打招呼 %d 个", totalSaved, totalSkipped, totalGreeted))
	} else {
		_, _ = r.db.AddTaskLog(task.ID, "warning", "当前页面未提取到可见候选人，请确认账号已登录且页面在推荐列表")
	}
	return map[string]any{
		"candidates_count": totalSaved,
		"skipped_count":    totalSkipped,
		"greeted_count":    totalGreeted,
		"failed_count":     totalFailed,
		"entry_url":        entryURL,
	}, nil
}

// scoreCandidates 使用本地 AI 给候选人评分。
// ctx 为请求上下文，task 为任务记录，candidates 为候选人列表。
func (r *Runner) scoreCandidates(ctx context.Context, task localdb.Task, candidates []map[string]any) ([]map[string]any, int, error) {
	config, err := r.db.GetAIConfig()
	if err != nil {
		return nil, 0, err
	}
	client := localai.New(config)
	result := make([]map[string]any, 0, len(candidates))
	skipped := 0
	for _, candidate := range candidates {
		decision, err := client.ScoreForGreet(ctx, task.PositionSnapshot, candidate)
		if err != nil {
			return nil, skipped, err
		}
		candidate["ai_greet_score"] = decision.Score
		candidate["ai_greet_reason"] = decision.Reason
		candidate["ai_greet_threshold"] = decision.Threshold
		candidate["ai_usage"] = decision.Usage
		candidate["ai_elapsed_ms"] = decision.ElapsedMS
		if !decision.ShouldGreet {
			candidate["status"] = "skipped"
			candidate["skip_reason"] = fmt.Sprintf("AI评分低于阈值：%.1f/%.1f，%s", decision.Score, decision.Threshold, decision.Reason)
			skipped++
		} else {
			candidate["status"] = "ai_passed"
		}
		result = append(result, candidate)
	}
	return result, skipped, nil
}

// greetCandidates 对通过筛选的候选人执行打招呼。
// ctx 为请求上下文，task 为任务记录，platformConfig 为平台配置，candidates 为候选人列表。
func (r *Runner) greetCandidates(ctx context.Context, task localdb.Task, platformConfig cloudapi.PlatformConfig, candidates []map[string]any, greetedBefore int) (int, int) {
	greeted := 0
	failed := 0
	for _, candidate := range candidates {
		status := stringFromMap(candidate, "status")
		if status != "passed" && status != "ai_passed" {
			continue
		}
		if task.MatchLimit > 0 && greetedBefore+greeted >= task.MatchLimit {
			candidate["status"] = "skipped"
			candidate["skip_reason"] = "已达到任务打招呼上限"
			continue
		}
		_, err := r.worker.Call(ctx, "/api/v1/boss/candidates/greet", map[string]any{
			"platform_config": platformConfig,
			"card_index":      intFromMap(candidate, "card_index"),
		})
		if err != nil {
			candidate["status"] = "failed"
			candidate["error"] = err.Error()
			failed++
			continue
		}
		candidate["status"] = "greeted"
		candidate["greeted_at"] = time.Now().UTC().Format(time.RFC3339Nano)
		greeted++
	}
	return greeted, failed
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

// freshCandidates 过滤已见过的候选人。
// candidates 为候选人列表，seen 为已见候选人 ID 集合。
func freshCandidates(candidates []map[string]any, seen map[string]struct{}) []map[string]any {
	result := []map[string]any{}
	for _, candidate := range candidates {
		id := stringFromMap(candidate, "id")
		if id == "" {
			id = stringFromMap(candidate, "candidate_name") + stringFromMap(candidate, "raw_text")
		}
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, candidate)
	}
	return result
}

// applyKeywordFilter 按任务岗位快照过滤候选人。
// task 为任务记录，candidates 为候选人列表。
func applyKeywordFilter(task localdb.Task, candidates []map[string]any) ([]map[string]any, int) {
	keywords := stringListFromMap(task.PositionSnapshot, "keywords")
	excludes := stringListFromMap(task.PositionSnapshot, "exclude_keywords")
	isAndMode := boolFromMap(task.PositionSnapshot, "is_and_mode")
	if len(keywords) == 0 && len(excludes) == 0 {
		return candidates, 0
	}
	result := []map[string]any{}
	skipped := 0
	for _, candidate := range candidates {
		text := strings.ToLower(stringFromMap(candidate, "filter_text") + " " + stringFromMap(candidate, "raw_text"))
		if matched := matchedWords(text, excludes); len(matched) > 0 {
			candidate["status"] = "skipped"
			candidate["skip_reason"] = "命中排除词：" + strings.Join(matched, "、")
			skipped++
			continue
		}
		matched := matchedWords(text, keywords)
		if len(keywords) > 0 && ((!isAndMode && len(matched) == 0) || (isAndMode && len(matched) < len(keywords))) {
			candidate["status"] = "skipped"
			candidate["skip_reason"] = "未命中关键词"
			skipped++
			continue
		}
		candidate["status"] = "passed"
		candidate["matched_keywords"] = matched
		result = append(result, candidate)
	}
	return result, skipped
}

// taskMode 返回任务运行模式。
// task 为任务记录。
func taskMode(task localdb.Task) string {
	mode := strings.ToLower(strings.TrimSpace(task.Mode))
	if mode == "" {
		return "ai"
	}
	return mode
}

// matchedWords 返回命中的关键词列表。
// text 为候选人文本，words 为关键词列表。
func matchedWords(text string, words []string) []string {
	result := []string{}
	for _, word := range words {
		safeWord := strings.ToLower(strings.TrimSpace(word))
		if safeWord != "" && strings.Contains(text, safeWord) {
			result = append(result, word)
		}
	}
	return result
}

// stringListFromMap 从 map 中读取字符串列表。
// item 为原始字典，key 为字段名。
func stringListFromMap(item map[string]any, key string) []string {
	if item == nil {
		return []string{}
	}
	switch value := item[key].(type) {
	case []string:
		return value
	case []any:
		result := []string{}
		for _, raw := range value {
			if text, ok := raw.(string); ok && strings.TrimSpace(text) != "" {
				result = append(result, strings.TrimSpace(text))
			}
		}
		return result
	default:
		return []string{}
	}
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

// intFromMap 从 map 中读取整数。
// item 为原始字典，key 为字段名。
func intFromMap(item map[string]any, key string) int {
	if item == nil {
		return 0
	}
	switch value := item[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	case json.Number:
		parsed, _ := value.Int64()
		return int(parsed)
	default:
		return 0
	}
}
