// Package taskrunner 文件作用：按职责承载本地任务运行流程的拆分实现。
package taskrunner

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/localai"
	"goodhr5/local-agent-go/internal/localdb"
	"goodhr5/local-agent-go/internal/platformcore"
	"strings"
	"time"
)

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
	r.taskLog(task.ID, "info", fmt.Sprintf("详情读取：准备打开详情，候选人=%s，模式=%s", candidateName, detailModeLabel(mode)))
	// 打开详情前模拟人工点击延时
	if err := r.delayRandomRange(ctx, task.ID, "点击详情前", options.DetailOpenDelayMin, options.DetailOpenDelayMax); err != nil {
		r.taskLog(task.ID, "warning", "详情读取：打开详情前等待被中断，候选人="+candidateName)
	}
	var detailResult platformcore.DetailResult
	closeDetail := func(closeCtx context.Context) error {
		return platformRuntime.CloseCandidateDetail(closeCtx, exec, platformConfig, platformcore.Candidate(candidate))
	}
	r.setPendingDetailClose(task.ID, closeDetail)
	err := r.withOperationTimeout(ctx, task.ID, candidateName, "读取候选人详情", detailFetchTimeout, func(opCtx context.Context) error {
		nextDetailResult, fetchErr := platformRuntime.FetchCandidateDetail(opCtx, exec, platformConfig, platformcore.Candidate(candidate), platformcore.DetailRequest{
			TaskID:         task.ID,
			Mode:           mode,
			ScreenshotsDir: r.screenshotsDir,
			Filename:       "detail-latest.png",
		})
		detailResult = nextDetailResult
		return fetchErr
	})
	if err != nil {
		candidate["detail_error"] = err.Error()
		r.taskLog(task.ID, "warning", fmt.Sprintf("详情读取：失败，候选人=%s，错误=%s", candidateName, err.Error()))
		if closeErr := r.closeCandidateDetailNow(context.WithoutCancel(ctx), task.ID, candidateName, "异常后关闭详情页", closeDetail); closeErr != nil {
			r.taskLog(task.ID, "warning", "异常后关闭"+candidateName+"详情失败："+closeErr.Error())
		}
		// 浏览器未启动或已关闭的错误应该直接返回出去让整个任务停止
		if isBrowserClosedTaskError(err) {
			return 0, fmt.Errorf("浏览器未启动或已关闭，任务已自动结束：%w", err)
		}
		return 0, nil
	}
	defer func() {
		// 关闭详情前模拟人工浏览延时，然后再执行关闭
		if !r.isUserStopped(task.ID) {
			_ = r.delayRandomRange(context.WithoutCancel(ctx), task.ID, "关闭详情前", options.DetailCloseDelayMin, options.DetailCloseDelayMax)
		}
		if err := r.closeCandidateDetailNow(context.WithoutCancel(ctx), task.ID, candidateName, "关闭详情页", closeDetail); err != nil {
			r.taskLog(task.ID, "warning", "关闭"+candidateName+"详情失败："+err.Error())
		}
	}()
	r.taskLog(task.ID, "info", "详情读取：详情接口返回成功，候选人="+candidateName)
	detailText := ""
	if mode == "dom" {
		detailText = strings.TrimSpace(detailResult.Text)
		candidate["detail_source"] = "dom"
	}
	if screenshot := detailResult.Screenshot; len(screenshot) > 0 {
		r.attachDetailScreenshot(candidate, screenshot)
		r.taskLog(task.ID, "info", fmt.Sprintf("详情读取：详情截图已返回，候选人=%s，图片=%s", candidateName, firstNonEmptyString(stringFromMap(screenshot, "file_path"), stringFromMap(screenshot, "path"))))
		if mode == "ocr" {
			if taskMode(task) == "keyword" {
				r.showKeywordOCRLoadingOverlay(ctx, exec, task, candidate)
			} else {
				overlayCtx, overlayCancel := context.WithTimeout(context.WithoutCancel(ctx), overlayActionTimeout)
				_, _ = exec.Post(overlayCtx, "/api/v1/page/ai-overlay", map[string]any{
					"action":   "show",
					"title":    "AI 正在分析详情",
					"subtitle": candidateName,
					"message":  "OCR图文识别中...",
				})
				overlayCancel()
			}
			ocrText := ""
			err := r.withOperationTimeout(ctx, task.ID, candidateName, "OCR识别详情截图", ocrRecognizeTimeout, func(ocrCtx context.Context) error {
				nextText, ocrErr := r.recognizeDetailScreenshot(ocrCtx, screenshot)
				ocrText = nextText
				return ocrErr
			})
			if err != nil {
				candidate["ocr_error"] = err.Error()
				r.taskLog(task.ID, "warning", fmt.Sprintf("OCR识别：失败，候选人=%s，错误=%s", candidateName, err.Error()))
			} else {
				detailText = platformRuntime.CleanCandidateDetailText(ocrText)
				candidate["ocr_text"] = detailText
				candidate["detail_source"] = "ocr"
				r.taskLog(task.ID, "info", fmt.Sprintf("OCR识别：完成，候选人=%s，文本长度=%d", candidateName, len([]rune(detailText))))
			}
		}
		if mode == "ai" {
			r.taskLog(task.ID, "info", fmt.Sprintf("AI图片详情：开始，候选人=%s，超时=%s", candidateName, aiDetailTimeout.Round(time.Second)))
			visibleClient, cleanup := r.aiClientForCall(ctx, exec, aiClient, "AI 正在分析详情", candidateName, "正在识别详情长图并判断是否打招呼")
			var decision localai.Decision
			err := r.withOperationTimeout(ctx, task.ID, candidateName, "AI图片详情评分", aiDetailTimeout, func(aiCtx context.Context) error {
				nextDecision, aiErr := r.scoreDetailScreenshotWithClient(aiCtx, task, candidate, screenshot, visibleClient)
				decision = nextDecision
				return aiErr
			})
			cleanup()
			if err != nil {
				candidate["ai_vision_error"] = err.Error()
				r.taskLog(task.ID, "warning", fmt.Sprintf("AI图片详情：失败，候选人=%s，错误=%s", candidateName, err.Error()))
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
					r.taskLog(task.ID, "info", fmt.Sprintf("AI图片详情：完成，候选人=%s，分数=%.1f，阈值=%.1f，是否打招呼=否", candidateName, decision.Score, decision.Threshold))
					return 1, nil
				}
				candidate["status"] = "ai_passed"
				r.taskLog(task.ID, "info", fmt.Sprintf("AI图片详情：完成，候选人=%s，分数=%.1f，阈值=%.1f，是否打招呼=是，文本长度=%d", candidateName, decision.Score, decision.Threshold, len([]rune(detailText))))
			}
		}
	} else if mode == "ai" {
		r.taskLog(task.ID, "warning", "AI图片详情：失败，候选人="+candidateName+"，错误=详情截图为空")
	} else {
		r.taskLog(task.ID, "info", fmt.Sprintf("详情读取：当前详情模式=%s，不调用图片详情 AI，候选人=%s", detailModeLabel(mode), candidateName))
	}
	detailText = platformRuntime.CleanCandidateDetailText(detailText)
	if detailText == "" {
		if mode == "ai" && stringFromMap(candidate, "status") == "ai_passed" {
			return 0, nil
		}
		candidate["status"] = "skipped"
		candidate["skip_reason"] = "详情文本为空"
		r.taskLog(task.ID, "warning", "详情读取：失败，候选人="+candidateName+"，错误=详情文本为空")
		return 1, nil
	}
	candidate["detail_text"] = detailText
	candidate["raw_text"] = mergeText(stringFromMap(candidate, "raw_text"), detailText)
	candidate["status"] = "detail_fetched"
	r.taskLog(task.ID, "info", fmt.Sprintf("详情读取：完成，候选人=%s，来源=%s，文本长度=%d", candidateName, detailModeLabel(mode), len([]rune(detailText))))
	return 0, nil
}

// setPendingDetailClose 登记当前候选人详情的清理动作，供正常流程和任务收尾共同使用。
func (r *Runner) setPendingDetailClose(taskID string, closeFn func(context.Context) error) {
	r.mu.Lock()
	if state := r.running[strings.TrimSpace(taskID)]; state != nil {
		state.pendingDetailClose = closeFn
	}
	r.mu.Unlock()
}

// closeCandidateDetailNow 执行当前候选人详情清理，成功后清除待处理标记。
func (r *Runner) closeCandidateDetailNow(ctx context.Context, taskID string, candidateName string, operation string, fallback func(context.Context) error) error {
	r.mu.Lock()
	state := r.running[strings.TrimSpace(taskID)]
	var closeFn func(context.Context) error
	if state != nil {
		closeFn = state.pendingDetailClose
	}
	r.mu.Unlock()
	if closeFn == nil {
		closeFn = fallback
	}
	if closeFn == nil {
		return nil
	}
	err := r.withOperationTimeout(ctx, taskID, candidateName, operation, detailCloseTimeout, closeFn)
	if err == nil {
		r.mu.Lock()
		if state := r.running[strings.TrimSpace(taskID)]; state != nil {
			state.pendingDetailClose = nil
		}
		r.mu.Unlock()
	}
	return err
}

// closePendingCandidateDetail 在任务退出前补关仍可能打开的候选人详情。
func (r *Runner) closePendingCandidateDetail(taskID string) {
	r.mu.Lock()
	state := r.running[strings.TrimSpace(taskID)]
	hasPending := state != nil && state.pendingDetailClose != nil
	r.mu.Unlock()
	if !hasPending {
		return
	}
	r.taskLog(taskID, "info", "任务收尾：检测到候选人详情可能仍打开，执行最后关闭")
	if err := r.closeCandidateDetailNow(context.Background(), taskID, "当前候选人", "任务收尾关闭详情页", nil); err != nil {
		r.taskLog(taskID, "warning", "任务收尾：关闭候选人详情失败，错误="+err.Error())
	}
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
