// Package taskrunner 负责管理 Go 版本本地任务启动、停止和运行锁。
package taskrunner

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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
	"goodhr5/local-agent-go/internal/ocr"
	"goodhr5/local-agent-go/internal/platformcore"
	"goodhr5/local-agent-go/internal/platforms"
)

const defaultScanRounds = 3
const defaultMaxItemsPerRound = 15
const defaultScrollDistance = 720
const defaultScrollDistanceJitter = 160
const defaultCandidatePipelineConcurrency = 5

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
	PageReadyDelay int
}

// New 创建本地任务运行器。
// db 为本地 SQLite 数据库，worker 为浏览器 Worker 管理器，profilesDir、downloadsDir 和 screenshotsDir 为本机浏览器目录。
func New(db *localdb.DB, worker BrowserWorker, ocr OCRRecognizer, profilesDir string, downloadsDir string, screenshotsDir string) *Runner {
	return &Runner{db: db, worker: worker, ocr: ocr, profilesDir: profilesDir, downloadsDir: downloadsDir, screenshotsDir: screenshotsDir, running: map[string]*runState{}}
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
	task, err := r.db.GetTask(taskID)
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
	r.taskLog(taskID, "info", "开始校验会员")
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
	r.taskLog(taskID, "info", fmt.Sprintf("会员校验通过：member_type=%s expires_at=%s", stringFromMap(subscription, "member_type"), stringFromMap(subscription, "expires_at")))
	r.updateProgress(taskID, Progress{Stage: "platform_config", Message: "正在读取平台配置", TotalRounds: totalRounds})
	platformID := strings.ToLower(strings.TrimSpace(task.PlatformID))
	if platformID == "" {
		platformID = "boss"
	}
	r.taskLog(taskID, "info", "开始读取云端平台配置：platform="+platformID)
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
	r.taskLog(taskID, "info", "云端平台配置读取成功：platform="+platformID)
	r.updateProgress(taskID, Progress{Stage: "running", Message: "任务已开始执行", TotalRounds: totalRounds})
	r.taskLog(taskID, "info", "本地任务运行器已启动，准备进入扫描流程")
	scanResult, err := r.scanOnce(ctx, task, platformConfig, options)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			r.updateProgress(taskID, Progress{Stage: "stopped", Message: "任务已停止", TotalRounds: totalRounds})
			_, _ = r.db.UpdateTaskStatus(taskID, "stopped")
			r.taskLog(taskID, "info", "本地任务收到停止信号")
			return
		}
		r.failStart(taskID, "本地任务扫描失败："+err.Error())
		return
	}
	r.updateProgress(taskID, Progress{Stage: "completed", Message: "任务已完成", Round: totalRounds, TotalRounds: totalRounds})
	_, _ = r.db.UpdateTaskStatus(taskID, "completed")
	r.taskLog(taskID, "info", fmt.Sprintf("后台任务已完成：%v", scanResult))
}

// Stop 停止本地任务运行器。
// taskID 为任务 ID。
func (r *Runner) Stop(taskID string) (map[string]any, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("任务 ID 不能为空")
	}
	r.taskLog(taskID, "info", "收到停止任务请求")
	r.cancel(taskID)
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r.taskLog(taskID, "info", "准备关闭浏览器")
	_, _ = r.worker.Call(stopCtx, "/api/v1/browser/stop", map[string]any{})
	task, err := r.db.UpdateTaskStatus(taskID, "stopped")
	if err != nil {
		return nil, err
	}
	r.taskLog(taskID, "info", "本地任务已停止")
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
	if _, err := r.worker.Call(ctx, "/api/v1/browser/start", map[string]any{
		"humanize":       true,
		"user_data_dir":  userDataDir,
		"downloads_path": r.browserDownloadDir(),
	}); err != nil {
		return nil, err
	}
	r.taskLog(task.ID, "info", "浏览器启动成功，准备打开入口页面")
	if err := platformRuntime.OpenEntryPage(ctx, exec, platformConfig, entryURL); err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	totalSaved := 0
	totalSkipped := 0
	totalGreeted := 0
	totalFailed := 0
	totalRounds := scanRounds(options)
	maxItems := maxItemsPerRound(options)
	for round := 1; round <= totalRounds; round++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// 2. 确认当前网页已经进入任务入口，并切到任务对应岗位。
		r.updateProgress(task.ID, Progress{Stage: "page_ready", Message: fmt.Sprintf("正在确认第 %d 轮页面和岗位", round), Round: round, TotalRounds: totalRounds})
		if err := r.waitTaskEntryPage(ctx, task.ID, platformRuntime, exec, platformConfig); err != nil {
			return nil, err
		}
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
		// 3. 读取当前屏幕可见候选人，并先做关键词过滤。
		r.updateProgress(task.ID, Progress{Stage: "extracting", Message: fmt.Sprintf("正在扫描第 %d 轮", round), Round: round, TotalRounds: totalRounds})
		r.taskLog(task.ID, "info", fmt.Sprintf("第 %d 轮开始提取候选人：max_items=%d", round, maxItems))
		platformCandidates, err := platformRuntime.ListVisibleCandidates(ctx, exec, platformConfig, maxItems)
		if err != nil {
			return nil, err
		}
		candidates := freshCandidates(candidateMaps(platformCandidates), seen)
		r.taskLog(task.ID, "info", fmt.Sprintf("第 %d 轮候选人提取完成：新候选人=%d", round, len(candidates)))
		if len(candidates) == 0 {
			r.taskLog(task.ID, "info", fmt.Sprintf("第 %d 轮未发现新候选人", round))
			break
		}
		filtered, skipped := applyKeywordFilter(task, candidates)
		totalSkipped += skipped
		r.taskLog(task.ID, "info", fmt.Sprintf("第 %d 轮关键词过滤完成：保留=%d 跳过=%d", round, len(filtered), skipped))
		if len(filtered) > 0 {
			r.updateProgress(task.ID, Progress{Stage: "pipeline", Message: fmt.Sprintf("正在并发处理第 %d 轮候选人", round), Round: round, TotalRounds: totalRounds})
			r.taskLog(task.ID, "info", fmt.Sprintf("第 %d 轮开始处理候选人流水线：数量=%d", round, len(filtered)))

			// 4. 并发做“是否值得看详情”的预评分，但主流程仍按页面顺序消费候选人。
			batchResult := batchProcessResult{}
			aiClient, err := r.pipelineAIClient(task)
			if err != nil {
				return nil, err
			}
			precheckCh := make(chan candidatePipelineResult, len(filtered))
			aiJobs := make(chan candidatePipelineResult, len(filtered))
			needsAI := taskMode(task) == "ai"
			if needsAI {
				workerCount := candidatePipelineConcurrency(len(filtered))
				r.taskLog(task.ID, "info", fmt.Sprintf("正在并发分析 %d 个候选人，并发数=%d", len(filtered), workerCount))
				r.startCandidateDetailWorkers(ctx, task, exec, aiClient, aiJobs, precheckCh, workerCount)
			}
			go r.feedCandidatePipeline(ctx, task, filtered, needsAI, aiJobs, precheckCh)

			pending := map[int]candidatePipelineResult{}
			nextIndex := 0
			for nextIndex < len(filtered) {
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
				r.taskLog(task.ID, "info", fmt.Sprintf("按页面顺序处理候选人：index=%d name=%s status=%s", item.Index, candidateLogName(candidate), stringFromMap(candidate, "status")))
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
						itemSkipped, err := r.enrichCandidateWithDetail(ctx, task, platformRuntime, exec, platformConfig, candidate, aiClient)
						batchResult.Skipped += itemSkipped
						if err != nil {
							return nil, err
						}
					}
				}

				// 6. 非 AI 主模式下，如果任务要求看详情，也按配置读取详情。
				if !needsAI && shouldFetchDetail(task) && canContinueCandidate(stringFromMap(candidate, "status")) {
					r.taskLog(task.ID, "info", fmt.Sprintf("准备读取候选人详情：index=%d name=%s", item.Index, candidateLogName(candidate)))
					itemSkipped, err := r.enrichCandidateWithDetail(ctx, task, platformRuntime, exec, platformConfig, candidate, aiClient)
					batchResult.Skipped += itemSkipped
					if err != nil {
						return nil, err
					}
				}

				// 7. AI 主模式下，详情没有一次性完成打招呼评分时，再做普通 AI 评分。
				if needsAI && canContinueCandidate(stringFromMap(candidate, "status")) && !boolFromMap(candidate, "ai_greet_scored") {
					visibleClient, cleanup := r.aiClientForCall(ctx, exec, aiClient, "AI 正在评分", candidateLogName(candidate), "正在判断是否适合打招呼")
					itemSkipped, err := r.scoreCandidate(ctx, task, candidate, visibleClient)
					cleanup()
					batchResult.Skipped += itemSkipped
					if err != nil {
						candidate["status"] = "failed"
						candidate["error"] = err.Error()
						batchResult.Failed++
						r.taskLog(task.ID, "warning", "打招呼评分失败："+err.Error())
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
				}

				if _, err := r.db.SaveCandidate(task.ID, candidate); err != nil {
					return nil, err
				}
				r.taskLog(task.ID, "info", fmt.Sprintf("候选人已保存：index=%d name=%s status=%s", item.Index, candidateLogName(candidate), stringFromMap(candidate, "status")))
				batchResult.Saved++
			}

			totalSaved += batchResult.Saved
			totalSkipped += batchResult.Skipped
			totalGreeted += batchResult.Greeted
			totalFailed += batchResult.Failed
			r.taskLog(task.ID, "info", fmt.Sprintf("第 %d 轮候选人流水线完成：保存=%d 跳过=%d 打招呼=%d 失败=%d", round, batchResult.Saved, batchResult.Skipped, batchResult.Greeted, batchResult.Failed))
		}
		if round < totalRounds {
			// 9. 本轮结束后滚动列表，进入下一轮候选人。
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			r.updateProgress(task.ID, Progress{Stage: "scrolling", Message: fmt.Sprintf("第 %d 轮完成，正在加载更多候选人", round), Round: round, TotalRounds: totalRounds})
			scrollDistance := randomScrollDistance(options)
			r.taskLog(task.ID, "info", fmt.Sprintf("第 %d 轮准备滚动候选人列表：distance=%d", round, scrollDistance))
			if err := platformRuntime.ScrollCandidateList(ctx, exec, platformConfig, scrollDistance); err != nil {
				r.taskLog(task.ID, "warning", "滚动候选人列表失败："+err.Error())
			} else {
				r.taskLog(task.ID, "info", fmt.Sprintf("第 %d 轮候选人列表滚动完成", round))
			}
		}
	}
	if totalSaved > 0 || totalSkipped > 0 {
		_, _ = r.db.IncrementTaskCounts(task.ID, totalSaved, totalGreeted, totalSkipped, totalFailed)
		r.taskLog(task.ID, "info", fmt.Sprintf("本次扫描保存 %d 个候选人，跳过 %d 个，打招呼 %d 个，失败 %d 个", totalSaved, totalSkipped, totalGreeted, totalFailed))
	} else {
		r.taskLog(task.ID, "warning", "当前页面未提取到可见候选人，请确认账号已登录且页面在推荐列表")
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

// ensureTaskPageReady 确认当前页面和岗位与任务匹配。
// ctx 为请求上下文，task 为任务记录，platformConfig 为云端平台配置。
func (r *Runner) ensureTaskPageReady(ctx context.Context, task localdb.Task, platformRuntime platformcore.Runtime, exec platformExecutor, platformConfig cloudapi.PlatformConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.waitTaskEntryPage(ctx, task.ID, platformRuntime, exec, platformConfig); err != nil {
		return err
	}
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
// task 为任务记录，只有 AI 模式或 AI 详情模式时才读取配置。
func (r *Runner) pipelineAIClient(task localdb.Task) (*localai.Client, error) {
	if taskMode(task) != "ai" && detailMode(task) != "ai" {
		return nil, nil
	}
	config, err := r.db.GetAIConfig()
	if err != nil {
		return nil, err
	}
	return localai.New(config), nil
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
	if err := waitBeforeGreet(ctx, options); err != nil {
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

// enrichCandidatesWithDetail 为候选人补充详情文本。
// ctx 为请求上下文，task 为任务记录，platformConfig 为云端平台配置，candidates 为候选人列表。
func (r *Runner) enrichCandidatesWithDetail(ctx context.Context, task localdb.Task, platformRuntime platformcore.Runtime, exec platformExecutor, platformConfig cloudapi.PlatformConfig, candidates []map[string]any) (int, error) {
	skipped := 0
	mode := detailMode(task)
	if mode == "" {
		return 0, nil
	}
	var aiClient *localai.Client
	var err error
	if mode == "ai" {
		aiClient, err = r.pipelineAIClient(task)
		if err != nil {
			return 0, err
		}
	}
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return skipped, err
		}
		itemSkipped, err := r.enrichCandidateWithDetail(ctx, task, platformRuntime, exec, platformConfig, candidate, aiClient)
		if err != nil {
			return skipped, err
		}
		skipped += itemSkipped
	}
	return skipped, nil
}

// enrichCandidateWithDetail 为单个候选人补充详情文本。
// ctx 为请求上下文，candidate 为候选人，aiClient 为空时按需临时创建。
func (r *Runner) enrichCandidateWithDetail(ctx context.Context, task localdb.Task, platformRuntime platformcore.Runtime, exec platformExecutor, platformConfig cloudapi.PlatformConfig, candidate map[string]any, aiClient *localai.Client) (int, error) {
	mode := detailMode(task)
	if mode == "" || !canContinueCandidate(stringFromMap(candidate, "status")) {
		return 0, nil
	}
	candidateName := candidateLogName(candidate)
	detailResult, err := platformRuntime.FetchCandidateDetail(ctx, exec, platformConfig, platformcore.Candidate(candidate), platformcore.DetailRequest{
		TaskID:         task.ID,
		Mode:           mode,
		ScreenshotsDir: r.screenshotsDir,
		Filename:       fmt.Sprintf("detail-%s.png", safePathName(stringFromMap(candidate, "id"))),
	})
	if err != nil {
		candidate["detail_error"] = err.Error()
		r.taskLog(task.ID, "warning", "读取候选人详情失败："+err.Error())
		_ = platformRuntime.CloseCandidateDetail(context.WithoutCancel(ctx), exec, platformConfig, platformcore.Candidate(candidate))
		return 0, nil
	}
	defer func() {
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
		r.saveDetailScreenshot(task.ID, candidate, screenshot)
		if mode == "ocr" {
			ocrText, err := r.recognizeDetailScreenshot(ctx, screenshot)
			if err != nil {
				candidate["ocr_error"] = err.Error()
				r.taskLog(task.ID, "warning", "OCR 识别失败："+err.Error())
			} else {
				detailText = strings.TrimSpace(ocrText)
				candidate["ocr_text"] = detailText
				candidate["detail_source"] = "ocr"
				r.taskLog(task.ID, "info", fmt.Sprintf("OCR 识别完成：name=%s length=%d", candidateName, len([]rune(detailText))))
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
				detailText = strings.TrimSpace(decision.DetailText)
				candidate["ai_vision_text"] = detailText
				candidate["detail_source"] = "ai"
				candidate["ai_greet_score"] = decision.Score
				candidate["ai_greet_reason"] = decision.Reason
				candidate["ai_greet_threshold"] = decision.Threshold
				candidate["ai_usage"] = decision.Usage
				candidate["ai_elapsed_ms"] = decision.ElapsedMS
				candidate["ai_greet_scored"] = true
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
	}
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

// analyzeDetailScreenshot 使用本地 AI 识别详情截图。
// ctx 为请求上下文，task 为任务记录，screenshot 为截图信息。
func (r *Runner) analyzeDetailScreenshot(ctx context.Context, task localdb.Task, screenshot map[string]any) (string, error) {
	config, err := r.db.GetAIConfig()
	if err != nil {
		return "", err
	}
	return r.analyzeDetailScreenshotWithClient(ctx, task, screenshot, localai.New(config))
}

// analyzeDetailScreenshotWithClient 使用指定 AI 客户端识别详情截图。
// ctx 为请求上下文，task 为任务记录，screenshot 为截图信息，client 为 AI 客户端。
func (r *Runner) analyzeDetailScreenshotWithClient(ctx context.Context, task localdb.Task, screenshot map[string]any, client *localai.Client) (string, error) {
	if client == nil {
		config, err := r.db.GetAIConfig()
		if err != nil {
			return "", err
		}
		client = localai.New(config)
	}
	filePath := firstNonEmptyString(stringFromMap(screenshot, "file_path"), stringFromMap(screenshot, "path"))
	if filePath == "" {
		return "", fmt.Errorf("详情截图路径为空")
	}
	imageBytes, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("读取详情截图失败：%w", err)
	}
	prompt := firstNonEmptyString(
		stringFromMap(mapValue(task.PositionSnapshot["ai_config"]), "open_detail_prompt"),
		"请识别图片中的候选人详情文字，保留学历、经验、技能、求职意向等关键信息，输出中文文本。",
	)
	content := []map[string]any{
		{"type": "text", "text": prompt},
		{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageBytes)}},
	}
	result, err := client.Chat(ctx, map[string]any{
		"messages":    []map[string]any{{"role": "user", "content": content}},
		"temperature": 0.1,
	})
	if err != nil {
		return "", err
	}
	return result.Content, nil
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
	_, _ = exec.Post(ctx, "/api/v1/page/ai-overlay", map[string]any{
		"action":   "show",
		"title":    title,
		"subtitle": subtitle,
		"message":  message,
	})
	progressClient := client.WithProgress(func(text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		_, _ = exec.Post(context.WithoutCancel(ctx), "/api/v1/page/ai-overlay", map[string]any{
			"action":   "update",
			"title":    title,
			"subtitle": subtitle,
			"message":  text,
		})
	})
	cleanup := func() {
		_, _ = exec.Post(context.WithoutCancel(ctx), "/api/v1/page/ai-overlay", map[string]any{"action": "hide"})
	}
	return progressClient, cleanup
}

// scoreDetailScreenshotWithClient 使用详情长图一次性完成识别和打招呼评分。
// ctx 为请求上下文，task 为任务记录，candidate 为候选人，screenshot 为拼接后的截图信息，client 为 AI 客户端。
func (r *Runner) scoreDetailScreenshotWithClient(ctx context.Context, task localdb.Task, candidate map[string]any, screenshot map[string]any, client *localai.Client) (localai.Decision, error) {
	if client == nil {
		config, err := r.db.GetAIConfig()
		if err != nil {
			return localai.Decision{}, err
		}
		client = localai.New(config)
	}
	filePath := firstNonEmptyString(stringFromMap(screenshot, "file_path"), stringFromMap(screenshot, "path"))
	if filePath == "" {
		return localai.Decision{}, fmt.Errorf("详情截图路径为空")
	}
	imageBytes, err := os.ReadFile(filePath)
	if err != nil {
		return localai.Decision{}, fmt.Errorf("读取详情截图失败：%w", err)
	}
	return client.ScoreVisionForGreet(ctx, task.PositionSnapshot, candidate, imageBytes)
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
		itemSkipped, err := r.scoreCandidate(ctx, task, candidate, client)
		if err != nil {
			return nil, skipped, err
		}
		skipped += itemSkipped
		result = append(result, candidate)
	}
	return result, skipped, nil
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
	decision, err := client.ScoreForGreet(ctx, task.PositionSnapshot, candidate)
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

// minInt 返回两个整数中的较小值。
// a 和 b 为参与比较的整数。
func minInt(a int, b int) int {
	if a < b {
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

// failStart 记录启动失败日志并清理运行锁。
// taskID 为任务 ID，msg 为失败原因。
func (r *Runner) failStart(taskID string, msg string) {
	r.taskLog(taskID, "error", msg)
	_, _ = r.db.UpdateTaskStatus(taskID, "failed")
	r.clear(taskID)
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
	log.Printf("[本地任务] task=%s progress stage=%s round=%d/%d message=%s", taskID, progress.Stage, progress.Round, progress.TotalRounds, progress.Message)
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

// candidateMaps 将平台候选人转换成主流程保存用 map。
// candidates 为平台 runtime 返回的候选人列表。
func candidateMaps(candidates []platformcore.Candidate) []map[string]any {
	result := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, map[string]any(candidate))
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
