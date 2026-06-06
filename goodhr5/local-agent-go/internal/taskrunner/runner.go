// Package taskrunner 负责管理 Go 版本本地任务启动、停止和运行锁。
package taskrunner

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"goodhr5/local-agent-go/internal/browser"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/localai"
	"goodhr5/local-agent-go/internal/localdb"
)

const defaultScanRounds = 3
const defaultMaxItemsPerRound = 30
const defaultScrollDistance = 720

// BrowserWorker 表示任务运行器需要的浏览器 Worker 能力。
type BrowserWorker interface {
	Start(ctx context.Context) (browser.WorkerStatus, error)
	Call(ctx context.Context, path string, payload any) (map[string]any, error)
}

// Runner 是本地任务运行器。
type Runner struct {
	db             *localdb.DB
	worker         BrowserWorker
	profilesDir    string
	downloadsDir   string
	screenshotsDir string
	mu             sync.Mutex
	running        map[string]*runState
}

// runState 保存单个运行任务的控制句柄。
type runState struct {
	cancel   context.CancelFunc
	progress Progress
}

// Progress 表示任务运行进度。
type Progress struct {
	Stage       string `json:"stage"`
	Message     string `json:"message"`
	Round       int    `json:"round"`
	TotalRounds int    `json:"total_rounds"`
	UpdatedAt   string `json:"updated_at"`
}

// StartOptions 表示本地任务启动参数。
type StartOptions struct {
	CloudAPIBase   string
	Token          string
	EnableGreet    bool
	GreetDelayMin  float64
	GreetDelayMax  float64
	GreetRetries   int
	ScanRounds     int
	MaxItems       int
	ScrollDistance int
}

// New 创建本地任务运行器。
// db 为本地 SQLite 数据库，worker 为浏览器 Worker 管理器，profilesDir、downloadsDir 和 screenshotsDir 为本机浏览器目录。
func New(db *localdb.DB, worker BrowserWorker, profilesDir string, downloadsDir string, screenshotsDir string) *Runner {
	return &Runner{db: db, worker: worker, profilesDir: profilesDir, downloadsDir: downloadsDir, screenshotsDir: screenshotsDir, running: map[string]*runState{}}
}

// Start 启动本地任务运行器。
// ctx 为请求上下文，taskID 为任务 ID，options 为启动参数。
func (r *Runner) Start(ctx context.Context, taskID string, options StartOptions) (map[string]any, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("任务 ID 不能为空")
	}
	task, err := r.db.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	if !r.setRunning(taskID, cancel) {
		cancel()
		return nil, fmt.Errorf("任务正在运行")
	}
	r.updateProgress(taskID, Progress{Stage: "starting", Message: "任务准备启动", TotalRounds: defaultScanRounds})
	updated, err := r.db.UpdateTaskStatus(taskID, "running")
	if err != nil {
		r.clear(taskID)
		cancel()
		return nil, err
	}
	_, _ = r.db.AddTaskLog(taskID, "info", "本地任务已进入后台运行")
	go r.runTask(runCtx, task, options)
	return map[string]any{"task": updated, "running": true}, nil
}

// runTask 在后台执行本地任务主流程。
// ctx 为运行上下文，task 为任务记录，options 为启动参数。
func (r *Runner) runTask(ctx context.Context, task localdb.Task, options StartOptions) {
	taskID := task.ID
	defer r.clear(taskID)
	totalRounds := scanRounds(options)
	r.updateProgress(taskID, Progress{Stage: "subscription", Message: "正在校验会员", TotalRounds: totalRounds})
	client := cloudapi.New(options.CloudAPIBase)
	subscription, err := client.FetchSubscription(ctx, options.Token)
	if err != nil {
		r.failStart(taskID, "会员校验失败："+err.Error())
		return
	}
	if !boolFromMap(subscription, "active") {
		msg := "会员已到期，请先订阅后再开始任务"
		r.failStart(taskID, msg)
		return
	}
	r.updateProgress(taskID, Progress{Stage: "platform_config", Message: "正在读取平台配置", TotalRounds: totalRounds})
	platformID := strings.ToLower(strings.TrimSpace(task.PlatformID))
	if platformID == "" {
		platformID = "boss"
	}
	platformConfig, err := client.FetchPlatformConfig(ctx, platformID)
	if err != nil {
		r.failStart(taskID, "读取云端平台配置失败："+err.Error())
		return
	}
	if len(platformConfig) == 0 {
		msg := "云端平台配置为空，任务无法启动"
		r.failStart(taskID, msg)
		return
	}
	if _, err := r.db.AddTaskLog(taskID, "info", "已从云端读取平台配置：platform="+platformID); err != nil {
		r.failStart(taskID, "写入任务日志失败："+err.Error())
		return
	}
	r.updateProgress(taskID, Progress{Stage: "running", Message: "任务已开始执行", TotalRounds: totalRounds})
	_, _ = r.db.AddTaskLog(taskID, "info", "本地任务运行器已启动")
	scanResult, err := r.scanOnce(ctx, task, platformConfig, options)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			r.updateProgress(taskID, Progress{Stage: "stopped", Message: "任务已停止", TotalRounds: totalRounds})
			_, _ = r.db.UpdateTaskStatus(taskID, "stopped")
			_, _ = r.db.AddTaskLog(taskID, "info", "本地任务收到停止信号")
			return
		}
		r.failStart(taskID, "本地任务扫描失败："+err.Error())
		return
	}
	r.updateProgress(taskID, Progress{Stage: "completed", Message: "任务已完成", Round: totalRounds, TotalRounds: totalRounds})
	_, _ = r.db.UpdateTaskStatus(taskID, "completed")
	_, _ = r.db.AddTaskLog(taskID, "info", fmt.Sprintf("后台任务已完成：%v", scanResult))
}

// Stop 停止本地任务运行器。
// taskID 为任务 ID。
func (r *Runner) Stop(taskID string) (map[string]any, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("任务 ID 不能为空")
	}
	r.cancel(taskID)
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = r.worker.Call(stopCtx, "/api/v1/browser/stop", map[string]any{})
	task, err := r.db.UpdateTaskStatus(taskID, "stopped")
	if err != nil {
		return nil, err
	}
	_, _ = r.db.AddTaskLog(taskID, "info", "本地任务已停止")
	return map[string]any{"task": task, "running": false}, nil
}

// Status 返回本地任务运行状态。
// taskID 为任务 ID。
func (r *Runner) Status(taskID string) (map[string]any, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("任务 ID 不能为空")
	}
	task, err := r.db.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	running := r.IsRunning(taskID)
	progress := r.Progress(taskID, task)
	logs, _ := r.db.ListTaskLogs(taskID, 20)
	return map[string]any{"task": task, "running": running, "progress": progress, "logs": logs}, nil
}

// Progress 返回任务当前进度。
// taskID 为任务 ID，task 为任务记录。
func (r *Runner) Progress(taskID string, task localdb.Task) Progress {
	r.mu.Lock()
	state := r.running[strings.TrimSpace(taskID)]
	r.mu.Unlock()
	if state != nil {
		return state.progress
	}
	stage := task.Status
	if stage == "" {
		stage = "unknown"
	}
	return Progress{
		Stage:       stage,
		Message:     statusMessage(stage),
		TotalRounds: defaultScanRounds,
		UpdatedAt:   task.UpdatedAt,
	}
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := r.worker.Start(ctx); err != nil {
		return nil, err
	}
	profileName := taskProfileName(task)
	userDataDir := filepath.Join(r.profilesDir, profileName)
	_, _ = r.db.AddTaskLog(task.ID, "info", "正在启动浏览器账号目录："+profileName)
	if _, err := r.worker.Call(ctx, "/api/v1/browser/start", map[string]any{
		"humanize":       true,
		"user_data_dir":  userDataDir,
		"downloads_path": r.browserDownloadDir(),
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
	totalRounds := scanRounds(options)
	maxItems := maxItemsPerRound(options)
	scrollDistance := scrollDistance(options)
	for round := 1; round <= totalRounds; round++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r.updateProgress(task.ID, Progress{Stage: "extracting", Message: fmt.Sprintf("正在扫描第 %d 轮", round), Round: round, TotalRounds: totalRounds})
		result, err := r.worker.Call(ctx, "/api/v1/boss/candidates/extract", map[string]any{
			"platform_config": platformConfig,
			"max_items":       maxItems,
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
		if len(filtered) > 0 && shouldFetchDetail(task) {
			r.updateProgress(task.ID, Progress{Stage: "detail", Message: fmt.Sprintf("正在读取第 %d 轮候选人详情", round), Round: round, TotalRounds: totalRounds})
			detailSkipped, err := r.enrichCandidatesWithDetail(ctx, task, platformConfig, filtered)
			if err != nil {
				return nil, err
			}
			totalSkipped += detailSkipped
		}
		if taskMode(task) == "ai" && len(filtered) > 0 {
			r.updateProgress(task.ID, Progress{Stage: "ai_scoring", Message: fmt.Sprintf("正在 AI 评分第 %d 轮候选人", round), Round: round, TotalRounds: totalRounds})
			scored, aiSkipped, err := r.scoreCandidates(ctx, task, filtered)
			if err != nil {
				return nil, err
			}
			filtered = scored
			totalSkipped += aiSkipped
		}
		if options.EnableGreet && len(filtered) > 0 {
			r.updateProgress(task.ID, Progress{Stage: "greeting", Message: fmt.Sprintf("正在打招呼第 %d 轮候选人", round), Round: round, TotalRounds: totalRounds})
			greeted, failed, err := r.greetCandidates(ctx, task, platformConfig, filtered, totalGreeted, options)
			if err != nil {
				return nil, err
			}
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
		if round < totalRounds {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			r.updateProgress(task.ID, Progress{Stage: "scrolling", Message: fmt.Sprintf("第 %d 轮完成，正在加载更多候选人", round), Round: round, TotalRounds: totalRounds})
			_, _ = r.worker.Call(ctx, "/api/v1/page/scroll", map[string]any{"distance": scrollDistance})
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

// taskProfileName 返回任务对应的本机浏览器目录名。
// task 为本地任务，优先使用平台账号 ID。
func taskProfileName(task localdb.Task) string {
	accountID := strings.TrimSpace(task.PlatformAccountID)
	if accountID == "" {
		accountID = strings.TrimSpace(task.PlatformID) + "_default"
	}
	return safePathName(accountID)
}

// safePathName 清理文件夹名中的危险字符。
// value 为原始名称，返回适合本机文件系统使用的名称。
func safePathName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}
	var builder strings.Builder
	for _, item := range value {
		if unicode.IsLetter(item) || unicode.IsDigit(item) || item == '-' || item == '_' || item == '.' {
			builder.WriteRune(item)
			continue
		}
		builder.WriteRune('_')
	}
	result := strings.Trim(builder.String(), "._ ")
	if result == "" {
		return "default"
	}
	if len(result) > 80 {
		return result[:80]
	}
	return result
}

// browserDownloadDir 返回任务运行时使用的下载目录。
// 优先读取本地设置，没有设置时使用默认下载目录。
func (r *Runner) browserDownloadDir() string {
	settings, err := r.db.GetSettings()
	if err == nil {
		if value := stringFromMap(settings, "browser_download_dir"); value != "" {
			return value
		}
		if value := stringFromMap(settings, "downloads_dir"); value != "" {
			return value
		}
	}
	return r.downloadsDir
}

// enrichCandidatesWithDetail 为候选人补充详情文本。
// ctx 为请求上下文，task 为任务记录，platformConfig 为云端平台配置，candidates 为候选人列表。
func (r *Runner) enrichCandidatesWithDetail(ctx context.Context, task localdb.Task, platformConfig cloudapi.PlatformConfig, candidates []map[string]any) (int, error) {
	skipped := 0
	mode := detailMode(task)
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return skipped, err
		}
		if !canContinueCandidate(stringFromMap(candidate, "status")) {
			continue
		}
		result, err := r.worker.Call(ctx, "/api/v1/boss/candidates/detail", map[string]any{
			"platform_config": platformConfig,
			"card_index":      intFromMap(candidate, "card_index"),
			"screenshot":      mode == "ocr",
			"dir":             filepath.Join(r.screenshotsDir, task.ID),
			"filename":        fmt.Sprintf("detail-%s.png", safePathName(stringFromMap(candidate, "id"))),
		})
		if err != nil {
			candidate["detail_error"] = err.Error()
			_, _ = r.db.AddTaskLog(task.ID, "warning", "读取候选人详情失败："+err.Error())
			continue
		}
		data := workerDataMap(result)
		detailText := strings.TrimSpace(firstNonEmptyString(stringFromMap(data, "detail_text"), stringFromMap(data, "text")))
		if screenshot := mapFromAny(data["screenshot"]); len(screenshot) > 0 {
			r.saveDetailScreenshot(task.ID, candidate, screenshot)
			if mode == "ocr" {
				visionText, err := r.analyzeDetailScreenshot(ctx, task, screenshot)
				if err != nil {
					candidate["vision_error"] = err.Error()
					_, _ = r.db.AddTaskLog(task.ID, "warning", "图片 AI 识别失败："+err.Error())
				} else if strings.TrimSpace(visionText) != "" {
					detailText = mergeText(detailText, visionText)
					candidate["vision_text"] = visionText
				}
			}
		}
		if detailText == "" {
			candidate["status"] = "skipped"
			candidate["skip_reason"] = "详情文本为空"
			skipped++
			continue
		}
		candidate["detail_text"] = detailText
		candidate["filter_text"] = mergeText(stringFromMap(candidate, "filter_text"), detailText)
		candidate["raw_text"] = mergeText(stringFromMap(candidate, "raw_text"), detailText)
		candidate["status"] = "detail_fetched"
		_, _ = r.db.AddTaskLog(task.ID, "info", fmt.Sprintf("%s 详情已读取，长度=%d", firstNonEmptyString(stringFromMap(candidate, "candidate_name"), "候选人"), len([]rune(detailText))))
	}
	return skipped, nil
}

// saveDetailScreenshot 保存详情截图记录。
// taskID 为任务 ID，candidate 为候选人，screenshot 为 Worker 返回的截图信息。
func (r *Runner) saveDetailScreenshot(taskID string, candidate map[string]any, screenshot map[string]any) {
	filePath := firstNonEmptyString(stringFromMap(screenshot, "file_path"), stringFromMap(screenshot, "path"))
	if filePath == "" {
		return
	}
	record, err := r.db.SaveScreenshot(map[string]any{
		"task_id":   taskID,
		"file_path": filePath,
		"label":     firstNonEmptyString(stringFromMap(candidate, "candidate_name"), "候选人详情"),
		"width":     screenshot["width"],
		"height":    screenshot["height"],
	})
	if err == nil {
		candidate["detail_screenshot"] = record
	}
}

// analyzeDetailScreenshot 使用本地 AI 识别详情截图。
// ctx 为请求上下文，task 为任务记录，screenshot 为截图信息。
func (r *Runner) analyzeDetailScreenshot(ctx context.Context, task localdb.Task, screenshot map[string]any) (string, error) {
	filePath := firstNonEmptyString(stringFromMap(screenshot, "file_path"), stringFromMap(screenshot, "path"))
	if filePath == "" {
		return "", fmt.Errorf("详情截图路径为空")
	}
	imageBytes, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("读取详情截图失败：%w", err)
	}
	config, err := r.db.GetAIConfig()
	if err != nil {
		return "", err
	}
	prompt := firstNonEmptyString(
		stringFromMap(mapValue(task.PositionSnapshot["ai_config"]), "open_detail_prompt"),
		"请识别图片中的候选人详情文字，保留学历、经验、技能、求职意向等关键信息，输出中文文本。",
	)
	content := []map[string]any{
		{"type": "text", "text": prompt},
		{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageBytes)}},
	}
	result, err := localai.New(config).Chat(ctx, map[string]any{
		"messages":    []map[string]any{{"role": "user", "content": content}},
		"temperature": 0.1,
	})
	if err != nil {
		return "", err
	}
	return result.Content, nil
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
		if err := ctx.Err(); err != nil {
			return nil, skipped, err
		}
		status := stringFromMap(candidate, "status")
		if !canContinueCandidate(status) {
			result = append(result, candidate)
			continue
		}
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
func (r *Runner) greetCandidates(ctx context.Context, task localdb.Task, platformConfig cloudapi.PlatformConfig, candidates []map[string]any, greetedBefore int, options StartOptions) (int, int, error) {
	greeted := 0
	failed := 0
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return greeted, failed, err
		}
		status := stringFromMap(candidate, "status")
		if status != "passed" && status != "ai_passed" && status != "detail_fetched" {
			continue
		}
		if task.MatchLimit > 0 && greetedBefore+greeted >= task.MatchLimit {
			candidate["status"] = "skipped"
			candidate["skip_reason"] = "已达到任务打招呼上限"
			continue
		}
		if err := waitBeforeGreet(ctx, options); err != nil {
			return greeted, failed, err
		}
		err := r.tryGreet(ctx, platformConfig, candidate, options)
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
	return greeted, failed, nil
}

// tryGreet 带重试地执行单个候选人打招呼。
// ctx 为请求上下文，platformConfig 为平台配置，candidate 为候选人。
func (r *Runner) tryGreet(ctx context.Context, platformConfig cloudapi.PlatformConfig, candidate map[string]any, options StartOptions) error {
	retries := maxInt(0, options.GreetRetries)
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := r.worker.Call(ctx, "/api/v1/boss/candidates/greet", map[string]any{
			"platform_config": platformConfig,
			"card_index":      intFromMap(candidate, "card_index"),
		})
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt < retries {
			if err := sleepWithContext(ctx, 300*time.Millisecond); err != nil {
				return err
			}
		}
	}
	return lastErr
}

// waitBeforeGreet 在打招呼前随机等待。
// ctx 为请求上下文，options 为任务启动参数。
func waitBeforeGreet(ctx context.Context, options StartOptions) error {
	minDelay := options.GreetDelayMin
	maxDelay := options.GreetDelayMax
	if minDelay <= 0 && maxDelay <= 0 {
		return nil
	}
	if maxDelay < minDelay {
		maxDelay = minDelay
	}
	delay := minDelay
	if maxDelay > minDelay {
		delay += rand.Float64() * (maxDelay - minDelay)
	}
	return sleepWithContext(ctx, time.Duration(delay*float64(time.Second)))
}

// sleepWithContext 带停止信号地等待。
// ctx 为请求上下文，duration 为等待时长。
func sleepWithContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// maxInt 返回两个整数中的较大值。
// a 和 b 为参与比较的整数。
func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

// scanRounds 返回本次任务扫描轮数。
// options 为任务启动参数。
func scanRounds(options StartOptions) int {
	if options.ScanRounds <= 0 {
		return defaultScanRounds
	}
	if options.ScanRounds > 20 {
		return 20
	}
	return options.ScanRounds
}

// maxItemsPerRound 返回每轮最多提取候选人数。
// options 为任务启动参数。
func maxItemsPerRound(options StartOptions) int {
	if options.MaxItems <= 0 {
		return defaultMaxItemsPerRound
	}
	if options.MaxItems > 100 {
		return 100
	}
	return options.MaxItems
}

// scrollDistance 返回每轮滚动距离。
// options 为任务启动参数。
func scrollDistance(options StartOptions) int {
	if options.ScrollDistance <= 0 {
		return defaultScrollDistance
	}
	if options.ScrollDistance > 3000 {
		return 3000
	}
	return options.ScrollDistance
}

// statusMessage 返回任务状态中文说明。
// status 为任务状态。
func statusMessage(status string) string {
	switch status {
	case "pending":
		return "任务等待开始"
	case "running":
		return "任务正在运行"
	case "completed":
		return "任务已完成"
	case "failed":
		return "任务运行失败"
	case "stopped":
		return "任务已停止"
	default:
		return "任务状态未知"
	}
}

// failStart 记录启动失败日志并清理运行锁。
// taskID 为任务 ID，msg 为失败原因。
func (r *Runner) failStart(taskID string, msg string) {
	_, _ = r.db.AddTaskLog(taskID, "error", msg)
	_, _ = r.db.UpdateTaskStatus(taskID, "failed")
	r.clear(taskID)
}

// setRunning 标记任务正在运行。
// taskID 为任务 ID，cancel 为停止回调。
func (r *Runner) setRunning(taskID string, cancel context.CancelFunc) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.running[taskID]; ok {
		return false
	}
	r.running[taskID] = &runState{cancel: cancel, progress: Progress{Stage: "starting", Message: "任务准备启动", TotalRounds: defaultScanRounds, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}}
	return true
}

// updateProgress 更新任务运行进度。
// taskID 为任务 ID，progress 为新进度。
func (r *Runner) updateProgress(taskID string, progress Progress) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.running[taskID]
	if state == nil {
		return
	}
	if progress.TotalRounds <= 0 {
		progress.TotalRounds = defaultScanRounds
	}
	if progress.UpdatedAt == "" {
		progress.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	state.progress = progress
}

// cancel 取消正在运行的任务。
// taskID 为任务 ID。
func (r *Runner) cancel(taskID string) {
	r.mu.Lock()
	state := r.running[taskID]
	delete(r.running, taskID)
	r.mu.Unlock()
	if state != nil && state.cancel != nil {
		state.cancel()
	}
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

// mapValue 将任意值转换为 map。
// value 为原始值。
func mapValue(value any) map[string]any {
	if item, ok := value.(map[string]any); ok && item != nil {
		return item
	}
	return map[string]any{}
}

// mapFromAny 将任意值转换为 map。
// value 为原始值。
func mapFromAny(value any) map[string]any {
	return mapValue(value)
}

// workerDataMap 从 Worker 返回中读取 data 字典。
// result 为 Worker 返回 JSON。
func workerDataMap(result map[string]any) map[string]any {
	if result == nil {
		return map[string]any{}
	}
	if data, ok := result["data"].(map[string]any); ok {
		return data
	}
	return result
}

// firstNonEmptyString 返回第一个非空字符串。
// values 为候选字符串。
func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// mergeText 合并两段文本并去掉空值。
// base 为原文本，extra 为补充文本。
func mergeText(base string, extra string) string {
	base = strings.TrimSpace(base)
	extra = strings.TrimSpace(extra)
	if base == "" {
		return extra
	}
	if extra == "" || strings.Contains(base, extra) {
		return base
	}
	return base + "\n" + extra
}

// shouldFetchDetail 判断任务是否需要读取候选人详情。
// task 为任务记录。
func shouldFetchDetail(task localdb.Task) bool {
	return detailMode(task) != ""
}

// detailMode 返回详情读取模式。
// task 为任务记录，支持 dom 和 ocr。
func detailMode(task localdb.Task) string {
	commonConfig := mapValue(task.PositionSnapshot["common_config"])
	keywordConfig := mapValue(task.PositionSnapshot["keyword_config"])
	mode := strings.ToLower(firstNonEmptyString(
		stringFromMap(commonConfig, "detail_mode"),
		stringFromMap(keywordConfig, "detail_mode"),
	))
	if mode == "ocr" || mode == "dom" {
		return mode
	}
	if strings.ToLower(strings.TrimSpace(task.PlatformID)) == "boss" {
		return "ocr"
	}
	return ""
}

// canContinueCandidate 判断候选人是否可以继续进入详情或 AI 阶段。
// status 为候选人当前状态。
func canContinueCandidate(status string) bool {
	status = strings.TrimSpace(status)
	return status == "" || status == "scanned" || status == "passed" || status == "detail_fetched" || status == "ai_passed"
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
