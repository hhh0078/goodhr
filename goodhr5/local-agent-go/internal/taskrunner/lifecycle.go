// Package taskrunner 文件作用：按职责承载本地任务运行流程的拆分实现。
package taskrunner

import (
	"context"
	"errors"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/localdb"
	"goodhr5/local-agent-go/internal/power"
	"strings"
	"time"
)

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
	r.taskLog(taskID, "info", "任务启动：正在准备本地运行环境")
	r.taskLog(taskID, "info", fmt.Sprintf("任务启动：任务配置读取完成，平台=%s，岗位=%s，模式=%s，轮次=%d", task.PlatformID, taskPositionName(task), task.Mode, scanRounds(options)))
	runCtx, cancel := context.WithCancel(context.Background())
	if !r.setRunning(taskID, cancel, options) {
		cancel()
		return nil, fmt.Errorf("任务正在运行")
	}
	r.taskLog(taskID, "info", "任务启动：本地运行锁已创建")
	if err := r.ensurePowerProtection(taskID); err != nil {
		r.taskLog(taskID, "warning", "任务启动：防睡眠保护启动失败，错误="+err.Error())
	} else {
		r.taskLog(taskID, "info", "任务启动：防睡眠保护已开启")
	}
	totalRounds := scanRounds(options)
	r.updateProgress(taskID, Progress{Stage: "starting", Message: "任务准备启动", TotalRounds: totalRounds})
	// 保存通知邮箱到运行状态
	r.mu.Lock()
	if state, ok := r.running[taskID]; ok {
		state.emailForNotify = options.EmailForNotify
	}
	r.mu.Unlock()
	snapshot, err := r.buildTaskRuntimeSnapshot(ctx, client, task, options, totalRounds)
	if err != nil {
		cancel()
		r.failStart(taskID, err.Error(), options)
		return map[string]any{"task": taskStatusAfterStartFailure(r.db, taskID, task), "running": false}, err
	}
	task = snapshot.Task
	options = snapshot.Options
	options.EnableSound = task.EnableSound
	updated, err := r.db.UpdateTaskStatus(taskID, "running")
	if err != nil {
		r.clear(taskID)
		cancel()
		return nil, err
	}
	syncCtx, syncCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if syncErr := client.SyncTaskStatus(syncCtx, options.Token, taskID, "running"); syncErr != nil {
		r.taskLog(taskID, "warning", "任务启动：云端运行状态同步失败，错误="+syncErr.Error())
	}
	syncCancel()
	r.taskLog(taskID, "info", "任务启动：已进入后台运行")
	go r.runTask(runCtx, task, options, snapshot)
	return map[string]any{"task": updated, "running": true}, nil
}

// runTask 在后台执行本地任务主流程。
// ctx 为运行上下文，task 为任务记录，options 为启动参数。
func (r *Runner) runTask(ctx context.Context, task localdb.Task, options StartOptions, snapshot TaskRuntimeSnapshot) {
	taskID := task.ID
	defer r.clear(taskID)
	defer r.closePendingCandidateDetail(taskID)
	totalRounds := scanRounds(options)
	task = snapshot.Task
	options = snapshot.Options
	options.EnableSound = task.EnableSound
	r.updateRunOptions(taskID, options)
	r.initRestState(taskID, options)
	r.updateProgress(taskID, Progress{Stage: "running", Message: "任务已开始执行", TotalRounds: totalRounds})
	r.taskLog(taskID, "info", "任务启动：本地任务运行器已启动，准备进入扫描流程")
	scanResult, err := r.scanOnce(ctx, task, snapshot.PlatformConfig, options)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			if reason := r.cancelReason(taskID); reason != "" {
				r.updateProgress(taskID, Progress{Stage: "failed", Message: reason, TotalRounds: totalRounds})
				r.failStart(taskID, reason, options)
				return
			}
			r.updateProgress(taskID, Progress{Stage: "stopped", Message: "任务已停止", TotalRounds: totalRounds})
			_, _ = r.db.UpdateTaskStatus(taskID, "stopped")
			r.taskLog(taskID, "info", "任务停止：收到停止信号，正在同步云端停止状态")
			r.notifyCloudTaskStopped(taskID, options)
			return
		}
		if isBrowserClosedTaskError(err) {
			r.updateProgress(taskID, Progress{Stage: "stopped", Message: "浏览器已关闭，任务已自动结束", TotalRounds: totalRounds})
			_, _ = r.db.UpdateTaskStatus(taskID, "stopped")
			message := "浏览器已关闭，任务已自动结束：" + err.Error()
			r.taskLog(taskID, "error", "任务失败：环节=浏览器运行，错误="+message)
			r.sendTaskFailNotification(context.Background(), taskID, message, options)
			return
		}
		var authErr cloudapi.AuthExpiredError
		if errors.As(err, &authErr) {
			r.notifyCloudTaskStopped(taskID, options)
			return
		}
		r.failStart(taskID, "本地任务扫描失败："+err.Error(), options)
		return
	}
	if r.isUserStopped(taskID) {
		r.taskLog(taskID, "info", "任务停止：任务已被用户停止，忽略扫描完成结果")
		return
	}
	r.updateProgress(taskID, Progress{Stage: "completed", Message: "任务已完成", Round: totalRounds, TotalRounds: totalRounds})
	_, _ = r.db.UpdateTaskStatus(taskID, "completed")
	r.taskLog(taskID, "info", fmt.Sprintf("任务完成：本次运行结束，扫描=%d，打招呼=%d，跳过=%d，失败=%d", intFromMap(scanResult, "saved"), intFromMap(scanResult, "greeted"), intFromMap(scanResult, "skipped"), intFromMap(scanResult, "failed")))
	r.notifyCloudTaskCompleted(taskID, options)
}

// Stop 停止本地任务运行器。
// taskID 为任务 ID。
func (r *Runner) Stop(taskID string) (map[string]any, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("任务 ID 不能为空")
	}
	r.taskLog(taskID, "info", "任务停止：收到用户停止请求")
	r.markUserStopped(taskID)
	task, err := r.db.UpdateTaskStatus(taskID, "stopped")
	if err != nil {
		return nil, err
	}
	if r.hasRunningLock(taskID) {
		r.updateProgress(taskID, Progress{Stage: "running", Message: "正在处理当前候选人，处理完会停止"})
		r.taskLog(taskID, "info", "任务停止：正在等待当前候选人处理完成")
		if !r.waitUntilStopped(taskID, stopGracefulTimeout) {
			r.taskLog(taskID, "warning", fmt.Sprintf("任务停止：等待超时，已强制停止，超过=%s", stopGracefulTimeout.Round(time.Second)))
			r.markUserStoppedAndCancel(taskID)
			r.updateProgress(taskID, Progress{Stage: "stopped", Message: "停止等待超时，已强制停止"})
			_, _ = r.db.UpdateTaskStatus(taskID, "stopped")
		}
	}
	r.taskLog(taskID, "info", "任务停止：当前候选人处理完成，任务停止，浏览器保持打开")
	if latest, getErr := r.db.GetTask(taskID); getErr == nil {
		task = latest
	}
	return map[string]any{"task": localTaskStatusMap(task), "running": r.hasRunningLock(taskID)}, nil
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
	taskID = strings.TrimSpace(taskID)
	if r.userStopped[taskID] {
		return false
	}
	state := r.running[taskID]
	if state != nil && isTerminalStage(state.progress.Stage) {
		return false
	}
	ok := state != nil
	return ok
}

// hasRunningLock 判断任务运行锁是否还存在，不受用户停止标记影响。
func (r *Runner) hasRunningLock(taskID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.running[strings.TrimSpace(taskID)]
	return ok
}

// waitUntilStopped 等待任务运行锁释放，超时返回 false。
func (r *Runner) waitUntilStopped(taskID string, timeout time.Duration) bool {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(stopPollInterval)
	defer ticker.Stop()
	for {
		if !r.hasRunningLock(taskID) {
			return true
		}
		select {
		case <-deadline.C:
			return !r.hasRunningLock(taskID)
		case <-ticker.C:
		}
	}
}

// failStart 记录启动失败日志并清理运行锁，自动播放失败提示音和发送邮件通知。
// taskID 为任务 ID，msg 为失败原因，options 为本次任务启动参数。
func (r *Runner) failStart(taskID string, msg string, options StartOptions) {
	r.taskLog(taskID, "error", "任务失败：环节=任务运行，错误="+msg)
	_, _ = r.db.UpdateTaskStatus(taskID, "failed")
	r.clear(taskID)
	// 自动播放失败提示音（如果任务开启了提示音）
	if task, err := r.db.GetTask(taskID); err == nil && task.EnableSound {
		r.playSound("failed.wav", taskID)
	}
	r.sendTaskFailNotification(context.Background(), taskID, msg, options)
}

// taskStatusAfterStartFailure 返回启动失败后的最新任务状态。
// db 为本地数据库，taskID 为任务 ID，fallback 为读取失败时的兜底任务。
func taskStatusAfterStartFailure(db *localdb.DB, taskID string, fallback localdb.Task) localdb.Task {
	if db != nil {
		if task, err := db.GetTask(taskID); err == nil {
			return task
		}
	}
	fallback.Status = "failed"
	return fallback
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

// isFatalCandidateDetailError 判断错误是否表示候选人详情容器在规定时间内未出现。
// err 为详情读取过程中返回的错误。
func isFatalCandidateDetailError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "候选人详情没找到")
}

// ensurePowerProtection 确保运行任务期间系统不会自动睡眠。
// taskID 为当前任务 ID，失败时返回错误但不阻断任务。
func (r *Runner) ensurePowerProtection(taskID string) error {
	r.mu.Lock()
	if r.powerGuard != nil {
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()
	guard, err := power.PreventSleep("GoodHR 任务运行中")
	if err != nil {
		return err
	}
	r.mu.Lock()
	if r.powerGuard != nil {
		r.mu.Unlock()
		_ = guard.Stop()
		return nil
	}
	r.powerGuard = guard
	if r.sleepCancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		r.sleepCancel = cancel
		go r.monitorSleepResume(ctx)
	}
	r.mu.Unlock()
	return nil
}

// releasePowerProtectionIfIdle 在没有运行任务时释放防睡眠保护。
func (r *Runner) releasePowerProtectionIfIdle() {
	r.mu.Lock()
	if len(r.running) > 0 {
		r.mu.Unlock()
		return
	}
	guard := r.powerGuard
	cancel := r.sleepCancel
	r.powerGuard = nil
	r.sleepCancel = nil
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if guard != nil {
		_ = guard.Stop()
	}
}

// monitorSleepResume 检测电脑是否发生过睡眠/休眠恢复。
// ctx 结束时检测停止；发现时间断层后会取消正在运行的任务并让任务失败邮件接管通知。
func (r *Runner) monitorSleepResume(ctx context.Context) {
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
				r.cancelRunningTasksAfterSleep(gap)
			}
		}
	}
}

// cancelRunningTasksAfterSleep 在检测到疑似睡眠恢复后取消所有运行任务。
// gap 为检测到的时间断层，用于日志和邮件说明。
func (r *Runner) cancelRunningTasksAfterSleep(gap time.Duration) {
	reason := fmt.Sprintf("检测到电脑可能已休眠或息屏，任务已停止；心跳中断=%s", gap.Round(time.Second))
	r.mu.Lock()
	items := make(map[string]context.CancelFunc, len(r.running))
	for taskID, state := range r.running {
		if state == nil {
			continue
		}
		state.cancelReason = reason
		items[taskID] = state.cancel
	}
	r.mu.Unlock()
	for taskID, cancel := range items {
		r.taskLog(taskID, "error", "任务失败：环节=电脑休眠检测，错误="+reason)
		if cancel != nil {
			cancel()
		}
	}
}

// setRunning 标记任务正在运行。
// taskID 为任务 ID，cancel 为停止回调。
func (r *Runner) setRunning(taskID string, cancel context.CancelFunc, options StartOptions) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.running[taskID]; ok {
		return false
	}
	delete(r.userStopped, taskID)
	r.running[taskID] = &runState{cancel: cancel, options: options, progress: Progress{Stage: "starting", Message: "任务准备启动", TotalRounds: defaultScanRounds, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}}
	return true
}

// updateRunOptions 更新运行任务使用的启动参数。
// taskID 为任务 ID，options 为最新启动参数。
func (r *Runner) updateRunOptions(taskID string, options StartOptions) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if state := r.running[strings.TrimSpace(taskID)]; state != nil {
		state.options = options
	}
}

// cancelReason 返回任务被系统取消的原因。
// taskID 为任务 ID，返回空字符串表示不是系统原因取消。
func (r *Runner) cancelReason(taskID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if state := r.running[strings.TrimSpace(taskID)]; state != nil {
		return strings.TrimSpace(state.cancelReason)
	}
	return ""
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

// markUserStopped 标记任务收到停止请求，但不打断当前候选人处理。
// taskID 为任务 ID。
func (r *Runner) markUserStopped(taskID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.userStopped[taskID] = true
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
	r.releasePowerProtectionIfIdle()
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
	delete(r.running, taskID)
	delete(r.userStopped, taskID)
	r.mu.Unlock()
	r.releasePowerProtectionIfIdle()
}
