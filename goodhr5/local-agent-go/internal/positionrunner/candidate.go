// Package positionrunner 文件作用：按职责承载本地岗位运行运行流程的拆分实现。
package positionrunner

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/localdb"
	"goodhr5/local-agent-go/internal/platformcore"
	"math/rand"
	"strings"
	"time"
)

// consumeCandidateForGreet 按顺序消费一个候选人并执行打招呼。
// greetedSoFar 为岗位运行已打招呼数量。
func (r *Runner) consumeCandidateForGreet(ctx context.Context, position localdb.Position, platformRuntime platformcore.Runtime, exec platformExecutor, platformConfig cloudapi.PlatformConfig, candidate map[string]any, greetedSoFar int, options StartOptions) (int, int, int, error) {
	status := stringFromMap(candidate, "status")
	if status != "passed" && status != "ai_passed" && status != "detail_fetched" {
		r.positionLog(position.ID, "info", fmt.Sprintf("打招呼执行：跳过，候选人=%s，状态=%s", candidateLogName(candidate), status))
		return 0, 0, 0, nil
	}
	if position.MatchLimit > 0 && greetedSoFar >= position.MatchLimit {
		candidate["status"] = "skipped"
		candidate["skip_reason"] = "已达到岗位运行打招呼上限"
		return 0, 0, 1, nil
	}
	// 打招呼前模拟人工点击延时
	if err := waitBeforeGreet(ctx, r, position.ID, options); err != nil {
		return 0, 0, 0, err
	}
	r.positionLog(position.ID, "info", fmt.Sprintf("打招呼执行：准备执行，候选人=%s，已打招呼=%d", candidateLogName(candidate), greetedSoFar))
	if err := r.tryGreet(ctx, position.ID, platformRuntime, exec, platformConfig, candidate, options); err != nil {
		candidate["status"] = "failed"
		candidate["error"] = err.Error()
		r.positionLog(position.ID, "warning", fmt.Sprintf("打招呼执行：失败，候选人=%s，错误=%s", candidateLogName(candidate), err.Error()))
		return 0, 1, 0, &candidateOperationError{Operation: "执行打招呼", Err: err}
	}
	candidate["status"] = "greeted"
	candidate["greeted_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	r.positionLog(position.ID, "info", "打招呼执行：成功，候选人="+candidateLogName(candidate))
	if position.EnableSound {
		r.playSound("success.wav", position.ID)
	}
	return 1, 0, 0, nil
}

// tryGreet 带重试地执行单个候选人打招呼。
// ctx 为请求上下文，platformConfig 为平台配置，candidate 为候选人。
func (r *Runner) tryGreet(ctx context.Context, positionID string, platformRuntime platformcore.Runtime, exec platformExecutor, platformConfig cloudapi.PlatformConfig, candidate map[string]any, options StartOptions) error {
	retries := maxInt(0, options.GreetRetries)
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		r.positionLog(positionID, "info", fmt.Sprintf("打招呼执行：准备调用平台接口，第%d次", attempt+1))
		err := r.withOperationTimeout(ctx, positionID, candidateLogName(candidate), fmt.Sprintf("调用打招呼接口第%d次", attempt+1), greetActionTimeout, func(greetCtx context.Context) error {
			return platformRuntime.GreetCandidate(greetCtx, exec, platformConfig, platformcore.Candidate(candidate))
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
// ctx 为请求上下文，options 为岗位运行启动参数。
// r 为 Runner 实例，用于写岗位运行日志。
func waitBeforeGreet(ctx context.Context, r *Runner, positionID string, options StartOptions) error {
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
	if r != nil && positionID != "" {
		r.positionLog(positionID, "info", fmt.Sprintf("模拟人工操作：打招呼前，等待 %.1f 秒", delay))
	}
	return sleepWithContext(ctx, time.Duration(delay*float64(time.Second)))
}

// initRestState 初始化本次岗位运行的模拟休息计划。
// positionID 为岗位运行 ID，options 为岗位运行启动参数。
func (r *Runner) initRestState(positionID string, options StartOptions) {
	maxTimes := randomIntRange(options.RestTimesMin, options.RestTimesMax)
	nextAfter := randomIntRange(options.RestAfterCandidatesMin, options.RestAfterCandidatesMax)
	if maxTimes <= 0 || nextAfter <= 0 || options.RestDurationMax <= 0 {
		return
	}
	r.mu.Lock()
	state := r.running[positionID]
	if state != nil {
		state.restMaxTimes = maxTimes
		state.restUsed = 0
		state.restNextAfter = nextAfter
		state.restSinceLast = 0
	}
	r.mu.Unlock()
	r.positionLog(positionID, "info", fmt.Sprintf("模拟休息已启用：最多休息 %d 次，首次约处理 %d 人后休息", maxTimes, nextAfter))
}

// maybeRestAfterCandidate 在候选人处理后按计划模拟休息。
// ctx 为岗位运行上下文，positionID 为岗位运行 ID，options 为岗位运行启动参数。
func (r *Runner) maybeRestAfterCandidate(ctx context.Context, positionID string, exec platformExecutor, options StartOptions) error {
	r.mu.Lock()
	state := r.running[positionID]
	if state == nil || state.restMaxTimes <= 0 || state.restUsed >= state.restMaxTimes || state.restNextAfter <= 0 {
		r.mu.Unlock()
		return nil
	}
	state.restSinceLast++
	if state.restSinceLast < state.restNextAfter {
		r.mu.Unlock()
		return nil
	}
	processed := state.restSinceLast
	state.restUsed++
	restIndex := state.restUsed
	state.restSinceLast = 0
	state.restNextAfter = randomIntRange(options.RestAfterCandidatesMin, options.RestAfterCandidatesMax)
	r.mu.Unlock()

	maxDuration := options.RestDurationMax
	if maxDuration < options.RestDurationMin {
		maxDuration = options.RestDurationMin
	}
	durationMinutes := randomFloatRange(options.RestDurationMin, maxDuration)
	if durationMinutes <= 0 {
		return nil
	}
	duration := time.Duration(durationMinutes * float64(time.Minute))
	endsAt := time.Now().Add(duration)
	r.positionLog(positionID, "info", fmt.Sprintf("模拟休息：开始，已连续处理 %d 人，第 %d 次休息，预计休息 %s，结束时间=%s", processed, restIndex, formatRestDuration(duration), endsAt.Format("15:04:05")))
	if err := r.waitForSimulatedRest(ctx, positionID, exec, restIndex, duration, endsAt); err != nil {
		return err
	}
	r.updateProgress(positionID, Progress{Stage: "running", Message: "模拟休息结束，继续处理候选人"})
	r.positionLog(positionID, "info", "模拟休息：结束，继续处理候选人")
	return nil
}

// waitForSimulatedRest 等待模拟休息结束，并在开始时更新页面浮层和岗位运行进度。
// 浮层调用始终异步且忽略错误，页面展示异常不会影响岗位运行主流程。
func (r *Runner) waitForSimulatedRest(ctx context.Context, positionID string, exec platformExecutor, restIndex int, duration time.Duration, endsAt time.Time) error {
	r.updateRestDisplay(positionID, exec, restIndex, duration, endsAt)
	return sleepWithContext(ctx, duration)
}

// updateRestDisplay 更新模拟休息进度，并以非阻塞方式显示浏览器页面浮层。
func (r *Runner) updateRestDisplay(positionID string, exec platformExecutor, restIndex int, duration time.Duration, endsAt time.Time) {
	message := fmt.Sprintf("本次休息 %s，预计 %s 继续处理", formatRestDuration(duration), endsAt.Format("15:04:05"))
	r.updateProgress(positionID, Progress{Stage: "resting", Message: message})
	r.showRestOverlayAsync(exec, restIndex, message, duration+time.Minute)
}

// showRestOverlayAsync 异步显示模拟休息浮层，任何 Worker 或页面异常都不会阻塞岗位运行。
func (r *Runner) showRestOverlayAsync(exec platformExecutor, restIndex int, message string, maxAge time.Duration) {
	if exec.runner == nil || exec.runner.worker == nil {
		return
	}
	go func() {
		defer func() { _ = recover() }()
		overlayCtx, overlayCancel := context.WithTimeout(context.Background(), overlayActionTimeout)
		defer overlayCancel()
		_, _ = exec.Post(overlayCtx, "/api/v1/page/ai-overlay", map[string]any{
			"action":     "show",
			"title":      "模拟休息中",
			"subtitle":   fmt.Sprintf("第 %d 次休息", restIndex),
			"message":    message,
			"max_age_ms": maxAge.Milliseconds(),
		})
	}()
}

// formatRestDuration 将休息时长格式化为便于用户阅读的分钟和秒数。
func formatRestDuration(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	duration = duration.Round(time.Second)
	minutes := int(duration / time.Minute)
	seconds := int((duration % time.Minute) / time.Second)
	if minutes <= 0 {
		return fmt.Sprintf("%d 秒", seconds)
	}
	if seconds <= 0 {
		return fmt.Sprintf("%d 分钟", minutes)
	}
	return fmt.Sprintf("%d 分 %d 秒", minutes, seconds)
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
// position 为岗位运行记录，candidates 为候选人列表；有详情阶段时不在列表阶段做关键词终判。
func (r *Runner) prepareCandidatesForFirstStage(position localdb.Position, candidates []map[string]any) ([]map[string]any, int) {
	if positionMode(position) == "keyword" && !shouldFetchDetail(position) {
		return applyKeywordFilter(position, candidates, func(message string) {
			r.positionLog(position.ID, "info", message)
		})
	}
	return prepareCandidatesForFirstStage(position, candidates)
}

// prepareCandidatesForFirstStage 处理第一次基础分析前的候选人队列。
// position 为岗位运行记录，candidates 为候选人列表；有详情阶段时不在列表阶段做关键词终判。
func prepareCandidatesForFirstStage(position localdb.Position, candidates []map[string]any) ([]map[string]any, int) {
	if positionMode(position) == "keyword" && !shouldFetchDetail(position) {
		return applyKeywordFilter(position, candidates, nil)
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(stringFromMap(candidate, "status")) == "" {
			candidate["status"] = "passed"
		}
	}
	return candidates, 0
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
