// Package lifecycle 管理任务启动前检查、运行锁、主流程分发、停止和最终状态。
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/flow/auto_reply"
	"goodhr5/local-agent-go-new/internal/flow/greeting"
	"goodhr5/local-agent-go-new/internal/flow/preflight"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/profile"
	"goodhr5/local-agent-go-new/internal/storage"
	"goodhr5/local-agent-go-new/internal/system/notification"
	"goodhr5/local-agent-go-new/internal/system/power"
)

const (
	sleepMonitorInterval = 30 * time.Second
	sleepResumeThreshold = 2 * time.Minute
)

// StartResult 表示任务通过启动前检查并被本地接收。
type StartResult struct {
	Task      storage.TaskRun        `json:"task"`
	Preflight []preflight.StepResult `json:"preflight"`
}

type activeTask struct {
	prepared  shared.PreparedTask
	state     storage.TaskRun
	cancel    context.CancelFunc
	done      chan struct{}
	stopped   bool
	interrupt error
}

// Runner 管理当前本地任务和两个独立主流程。
type Runner struct {
	mu       sync.Mutex
	active   map[string]*activeTask
	checker  *preflight.Checker
	greeting *greeting.Flow
	reply    *auto_reply.Flow
	store    *storage.Store
	profiles *profile.Manager
	power    *power.Guard
	cloud    *cloud.Client
	notifier *notification.Notifier
	logger   shared.Logger
}

// New 创建任务生命周期管理器。
func New(checker *preflight.Checker, greetingFlow *greeting.Flow, replyFlow *auto_reply.Flow, store *storage.Store, profiles *profile.Manager, powerGuard *power.Guard, cloudClient *cloud.Client, notifier *notification.Notifier, logger shared.Logger) *Runner {
	return &Runner{
		active: make(map[string]*activeTask), checker: checker,
		greeting: greetingFlow, reply: replyFlow, store: store,
		profiles: profiles, power: powerGuard, cloud: cloudClient,
		notifier: notifier, logger: logger,
	}
}

// StartTask 执行启动前检查、获取运行锁、保存状态并异步分发主流程。
func (r *Runner) StartTask(ctx context.Context, request shared.StartRequest) (StartResult, error) {
	preflightResult, err := r.checker.RunPreflightChecks(ctx, request)
	if err != nil {
		r.savePreflightFailure(ctx, request, preflightResult.Prepared, err)
		r.notifyStartFailure(preflightResult.Prepared, err)
		return StartResult{Preflight: preflightResult.Steps}, err
	}
	prepared := preflightResult.Prepared
	r.mu.Lock()
	if len(r.active) > 0 {
		r.mu.Unlock()
		return StartResult{Preflight: preflightResult.Steps}, fmt.Errorf("本地已经有任务在运行，请先停掉再开始新的")
	}
	if err := r.profiles.Acquire(prepared.Request.ProfileID, prepared.Request.TaskID); err != nil {
		r.mu.Unlock()
		return StartResult{Preflight: preflightResult.Steps}, err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	task := storage.TaskRun{
		TaskID: prepared.Request.TaskID, PositionID: prepared.Position.ID,
		PlatformID: prepared.Position.PlatformID, TaskType: prepared.Request.TaskType,
		Status: "running", CurrentStep: "dispatch_task_flow", StartedAt: time.Now().UTC(),
	}
	active := &activeTask{prepared: prepared, state: task, cancel: cancel, done: make(chan struct{})}
	r.active[task.TaskID] = active
	r.mu.Unlock()
	if err := r.store.SaveTask(ctx, task); err != nil {
		r.release(active)
		return StartResult{Preflight: preflightResult.Steps}, err
	}
	if err := r.cloud.RequestPositionStart(ctx, prepared.Request.Token, prepared.Position.ID, prepared.Request.TaskType); err != nil {
		task.Status = "failed"
		task.CurrentStep = "request_cloud_start"
		task.ErrorCode = "CLOUD_START_DENIED"
		var apiErr *cloud.APIError
		if errors.As(err, &apiErr) && strings.TrimSpace(apiErr.Code) != "" {
			task.ErrorCode = apiErr.Code
		}
		task.ErrorMessage = err.Error()
		task.Summary = "云端启动检查没有通过，本次任务没有启动"
		task.FinishedAt = time.Now().UTC()
		if saveErr := r.store.SaveTask(context.Background(), task); saveErr != nil {
			r.logNotification(task.TaskID, "save_running_sync_failure", saveErr)
		}
		r.release(active)
		return StartResult{Preflight: preflightResult.Steps}, err
	}
	if err := r.power.Start(); err != nil && r.logger != nil {
		r.logger.Step(task.TaskID, "lifecycle", "start_power_guard", "warning", time.Now(), err)
	}
	go r.monitorSleepResume(runCtx, active)
	go r.run(runCtx, active)
	return StartResult{Task: task, Preflight: preflightResult.Steps}, nil
}

// StopPosition 停止指定岗位当前正在运行的任务。
func (r *Runner) StopPosition(ctx context.Context, positionID string) (storage.TaskRun, error) {
	taskID := r.ActiveTaskIDForPosition(positionID)
	if taskID == "" {
		return r.store.LatestTaskForPosition(ctx, positionID)
	}
	return r.StopTask(ctx, taskID)
}

// ActiveTaskIDForPosition 返回指定岗位当前运行中的任务编号。
func (r *Runner) ActiveTaskIDForPosition(positionID string) string {
	positionID = strings.TrimSpace(positionID)
	r.mu.Lock()
	defer r.mu.Unlock()
	for taskID, active := range r.active {
		if active != nil && active.state.PositionID == positionID {
			return taskID
		}
	}
	return ""
}

// StopTask 请求当前任务在安全点停止并等待流程收尾。
func (r *Runner) StopTask(ctx context.Context, taskID string) (storage.TaskRun, error) {
	r.mu.Lock()
	active := r.active[taskID]
	if active == nil {
		r.mu.Unlock()
		return r.store.Task(ctx, taskID)
	}
	active.stopped = true
	active.cancel()
	done := active.done
	r.mu.Unlock()
	select {
	case <-ctx.Done():
		return storage.TaskRun{}, ctx.Err()
	case <-done:
		return r.store.Task(context.Background(), taskID)
	}
}

// TaskStatus 返回运行中内存状态或 SQLite 最终状态。
func (r *Runner) TaskStatus(ctx context.Context, taskID string) (storage.TaskRun, error) {
	return r.store.Task(ctx, taskID)
}

// HasActive 返回本地是否有任务正在使用唯一浏览器会话。
func (r *Runner) HasActive() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.active) > 0
}

// StopAll 停止程序退出时仍在运行的任务。
func (r *Runner) StopAll(ctx context.Context) {
	r.mu.Lock()
	tasks := make([]*activeTask, 0, len(r.active))
	for _, active := range r.active {
		active.stopped = true
		active.cancel()
		tasks = append(tasks, active)
	}
	r.mu.Unlock()
	for _, active := range tasks {
		select {
		case <-ctx.Done():
			return
		case <-active.done:
		}
	}
}

// run 获取平台运行时并按 task_type 分发独立主流程，并统一兜住 panic。
func (r *Runner) run(ctx context.Context, active *activeTask) {
	prepared := active.prepared
	ctx = client.WithTraceID(ctx, prepared.Request.TaskID)
	stats := shared.Stats{}
	runtime, err := platform.RuntimeFor(prepared.Position.PlatformID)
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf(
				"task_id=%s flow=lifecycle step=dispatch_task_flow status=panic error=%q stack=%q",
				prepared.Request.TaskID,
				fmt.Sprint(recovered),
				string(debug.Stack()),
			)
			err = panicTaskError(recovered)
		}
		r.finish(active, stats, err)
	}()
	if err != nil {
		return
	}
	switch prepared.Request.TaskType {
	case "greeting":
		stats, err = r.greeting.Run(ctx, prepared, runtime)
	case "auto_reply":
		stats, err = r.reply.Run(ctx, prepared, runtime)
	default:
		err = fmt.Errorf("不支持的任务类型 %s", prepared.Request.TaskType)
	}
}

// panicTaskError 把未捕获 panic 转成统一任务失败原因，交给生命周期正常收尾。
func panicTaskError(recovered any) error {
	return fmt.Errorf("任务流程发生未捕获异常：%v", recovered)
}

// finish 根据错误和用户停止标记保存唯一最终状态。
func (r *Runner) finish(active *activeTask, stats shared.Stats, runErr error) {
	active.cancel()
	r.mu.Lock()
	state := active.state
	if active.interrupt != nil {
		runErr = active.interrupt
	}
	if active.stopped || (errors.Is(runErr, context.Canceled) && active.interrupt == nil) || cloud.IsAuthExpired(runErr) {
		state.Status = "stopped"
		state.Summary = "任务已按你的要求停下来了"
	} else if runErr != nil {
		state.Status = "failed"
		state.ErrorCode = "TASK_FLOW_FAILED"
		state.ErrorMessage = runErr.Error()
		state.Summary = "我没处理成功，但问题不大，运行记录已经留好了"
	} else {
		state.Status = "completed"
		state.Summary = "任务已经处理完成"
	}
	state.CurrentStep = "finished"
	state.FinishedAt = time.Now().UTC()
	active.state = state
	delete(r.active, state.TaskID)
	r.mu.Unlock()
	if err := r.store.SaveTask(context.Background(), state); err != nil {
		r.logNotification(state.TaskID, "save_final_task", err)
	}
	r.notifyFinished(active.prepared, state, stats, runErr)
	r.profiles.Release(active.prepared.Request.ProfileID, state.TaskID)
	r.power.Stop()
	close(active.done)
}

// monitorSleepResume 检测任务期间的时间断层，疑似电脑休眠恢复时停止当前任务。
func (r *Runner) monitorSleepResume(ctx context.Context, active *activeTask) {
	ticker := time.NewTicker(sleepMonitorInterval)
	defer ticker.Stop()
	last := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			gap := now.Sub(last)
			last = now
			if gap > sleepResumeThreshold {
				r.interruptAfterSleep(active, gap)
				return
			}
		}
	}
}

// interruptAfterSleep 记录休眠恢复原因并取消任务，让统一生命周期按失败状态收尾。
func (r *Runner) interruptAfterSleep(active *activeTask, gap time.Duration) {
	reason := fmt.Errorf("检测到电脑可能已休眠或息屏，任务先停一下；心跳中断=%s", gap.Round(time.Second))
	r.mu.Lock()
	if current := r.active[active.state.TaskID]; current == active && !active.stopped {
		active.interrupt = reason
	}
	shouldCancel := active.interrupt != nil
	r.mu.Unlock()
	if shouldCancel {
		if r.logger != nil {
			r.logger.Step(active.state.TaskID, "lifecycle", "detect_sleep_resume", "failed", time.Now(), reason)
		}
		active.cancel()
	}
}

// notifyStartFailure 在岗位快照已经加载时播放失败音并发送启动失败邮件。
func (r *Runner) notifyStartFailure(prepared shared.PreparedTask, startErr error) {
	if strings.TrimSpace(prepared.Position.ID) == "" {
		return
	}
	r.notifyFailure(prepared, startErr, 0, 0)
}

// notifyFinished 根据最终状态统一同步统计、状态和结束通知。
func (r *Runner) notifyFinished(prepared shared.PreparedTask, state storage.TaskRun, stats shared.Stats, runErr error) {
	if r.cloud == nil || strings.TrimSpace(prepared.Position.ID) == "" {
		return
	}
	summary := cloud.TaskSummary{
		TaskID: prepared.Request.TaskID, PositionID: prepared.Position.ID,
		TaskType: prepared.Request.TaskType, Status: state.Status,
		Processed:       prepared.Position.ScannedCount + stats.Processed,
		Succeeded:       prepared.Position.GreetedCount + stats.Succeeded,
		Skipped:         prepared.Position.SkippedCount + stats.Skipped,
		Failed:          prepared.Position.FailedCount + stats.Failed,
		RunGreetedCount: stats.Succeeded, RunSkippedCount: stats.Skipped,
		ErrorCode: state.ErrorCode, ErrorMessage: state.ErrorMessage,
	}
	notifyCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	switch state.Status {
	case "completed":
		if err := r.cloud.SyncCompletedSummary(notifyCtx, prepared.Request.Token, summary); err != nil {
			r.logNotification(prepared.Request.TaskID, "sync_completed_status", err)
		}
	case "failed":
		if strings.EqualFold(prepared.Request.TaskType, "greeting") {
			if err := r.cloud.SyncPositionCounts(notifyCtx, prepared.Request.Token, summary); err != nil {
				r.logNotification(prepared.Request.TaskID, "sync_failed_counts", err)
			}
		}
		// /api/fail-notice 会在发送失败邮件前把云端岗位状态更新为 failed。
		r.notifyFailure(prepared, runErr, stats.Succeeded, stats.Skipped)
	case "stopped":
		if cloud.IsAuthExpired(runErr) {
			r.notifyFailure(prepared, fmt.Errorf("账号已在其他地方登录，当前任务已停止：%w", runErr), stats.Succeeded, stats.Skipped)
			return
		}
		if err := r.cloud.SyncSummary(notifyCtx, prepared.Request.Token, summary); err != nil {
			r.logNotification(prepared.Request.TaskID, "sync_stopped_status", err)
		}
	}
}

// notifyFailure 同步播放失败提示音并请求云端发送失败邮件。
func (r *Runner) notifyFailure(prepared shared.PreparedTask, failure error, runGreetedCount int, runSkippedCount int) {
	message := "任务执行失败"
	if failure != nil {
		message = failure.Error()
	}
	if prepared.Position.EnableSound && r.notifier != nil {
		soundCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := r.notifier.PlayFailure(soundCtx); err != nil {
			r.logNotification(prepared.Request.TaskID, "play_failure_sound", err)
		}
		cancel()
	}
	if r.cloud == nil || strings.TrimSpace(prepared.Request.Token) == "" || strings.TrimSpace(prepared.Position.ID) == "" {
		return
	}
	noticeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.cloud.SendFailNotice(noticeCtx, prepared.Request.Token, prepared.Position.ID, message, runGreetedCount, runSkippedCount); err != nil {
		r.logNotification(prepared.Request.TaskID, "send_failure_notice", err)
	}
}

// logNotification 记录不影响任务最终状态的通知错误。
func (r *Runner) logNotification(taskID string, step string, err error) {
	if r.logger != nil {
		r.logger.Step(taskID, "lifecycle", step, "warning", time.Now(), err)
	}
}

// release 处理任务尚未启动时的锁和上下文清理。
func (r *Runner) release(active *activeTask) {
	active.cancel()
	r.mu.Lock()
	delete(r.active, active.state.TaskID)
	r.mu.Unlock()
	r.profiles.Release(active.prepared.Request.ProfileID, active.state.TaskID)
	close(active.done)
}

// savePreflightFailure 保存未进入主流程的启动失败状态，并关联已经产生的启动日志。
func (r *Runner) savePreflightFailure(ctx context.Context, request shared.StartRequest, prepared shared.PreparedTask, failure error) {
	if strings.TrimSpace(request.TaskID) == "" || strings.TrimSpace(request.PositionID) == "" {
		return
	}
	if exists, err := r.store.TaskExists(ctx, request.TaskID); err != nil || exists {
		return
	}
	platformID := prepared.Position.PlatformID
	task := storage.TaskRun{
		TaskID: request.TaskID, PositionID: request.PositionID, PlatformID: platformID,
		TaskType: request.TaskType, Status: "failed", CurrentStep: "preflight",
		Summary:   "启动检查没有通过，我把原因记下来了",
		ErrorCode: "PREFLIGHT_FAILED", ErrorMessage: failure.Error(),
		StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC(),
	}
	_ = r.store.SaveTask(context.WithoutCancel(ctx), task)
}
