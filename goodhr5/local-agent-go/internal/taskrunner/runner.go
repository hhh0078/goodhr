// Package taskrunner 负责管理 Go 版本本地任务启动、停止和运行锁。
package taskrunner

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf16"

	"goodhr5/local-agent-go/internal/browser"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/localai"
	"goodhr5/local-agent-go/internal/localdb"
	"goodhr5/local-agent-go/internal/ocr"
	"goodhr5/local-agent-go/internal/platformcore"
	"goodhr5/local-agent-go/internal/platforms"
)

const defaultScanRounds = 3
const defaultMaxItemsPerRound = 0
const defaultScrollDistance = 720
const defaultScrollDistanceJitter = 160
const defaultCandidatePipelineConcurrency = 5
const pendingAIVisionDecisionKey = "_pending_ai_vision_decision"

// pageEntryCheckAttempts 是入口页面加载检查的最大次数。
var pageEntryCheckAttempts = 10

// pageEntryCheckDelay 是每次入口页面检查前的等待时间。
var pageEntryCheckDelay = time.Second

// currentPositionCheckAttempts 是当前岗位名称读取的最大次数。
var currentPositionCheckAttempts = 10

// currentPositionCheckDelay 是每次读取当前岗位名称前的等待时间。
var currentPositionCheckDelay = time.Second

// BrowserWorker 表示任务运行器需要的浏览器 Worker 能力。
type BrowserWorker interface {
	Start(ctx context.Context) (browser.WorkerStatus, error)
	Call(ctx context.Context, path string, payload any) (map[string]any, error)
}

// OCRRecognizer 表示任务运行器需要的 OCR 能力。
type OCRRecognizer interface {
	Recognize(ctx context.Context, imagePath string) (ocr.Result, error)
}

// Runner 是本地任务运行器。
type Runner struct {
	db             *localdb.DB
	worker         BrowserWorker
	ocr            OCRRecognizer
	profilesDir    string
	downloadsDir   string
	screenshotsDir string
	audioDir       string
	cloudAPIBase   string
	mu             sync.Mutex
	running        map[string]*runState
	userStopped    map[string]bool
}

// runState 保存单个运行任务的控制句柄。
type runState struct {
	cancel         context.CancelFunc
	progress       Progress
	emailForNotify string // 失败通知邮箱
	runGreeted     int    // 本次运行已打招呼数量
	// 摸鱼休息状态
	restMaxTimes  int
	restUsed      int
	restNextAfter int
	restSinceLast int
}

// Progress 表示任务运行进度。
type Progress struct {
	Stage       string `json:"stage"`
	Message     string `json:"message"`
	Round       int    `json:"round"`
	TotalRounds int    `json:"total_rounds"`
	UpdatedAt   string `json:"updated_at"`
}

// TaskRuntimeSnapshot 保存一次任务运行开始时从云端读取到的完整快照。
type TaskRuntimeSnapshot struct {
	Task           localdb.Task
	Options        StartOptions
	PlatformConfig cloudapi.PlatformConfig
	Preferences    map[string]any
	AIConfig       localdb.AIConfig
}

// platformExecutor 适配平台 runtime 调用 Worker 和写任务日志。
type platformExecutor struct {
	runner *Runner
	taskID string
}

// Post 调用浏览器 Worker。
// ctx 为请求上下文，path 为 Worker 路径，payload 为请求体。
func (e platformExecutor) Post(ctx context.Context, path string, payload any) (map[string]any, error) {
	return e.runner.worker.Call(ctx, path, payload)
}

// Log 写入任务日志。
// level 为日志级别，message 为日志内容。
func (e platformExecutor) Log(level string, message string) {
	e.runner.taskLog(e.taskID, level, message)
}

// Delay 按业务动作等待指定秒数。
// ctx 为请求上下文，label 为动作名称，seconds 为等待秒数。
func (e platformExecutor) Delay(ctx context.Context, label string, seconds float64) error {
	if seconds <= 0 {
		return nil
	}
	e.runner.taskLog(e.taskID, "info", fmt.Sprintf("%s等待 %.1f 秒", label, seconds))
	return sleepWithContext(ctx, time.Duration(seconds*float64(time.Second)))
}

// StartOptions 表示本地任务启动参数（含模拟人工操作的各类延时）。
type StartOptions struct {
	CloudAPIBase   string
	Token          string
	AIConfig       localdb.AIConfig
	EnableGreet    bool
	GreetDelayMin  float64
	GreetDelayMax  float64
	GreetRetries   int
	ScanRounds     int
	MaxItems       int
	ScrollDistance int
	PageReadyDelay int
	// 以下为模拟人工操作的延时配置（随机范围）
	ScrollDelayMin           int // 两次滚动之间的延时（秒）
	ScrollDelayMax           int
	ListViewDelayMin         float64 // 查看候选人列表后的停留（秒）
	ListViewDelayMax         float64
	DetailViewDelayMin       float64 // 查看候选人详情后的停留（秒）
	DetailViewDelayMax       float64
	DetailOpenProbability    int     // 打开详情概率（0-100）
	detailOpenProbabilitySet bool    // 是否已从个人配置读取打开详情概率
	DetailOpenDelayMin       float64 // 打开详情前的延时（秒）
	DetailOpenDelayMax       float64
	DetailCloseDelayMin      float64 // 关闭详情前的延时（秒）
	DetailCloseDelayMax      float64
	GreetBeforeDelayMin      float64 // 打招呼前点击按钮的延时（秒）
	GreetBeforeDelayMax      float64
	RestAfterCandidatesMin   int // 处理多少候选人后摸鱼休息
	RestAfterCandidatesMax   int
	RestTimesMin             int // 整个任务最多摸鱼休息几次
	RestTimesMax             int
	RestDurationMin          float64 // 每次摸鱼休息多少分钟
	RestDurationMax          float64
	// 提示音和通知
	EnableSound    bool   `json:"enable_sound"`     // 是否开启提示音
	EmailForNotify string `json:"email_for_notify"` // 失败通知邮箱
}

// New 创建本地任务运行器。
// db 为本地 SQLite 数据库，worker 为浏览器 Worker 管理器，profilesDir、downloadsDir 和 screenshotsDir 为本机浏览器目录。
func New(db *localdb.DB, worker BrowserWorker, ocr OCRRecognizer, profilesDir string, downloadsDir string, screenshotsDir string, audioDir string, cloudAPIBase string) *Runner {
	return &Runner{db: db, worker: worker, ocr: ocr, profilesDir: profilesDir, downloadsDir: downloadsDir, screenshotsDir: screenshotsDir, audioDir: audioDir, cloudAPIBase: cloudAPIBase, running: map[string]*runState{}, userStopped: map[string]bool{}}
}

// Start 启动本地任务运行器。
// ctx 为请求上下文，taskID 为任务 ID，options 为启动参数。
func (r *Runner) Start(ctx context.Context, taskID string, options StartOptions) (map[string]any, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("任务 ID 不能为空")
	}
	options.Token = strings.TrimSpace(options.Token)
	if options.Token == "" {
		return nil, fmt.Errorf("请先登录后再校验会员")
	}
	client := cloudapi.New(options.CloudAPIBase)
	cloudTask, err := client.FetchTask(ctx, options.Token, taskID)
	if err != nil {
		return nil, err
	}
	task, err := r.db.UpsertTaskSnapshot(localTaskSnapshotFromCloud(cloudTask))
	if err != nil {
		return nil, err
	}
	r.taskLog(taskID, "info", fmt.Sprintf("收到任务启动请求：name=%s platform=%s mode=%s", task.Name, task.PlatformID, task.Mode))
	runCtx, cancel := context.WithCancel(context.Background())
	if !r.setRunning(taskID, cancel) {
		cancel()
		return nil, fmt.Errorf("任务正在运行")
	}
	r.updateProgress(taskID, Progress{Stage: "starting", Message: "任务准备启动", TotalRounds: defaultScanRounds})
	// 保存通知邮箱到运行状态
	r.mu.Lock()
	if state, ok := r.running[taskID]; ok {
		state.emailForNotify = options.EmailForNotify
	}
	r.mu.Unlock()
	updated, err := r.db.UpdateTaskStatus(taskID, "running")
	if err != nil {
		r.clear(taskID)
		cancel()
		return nil, err
	}
	r.taskLog(taskID, "info", "本地任务已进入后台运行")
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
	snapshot, err := r.buildTaskRuntimeSnapshot(ctx, client, task, options, totalRounds)
	if err != nil {
		r.failStart(taskID, err.Error(), options)
		return
	}
	task = snapshot.Task
	options = snapshot.Options
	options.EnableSound = task.EnableSound
	r.updateProgress(taskID, Progress{Stage: "running", Message: "任务已开始执行", TotalRounds: totalRounds})
	r.taskLog(taskID, "info", "本地任务运行器已启动，准备进入扫描流程")
	scanResult, err := r.scanOnce(ctx, task, snapshot.PlatformConfig, options)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			r.updateProgress(taskID, Progress{Stage: "stopped", Message: "任务已停止", TotalRounds: totalRounds})
			_, _ = r.db.UpdateTaskStatus(taskID, "stopped")
			r.taskLog(taskID, "info", "本地任务收到停止信号")
			r.notifyCloudTaskStopped(taskID, options)
			return
		}
		if isBrowserClosedTaskError(err) {
			r.updateProgress(taskID, Progress{Stage: "stopped", Message: "浏览器已关闭，任务已自动结束", TotalRounds: totalRounds})
			_, _ = r.db.UpdateTaskStatus(taskID, "stopped")
			r.taskLog(taskID, "warning", "浏览器已关闭，任务已自动结束："+err.Error())
			r.sendTaskFailNotification(context.Background(), taskID, "浏览器已关闭，任务已自动结束："+err.Error(), options)
			return
		}
		r.failStart(taskID, "本地任务扫描失败："+err.Error(), options)
		return
	}
	if r.isUserStopped(taskID) {
		r.taskLog(taskID, "info", "任务已被用户停止，忽略扫描完成结果")
		r.notifyCloudTaskStopped(taskID, options)
		return
	}
	r.updateProgress(taskID, Progress{Stage: "completed", Message: "任务已完成", Round: totalRounds, TotalRounds: totalRounds})
	_, _ = r.db.UpdateTaskStatus(taskID, "completed")
	r.taskLog(taskID, "info", fmt.Sprintf("后台任务已完成：%v", scanResult))
	r.notifyCloudTaskStopped(taskID, options)
}

// Stop 停止本地任务运行器。
// taskID 为任务 ID。
func (r *Runner) Stop(taskID string) (map[string]any, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("任务 ID 不能为空")
	}
	r.taskLog(taskID, "info", "收到停止任务请求")
	r.markUserStoppedAndCancel(taskID)
	task, err := r.db.UpdateTaskStatus(taskID, "stopped")
	if err != nil {
		return nil, err
	}
	r.taskLog(taskID, "info", "本地任务已停止，浏览器保持打开")
	return map[string]any{"task": task, "running": false}, nil
}

// StopAll 停止所有正在运行的本地任务。
// reason 为停止原因，返回停止的任务数量。
func (r *Runner) StopAll(reason string) int {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "任务已停止"
	}
	r.mu.Lock()
	ids := make([]string, 0, len(r.running))
	for taskID, state := range r.running {
		ids = append(ids, taskID)
		if state != nil && state.cancel != nil {
			state.cancel()
		}
	}
	r.mu.Unlock()
	for _, taskID := range ids {
		r.updateProgress(taskID, Progress{Stage: "stopped", Message: reason, TotalRounds: defaultScanRounds})
		_, _ = r.db.UpdateTaskStatus(taskID, "stopped")
		r.taskLog(taskID, "warning", reason)
	}
	return len(ids)
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
		if isLocalTaskMissing(err) {
			return map[string]any{
				"task": map[string]any{
					"id":     taskID,
					"status": "pending",
				},
				"running": false,
				"progress": Progress{
					Stage:       "pending",
					Message:     "本地任务尚未启动",
					TotalRounds: defaultScanRounds,
					UpdatedAt:   time.Now().Format(time.RFC3339),
				},
				"logs": []localdb.Log{},
			}, nil
		}
		return nil, err
	}
	running := r.IsRunning(taskID)
	progress := r.Progress(taskID, task)
	logs, _ := r.db.ListTaskLogs(taskID, 20)
	taskMap := localTaskStatusMap(task)
	taskMap["current_run_greeted_count"] = r.currentRunGreeted(taskID)
	return map[string]any{"task": taskMap, "running": running, "progress": progress, "logs": logs}, nil
}

// isLocalTaskMissing 判断错误是否表示本地任务尚未创建。
// err 为数据库返回的错误。
func isLocalTaskMissing(err error) bool {
	return err != nil && strings.Contains(err.Error(), "本地任务不存在")
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
	platformRuntime, err := platforms.RuntimeFor(task.PlatformID)
	if err != nil {
		return nil, err
	}
	exec := platformExecutor{runner: r, taskID: task.ID}
	entryURL := platformEntryURL(platformConfig)
	if entryURL == "" {
		return nil, fmt.Errorf("云端平台配置缺少入口页面地址")
	}
	// 1. 准备平台运行时和浏览器。
	r.taskLog(task.ID, "info", "平台入口地址已确认："+entryURL)
	r.taskLog(task.ID, "info", "准备启动浏览器 Worker")
	workerStatus, err := r.worker.Start(ctx)
	if err != nil {
		return nil, err
	}
	r.taskLog(task.ID, "info", fmt.Sprintf("浏览器 Worker 已启动：running=%v base_url=%s", workerStatus.Running, workerStatus.BaseURL))
	profileName := taskProfileName(task)
	userDataDir := filepath.Join(r.profilesDir, profileName)
	r.taskLog(task.ID, "info", "正在启动浏览器账号目录："+profileName)
	r.taskLog(task.ID, "info", "准备调用浏览器启动接口：/api/v1/browser/start")
	viewportWidth, viewportHeight := taskBrowserViewport()
	if _, err := r.worker.Call(ctx, "/api/v1/browser/start", map[string]any{
		"humanize":        true,
		"user_data_dir":   userDataDir,
		"downloads_path":  r.browserDownloadDir(),
		"viewport_width":  viewportWidth,
		"viewport_height": viewportHeight,
	}); err != nil {
		return nil, err
	}
	r.taskLog(task.ID, "info", "浏览器启动成功，准备确认当前页面")
	onEntryPage, err := platformRuntime.IsTaskEntryPage(ctx, exec, platformConfig)
	if err != nil {
		r.taskLog(task.ID, "warning", "读取当前页面地址失败，将打开入口页面："+err.Error())
	}
	if onEntryPage {
		r.taskLog(task.ID, "info", "当前页面已命中入口地址，跳过入口页跳转")
	} else {
		r.taskLog(task.ID, "info", "当前页面未命中入口地址，准备打开入口页面")
		if err := platformRuntime.OpenEntryPage(ctx, exec, platformConfig, entryURL); err != nil {
			return nil, err
		}
	}
	seen := map[string]struct{}{}
	queue := make([]map[string]any, 0)
	totalSaved := 0
	totalSkipped := 0
	totalGreeted := 0
	totalFailed := 0
	processedCount := 0
	emptyLoads := 0
	emptyLimit := emptyLoadLimit(options)
	maxItems := maxItemsPerLoad(options)
scanLoop:
	for emptyLoads < emptyLimit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(queue) == 0 {
			// 2. 确认当前网页已经进入任务入口，并切到任务对应岗位。
			r.updateProgress(task.ID, Progress{Stage: "page_ready", Message: "正在确认页面和岗位"})
			if err := r.waitTaskEntryPage(ctx, task.ID, platformRuntime, exec, platformConfig); err != nil {
				return nil, err
			}
			r.prepareEntryPage(ctx, task.ID, platformRuntime, exec, platformConfig)
			positionName := taskPositionName(task)
			if strings.TrimSpace(positionName) == "" {
				return nil, fmt.Errorf("任务岗位名称为空，无法确认页面岗位")
			}
			currentName, err := r.waitCurrentPositionName(ctx, task.ID, platformRuntime, exec, platformConfig)
			if err != nil {
				return nil, fmt.Errorf("获取页面当前岗位失败：%w", err)
			}
			if strings.Contains(normalizeTaskPositionName(currentName), normalizeTaskPositionName(positionName)) {
				r.taskLog(task.ID, "info", "页面岗位匹配："+currentName)
			} else {
				r.taskLog(task.ID, "warning", fmt.Sprintf("页面岗位与任务岗位不一致，准备切换：页面=%s，任务=%s", currentName, positionName))
				if err := platformRuntime.SelectPosition(ctx, exec, platformConfig, positionName); err != nil {
					return nil, fmt.Errorf("切换页面岗位失败：%w", err)
				}
				confirmedName, err := r.waitCurrentPositionName(ctx, task.ID, platformRuntime, exec, platformConfig)
				if err != nil {
					return nil, fmt.Errorf("切换后确认页面岗位失败：%w", err)
				}
				if !strings.Contains(normalizeTaskPositionName(confirmedName), normalizeTaskPositionName(positionName)) {
					return nil, fmt.Errorf("页面切换岗位失败，请手动操作后再点击开始。当前页面岗位=%s，任务岗位=%s", confirmedName, positionName)
				}
				r.taskLog(task.ID, "info", "页面岗位已切换为："+confirmedName)
			}
			delay := pageReadyDelay(options)
			r.taskLog(task.ID, "info", fmt.Sprintf("候选人提取前等待页面稳定：%s", delay.String()))
			if err := sleepWithContext(ctx, delay); err != nil {
				return nil, err
			}
			// 3. 读取当前屏幕可见候选人，并追加到待处理队列。
			r.updateProgress(task.ID, Progress{Stage: "extracting", Message: "正在提取候选人"})
			r.taskLog(task.ID, "info", fmt.Sprintf("开始提取候选人：max_items=%d", maxItems))
			platformCandidates, err := platformRuntime.ListVisibleCandidates(ctx, exec, platformConfig, maxItems)
			if err != nil {
				return nil, err
			}
			candidates, duplicateCount := freshCandidates(candidateMaps(platformCandidates), seen)
			if len(candidates) == 0 {
				emptyLoads++
				r.taskLog(task.ID, "info", fmt.Sprintf("本次未发现新候选人，重复跳过=%d，连续空加载=%d/%d", duplicateCount, emptyLoads, emptyLimit))
				if emptyLoads >= emptyLimit {
					break
				}
				if err := r.scrollForMoreCandidates(ctx, task.ID, platformRuntime, exec, platformConfig, options); err != nil {
					return nil, err
				}
				continue
			}
			emptyLoads = 0
			queue = append(queue, candidates...)
			r.syncProcessedResumeCount(ctx, task, len(candidates), options)
			r.taskLog(task.ID, "info", fmt.Sprintf("候选人提取完成：本次新增=%d，重复跳过=%d，待处理=%d，已处理=%d", len(candidates), duplicateCount, len(queue), processedCount))
		}
		candidates := queue
		queue = nil
		filtered, skipped := r.prepareCandidatesForFirstStage(task, candidates)
		totalSkipped += skipped
		if skipped > 0 {
			r.taskLog(task.ID, "info", fmt.Sprintf("列表关键词过滤完成：保留=%d 跳过=%d", len(filtered), skipped))
		}
		if len(filtered) > 0 {
			r.updateProgress(task.ID, Progress{Stage: "pipeline", Message: fmt.Sprintf("正在处理候选人队列，待处理 %d 个", len(filtered))})
			r.taskLog(task.ID, "info", fmt.Sprintf("开始处理候选人队列：数量=%d", len(filtered)))

			// 4. 并发做“是否值得看详情”的预评分，但主流程仍按页面顺序消费候选人。
			batchResult := batchProcessResult{}
			aiClient, err := r.pipelineAIClient(task, options)
			if err != nil {
				return nil, err
			}
			precheckCh := make(chan candidatePipelineResult, len(filtered))
			aiJobs := make(chan candidatePipelineResult, len(filtered))
			needsAI := taskMode(task) == "ai"
			if needsAI {
				workerCount := candidatePipelineConcurrency(len(filtered))
				r.taskLog(task.ID, "info", fmt.Sprintf("正在并发分析多个候选人：数量=%d，并发数=%d", len(filtered), workerCount))
				r.startCandidateDetailWorkers(ctx, task, exec, aiClient, aiJobs, precheckCh, workerCount)
			}
			go r.feedCandidatePipeline(ctx, task, filtered, needsAI, aiJobs, precheckCh)

			pending := map[int]candidatePipelineResult{}
			nextIndex := 0
			for nextIndex < len(filtered) {
				if reachedRunGreetLimit(task, totalGreeted+batchResult.Greeted) {
					r.taskLog(task.ID, "info", fmt.Sprintf("已达到本次上限 %d，停止继续处理候选人", task.MatchLimit))
					break
				}
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				item, ok := pending[nextIndex]
				if !ok {
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case received := <-precheckCh:
						pending[received.Index] = received
						continue
					case <-time.After(150 * time.Millisecond):
						continue
					}
				}
				delete(pending, nextIndex)
				nextIndex++
				if item.Err != nil {
					r.taskLog(task.ID, "error", fmt.Sprintf("候选人处理失败：index=%d err=%v", item.Index, item.Err))
					return nil, item.Err
				}

				candidate := item.Candidate
				processedCount++
				r.taskLog(task.ID, "info", fmt.Sprintf("按队列顺序处理候选人：序号=%d name=%s status=%s", processedCount, candidateLogName(candidate), stringFromMap(candidate, "status")))
				batchResult.Skipped += item.Skipped

				// 5. 如果预评分通过，再打开详情；详情模式由任务配置决定：DOM、OCR 或 AI 图片。
				if item.DetailDecision != nil {
					decision := item.DetailDecision
					candidate["ai_detail_score"] = decision.Score
					candidate["ai_detail_reason"] = decision.Reason
					candidate["ai_detail_threshold"] = decision.Threshold
					candidate["ai_detail_usage"] = decision.Usage
					candidate["ai_detail_elapsed_ms"] = decision.ElapsedMS
					if !decision.ShouldOpenDetail {
						candidate["status"] = "skipped"
						candidate["skip_reason"] = fmt.Sprintf("详情评分低于阈值：%.1f/%.1f，%s", decision.Score, decision.Threshold, decision.Reason)
						batchResult.Skipped++
						r.taskLog(task.ID, "info", fmt.Sprintf("看详情评分跳过：name=%s score=%.1f threshold=%.1f reason=%s", candidateLogName(candidate), decision.Score, decision.Threshold, decision.Reason))
					} else {
						r.taskLog(task.ID, "info", fmt.Sprintf("看详情评分通过，准备打开详情：name=%s score=%.1f threshold=%.1f", candidateLogName(candidate), decision.Score, decision.Threshold))
						itemSkipped, err := r.enrichCandidateWithDetail(ctx, task, platformRuntime, exec, platformConfig, candidate, aiClient, options)
						batchResult.Skipped += itemSkipped
						if err != nil {
							return nil, err
						}
					}
				}

				// 6. 非 AI 主模式下，如果任务要求看详情，也按配置读取详情。
				if !needsAI && shouldFetchDetail(task) && canContinueCandidate(stringFromMap(candidate, "status")) {
					if taskMode(task) == "keyword" && !shouldOpenDetailByProbability(options) {
						candidate["status"] = "skipped"
						candidate["skip_reason"] = fmt.Sprintf("未命中打开详情概率：%d%%", detailOpenProbability(options))
						batchResult.Skipped++
						r.taskLog(task.ID, "info", fmt.Sprintf("打开详情概率跳过：name=%s probability=%d%%", candidateLogName(candidate), detailOpenProbability(options)))
						continue
					}
					r.taskLog(task.ID, "info", fmt.Sprintf("准备读取候选人详情：index=%d name=%s", item.Index, candidateLogName(candidate)))
					itemSkipped, err := r.enrichCandidateWithDetail(ctx, task, platformRuntime, exec, platformConfig, candidate, aiClient, options)
					batchResult.Skipped += itemSkipped
					if err != nil {
						return nil, err
					}
				}

				// 7. 第二次详情分析：详情 AI 已经一次性评分时跳过；否则按任务模式做最终判断。
				if canContinueCandidate(stringFromMap(candidate, "status")) && !boolFromMap(candidate, "ai_greet_scored") {
					itemSkipped, err := r.finalizeCandidateGreetDecision(ctx, task, exec, candidate, aiClient)
					batchResult.Skipped += itemSkipped
					if err != nil {
						candidate["status"] = "failed"
						candidate["error"] = err.Error()
						batchResult.Failed++
						r.taskLog(task.ID, "warning", "最终打招呼判断失败："+err.Error())
					}
				}

				// 8. 评分通过后执行打招呼，然后保存候选人结果。
				if options.EnableGreet {
					greeted, failed, itemSkipped, err := r.consumeCandidateForGreet(ctx, task, platformRuntime, exec, platformConfig, candidate, totalGreeted+batchResult.Greeted, options)
					if err != nil {
						return nil, err
					}
					batchResult.Greeted += greeted
					batchResult.Failed += failed
					batchResult.Skipped += itemSkipped
					if greeted > 0 {
						r.incrementRunGreeted(task.ID, greeted)
					}
				}

				status := stringFromMap(candidate, "status")
				if shouldSaveCandidateResult(status) {
					r.saveCandidateResult(ctx, task, candidate, options)
					r.taskLog(task.ID, "info", fmt.Sprintf("候选人已处理：index=%d name=%s status=%s", item.Index, candidateLogName(candidate), status))
					batchResult.Saved++
				}
			}

			totalSaved += batchResult.Saved
			totalSkipped += batchResult.Skipped
			totalGreeted += batchResult.Greeted
			totalFailed += batchResult.Failed
			r.taskLog(task.ID, "info", fmt.Sprintf("候选人队列处理完成：保存=%d 跳过=%d 打招呼=%d 失败=%d", batchResult.Saved, batchResult.Skipped, batchResult.Greeted, batchResult.Failed))
			if reachedRunGreetLimit(task, totalGreeted) {
				break scanLoop
			}
		}
		if err := r.scrollForMoreCandidates(ctx, task.ID, platformRuntime, exec, platformConfig, options); err != nil {
			return nil, err
		}
	}
	if totalSaved > 0 || totalSkipped > 0 {
		_, _ = r.db.IncrementTaskCounts(task.ID, totalSaved, totalGreeted, totalSkipped, totalFailed)
		r.taskLog(task.ID, "info", fmt.Sprintf("本次扫描处理 %d 个候选人，跳过 %d 个，打招呼 %d 个，失败 %d 个", totalSaved, totalSkipped, totalGreeted, totalFailed))
	} else {
		r.taskLog(task.ID, "warning", "当前页面未提取到可见候选人，请确认账号已登录且页面在推荐列表")
	}
	return map[string]any{
		"candidates_count": totalSaved,
		"skipped_count":    totalSkipped,
		"greeted_count":    totalGreeted,
		"failed_count":     totalFailed,
		"processed_count":  processedCount,
		"entry_url":        entryURL,
	}, nil
}

// scrollForMoreCandidates 滚动候选人列表以加载更多候选人。
// ctx 为请求上下文，taskID 为任务 ID，platformRuntime 为平台执行器，exec 为 Worker 执行器，platformConfig 为平台配置，options 为任务启动参数。
func (r *Runner) scrollForMoreCandidates(ctx context.Context, taskID string, platformRuntime platformcore.Runtime, exec platformExecutor, platformConfig cloudapi.PlatformConfig, options StartOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.updateProgress(taskID, Progress{Stage: "scrolling", Message: "正在加载更多候选人"})
	scrollDistance := randomScrollDistance(options)
	r.taskLog(taskID, "info", fmt.Sprintf("准备滚动候选人列表：distance=%d", scrollDistance))
	if err := platformRuntime.ScrollCandidateList(ctx, exec, platformConfig, scrollDistance); err != nil {
		r.taskLog(taskID, "warning", "滚动候选人列表失败："+err.Error())
		return nil
	}
	r.taskLog(taskID, "info", "候选人列表滚动完成")
	return nil
}

// syncProcessedResumeCount 将去重后的新增候选人数量同步给云端公开统计。
// ctx 为请求上下文，task 为任务记录，count 为新增候选人数量，options 为任务启动参数。
func (r *Runner) syncProcessedResumeCount(ctx context.Context, task localdb.Task, count int, options StartOptions) {
	if count <= 0 || strings.TrimSpace(options.Token) == "" {
		return
	}
	if err := cloudapi.New(options.CloudAPIBase).AddProcessedResumes(ctx, options.Token, task.ID, count); err != nil {
		r.taskLog(task.ID, "warning", "同步已处理简历数失败："+err.Error())
	}
}

// saveCandidateResult 将候选人结果同步到云端简历库。
// ctx 为请求上下文，task 为任务记录，candidate 为候选人结果，options 为启动参数。
func (r *Runner) saveCandidateResult(ctx context.Context, task localdb.Task, candidate map[string]any, options StartOptions) {
	if strings.TrimSpace(options.Token) == "" {
		r.taskLog(task.ID, "warning", "候选人未同步云端：缺少登录 token")
		return
	}
	if r.savePendingAIVisionCandidateAsync(ctx, task, candidate, options) {
		return
	}
	payload := cloneCandidateForCloud(task, candidate)
	r.saveCandidatePayload(ctx, task, payload, options)
}

// cloneCandidateForCloud 生成候选人云端入库 JSON。
// task 为任务记录，candidate 为本地候选人结果。
func cloneCandidateForCloud(task localdb.Task, candidate map[string]any) map[string]any {
	payload := make(map[string]any, len(candidate)+5)
	for key, value := range candidate {
		if strings.HasPrefix(key, "_pending_") {
			continue
		}
		payload[key] = value
	}
	payload["task_id"] = task.ID
	payload["platform_id"] = task.PlatformID
	payload["position_id"] = task.PositionID
	payload["platform_account_id"] = task.PlatformAccountID
	if _, ok := payload["candidate_name"]; !ok {
		payload["candidate_name"] = candidateLogName(candidate)
	}
	return payload
}

// savePendingAIVisionCandidateAsync 在后台等待图片详情 AI 完整输出并入库。
// ctx 为请求上下文，task 为任务记录，candidate 为候选人结果，options 为启动参数。
func (r *Runner) savePendingAIVisionCandidateAsync(ctx context.Context, task localdb.Task, candidate map[string]any, options StartOptions) bool {
	raw := candidate[pendingAIVisionDecisionKey]
	resultCh, ok := raw.(<-chan pendingAIDecisionResult)
	if !ok || resultCh == nil {
		return false
	}
	delete(candidate, pendingAIVisionDecisionKey)
	payload := cloneCandidateForCloud(task, candidate)
	name := candidateLogName(candidate)
	r.taskLog(task.ID, "info", "AI 完整详情输出将后台同步简历："+name)
	go func() {
		select {
		case result := <-resultCh:
			if result.Err != nil {
				r.taskLog(task.ID, "warning", "AI 完整详情输出失败："+result.Err.Error())
				return
			}
			mergeVisionDecisionIntoCandidate(payload, result.Decision)
			r.taskLog(task.ID, "info", "AI 完整详情输出已合并："+name)
			saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
			defer cancel()
			r.saveCandidatePayload(saveCtx, task, payload, options)
		case <-ctx.Done():
			r.taskLog(task.ID, "warning", "等待 AI 完整详情输出被中断："+ctx.Err().Error())
		}
	}()
	return true
}

// saveCandidatePayload 将候选人入库 payload 同步到云端。
// ctx 为请求上下文，task 为任务记录，payload 为候选人 JSON，options 为启动参数。
func (r *Runner) saveCandidatePayload(ctx context.Context, task localdb.Task, payload map[string]any, options StartOptions) {
	if err := cloudapi.New(options.CloudAPIBase).SaveTaskCandidate(ctx, options.Token, task.ID, payload); err != nil {
		r.taskLog(task.ID, "warning", "候选人同步云端失败："+err.Error())
		return
	}
	r.taskLog(task.ID, "info", "候选人已同步云端："+candidateLogName(payload))
}

// mergeVisionDecisionIntoCandidate 合并图片详情 AI 的最终输出。
// candidate 为候选人结果，decision 为完整 AI 决策。
func mergeVisionDecisionIntoCandidate(candidate map[string]any, decision localai.Decision) {
	if text := strings.TrimSpace(decision.DetailText); text != "" {
		candidate["ai_vision_text"] = text
	}
	if len(decision.Usage) > 0 {
		candidate["ai_usage"] = decision.Usage
	}
	if decision.ElapsedMS > 0 {
		candidate["ai_elapsed_ms"] = decision.ElapsedMS
	}
	if decision.ResumeData != nil && len(decision.ResumeData) > 0 {
		for key, value := range decision.ResumeData {
			if _, exists := candidate[key]; !exists && value != nil {
				candidate[key] = value
			}
		}
	}
}

// buildTaskRuntimeSnapshot 在任务启动时集中读取云端运行配置。
// ctx 为请求上下文，client 为云端 API 客户端，task 为任务快照，options 为启动参数，totalRounds 为进度显示总轮次。
func (r *Runner) buildTaskRuntimeSnapshot(ctx context.Context, client *cloudapi.Client, task localdb.Task, options StartOptions, totalRounds int) (TaskRuntimeSnapshot, error) {
	taskID := task.ID
	if client == nil {
		return TaskRuntimeSnapshot{}, fmt.Errorf("云端客户端未初始化")
	}
	requiresAI := taskRequiresAI(task)
	r.taskLog(taskID, "info", "开始校验会员")
	subscription, err := client.FetchSubscription(ctx, options.Token)
	if err != nil {
		return TaskRuntimeSnapshot{}, fmt.Errorf("会员校验失败：%w", err)
	}
	if !boolFromMap(subscription, "active") {
		if requiresAI {
			return TaskRuntimeSnapshot{}, fmt.Errorf("会员已到期，当前任务使用了 AI 筛选或 AI 详情识别，请先订阅后再开始任务")
		}
		r.taskLog(taskID, "info", "当前为免费版，任务未使用会员功能，允许启动")
	} else {
		r.taskLog(taskID, "info", fmt.Sprintf("会员校验通过：member_type=%s expires_at=%s", stringFromMap(subscription, "member_type"), stringFromMap(subscription, "expires_at")))
	}
	if strings.TrimSpace(task.PositionID) != "" && len(task.PositionSnapshot) == 0 {
		return TaskRuntimeSnapshot{}, fmt.Errorf("云端岗位模板为空，任务无法启动")
	}

	r.updateProgress(taskID, Progress{Stage: "preferences", Message: "正在读取云端个人配置", TotalRounds: totalRounds})
	preferences, err := client.FetchUserPreferences(ctx, options.Token)
	if err != nil {
		return TaskRuntimeSnapshot{}, fmt.Errorf("读取云端个人配置失败：%w", err)
	}
	options = applyCloudPreferences(options, preferences)

	if requiresAI {
		r.updateProgress(taskID, Progress{Stage: "ai_config", Message: "正在读取云端 AI 配置", TotalRounds: totalRounds})
		aiConfig, err := client.FetchEffectiveAIConfig(ctx, options.Token)
		if err != nil {
			return TaskRuntimeSnapshot{}, fmt.Errorf("读取云端 AI 配置失败：%w", err)
		}
		options.AIConfig = aiConfigFromCloud(aiConfig)
		if err := validateAIConfig(options.AIConfig); err != nil {
			return TaskRuntimeSnapshot{}, err
		}
		r.taskLog(taskID, "info", fmt.Sprintf("云端 AI 配置读取成功：model=%s", options.AIConfig.Model))
	}

	r.updateProgress(taskID, Progress{Stage: "platform_config", Message: "正在读取平台配置", TotalRounds: totalRounds})
	platformID := strings.ToLower(strings.TrimSpace(task.PlatformID))
	if platformID == "" {
		platformID = "boss"
	}
	r.taskLog(taskID, "info", "开始读取云端平台配置：platform="+platformID)
	platformConfig, err := client.FetchPlatformConfig(ctx, platformID)
	if err != nil {
		return TaskRuntimeSnapshot{}, fmt.Errorf("读取云端平台配置失败：%w", err)
	}
	if len(platformConfig) == 0 {
		return TaskRuntimeSnapshot{}, fmt.Errorf("云端平台配置为空，任务无法启动")
	}
	r.taskLog(taskID, "info", "云端平台配置读取成功：platform="+platformID)

	return TaskRuntimeSnapshot{
		Task:           task,
		Options:        options,
		PlatformConfig: platformConfig,
		Preferences:    preferences,
		AIConfig:       options.AIConfig,
	}, nil
}

// localTaskSnapshotFromCloud 将云端任务转换为本地运行快照。
// task 为云端任务响应对象，返回可写入本地轻量任务表的字段。
func localTaskSnapshotFromCloud(task map[string]any) map[string]any {
	position := mapValue(task["position"])
	if len(position) == 0 {
		position = mapValue(task["position_snapshot"])
	}
	return map[string]any{
		"id":                  stringFromMap(task, "id"),
		"name":                stringFromMap(task, "name"),
		"platform_id":         stringFromMap(task, "platform_id"),
		"platform_account_id": stringFromMap(task, "platform_account_id"),
		"position_id":         stringFromMap(task, "position_id"),
		"mode":                stringFromMap(task, "mode"),
		"match_limit":         intFromMap(task, "match_limit"),
		"enable_sound":        boolFromMap(task, "enable_sound"),
		"enable_thinking":     boolFromMap(task, "enable_thinking"),
		"position_snapshot":   position,
	}
}

// localTaskStatusMap 将本地任务记录转换为状态接口返回 map。
// task 为本地任务记录。
func localTaskStatusMap(task localdb.Task) map[string]any {
	return map[string]any{
		"id":                  task.ID,
		"name":                task.Name,
		"platform_id":         task.PlatformID,
		"platform_account_id": task.PlatformAccountID,
		"position_id":         task.PositionID,
		"mode":                task.Mode,
		"match_limit":         task.MatchLimit,
		"status":              task.Status,
		"scanned_count":       task.ScannedCount,
		"greeted_count":       task.GreetedCount,
		"skipped_count":       task.SkippedCount,
		"failed_count":        task.FailedCount,
		"enable_sound":        task.EnableSound,
		"enable_thinking":     task.EnableThinking,
		"position_snapshot":   task.PositionSnapshot,
		"created_at":          task.CreatedAt,
		"updated_at":          task.UpdatedAt,
	}
}

// applyCloudPreferences 使用云端个人配置覆盖任务启动参数。
// options 为当前启动参数，preferences 为云端 /api/config/user-preferences 返回的配置。
func applyCloudPreferences(options StartOptions, preferences map[string]any) StartOptions {
	if len(preferences) == 0 {
		return options
	}
	options.ScrollDelayMin = intFromMapOr(preferences, "scroll_delay_min", options.ScrollDelayMin)
	options.ScrollDelayMax = intFromMapOr(preferences, "scroll_delay_max", options.ScrollDelayMax)
	options.ListViewDelayMin = floatFromMapOr(preferences, "list_view_delay_min", options.ListViewDelayMin)
	options.ListViewDelayMax = floatFromMapOr(preferences, "list_view_delay_max", options.ListViewDelayMax)
	options.DetailViewDelayMin = floatFromMapOr(preferences, "detail_view_delay_min", options.DetailViewDelayMin)
	options.DetailViewDelayMax = floatFromMapOr(preferences, "detail_view_delay_max", options.DetailViewDelayMax)
	if _, ok := preferences["detail_open_probability"]; ok {
		options.DetailOpenProbability = intFromMapOr(preferences, "detail_open_probability", options.DetailOpenProbability)
		options.detailOpenProbabilitySet = true
	}
	options.DetailOpenDelayMin = floatFromMapOr(preferences, "detail_open_delay_min", options.DetailOpenDelayMin)
	options.DetailOpenDelayMax = floatFromMapOr(preferences, "detail_open_delay_max", options.DetailOpenDelayMax)
	options.DetailCloseDelayMin = floatFromMapOr(preferences, "detail_close_delay_min", options.DetailCloseDelayMin)
	options.DetailCloseDelayMax = floatFromMapOr(preferences, "detail_close_delay_max", options.DetailCloseDelayMax)
	options.GreetBeforeDelayMin = floatFromMapOr(preferences, "greet_before_delay_min", options.GreetBeforeDelayMin)
	options.GreetBeforeDelayMax = floatFromMapOr(preferences, "greet_before_delay_max", options.GreetBeforeDelayMax)
	options.RestAfterCandidatesMin = intFromMapOr(preferences, "rest_after_candidates_min", options.RestAfterCandidatesMin)
	options.RestAfterCandidatesMax = intFromMapOr(preferences, "rest_after_candidates_max", options.RestAfterCandidatesMax)
	options.RestTimesMin = intFromMapOr(preferences, "rest_times_min", options.RestTimesMin)
	options.RestTimesMax = intFromMapOr(preferences, "rest_times_max", options.RestTimesMax)
	options.RestDurationMin = floatFromMapOr(preferences, "rest_duration_min", options.RestDurationMin)
	options.RestDurationMax = floatFromMapOr(preferences, "rest_duration_max", options.RestDurationMax)
	return options
}

// aiConfigFromCloud 将云端 AI 配置转换为本地 AI 客户端配置。
// config 为云端 /api/config/effective-ai 返回的配置。
func aiConfigFromCloud(config map[string]any) localdb.AIConfig {
	return localdb.AIConfig{
		ID:          "cloud",
		BaseURL:     stringFromMap(config, "base_url"),
		APIKey:      stringFromMap(config, "api_key"),
		Model:       stringFromMap(config, "model"),
		Temperature: floatFromMapOr(config, "temperature", 0.2),
		Timeout:     intFromMapOr(config, "timeout", 120),
		Extra:       mapValue(config["extra"]),
	}
}

// validateAIConfig 校验任务运行需要的 AI 配置是否完整。
// config 为云端下发的 AI 配置。
func validateAIConfig(config localdb.AIConfig) error {
	if strings.TrimSpace(config.BaseURL) == "" {
		return fmt.Errorf("请先在个人配置里填写云端 AI 接口地址")
	}
	if strings.TrimSpace(config.APIKey) == "" {
		return fmt.Errorf("请先在个人配置里填写云端 AI Key")
	}
	if strings.TrimSpace(config.Model) == "" {
		return fmt.Errorf("请先在个人配置里填写 AI 模型")
	}
	return nil
}

// taskRequiresAI 判断任务是否需要 AI 配置。
// task 为本地运行任务，AI 筛选或 AI 详情识别时返回 true。
func taskRequiresAI(task localdb.Task) bool {
	return taskMode(task) == "ai" || detailMode(task) == "ai"
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

// ensureTaskPageReady 确认当前页面和岗位与任务匹配。
// ctx 为请求上下文，task 为任务记录，platformConfig 为云端平台配置。
func (r *Runner) ensureTaskPageReady(ctx context.Context, task localdb.Task, platformRuntime platformcore.Runtime, exec platformExecutor, platformConfig cloudapi.PlatformConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.waitTaskEntryPage(ctx, task.ID, platformRuntime, exec, platformConfig); err != nil {
		return err
	}
	r.prepareEntryPage(ctx, task.ID, platformRuntime, exec, platformConfig)
	positionName := taskPositionName(task)
	if strings.TrimSpace(positionName) == "" {
		return fmt.Errorf("任务岗位名称为空，无法确认页面岗位")
	}
	currentName, err := r.waitCurrentPositionName(ctx, task.ID, platformRuntime, exec, platformConfig)
	if err != nil {
		return fmt.Errorf("获取页面当前岗位失败：%w", err)
	}
	if strings.Contains(normalizeTaskPositionName(currentName), normalizeTaskPositionName(positionName)) {
		r.taskLog(task.ID, "info", "页面岗位匹配："+currentName)
		return nil
	}
	r.taskLog(task.ID, "warning", fmt.Sprintf("页面岗位与任务岗位不一致，准备切换：页面=%s，任务=%s", currentName, positionName))
	if err := platformRuntime.SelectPosition(ctx, exec, platformConfig, positionName); err != nil {
		return fmt.Errorf("切换页面岗位失败：%w", err)
	}
	confirmedName, err := r.waitCurrentPositionName(ctx, task.ID, platformRuntime, exec, platformConfig)
	if err != nil {
		return fmt.Errorf("切换后确认页面岗位失败：%w", err)
	}
	if strings.Contains(normalizeTaskPositionName(confirmedName), normalizeTaskPositionName(positionName)) {
		r.taskLog(task.ID, "info", "页面岗位已切换为："+confirmedName)
		return nil
	}
	return fmt.Errorf("页面切换岗位失败，请手动操作后再点击开始。当前页面岗位=%s，任务岗位=%s", confirmedName, positionName)
}

// prepareEntryPage 调用平台入口页准备动作，失败时只记录日志不中断主流程。
// taskID 为任务 ID，platformRuntime 为平台实现，exec 为浏览器执行器，platformConfig 为云端平台配置。
func (r *Runner) prepareEntryPage(ctx context.Context, taskID string, platformRuntime platformcore.Runtime, exec platformExecutor, platformConfig cloudapi.PlatformConfig) {
	if err := ctx.Err(); err != nil {
		return
	}
	r.taskLog(taskID, "info", "正在执行平台入口页准备动作")
	if err := platformRuntime.PrepareEntryPage(ctx, exec, platformConfig); err != nil {
		r.taskLog(taskID, "warning", "平台入口页准备动作失败，继续主流程："+err.Error())
		return
	}
	r.taskLog(taskID, "info", "平台入口页准备动作完成")
}

// waitTaskEntryPage 等待当前页面加载到任务入口页。
// ctx 为请求上下文，taskID 为任务 ID，platformConfig 为平台配置。
func (r *Runner) waitTaskEntryPage(ctx context.Context, taskID string, platformRuntime platformcore.Runtime, exec platformExecutor, platformConfig cloudapi.PlatformConfig) error {
	attempts := pageEntryCheckAttempts
	if attempts <= 0 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		r.taskLog(taskID, "info", fmt.Sprintf("正在等待页面加载，第 %d/%d 次", attempt, attempts))
		if err := sleepWithContext(ctx, pageEntryCheckDelay); err != nil {
			return err
		}
		ok, err := platformRuntime.IsTaskEntryPage(ctx, exec, platformConfig)
		if err != nil {
			lastErr = err
			r.taskLog(taskID, "warning", fmt.Sprintf("检查当前页面失败，第 %d/%d 次：%s", attempt, attempts, err.Error()))
			continue
		}
		if ok {
			r.taskLog(taskID, "info", fmt.Sprintf("当前页面已确认，第 %d/%d 次检查成功", attempt, attempts))
			return nil
		}
		lastErr = fmt.Errorf("网页还没有加载到任务入口页")
	}
	if lastErr != nil {
		return fmt.Errorf("检查当前页面失败：%w", lastErr)
	}
	return fmt.Errorf("检查当前页面失败")
}

// waitCurrentPositionName 等待页面当前岗位名称可读取。
// ctx 为请求上下文，taskID 为任务 ID，platformConfig 为平台配置。
func (r *Runner) waitCurrentPositionName(ctx context.Context, taskID string, platformRuntime platformcore.Runtime, exec platformExecutor, platformConfig cloudapi.PlatformConfig) (string, error) {
	attempts := currentPositionCheckAttempts
	if attempts <= 0 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		r.taskLog(taskID, "info", fmt.Sprintf("正在读取页面当前岗位，第 %d/%d 次", attempt, attempts))
		if err := sleepWithContext(ctx, currentPositionCheckDelay); err != nil {
			return "", err
		}
		name, err := platformRuntime.CurrentPositionName(ctx, exec, platformConfig)
		if err == nil {
			return name, nil
		}
		lastErr = err
		r.taskLog(taskID, "warning", fmt.Sprintf("读取页面当前岗位失败，第 %d/%d 次：%s", attempt, attempts, err.Error()))
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("页面当前岗位为空")
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

// batchProcessResult 表示一批候选人的流水线处理结果。
type batchProcessResult struct {
	Saved   int
	Skipped int
	Greeted int
	Failed  int
}

// pendingAIDecisionResult 表示后台等待完整 AI 输出的结果。
type pendingAIDecisionResult struct {
	Decision localai.Decision
	Err      error
}

// candidatePipelineResult 表示单个候选人后台处理结果。
type candidatePipelineResult struct {
	Index          int
	Candidate      map[string]any
	Skipped        int
	Err            error
	DetailDecision *localai.Decision
}

// startCandidateDetailWorkers 启动候选人看详情评分并发处理池。
// workerCount 为并发数量，aiJobs 为待评分候选人队列，resultCh 为完成结果队列。
func (r *Runner) startCandidateDetailWorkers(ctx context.Context, task localdb.Task, exec platformExecutor, aiClient *localai.Client, aiJobs <-chan candidatePipelineResult, resultCh chan<- candidatePipelineResult, workerCount int) {
	if workerCount <= 0 {
		workerCount = 1
	}
	for i := 0; i < workerCount; i++ {
		go func() {
			for item := range aiJobs {
				if err := ctx.Err(); err != nil {
					item.Err = err
					resultCh <- item
					continue
				}
				showOverlay := item.Index == 0
				title := ""
				subtitle := ""
				if showOverlay {
					title = "AI 正在预分析"
					subtitle = candidateLogName(item.Candidate)
				}
				visibleClient, cleanup := r.aiClientForCall(ctx, exec, aiClient, title, subtitle, "正在判断是否值得打开详情")
				decision, err := r.scoreCandidateForDetail(ctx, task, item.Candidate, visibleClient)
				cleanup()
				if err == nil {
					item.DetailDecision = &decision
					if showOverlay {
						r.showAIReply(ctx, exec, title, subtitle, formatDetailDecisionReply(decision))
					}
				}
				item.Err = err
				resultCh <- item
			}
		}()
	}
}

// feedCandidatePipeline 按页面顺序把候选人送入看详情评分队列。
// needsAI 表示是否需要 AI 评分，aiJobs 为 AI 队列，resultCh 为最终结果队列。
func (r *Runner) feedCandidatePipeline(ctx context.Context, task localdb.Task, candidates []map[string]any, needsAI bool, aiJobs chan<- candidatePipelineResult, resultCh chan<- candidatePipelineResult) {
	if needsAI {
		defer close(aiJobs)
	}
	for index, candidate := range candidates {
		item := candidatePipelineResult{Index: index, Candidate: candidate}
		if err := ctx.Err(); err != nil {
			item.Err = err
			resultCh <- item
			return
		}
		if item.Err != nil || !needsAI || !canContinueCandidate(stringFromMap(candidate, "status")) {
			resultCh <- item
			continue
		}
		select {
		case aiJobs <- item:
		case <-ctx.Done():
			item.Err = ctx.Err()
			resultCh <- item
			return
		}
	}
}

// pipelineAIClient 创建流水线使用的 AI 客户端。
// task 为任务记录，options 为任务启动参数，只有 AI 模式或 AI 详情模式时才读取配置。
func (r *Runner) pipelineAIClient(task localdb.Task, options StartOptions) (*localai.Client, error) {
	if taskMode(task) != "ai" && detailMode(task) != "ai" {
		return nil, nil
	}
	config := options.AIConfig
	if err := validateAIConfig(config); err != nil {
		return nil, err
	}
	client := localai.New(config)
	client.EnableThinking = task.EnableThinking
	return client, nil
}

// consumeCandidateForGreet 按顺序消费一个候选人并执行打招呼。
// greetedSoFar 为任务已打招呼数量。
func (r *Runner) consumeCandidateForGreet(ctx context.Context, task localdb.Task, platformRuntime platformcore.Runtime, exec platformExecutor, platformConfig cloudapi.PlatformConfig, candidate map[string]any, greetedSoFar int, options StartOptions) (int, int, int, error) {
	status := stringFromMap(candidate, "status")
	if status != "passed" && status != "ai_passed" && status != "detail_fetched" {
		r.taskLog(task.ID, "info", fmt.Sprintf("跳过打招呼：name=%s status=%s", candidateLogName(candidate), status))
		return 0, 0, 0, nil
	}
	if task.MatchLimit > 0 && greetedSoFar >= task.MatchLimit {
		candidate["status"] = "skipped"
		candidate["skip_reason"] = "已达到任务打招呼上限"
		return 0, 0, 1, nil
	}
	// 打招呼前模拟人工点击延时
	if err := waitBeforeGreet(ctx, r, task.ID, options); err != nil {
		return 0, 0, 0, err
	}
	r.taskLog(task.ID, "info", fmt.Sprintf("准备打招呼：name=%s greeted_so_far=%d", candidateLogName(candidate), greetedSoFar))
	if err := r.tryGreet(ctx, platformRuntime, exec, platformConfig, candidate, options); err != nil {
		candidate["status"] = "failed"
		candidate["error"] = err.Error()
		r.taskLog(task.ID, "warning", "打招呼失败："+err.Error())
		return 0, 1, 0, nil
	}
	candidate["status"] = "greeted"
	candidate["greeted_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	r.taskLog(task.ID, "info", "打招呼成功："+candidateLogName(candidate))
	if task.EnableSound {
		r.playSound("success.wav", task.ID)
	}
	return 1, 0, 0, nil
}

// candidatePipelineConcurrency 返回候选人后台处理并发数。
// total 为本批候选人数量。
func candidatePipelineConcurrency(total int) int {
	if total <= 0 {
		return 1
	}
	if total < defaultCandidatePipelineConcurrency {
		return total
	}
	return defaultCandidatePipelineConcurrency
}

// reachedRunGreetLimit 判断本次运行是否已经达到打招呼上限。
// task 为任务配置，greeted 为本次运行已成功打招呼数量。
func reachedRunGreetLimit(task localdb.Task, greeted int) bool {
	return task.MatchLimit > 0 && greeted >= task.MatchLimit
}

// enrichCandidatesWithDetail 为候选人补充详情文本。
// ctx 为请求上下文，task 为任务记录，platformConfig 为云端平台配置，candidates 为候选人列表。
func (r *Runner) enrichCandidatesWithDetail(ctx context.Context, task localdb.Task, platformRuntime platformcore.Runtime, exec platformExecutor, platformConfig cloudapi.PlatformConfig, candidates []map[string]any, options StartOptions) (int, error) {
	skipped := 0
	mode := detailMode(task)
	if mode == "" {
		return 0, nil
	}
	var aiClient *localai.Client
	var err error
	if mode == "ai" {
		aiClient, err = r.pipelineAIClient(task, options)
		if err != nil {
			return 0, err
		}
	}
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return skipped, err
		}
		itemSkipped, err := r.enrichCandidateWithDetail(ctx, task, platformRuntime, exec, platformConfig, candidate, aiClient, options)
		if err != nil {
			return skipped, err
		}
		skipped += itemSkipped
	}
	return skipped, nil
}

// enrichCandidateWithDetail 为单个候选人补充详情文本。
// ctx 为请求上下文，candidate 为候选人，aiClient 为空时按需临时创建。
func (r *Runner) enrichCandidateWithDetail(ctx context.Context, task localdb.Task, platformRuntime platformcore.Runtime, exec platformExecutor, platformConfig cloudapi.PlatformConfig, candidate map[string]any, aiClient *localai.Client, options StartOptions) (int, error) {
	mode := detailMode(task)
	if mode == "" || !canContinueCandidate(stringFromMap(candidate, "status")) {
		return 0, nil
	}
	candidateName := candidateLogName(candidate)
	r.taskLog(task.ID, "info", fmt.Sprintf("准备读取候选人详情：name=%s detail_mode=%s", candidateName, detailModeLabel(mode)))
	// 打开详情前模拟人工点击延时
	if err := r.delayRandomRange(ctx, task.ID, "点击详情前", options.DetailOpenDelayMin, options.DetailOpenDelayMax); err != nil {
		r.taskLog(task.ID, "warning", "打开详情前延时被中断")
	}
	detailResult, err := platformRuntime.FetchCandidateDetail(ctx, exec, platformConfig, platformcore.Candidate(candidate), platformcore.DetailRequest{
		TaskID:         task.ID,
		Mode:           mode,
		ScreenshotsDir: r.screenshotsDir,
		Filename:       "detail-latest.png",
	})
	if err != nil {
		candidate["detail_error"] = err.Error()
		r.taskLog(task.ID, "warning", "读取候选人详情失败："+err.Error())
		if !r.isUserStopped(task.ID) {
			_ = platformRuntime.CloseCandidateDetail(context.WithoutCancel(ctx), exec, platformConfig, platformcore.Candidate(candidate))
		}
		// 浏览器未启动或已关闭的错误应该直接返回出去让整个任务停止
		if isBrowserClosedTaskError(err) {
			return 0, fmt.Errorf("浏览器未启动或已关闭，任务已自动结束：%w", err)
		}
		return 0, nil
	}
	defer func() {
		if r.isUserStopped(task.ID) {
			r.taskLog(task.ID, "info", "任务已被用户停止，跳过详情关闭动作")
			return
		}
		// 关闭详情前模拟人工浏览延时，然后再执行关闭
		_ = r.delayRandomRange(context.WithoutCancel(ctx), task.ID, "关闭详情前", options.DetailCloseDelayMin, options.DetailCloseDelayMax)
		if err := platformRuntime.CloseCandidateDetail(context.WithoutCancel(ctx), exec, platformConfig, platformcore.Candidate(candidate)); err != nil {
			r.taskLog(task.ID, "warning", "关闭"+candidateName+"详情失败："+err.Error())
		}
	}()
	r.taskLog(task.ID, "info", "详情提取接口返回成功："+candidateName)
	detailText := ""
	if mode == "dom" {
		detailText = strings.TrimSpace(detailResult.Text)
		candidate["detail_source"] = "dom"
	}
	if screenshot := detailResult.Screenshot; len(screenshot) > 0 {
		r.attachDetailScreenshot(candidate, screenshot)
		r.taskLog(task.ID, "info", fmt.Sprintf("详情截图已返回：name=%s path=%s", candidateName, firstNonEmptyString(stringFromMap(screenshot, "file_path"), stringFromMap(screenshot, "path"))))
		if mode == "ocr" {
			if taskMode(task) == "keyword" {
				r.showKeywordOCRLoadingOverlay(ctx, exec, task, candidate)
			} else {
				_, _ = exec.Post(context.WithoutCancel(ctx), "/api/v1/page/ai-overlay", map[string]any{
					"action":   "show",
					"title":    "AI 正在分析详情",
					"subtitle": candidateName,
					"message":  "OCR图文识别中...",
				})
			}
			ocrText, err := r.recognizeDetailScreenshot(ctx, screenshot)
			if err != nil {
				candidate["ocr_error"] = err.Error()
				r.taskLog(task.ID, "warning", "OCR 识别失败："+err.Error())
			} else {
				detailText = platformRuntime.CleanCandidateDetailText(ocrText)
				candidate["ocr_text"] = detailText
				candidate["detail_source"] = "ocr"
				r.taskLog(task.ID, "info", fmt.Sprintf("OCR 识别完成：name=%s length=%d", candidateName, len([]rune(detailText))))
				r.taskLog(task.ID, "info", fmt.Sprintf("OCR 识别内容：name=%s text=%s", candidateName, logTextPreview(detailText, 800)))
			}
		}
		if mode == "ai" {
			r.taskLog(task.ID, "info", "开始 AI 图片详情评分："+candidateName)
			visibleClient, cleanup := r.aiClientForCall(ctx, exec, aiClient, "AI 正在分析详情", candidateName, "正在识别详情长图并判断是否打招呼")
			decision, err := r.scoreDetailScreenshotWithClient(ctx, task, candidate, screenshot, visibleClient)
			cleanup()
			if err != nil {
				candidate["ai_vision_error"] = err.Error()
				r.taskLog(task.ID, "warning", "AI 图片详情评分失败："+err.Error())
			} else {
				r.showAIReply(ctx, exec, "AI 详情分析完成", candidateName, formatVisionDecisionReply(decision))
				detailText = platformRuntime.CleanCandidateDetailText(decision.DetailText)
				candidate["ai_vision_text"] = detailText
				candidate["detail_source"] = "ai"
				candidate["ai_greet_score"] = decision.Score
				candidate["ai_greet_reason"] = decision.Reason
				candidate["ai_greet_threshold"] = decision.Threshold
				candidate["ai_usage"] = decision.Usage
				candidate["ai_elapsed_ms"] = decision.ElapsedMS
				candidate["ai_greet_scored"] = true
				mergeVisionDecisionIntoCandidate(candidate, decision)
				if !decision.ShouldGreet {
					candidate["status"] = "skipped"
					candidate["skip_reason"] = fmt.Sprintf("AI评分低于阈值：%.1f/%.1f，%s", decision.Score, decision.Threshold, decision.Reason)
					r.taskLog(task.ID, "info", fmt.Sprintf("AI 图片详情评分未通过：name=%s score=%.1f threshold=%.1f", candidateName, decision.Score, decision.Threshold))
					return 1, nil
				}
				candidate["status"] = "ai_passed"
				r.taskLog(task.ID, "info", fmt.Sprintf("AI 图片详情评分通过：name=%s score=%.1f threshold=%.1f length=%d", candidateName, decision.Score, decision.Threshold, len([]rune(detailText))))
			}
		}
	} else if mode == "ai" {
		r.taskLog(task.ID, "warning", "详情模式为 AI，但详情截图为空，无法调用详情 AI："+candidateName)
	} else {
		r.taskLog(task.ID, "info", fmt.Sprintf("当前详情模式=%s，不调用图片详情 AI：%s", detailModeLabel(mode), candidateName))
	}
	detailText = platformRuntime.CleanCandidateDetailText(detailText)
	if detailText == "" {
		if mode == "ai" && stringFromMap(candidate, "status") == "ai_passed" {
			return 0, nil
		}
		candidate["status"] = "skipped"
		candidate["skip_reason"] = "详情文本为空"
		r.taskLog(task.ID, "warning", "候选人详情文本为空，已跳过："+candidateName)
		return 1, nil
	}
	candidate["detail_text"] = detailText
	candidate["filter_text"] = mergeText(stringFromMap(candidate, "filter_text"), detailText)
	candidate["raw_text"] = mergeText(stringFromMap(candidate, "raw_text"), detailText)
	candidate["status"] = "detail_fetched"
	r.taskLog(task.ID, "info", fmt.Sprintf("%s 详情已读取，模式=%s，长度=%d", candidateName, detailModeLabel(mode), len([]rune(detailText))))
	return 0, nil
}

// attachDetailScreenshot 将详情截图路径挂到候选人结果上，不再写入本地截图记录表。
// candidate 为候选人结果，screenshot 为 Worker 返回的截图信息。
func (r *Runner) attachDetailScreenshot(candidate map[string]any, screenshot map[string]any) {
	filePath := firstNonEmptyString(stringFromMap(screenshot, "file_path"), stringFromMap(screenshot, "path"))
	if filePath == "" {
		return
	}
	candidate["detail_screenshot"] = map[string]any{
		"file_path": filePath,
		"path":      filePath,
		"width":     screenshot["width"],
		"height":    screenshot["height"],
	}
}

// recognizeDetailScreenshot 使用本地 OCR 识别详情截图。
// ctx 为请求上下文，screenshot 为截图信息。
func (r *Runner) recognizeDetailScreenshot(ctx context.Context, screenshot map[string]any) (string, error) {
	if r.ocr == nil {
		return "", fmt.Errorf("OCR 组件未配置")
	}
	filePath := firstNonEmptyString(stringFromMap(screenshot, "file_path"), stringFromMap(screenshot, "path"))
	if filePath == "" {
		return "", fmt.Errorf("详情截图路径为空")
	}
	result, err := r.ocr.Recognize(ctx, filePath)
	if err != nil {
		return "", err
	}
	return result.Text, nil
}

// aiClientForCall 返回本次 AI 调用使用的客户端和清理函数。
// title 和 subtitle 为空时不会画浏览器浮层；找不到浏览器时也不会影响 AI 请求。
func (r *Runner) aiClientForCall(ctx context.Context, exec platformExecutor, client *localai.Client, title string, subtitle string, message string) (*localai.Client, func()) {
	if client == nil {
		return client, func() {}
	}
	title = strings.TrimSpace(title)
	subtitle = strings.TrimSpace(subtitle)
	if title == "" && subtitle == "" {
		return client, func() {}
	}
	if strings.TrimSpace(message) == "" {
		message = "正在等待 AI 返回结果"
	}
	steps := aiThinkingSteps(message)
	_, _ = exec.Post(ctx, "/api/v1/page/ai-overlay", map[string]any{
		"action":   "show",
		"title":    title,
		"subtitle": subtitle,
		"message":  steps[0],
	})
	done := make(chan struct{})
	thinkingCh := make(chan string, 100)
	go r.playAIThinking(ctx, exec, title, subtitle, steps, thinkingCh, done)

	streamingClient := client.WithProgress(func(text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		select {
		case thinkingCh <- text:
		default:
		}
	})

	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			close(done)
			// 不再主动隐藏浮层，由 JS show 端管理旧的卡片 5 秒后自动移除
		})
	}
	return streamingClient, cleanup
}

// showAIReply 在浏览器 AI 浮层里显示本次 AI 的最终回复。
// ctx 为请求上下文，exec 为 Worker 执行器，title、subtitle 和 reply 为展示文本。
func (r *Runner) showAIReply(ctx context.Context, exec platformExecutor, title string, subtitle string, reply string) {
	reply = strings.TrimSpace(reply)
	if reply == "" {
		reply = "AI 已完成分析"
	}
	_, _ = exec.Post(context.WithoutCancel(ctx), "/api/v1/page/ai-overlay", map[string]any{
		"action":   "show",
		"title":    title,
		"subtitle": subtitle,
		"message":  reply,
	})
}

// showKeywordMatchOverlay 在浏览器浮层中展示 OCR 关键词匹配结果。
// ctx 为请求上下文，exec 为 Worker 执行器，task 为任务记录，candidate 为候选人。
func (r *Runner) showKeywordMatchOverlay(ctx context.Context, exec platformExecutor, task localdb.Task, candidate map[string]any) {
	state := buildKeywordMatchState(task, candidate)
	_, _ = exec.Post(context.WithoutCancel(ctx), "/api/v1/page/keyword-overlay", map[string]any{
		"action":           "show",
		"title":            "关键词匹配",
		"subtitle":         candidateLogName(candidate),
		"keywords":         state.Keywords,
		"exclude_keywords": state.Excludes,
		"matched_keywords": state.Matched,
		"matched_excludes": state.Excluded,
		"text":             state.Text,
	})
}

// showKeywordOCRLoadingOverlay 在浏览器浮层中展示 OCR 识别等待状态。
// ctx 为请求上下文，exec 为 Worker 执行器，task 为任务记录，candidate 为候选人。
func (r *Runner) showKeywordOCRLoadingOverlay(ctx context.Context, exec platformExecutor, task localdb.Task, candidate map[string]any) {
	state := buildKeywordMatchState(task, candidate)
	_, _ = exec.Post(context.WithoutCancel(ctx), "/api/v1/page/keyword-overlay", map[string]any{
		"action":           "show",
		"title":            "关键词匹配",
		"subtitle":         candidateLogName(candidate),
		"keywords":         state.Keywords,
		"exclude_keywords": state.Excludes,
		"loading":          true,
		"text":             "OCR图文识别中...",
		"max_age_ms":       30000,
	})
}

// playAIThinking 周期性刷新浏览器里的 AI 思考步骤。
// ctx 为请求上下文，exec 为 Worker 执行器，steps 为要展示的思考过程。
func (r *Runner) playAIThinking(ctx context.Context, exec platformExecutor, title string, subtitle string, steps []string, thinkingCh <-chan string, done <-chan struct{}) {
	if len(steps) == 0 {
		return
	}
	ticker := time.NewTicker(1400 * time.Millisecond)
	defer ticker.Stop()
	index := 1
	streamingStarted := false
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case thinking := <-thinkingCh:
			// 标记已收到流式内容，后续 ticker 不再覆盖
			streamingStarted = true
			_, _ = exec.Post(context.WithoutCancel(ctx), "/api/v1/page/ai-overlay", map[string]any{
				"action":   "show",
				"title":    title,
				"subtitle": subtitle,
				"message":  thinking,
			})
		case <-ticker.C:
			// 收到真实流式内容后不再显示固定步骤
			if streamingStarted {
				continue
			}
			_, _ = exec.Post(context.WithoutCancel(ctx), "/api/v1/page/ai-overlay", map[string]any{
				"action":   "show",
				"title":    title,
				"subtitle": subtitle,
				"message":  steps[index%len(steps)],
			})
			index++
		}
	}
}

// aiThinkingSteps 返回 AI 等待时展示的思考过程。
// seed 为当前 AI 调用的基础说明。
func aiThinkingSteps(seed string) []string {
	base := strings.TrimSpace(seed)
	if base == "" {
		base = "正在分析候选人"
	}
	return []string{
		base,
		"正在读取岗位要求和硬性条件",
		"正在对比候选人经历、学历和技能",
		"正在判断是否达到当前任务阈值",
		"正在整理评分原因和下一步动作",
	}
}

// formatDetailDecisionReply 格式化“是否查看详情”的 AI 回复。
// decision 为 AI 决策结果。
func formatDetailDecisionReply(decision localai.Decision) string {
	action := "不打开详情"
	if decision.ShouldOpenDetail {
		action = "打开详情"
	}
	return fmt.Sprintf("AI 回复：%s\n评分：%.1f / %.1f\n原因：%s", action, decision.Score, decision.Threshold, firstNonEmptyString(decision.Reason, "AI未给出原因"))
}

// formatVisionDecisionReply 格式化详情图片 AI 回复。
// decision 为 AI 决策结果。
func formatVisionDecisionReply(decision localai.Decision) string {
	action := "不打招呼"
	if decision.ShouldGreet {
		action = "建议打招呼"
	}
	detail := strings.TrimSpace(decision.DetailText)
	if len([]rune(detail)) > 80 {
		detail = string([]rune(detail)[:80]) + "..."
	}
	if detail == "" {
		detail = "未返回详情摘要"
	}
	return fmt.Sprintf("AI 回复：%s\n评分：%.1f / %.1f\n原因：%s\n摘要：%s", action, decision.Score, decision.Threshold, firstNonEmptyString(decision.Reason, "AI未给出原因"), detail)
}

// formatGreetCandidateReply 格式化打招呼 AI 回复。
// candidate 为已写入 AI 评分字段的候选人。
func formatGreetCandidateReply(candidate map[string]any) string {
	action := "建议打招呼"
	status := stringFromMap(candidate, "status")
	if status == "skipped" {
		action = "不打招呼"
	}
	return fmt.Sprintf("AI 回复：%s\n评分：%.1f / %.1f\n原因：%s", action, floatFromMap(candidate, "ai_greet_score"), floatFromMap(candidate, "ai_greet_threshold"), firstNonEmptyString(stringFromMap(candidate, "ai_greet_reason"), stringFromMap(candidate, "skip_reason"), "AI未给出原因"))
}

// scoreDetailScreenshotWithClient 使用详情长图一次性完成识别和打招呼评分。
// ctx 为请求上下文，task 为任务记录，candidate 为候选人，screenshot 为拼接后的截图信息，client 为 AI 客户端。
func (r *Runner) scoreDetailScreenshotWithClient(ctx context.Context, task localdb.Task, candidate map[string]any, screenshot map[string]any, client *localai.Client) (localai.Decision, error) {
	if client == nil {
		return localai.Decision{}, fmt.Errorf("AI 客户端未配置")
	}
	filePath := firstNonEmptyString(stringFromMap(screenshot, "file_path"), stringFromMap(screenshot, "path"))
	if filePath == "" {
		return localai.Decision{}, fmt.Errorf("详情截图路径为空")
	}
	imageBytes, err := os.ReadFile(filePath)
	if err != nil {
		return localai.Decision{}, fmt.Errorf("读取详情截图失败：%w", err)
	}
	earlyCh := make(chan localai.Decision, 1)
	finalCh := make(chan pendingAIDecisionResult, 1)
	streamingClient := client.WithEarlyDecision(func(decision localai.Decision) {
		select {
		case earlyCh <- decision:
		default:
		}
	})
	go func() {
		decision, err := streamingClient.ScoreVisionForGreet(ctx, task.PositionSnapshot, candidate, imageBytes)
		finalCh <- pendingAIDecisionResult{Decision: decision, Err: err}
	}()
	select {
	case decision := <-earlyCh:
		candidate[pendingAIVisionDecisionKey] = (<-chan pendingAIDecisionResult)(finalCh)
		r.taskLog(task.ID, "info", fmt.Sprintf("AI 图片详情流式评分已提前解析：name=%s score=%.1f reason=%s", candidateLogName(candidate), decision.Score, decision.Reason))
		return decision, nil
	case final := <-finalCh:
		return final.Decision, final.Err
	case <-ctx.Done():
		return localai.Decision{}, ctx.Err()
	}
}

// scoreCandidateForDetail 使用本地 AI 给单个候选人计算看详情评分。
// ctx 为请求上下文，candidate 为候选人，client 为空时会返回配置错误。
func (r *Runner) scoreCandidateForDetail(ctx context.Context, task localdb.Task, candidate map[string]any, client *localai.Client) (localai.Decision, error) {
	status := stringFromMap(candidate, "status")
	if !canContinueCandidate(status) {
		return localai.Decision{Score: 0, Reason: "候选人状态不可继续", ShouldOpenDetail: false}, nil
	}
	if client == nil {
		return localai.Decision{}, fmt.Errorf("AI 客户端未配置")
	}
	decision, err := client.ScoreForDetail(ctx, task.PositionSnapshot, candidate)
	if err != nil {
		r.taskLog(task.ID, "warning", "看详情评分失败："+err.Error())
		return localai.Decision{}, err
	}
	return decision, nil
}

// finalizeCandidateGreetDecision 执行第二次详情分析后的最终打招呼判断。
// ctx 为请求上下文，task 为任务记录，exec 为浏览器执行器，candidate 为候选人，client 为 AI 客户端。
func (r *Runner) finalizeCandidateGreetDecision(ctx context.Context, task localdb.Task, exec platformExecutor, candidate map[string]any, client *localai.Client) (int, error) {
	if !canContinueCandidate(stringFromMap(candidate, "status")) {
		return 0, nil
	}
	if taskMode(task) == "keyword" {
		r.showKeywordMatchOverlay(ctx, exec, task, candidate)
		return r.applyKeywordGreetDecision(task, candidate), nil
	}
	visibleClient, cleanup := r.aiClientForCall(ctx, exec, client, "AI 正在评分", candidateLogName(candidate), "正在根据候选人详情判断是否适合打招呼")
	itemSkipped, err := r.scoreCandidate(ctx, task, candidate, visibleClient)
	cleanup()
	if err == nil {
		r.showAIReply(ctx, exec, "AI 评分完成", candidateLogName(candidate), formatGreetCandidateReply(candidate))
	}
	return itemSkipped, err
}

// applyKeywordGreetDecision 使用云端岗位模板关键词做最终打招呼判断。
// task 为任务记录，candidate 为已补充详情的候选人，返回本次是否跳过。
func (r *Runner) applyKeywordGreetDecision(task localdb.Task, candidate map[string]any) int {
	return applyKeywordGreetDecisionWithLog(task, candidate, func(message string) {
		r.taskLog(task.ID, "info", message)
	})
}

// applyKeywordGreetDecision 使用云端岗位模板关键词做最终打招呼判断。
// task 为任务记录，candidate 为已补充详情的候选人，logf 为空时不写日志。
func applyKeywordGreetDecision(task localdb.Task, candidate map[string]any) int {
	return applyKeywordGreetDecisionWithLog(task, candidate, nil)
}

// keywordMatchState 保存一次关键词匹配结果。
type keywordMatchState struct {
	Keywords []string
	Excludes []string
	Matched  []string
	Excluded []string
	Text     string
	AndMode  bool
}

// buildKeywordMatchState 汇总候选人文本并计算关键词命中情况。
// task 为任务记录，candidate 为候选人。
func buildKeywordMatchState(task localdb.Task, candidate map[string]any) keywordMatchState {
	keywords := stringListFromMap(task.PositionSnapshot, "keywords")
	excludes := stringListFromMap(task.PositionSnapshot, "exclude_keywords")
	text := strings.TrimSpace(strings.Join([]string{
		stringFromMap(candidate, "detail_text"),
		stringFromMap(candidate, "filter_text"),
		stringFromMap(candidate, "raw_text"),
		stringFromMap(candidate, "ocr_text"),
		stringFromMap(candidate, "ai_vision_text"),
	}, " "))
	lowerText := strings.ToLower(text)
	return keywordMatchState{
		Keywords: keywords,
		Excludes: excludes,
		Matched:  matchedWords(lowerText, keywords),
		Excluded: matchedWords(lowerText, excludes),
		Text:     text,
		AndMode:  boolFromMap(task.PositionSnapshot, "is_and_mode"),
	}
}

// applyKeywordGreetDecision 使用云端岗位模板关键词做最终打招呼判断。
// task 为任务记录，candidate 为已补充详情的候选人，logf 为空时不写日志。
func applyKeywordGreetDecisionWithLog(task localdb.Task, candidate map[string]any, logf func(string)) int {
	state := buildKeywordMatchState(task, candidate)
	if len(state.Excluded) > 0 {
		candidate["status"] = "skipped"
		candidate["skip_reason"] = "命中排除词：" + strings.Join(state.Excluded, "、")
		logKeywordDecision(logf, "详情关键词跳过", candidate, "命中排除词="+strings.Join(state.Excluded, "、"))
		return 1
	}
	if len(state.Keywords) > 0 && ((!state.AndMode && len(state.Matched) == 0) || (state.AndMode && len(state.Matched) < len(state.Keywords))) {
		candidate["status"] = "skipped"
		candidate["skip_reason"] = "详情未命中关键词"
		logKeywordDecision(logf, "详情关键词跳过", candidate, fmt.Sprintf("命中=%s 需要=%s", keywordListLabel(state.Matched), keywordListLabel(state.Keywords)))
		return 1
	}
	candidate["status"] = "passed"
	candidate["matched_keywords"] = state.Matched
	logKeywordDecision(logf, "详情关键词通过", candidate, "命中="+keywordListLabel(state.Matched))
	return 0
}

// scoreCandidate 使用本地 AI 给单个候选人评分。
// ctx 为请求上下文，candidate 为候选人，client 为空时会返回配置错误。
func (r *Runner) scoreCandidate(ctx context.Context, task localdb.Task, candidate map[string]any, client *localai.Client) (int, error) {
	status := stringFromMap(candidate, "status")
	if !canContinueCandidate(status) {
		return 0, nil
	}
	if client == nil {
		return 0, fmt.Errorf("AI 客户端未配置")
	}
	candidateName := candidateLogName(candidate)
	r.taskLog(task.ID, "info", "开始 AI 评分："+candidateName)
	decision, err := r.scoreCandidateForGreetWithEarlyReturn(ctx, task, candidate, client)
	if err != nil {
		r.taskLog(task.ID, "warning", "AI 评分失败："+err.Error())
		return 0, err
	}
	candidate["ai_greet_score"] = decision.Score
	candidate["ai_greet_reason"] = decision.Reason
	candidate["ai_greet_threshold"] = decision.Threshold
	candidate["ai_usage"] = decision.Usage
	candidate["ai_elapsed_ms"] = decision.ElapsedMS
	if !decision.ShouldGreet {
		candidate["status"] = "skipped"
		candidate["skip_reason"] = fmt.Sprintf("AI评分低于阈值：%.1f/%.1f，%s", decision.Score, decision.Threshold, decision.Reason)
		r.taskLog(task.ID, "info", fmt.Sprintf("AI 评分未通过：name=%s score=%.1f threshold=%.1f", candidateName, decision.Score, decision.Threshold))
		return 1, nil
	}
	candidate["status"] = "ai_passed"
	r.taskLog(task.ID, "info", fmt.Sprintf("AI 评分通过：name=%s score=%.1f threshold=%.1f", candidateName, decision.Score, decision.Threshold))
	return 0, nil
}

// scoreCandidateForGreetWithEarlyReturn 流式评分时提前返回已完整解析到的 score/reason。
// ctx 为请求上下文，task 为任务记录，candidate 为候选人，client 为 AI 客户端。
func (r *Runner) scoreCandidateForGreetWithEarlyReturn(ctx context.Context, task localdb.Task, candidate map[string]any, client *localai.Client) (localai.Decision, error) {
	type result struct {
		decision localai.Decision
		err      error
	}
	earlyCh := make(chan localai.Decision, 1)
	resultCh := make(chan result, 1)
	streamingClient := client.WithEarlyDecision(func(decision localai.Decision) {
		select {
		case earlyCh <- decision:
		default:
		}
	})
	go func() {
		decision, err := streamingClient.ScoreForGreet(ctx, task.PositionSnapshot, candidate)
		resultCh <- result{decision: decision, err: err}
	}()
	select {
	case decision := <-earlyCh:
		r.taskLog(task.ID, "info", fmt.Sprintf("AI 流式评分已提前解析：name=%s score=%.1f reason=%s", candidateLogName(candidate), decision.Score, decision.Reason))
		go func() {
			if final := <-resultCh; final.err != nil {
				r.taskLog(task.ID, "warning", "AI 完整评分输出结束失败："+final.err.Error())
			}
		}()
		return decision, nil
	case final := <-resultCh:
		return final.decision, final.err
	case <-ctx.Done():
		return localai.Decision{}, ctx.Err()
	}
}

// tryGreet 带重试地执行单个候选人打招呼。
// ctx 为请求上下文，platformConfig 为平台配置，candidate 为候选人。
func (r *Runner) tryGreet(ctx context.Context, platformRuntime platformcore.Runtime, exec platformExecutor, platformConfig cloudapi.PlatformConfig, candidate map[string]any, options StartOptions) error {
	retries := maxInt(0, options.GreetRetries)
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		log.Printf("[本地任务] level=info 准备调用打招呼接口 attempt=%d", attempt+1)
		err := platformRuntime.GreetCandidate(ctx, exec, platformConfig, platformcore.Candidate(candidate))
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
// r 为 Runner 实例，用于写任务日志。
func waitBeforeGreet(ctx context.Context, r *Runner, taskID string, options StartOptions) error {
	minDelay := options.GreetBeforeDelayMin
	maxDelay := options.GreetBeforeDelayMax
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
	if r != nil && taskID != "" {
		r.taskLog(taskID, "info", fmt.Sprintf("模拟人工操作：打招呼前，等待 %.1f 秒", delay))
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

// minInt 返回两个整数中的较小值。
// a 和 b 为参与比较的整数。
func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

// scanRounds 返回旧版扫描进度总数。
// options 为任务启动参数，保留该函数用于兼容前端旧进度字段。
func scanRounds(options StartOptions) int {
	if options.ScanRounds <= 0 {
		return defaultScanRounds
	}
	if options.ScanRounds > 20 {
		return 20
	}
	return options.ScanRounds
}

// emptyLoadLimit 返回连续未加载到新候选人的停止阈值。
// options 为任务启动参数，沿用 ScanRounds 字段作为空加载保护次数。
func emptyLoadLimit(options StartOptions) int {
	return scanRounds(options)
}

// maxItemsPerLoad 返回每次最多提取候选人数，0 表示读取当前 DOM 中全部候选人。
// options 为任务启动参数。
func maxItemsPerLoad(options StartOptions) int {
	if options.MaxItems <= 0 {
		return defaultMaxItemsPerRound
	}
	return options.MaxItems
}

// maxItemsPerRound 返回旧版每轮最多提取候选人数。
// options 为任务启动参数，保留该函数用于兼容旧测试和旧调用。
func maxItemsPerRound(options StartOptions) int {
	return maxItemsPerLoad(options)
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

// detailOpenProbability 返回关键词详情阶段的打开概率。
// options 为任务启动参数，未读取到个人配置时默认 100，避免旧任务突然不打开详情。
func detailOpenProbability(options StartOptions) int {
	if !options.detailOpenProbabilitySet && options.DetailOpenProbability <= 0 {
		return 100
	}
	if options.DetailOpenProbability < 0 {
		return 0
	}
	if options.DetailOpenProbability > 100 {
		return 100
	}
	return options.DetailOpenProbability
}

// shouldOpenDetailByProbability 判断本次是否按个人概率打开详情。
// options 为任务启动参数，返回 true 表示继续打开候选人详情。
func shouldOpenDetailByProbability(options StartOptions) bool {
	probability := detailOpenProbability(options)
	if probability >= 100 {
		return true
	}
	if probability <= 0 {
		return false
	}
	return rand.Intn(100) < probability
}

// randomFloatRange 从浮点范围中随机一个值。
// minValue 为最小值，maxValue 为最大值。
func randomFloatRange(minValue float64, maxValue float64) float64 {
	if minValue >= maxValue || maxValue <= 0 {
		return minValue
	}
	return minValue + float64(rand.Intn(int((maxValue-minValue)*100+1)))/100.0
}

// randomIntRange 从整数范围中随机一个值。
// minValue 为最小值，maxValue 为最大值。
func randomIntRange(minValue int, maxValue int) int {
	if minValue >= maxValue || maxValue <= 0 {
		return minValue
	}
	return minValue + rand.Intn(maxValue-minValue+1)
}

// delayRandomRange 随机等待指定范围秒数，写任务日志让前端可见。
// ctx 为运行上下文，taskID 为任务 ID，label 为动作名称，minSeconds 和 maxSeconds 为秒数范围。
// r 为 Runner，传 nil 时不写日志。
func (r *Runner) delayRandomRange(ctx context.Context, taskID string, label string, minSeconds float64, maxSeconds float64) error {
	if maxSeconds <= 0 {
		return nil
	}
	seconds := randomFloatRange(minSeconds, maxSeconds)
	if seconds <= 0 {
		return nil
	}
	if r != nil && taskID != "" {
		r.taskLog(taskID, "info", fmt.Sprintf("模拟人工操作：%s，等待 %.1f 秒", label, seconds))
	}
	return sleepWithContext(ctx, time.Duration(seconds*float64(time.Second)))
}

// randomScrollDistance 返回带随机抖动的滚动距离。
// options 为任务启动参数，默认围绕 720 像素上下随机，避免每轮滚动完全一致。
func randomScrollDistance(options StartOptions) int {
	base := scrollDistance(options)
	minDistance := maxInt(120, base-defaultScrollDistanceJitter)
	maxDistance := base + defaultScrollDistanceJitter
	if maxDistance <= minDistance {
		return minDistance
	}
	return minDistance + rand.Intn(maxDistance-minDistance+1)
}

// pageReadyDelay 返回提取候选人前等待页面稳定的时间。
// options 为任务启动参数。
func pageReadyDelay(options StartOptions) time.Duration {
	if options.PageReadyDelay > 0 {
		return time.Duration(options.PageReadyDelay) * time.Millisecond
	}
	return 5 * time.Second
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

// failStart 记录启动失败日志并清理运行锁，自动播放失败提示音和发送邮件通知。
// taskID 为任务 ID，msg 为失败原因，options 为本次任务启动参数。
func (r *Runner) failStart(taskID string, msg string, options StartOptions) {
	r.taskLog(taskID, "error", msg)
	_, _ = r.db.UpdateTaskStatus(taskID, "failed")
	r.clear(taskID)
	// 自动播放失败提示音（如果任务开启了提示音）
	if task, err := r.db.GetTask(taskID); err == nil && task.EnableSound {
		r.playSound("failed.wav", taskID)
	}
	r.sendTaskFailNotification(context.Background(), taskID, msg, options)
}

// isBrowserClosedTaskError 判断错误是否来自用户关闭浏览器。
// err 为任务执行中的错误。
func isBrowserClosedTaskError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	keywords := []string{
		"浏览器已关闭",
		"浏览器未启动",
		"target page, context or browser has been closed",
		"browser has been closed",
		"context closed",
		"target closed",
	}
	for _, keyword := range keywords {
		if strings.Contains(text, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

// taskLog 输出任务日志到命令行并写入本地任务日志。
// taskID 为任务 ID，level 为日志等级，msg 为日志内容。
func (r *Runner) taskLog(taskID string, level string, msg string) {
	taskID = strings.TrimSpace(taskID)
	level = strings.TrimSpace(level)
	msg = strings.TrimSpace(msg)
	if level == "" {
		level = "info"
	}
	if msg == "" {
		return
	}
	log.Printf("[本地任务] task=%s level=%s %s", taskID, level, msg)
	if r.db != nil && taskID != "" {
		_, _ = r.db.AddTaskLog(taskID, level, msg)
	}
}

// setRunning 标记任务正在运行。
// taskID 为任务 ID，cancel 为停止回调。
func (r *Runner) setRunning(taskID string, cancel context.CancelFunc) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.running[taskID]; ok {
		return false
	}
	delete(r.userStopped, taskID)
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
	if progress.Round > 0 {
		log.Printf("[本地任务] task=%s progress stage=%s round=%d/%d message=%s", taskID, progress.Stage, progress.Round, progress.TotalRounds, progress.Message)
		return
	}
	log.Printf("[本地任务] task=%s progress stage=%s message=%s", taskID, progress.Stage, progress.Message)
}

// incrementRunGreeted 增加当前任务本次运行已打招呼数量。
// taskID 为任务 ID，count 为本次新增打招呼数量。
func (r *Runner) incrementRunGreeted(taskID string, count int) {
	if count <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if state := r.running[strings.TrimSpace(taskID)]; state != nil {
		state.runGreeted += count
	}
}

// currentRunGreeted 返回当前任务本次运行已打招呼数量。
// taskID 为任务 ID。
func (r *Runner) currentRunGreeted(taskID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if state := r.running[strings.TrimSpace(taskID)]; state != nil {
		return state.runGreeted
	}
	return 0
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

// markUserStoppedAndCancel 标记用户主动停止并取消运行任务。
// taskID 为任务 ID，标记会保留到任务协程清理，供收尾动作判断是否应跳过页面操作。
func (r *Runner) markUserStoppedAndCancel(taskID string) {
	r.mu.Lock()
	state := r.running[taskID]
	delete(r.running, taskID)
	r.userStopped[taskID] = true
	r.mu.Unlock()
	if state != nil && state.cancel != nil {
		state.cancel()
	}
}

// isUserStopped 判断任务是否由用户主动停止。
// taskID 为任务 ID，返回 true 时后续收尾逻辑不应再操作浏览器页面。
func (r *Runner) isUserStopped(taskID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.userStopped[strings.TrimSpace(taskID)]
}

// clear 清理任务运行锁。
// taskID 为任务 ID。
func (r *Runner) clear(taskID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.running, taskID)
	delete(r.userStopped, taskID)
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
	if url := stringFromMap(platformEntryPage(platformConfig), "url"); url != "" {
		return url
	}
	if url := stringFromMap(platformConfig, "url"); url != "" {
		return url
	}
	return pageEntryURL(platformConfig)
}

// platformEntryPage 读取平台任务入口页配置。
// platformConfig 为云端平台配置。
func platformEntryPage(platformConfig cloudapi.PlatformConfig) map[string]any {
	if page := entryPageFromAny(mapFromAny(platformConfig["auth"])); len(page) > 0 {
		return page
	}
	if page := entryPageFromAny(platformConfig); len(page) > 0 {
		return page
	}
	if url := stringFromMap(platformConfig, "url"); url != "" {
		return map[string]any{"url": url}
	}
	return nil
}

// entryPageFromAny 从包含 pages 的对象中读取入口页。
// value 为配置对象。
func entryPageFromAny(value any) map[string]any {
	pages := pageList(value)
	if len(pages) == 0 {
		return nil
	}
	for _, page := range pages {
		if boolFromMap(page, "entry") && stringFromMap(page, "url") != "" {
			return page
		}
	}
	for _, page := range pages {
		if stringFromMap(page, "url") != "" {
			return page
		}
	}
	return nil
}

// pageEntryURL 从页面配置中读取入口地址。
// value 为包含 pages 的配置对象或 pages 数组，优先返回 entry=true 的页面。
func pageEntryURL(value any) string {
	pages := pageList(value)
	if len(pages) == 0 {
		return ""
	}
	for _, page := range pages {
		if boolFromMap(page, "entry") {
			if url := stringFromMap(page, "url"); url != "" {
				return url
			}
		}
	}
	for _, page := range pages {
		if url := stringFromMap(page, "url"); url != "" {
			return url
		}
	}
	return ""
}

// pageList 从平台配置对象或数组中读取 pages 列表。
// value 为配置对象或 pages 数组。
func pageList(value any) []map[string]any {
	if value == nil {
		return nil
	}
	if section, ok := value.(cloudapi.PlatformConfig); ok {
		value = section["pages"]
	}
	if section, ok := value.(map[string]any); ok {
		value = section["pages"]
	}
	if typedPages, ok := value.([]map[string]any); ok {
		return typedPages
	}
	pages, ok := value.([]any)
	if !ok || len(pages) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, len(pages))
	for _, item := range pages {
		if page, ok := item.(map[string]any); ok {
			result = append(result, page)
		}
	}
	return result
}

// taskPositionName 返回任务岗位名称。
// task 为任务记录。
func taskPositionName(task localdb.Task) string {
	return stringFromMap(task.PositionSnapshot, "name")
}

// normalizeTaskPositionName 规范化岗位名称用于比较。
// value 为原始岗位名称。
func normalizeTaskPositionName(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), "")
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
// candidates 为候选人列表，seen 为已见候选人 ID 集合，返回新增候选人和重复数量。
func freshCandidates(candidates []map[string]any, seen map[string]struct{}) ([]map[string]any, int) {
	result := []map[string]any{}
	duplicateCount := 0
	for _, candidate := range candidates {
		id := stringFromMap(candidate, "id")
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			duplicateCount++
			continue
		}
		seen[id] = struct{}{}
		result = append(result, candidate)
	}
	return result, duplicateCount
}

// candidateMaps 将平台候选人转换成主流程保存用 map。
// candidates 为平台 runtime 返回的候选人列表。
func candidateMaps(candidates []platformcore.Candidate) []map[string]any {
	result := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, map[string]any(candidate))
	}
	return result
}

// prepareCandidatesForFirstStage 处理第一次基础分析前的候选人队列。
// task 为任务记录，candidates 为候选人列表；有详情阶段时不在列表阶段做关键词终判。
func (r *Runner) prepareCandidatesForFirstStage(task localdb.Task, candidates []map[string]any) ([]map[string]any, int) {
	if taskMode(task) == "keyword" && !shouldFetchDetail(task) {
		return applyKeywordFilter(task, candidates, func(message string) {
			r.taskLog(task.ID, "info", message)
		})
	}
	return prepareCandidatesForFirstStage(task, candidates)
}

// prepareCandidatesForFirstStage 处理第一次基础分析前的候选人队列。
// task 为任务记录，candidates 为候选人列表；有详情阶段时不在列表阶段做关键词终判。
func prepareCandidatesForFirstStage(task localdb.Task, candidates []map[string]any) ([]map[string]any, int) {
	if taskMode(task) == "keyword" && !shouldFetchDetail(task) {
		return applyKeywordFilter(task, candidates, nil)
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(stringFromMap(candidate, "status")) == "" {
			candidate["status"] = "passed"
		}
	}
	return candidates, 0
}

// applyKeywordFilter 按任务岗位快照过滤候选人。
// task 为任务记录，candidates 为候选人列表，logf 为空时不写日志。
func applyKeywordFilter(task localdb.Task, candidates []map[string]any, logf func(string)) ([]map[string]any, int) {
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
			logKeywordDecision(logf, "列表关键词跳过", candidate, "命中排除词="+strings.Join(matched, "、"))
			skipped++
			continue
		}
		matched := matchedWords(text, keywords)
		if len(keywords) > 0 && ((!isAndMode && len(matched) == 0) || (isAndMode && len(matched) < len(keywords))) {
			candidate["status"] = "skipped"
			candidate["skip_reason"] = "未命中关键词"
			logKeywordDecision(logf, "列表关键词跳过", candidate, fmt.Sprintf("命中=%s 需要=%s", keywordListLabel(matched), keywordListLabel(keywords)))
			skipped++
			continue
		}
		candidate["status"] = "passed"
		candidate["matched_keywords"] = matched
		logKeywordDecision(logf, "列表关键词通过", candidate, "命中="+keywordListLabel(matched))
		result = append(result, candidate)
	}
	return result, skipped
}

// logKeywordDecision 写入关键词筛选日志。
// logf 为日志函数，candidate 为候选人，detail 为命中详情。
func logKeywordDecision(logf func(string), prefix string, candidate map[string]any, detail string) {
	if logf == nil {
		return
	}
	logf(fmt.Sprintf("%s：name=%s %s", prefix, candidateLogName(candidate), detail))
}

// keywordListLabel 返回关键词列表日志文案。
// words 为关键词列表，空列表返回“无”。
func keywordListLabel(words []string) string {
	if len(words) == 0 {
		return "无"
	}
	return strings.Join(words, "、")
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
		return cleanStringList(value)
	case []any:
		result := []string{}
		for _, raw := range value {
			if text, ok := raw.(string); ok && strings.TrimSpace(text) != "" {
				result = append(result, strings.TrimSpace(text))
			}
		}
		return cleanStringList(result)
	case string:
		return splitKeywordText(value)
	default:
		return []string{}
	}
}

// splitKeywordText 拆分关键词文本，兼容中文逗号、英文逗号、顿号、分号、空格和换行。
// text 为原始关键词文本。
func splitKeywordText(text string) []string {
	return cleanStringList(strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || r == '，' || r == '、' || r == ';' || r == '；' || unicode.IsSpace(r)
	}))
}

// cleanStringList 清理字符串数组里的空项和重复项。
// items 为原始字符串数组。
func cleanStringList(items []string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, item := range items {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
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

// firstStringFromAny 从任意数组中读取第一个字符串。
// value 为原始数组值。
func firstStringFromAny(value any) string {
	items, ok := value.([]any)
	if !ok {
		return ""
	}
	for _, item := range items {
		if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
			return text
		}
	}
	return ""
}

// candidateLogName 返回候选人日志展示名称。
// candidate 为候选人字段集合。
func candidateLogName(candidate map[string]any) string {
	return firstNonEmptyString(
		stringFromMap(candidate, "candidate_name"),
		stringFromMap(candidate, "name"),
		stringFromMap(candidate, "id"),
		"候选人",
	)
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

// logTextPreview 返回适合写入日志的文本摘要。
// text 为原始文本，limit 为最大字符数。
func logTextPreview(text string, limit int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "空"
	}
	lines := strings.FieldsFunc(text, func(r rune) bool {
		return r == '\r' || r == '\n' || r == '\t'
	})
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		if item := strings.TrimSpace(line); item != "" {
			parts = append(parts, item)
		}
	}
	preview := strings.Join(parts, " / ")
	if limit <= 0 {
		limit = 800
	}
	runes := []rune(preview)
	if len(runes) > limit {
		return string(runes[:limit]) + "..."
	}
	return preview
}

// shouldFetchDetail 判断任务是否需要读取候选人详情。
// task 为任务记录。
func shouldFetchDetail(task localdb.Task) bool {
	return detailMode(task) != ""
}

// detailMode 返回详情读取模式。
// task 为任务记录，支持 dom、ocr 和 ai。
func detailMode(task localdb.Task) string {
	commonConfig := mapValue(task.PositionSnapshot["common_config"])
	keywordConfig := mapValue(task.PositionSnapshot["keyword_config"])
	mode := strings.ToLower(firstNonEmptyString(
		stringFromMap(commonConfig, "detail_mode"),
		stringFromMap(keywordConfig, "detail_mode"),
	))
	if mode == "ocr" || mode == "dom" || mode == "ai" {
		return mode
	}
	if taskMode(task) == "ai" {
		return "dom"
	}
	return ""
}

// detailModeLabel 返回详情模式中文名称。
// mode 为详情模式标识。
func detailModeLabel(mode string) string {
	switch mode {
	case "dom":
		return "DOM"
	case "ocr":
		return "OCR"
	case "ai":
		return "AI"
	default:
		return "未知"
	}
}

// canContinueCandidate 判断候选人是否可以继续进入详情或 AI 阶段。
// status 为候选人当前状态。
func canContinueCandidate(status string) bool {
	status = strings.TrimSpace(status)
	return status == "" || status == "scanned" || status == "passed" || status == "detail_fetched" || status == "ai_passed"
}

// shouldSaveCandidateResult 判断候选人结果是否需要入库。
// status 为候选人当前状态，返回 true 表示该候选人是有效扫描结果。
func shouldSaveCandidateResult(status string) bool {
	status = strings.TrimSpace(status)
	return status == "scanned" || status == "passed" || status == "detail_fetched" || status == "ai_passed" || status == "greeted"
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

// intFromMapOr 从 map 中读取整数，空值使用默认值。
// item 为原始字典，key 为字段名，fallback 为默认值。
func intFromMapOr(item map[string]any, key string, fallback int) int {
	if item == nil {
		return fallback
	}
	if _, ok := item[key]; !ok {
		return fallback
	}
	value := intFromMap(item, key)
	if value == 0 {
		return fallback
	}
	return value
}

// floatFromMapOr 从 map 中读取浮点数，空值使用默认值。
// item 为原始字典，key 为字段名，fallback 为默认值。
func floatFromMapOr(item map[string]any, key string, fallback float64) float64 {
	if item == nil {
		return fallback
	}
	value, ok := item[key]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case json.Number:
		parsed, err := typed.Float64()
		if err == nil {
			return parsed
		}
	}
	return fallback
}

// floatFromMap 从 map 中读取浮点数。
// item 为原始字典，key 为字段名。
// float64Value 从任意值中读取 float64，为空时返回默认值。
func float64Value(value any, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case json.Number:
		if parsed, err := typed.Float64(); err == nil {
			return parsed
		}
	case string:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64); err == nil {
			return parsed
		}
	}
	return fallback
}

func floatFromMap(item map[string]any, key string) float64 {
	if item == nil {
		return 0
	}
	switch value := item[key].(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case json.Number:
		parsed, _ := value.Float64()
		return parsed
	case string:
		parsed, _ := strconv.ParseFloat(strings.TrimSpace(value), 64)
		return parsed
	default:
		return 0
	}
}

// playSound 播放提示音文件。
// soundName 为文件名（如 success.wav），taskID 为任务 ID（用于日志）。
func (r *Runner) playSound(soundName string, taskID string) {
	filePath := filepath.Join(r.audioDir, soundName)
	info, err := os.Stat(filePath)
	if err != nil || info.Size() == 0 {
		r.taskLog(taskID, "warning", "音频文件不存在或为空："+filePath)
		playCmd, cmdErr := fallbackSoundCommand()
		if cmdErr != nil {
			r.taskLog(taskID, "warning", "播放系统提示音失败："+cmdErr.Error())
			return
		}
		r.startSoundCommand(playCmd, taskID, "系统提示音")
		return
	}
	playCmd, err := soundPlayCommand(filePath)
	if err != nil {
		r.taskLog(taskID, "warning", "播放提示音失败："+err.Error())
		return
	}
	r.startSoundCommand(playCmd, taskID, soundName)
}

// startSoundCommand 启动提示音命令并记录启动后失败日志。
// cmd 为播放命令，taskID 为任务 ID，label 为提示音名称。
func (r *Runner) startSoundCommand(cmd *exec.Cmd, taskID string, label string) {
	hideCommandWindow(cmd)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		r.taskLog(taskID, "warning", "播放提示音失败："+err.Error())
		return
	}
	r.taskLog(taskID, "info", "提示音播放命令已启动："+label)
	// 非阻塞——不等待播放结束，避免卡主流程
	go func() {
		if err := cmd.Wait(); err != nil {
			detail := strings.TrimSpace(output.String())
			if detail != "" {
				r.taskLog(taskID, "warning", "提示音播放进程异常："+err.Error()+"，输出："+detail)
				return
			}
			r.taskLog(taskID, "warning", "提示音播放进程异常："+err.Error())
			return
		}
		r.taskLog(taskID, "info", "提示音播放成功："+label)
	}()
}

// soundPlayCommand 根据当前系统创建音频播放命令。
// filePath 为本地音频文件路径，返回可执行命令。
func soundPlayCommand(filePath string) (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("afplay"); err != nil {
			return nil, fmt.Errorf("系统未找到 afplay 播放器")
		}
		return exec.Command("afplay", filePath), nil
	case "windows":
		powershell, err := lookPathAny("powershell.exe", "powershell", "pwsh.exe", "pwsh")
		if err != nil {
			return nil, fmt.Errorf("系统未找到 PowerShell 播放器")
		}
		escapedPath := strings.ReplaceAll(filePath, "'", "''")
		script := fmt.Sprintf(`$player = New-Object System.Media.SoundPlayer; $player.SoundLocation = '%s'; $player.PlaySync()`, escapedPath)
		return exec.Command(powershell, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", powershellEncodedCommand(script)), nil
	default:
		if player, err := exec.LookPath("paplay"); err == nil {
			return exec.Command(player, filePath), nil
		}
		if player, err := exec.LookPath("aplay"); err == nil {
			return exec.Command(player, filePath), nil
		}
		return nil, fmt.Errorf("当前系统未找到可用音频播放器")
	}
}

// powershellEncodedCommand 将 PowerShell 脚本编码为 -EncodedCommand 需要的 UTF-16LE Base64。
// script 为待执行脚本，返回可直接传给 PowerShell 的编码字符串。
func powershellEncodedCommand(script string) string {
	encoded := utf16.Encode([]rune(script))
	data := make([]byte, 0, len(encoded)*2)
	for _, value := range encoded {
		data = append(data, byte(value), byte(value>>8))
	}
	return base64.StdEncoding.EncodeToString(data)
}

// fallbackSoundCommand 创建系统默认提示音命令。
// 当 success.wav/failed.wav 缺失时使用，保证用户仍能听到反馈。
func fallbackSoundCommand() (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		if player, err := exec.LookPath("osascript"); err == nil {
			return exec.Command(player, "-e", "beep 1"), nil
		}
		return nil, fmt.Errorf("系统未找到 osascript 播放器")
	case "windows":
		powershell, err := lookPathAny("powershell.exe", "powershell", "pwsh.exe", "pwsh")
		if err != nil {
			return nil, fmt.Errorf("系统未找到 PowerShell 播放器")
		}
		return exec.Command(powershell, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", "[console]::beep(880,180)"), nil
	default:
		return nil, fmt.Errorf("当前系统未配置默认提示音")
	}
}

// lookPathAny 返回第一个可用命令路径。
// names 为候选命令名称。
func lookPathAny(names ...string) (string, error) {
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("没有找到可用命令")
}

// taskBrowserViewport 返回任务启动浏览器时使用的窗口尺寸。
// 尺寸与本地账号打开入口保持同一套保守范围，避免任务窗口过大。
func taskBrowserViewport() (int, int) {
	screenWidth, screenHeight := taskCurrentScreenSize()
	if screenWidth <= 0 || screenHeight <= 0 {
		return 1100, 780
	}
	width := clampInt(int(float64(screenWidth)*0.75), 960, 1180)
	height := clampInt(int(float64(screenHeight)*0.78), 680, 820)
	if width > screenWidth-120 {
		width = screenWidth - 120
	}
	if height > screenHeight-120 {
		height = screenHeight - 120
	}
	return clampInt(width, 900, 1180), clampInt(height, 640, 820)
}

// taskCurrentScreenSize 读取当前主屏幕工作区尺寸。
// 读取失败时返回 0，由调用方使用默认尺寸。
func taskCurrentScreenSize() (int, int) {
	switch runtime.GOOS {
	case "darwin":
		if out, err := exec.Command("/bin/sh", "-c", `osascript -l JavaScript -e 'ObjC.import("AppKit"); const f=$.NSScreen.mainScreen.visibleFrame; console.log(Math.round(f.size.width)+","+Math.round(f.size.height));'`).Output(); err == nil {
			return parseScreenSize(string(out))
		}
	case "windows":
		powershell, err := lookPathAny("powershell.exe", "powershell", "pwsh.exe", "pwsh")
		if err != nil {
			return 0, 0
		}
		script := `Add-Type -AssemblyName System.Windows.Forms; $r=[System.Windows.Forms.Screen]::PrimaryScreen.WorkingArea; Write-Output "$($r.Width),$($r.Height)"`
		cmd := exec.Command(powershell, "-NoProfile", "-NonInteractive", "-Command", script)
		hideCommandWindow(cmd)
		if out, err := cmd.Output(); err == nil {
			return parseScreenSize(string(out))
		}
	}
	return 0, 0
}

// parseScreenSize 解析屏幕尺寸输出。
// value 格式为 宽,高，解析失败返回 0。
func parseScreenSize(value string) (int, int) {
	parts := strings.Split(strings.TrimSpace(value), ",")
	if len(parts) < 2 {
		return 0, 0
	}
	return parseLooseInt(parts[0]), parseLooseInt(parts[1])
}

// clampInt 将整数限制在指定范围内。
// value 为原始数值，min 和 max 为上下限。
func clampInt(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// parseLooseInt 从字符串中读取整数。
// value 为原始字符串，解析失败返回 0。
func parseLooseInt(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}

// sendTaskFailNotification 通知云端任务失败，由云端按任务 ID 查用户并发邮件。
// ctx 为请求上下文，taskID 为任务 ID，errorMsg 为失败原因，options 为本次任务启动参数。
func (r *Runner) sendTaskFailNotification(ctx context.Context, taskID string, errorMsg string, options StartOptions) {
	baseURL := strings.TrimSpace(options.CloudAPIBase)
	if baseURL == "" {
		baseURL = strings.TrimSpace(r.cloudAPIBase)
	}
	if baseURL == "" {
		baseURL = "https://goodhr5.58it.cn"
	}
	client := cloudapi.New(baseURL)
	if err := client.SendTaskFailNotice(ctx, options.Token, taskID, errorMsg); err != nil {
		r.taskLog(taskID, "warning", "发送失败邮件通知失败："+err.Error())
	}
}

// notifyCloudTaskStopped 通知云端任务已经停止或完成。
// taskID 为云端任务 ID，options 为本次启动参数。
func (r *Runner) notifyCloudTaskStopped(taskID string, options StartOptions) {
	token := strings.TrimSpace(options.Token)
	if token == "" {
		return
	}
	baseURL := strings.TrimSpace(options.CloudAPIBase)
	if baseURL == "" {
		baseURL = strings.TrimSpace(r.cloudAPIBase)
	}
	if baseURL == "" {
		baseURL = "https://goodhr5.58it.cn"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	client := cloudapi.New(baseURL)
	if err := client.StopTask(ctx, token, taskID); err != nil {
		r.taskLog(taskID, "warning", "同步云端任务停止状态失败："+err.Error())
	}
}
