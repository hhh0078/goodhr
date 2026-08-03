// Package positionrunner 文件作用：按职责承载本地岗位运行运行流程的拆分实现。
package positionrunner

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/localai"
	"goodhr5/local-agent-go/internal/localdb"
	"goodhr5/local-agent-go/internal/platformcore"
	"math/rand/v2"
	"strings"
	"sync"
	"time"
)

// candidateDetailSession 管理单个候选人已打开详情的唯一关闭动作。
type candidateDetailSession struct {
	closeOnce sync.Once
	closeErr  error
	closeFn   func(context.Context) error
}

// candidateDetailScrollDelay 返回相邻两次候选人详情滚动之间的随机等待时长。
func candidateDetailScrollDelay() time.Duration {
	return time.Duration(rand.Int64N(int64(time.Second) + 1))
}

// Close 立即关闭当前候选人详情，重复调用只执行一次。
// ctx 为关闭详情使用的上下文。
func (s *candidateDetailSession) Close(ctx context.Context) error {
	if s == nil || s.closeFn == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.closeErr = s.closeFn(ctx)
	})
	return s.closeErr
}

// startCandidateDetailScrolling 在最终 AI 分析期间后台滚动详情，并返回立即停止函数。
// ctx 为候选人上下文，positionID 为岗位运行 ID，platformRuntime 为平台运行时，exec 为执行器，platformConfig 为平台配置，candidate 为候选人。
func (r *Runner) startCandidateDetailScrolling(ctx context.Context, positionID string, platformRuntime platformcore.Runtime, exec platformExecutor, platformConfig cloudapi.PlatformConfig, candidate map[string]any) func() {
	scroller, ok := platformRuntime.(platformcore.DetailAnalysisScroller)
	if !ok {
		return func() {}
	}
	scrollCtx, cancel := context.WithCancel(ctx)
	started := make(chan struct{})
	done := make(chan struct{})
	r.positionLog(positionID, "info", "详情浏览：AI 分析期间开始同步滚动，候选人="+candidateLogName(candidate))
	go func() {
		defer close(done)
		distances := []int{260, 320, 240, -180}
		for index := 0; ; index++ {
			if err := scrollCtx.Err(); err != nil {
				if index == 0 {
					close(started)
				}
				return
			}
			if index == 0 {
				close(started)
			}
			// 当前滚轮动作开始后允许其完整返回，避免 AI 先返回时关闭弹框截断滚动。
			actionCtx, actionCancel := context.WithTimeout(context.WithoutCancel(scrollCtx), 30*time.Second)
			err := scroller.ScrollCandidateDetail(actionCtx, exec, platformConfig, platformcore.Candidate(candidate), distances[index%len(distances)])
			actionCancel()
			if err != nil {
				if scrollCtx.Err() == nil {
					r.positionLog(positionID, "warning", "详情浏览：同步滚动停止，候选人="+candidateLogName(candidate)+"，错误="+err.Error())
				}
				return
			}
			if err := sleepWithContext(scrollCtx, candidateDetailScrollDelay()); err != nil {
				return
			}
		}
	}()
	<-started
	return func() {
		cancel()
		<-done
		r.positionLog(positionID, "info", "详情浏览：AI 已返回，滚动已停止，候选人="+candidateLogName(candidate))
	}
}

// enrichCandidatesWithDetail 为候选人补充详情文本。
// ctx 为请求上下文，position 为岗位运行记录，platformConfig 为云端平台配置，candidates 为候选人列表。
func (r *Runner) enrichCandidatesWithDetail(ctx context.Context, position localdb.Position, platformRuntime platformcore.Runtime, exec platformExecutor, platformConfig cloudapi.PlatformConfig, candidates []map[string]any, options StartOptions) (int, error) {
	skipped := 0
	mode := detailMode(position)
	if mode == "" {
		return 0, nil
	}
	var aiClient *localai.Client
	var err error
	if mode == "ai" {
		aiClient, err = r.pipelineAIClient(position, options)
		if err != nil {
			return 0, err
		}
	}
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return skipped, err
		}
		itemSkipped, detailSession, err := r.enrichCandidateWithDetail(ctx, position, platformRuntime, exec, platformConfig, candidate, aiClient, options)
		if err != nil {
			return skipped, err
		}
		if detailSession != nil {
			if closeErr := detailSession.Close(context.WithoutCancel(ctx)); closeErr != nil {
				return skipped, fmt.Errorf("候选人详情无法关闭，岗位运行已自动停止：%w", closeErr)
			}
		}
		skipped += itemSkipped
	}
	return skipped, nil
}

// enrichCandidateWithDetail 为单个候选人补充详情文本。
// ctx 为请求上下文，candidate 为候选人，aiClient 为空时按需临时创建。
func (r *Runner) enrichCandidateWithDetail(ctx context.Context, position localdb.Position, platformRuntime platformcore.Runtime, exec platformExecutor, platformConfig cloudapi.PlatformConfig, candidate map[string]any, aiClient *localai.Client, options StartOptions) (int, *candidateDetailSession, error) {
	mode := detailMode(position)
	if mode == "" || !canContinueCandidate(stringFromMap(candidate, "status")) {
		return 0, nil, nil
	}
	candidateName := candidateLogName(candidate)
	r.positionLog(position.ID, "info", fmt.Sprintf("详情读取：准备打开详情，候选人=%s，模式=%s", candidateName, detailModeLabel(mode)))
	// 打开详情前模拟人工点击延时
	if err := r.delayRandomRange(ctx, position.ID, "点击详情前", options.DetailOpenDelayMin, options.DetailOpenDelayMax); err != nil {
		r.positionLog(position.ID, "warning", "详情读取：打开详情前等待被中断，候选人="+candidateName)
	}
	var detailResult platformcore.DetailResult
	closeDetail := func(closeCtx context.Context) error {
		return platformRuntime.CloseCandidateDetail(closeCtx, exec, platformConfig, platformcore.Candidate(candidate))
	}
	r.setPendingDetailClose(position.ID, closeDetail)
	err := r.withOperationTimeout(ctx, position.ID, candidateName, "读取候选人详情", detailFetchTimeout, func(opCtx context.Context) error {
		nextDetailResult, fetchErr := platformRuntime.FetchCandidateDetail(opCtx, exec, platformConfig, platformcore.Candidate(candidate), platformcore.DetailRequest{
			PositionID:     position.ID,
			Mode:           mode,
			ScreenshotsDir: r.screenshotsDir,
			Filename:       "detail-latest.png",
		})
		detailResult = nextDetailResult
		return fetchErr
	})
	if err != nil {
		candidate["detail_error"] = err.Error()
		r.positionLog(position.ID, "warning", fmt.Sprintf("详情读取：失败，候选人=%s，错误=%s", candidateName, err.Error()))
		if closeErr := r.closeCandidateDetailNow(context.WithoutCancel(ctx), position.ID, candidateName, "异常后关闭详情页", closeDetail); closeErr != nil {
			r.positionLog(position.ID, "warning", "异常后关闭"+candidateName+"详情失败："+closeErr.Error())
			return 0, nil, fmt.Errorf("候选人详情无法关闭，岗位运行已自动停止：%w", closeErr)
		}
		// 浏览器未启动或已关闭的错误应该直接返回出去让整个岗位运行停止
		if isBrowserClosedPositionError(err) {
			return 0, nil, fmt.Errorf("浏览器未启动或已关闭，岗位运行已自动结束：%w", err)
		}
		if isFatalCandidateDetailError(err) {
			return 0, nil, fmt.Errorf("候选人详情没找到，岗位运行已自动停止：%w", err)
		}
		if r.isUserStopped(position.ID) {
			return 0, nil, nil
		}
		return 0, nil, &candidateOperationError{Operation: "读取候选人详情", Err: err}
	}
	_, closesAfterAI := platformRuntime.(platformcore.DetailAnalysisScroller)
	detailSession := &candidateDetailSession{closeFn: func(closeCtx context.Context) error {
		if !closesAfterAI && !r.isUserStopped(position.ID) {
			_ = r.delayRandomRange(closeCtx, position.ID, "关闭详情前", options.DetailCloseDelayMin, options.DetailCloseDelayMax)
		}
		if err := r.closeCandidateDetailNow(closeCtx, position.ID, candidateName, "关闭详情页", closeDetail); err != nil {
			r.positionLog(position.ID, "warning", "关闭"+candidateName+"详情失败："+err.Error())
			return err
		}
		return nil
	}}
	r.positionLog(position.ID, "info", "详情读取：详情接口返回成功，候选人="+candidateName)
	detailText := ""
	if mode == "dom" {
		detailText = strings.TrimSpace(detailResult.Text)
		candidate["detail_source"] = "dom"
	}
	if screenshot := detailResult.Screenshot; len(screenshot) > 0 {
		r.attachDetailScreenshot(candidate, screenshot)
		r.positionLog(position.ID, "info", fmt.Sprintf("详情读取：详情截图已返回，候选人=%s，图片=%s", candidateName, firstNonEmptyString(stringFromMap(screenshot, "file_path"), stringFromMap(screenshot, "path"))))
		if mode == "ocr" {
			if positionMode(position) == "keyword" {
				r.showKeywordOCRLoadingOverlay(ctx, exec, position, candidate)
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
			err := r.withOperationTimeout(ctx, position.ID, candidateName, "OCR识别详情截图", ocrRecognizeTimeout, func(ocrCtx context.Context) error {
				nextText, ocrErr := r.recognizeDetailScreenshot(ocrCtx, screenshot)
				ocrText = nextText
				return ocrErr
			})
			if err != nil {
				candidate["ocr_error"] = err.Error()
				r.positionLog(position.ID, "warning", fmt.Sprintf("OCR识别：失败，候选人=%s，错误=%s", candidateName, err.Error()))
				if isFatalOCRError(err) {
					return 0, detailSession, fmt.Errorf("OCR运行组件不可用，岗位运行已自动停止：%w", err)
				}
			} else {
				detailText = platformRuntime.CleanCandidateDetailText(ocrText)
				candidate["ocr_text"] = detailText
				candidate["detail_source"] = "ocr"
				r.positionLog(position.ID, "info", fmt.Sprintf("OCR识别：完成，候选人=%s，文本长度=%d", candidateName, len([]rune(detailText))))
			}
		}
		if mode == "ai" {
			r.positionLog(position.ID, "info", fmt.Sprintf("AI图片详情：开始，候选人=%s，超时=%s", candidateName, aiDetailTimeout.Round(time.Second)))
			visibleClient, cleanup := r.aiClientForCall(ctx, exec, aiClient, "AI 正在分析详情", candidateName, "正在识别详情长图并判断是否打招呼")
			var decision localai.Decision
			err := r.withOperationTimeout(ctx, position.ID, candidateName, "AI图片详情评分", aiDetailTimeout, func(aiCtx context.Context) error {
				nextDecision, aiErr := r.scoreDetailScreenshotWithClient(aiCtx, position, candidate, screenshot, visibleClient)
				decision = nextDecision
				return aiErr
			})
			cleanup()
			if err != nil {
				candidate["ai_vision_error"] = err.Error()
				r.positionLog(position.ID, "warning", fmt.Sprintf("AI图片详情：失败，候选人=%s，错误=%s", candidateName, err.Error()))
				if localai.IsPositionStoppingError(err) {
					return 0, detailSession, fmt.Errorf("AI图片详情分析持续不可用，岗位运行已自动停止：%w", err)
				}
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
					r.positionLog(position.ID, "info", fmt.Sprintf("AI图片详情：完成，候选人=%s，分数=%.1f，阈值=%.1f，是否打招呼=否", candidateName, decision.Score, decision.Threshold))
					return 1, detailSession, nil
				}
				candidate["status"] = "ai_passed"
				r.positionLog(position.ID, "info", fmt.Sprintf("AI图片详情：完成，候选人=%s，分数=%.1f，阈值=%.1f，是否打招呼=是，文本长度=%d", candidateName, decision.Score, decision.Threshold, len([]rune(detailText))))
			}
		}
	} else if mode == "ai" {
		r.positionLog(position.ID, "warning", "AI图片详情：失败，候选人="+candidateName+"，错误=详情截图为空")
	} else {
		r.positionLog(position.ID, "info", fmt.Sprintf("详情读取：当前详情模式=%s，不调用图片详情 AI，候选人=%s", detailModeLabel(mode), candidateName))
	}
	detailText = platformRuntime.CleanCandidateDetailText(detailText)
	if detailText == "" {
		if mode == "ai" && stringFromMap(candidate, "status") == "ai_passed" {
			return 0, detailSession, nil
		}
		candidate["status"] = "skipped"
		candidate["skip_reason"] = "详情文本为空"
		r.positionLog(position.ID, "warning", "详情读取：失败，候选人="+candidateName+"，错误=详情文本为空")
		return 1, detailSession, nil
	}
	candidate["detail_text"] = detailText
	candidate["raw_text"] = mergeText(stringFromMap(candidate, "raw_text"), detailText)
	candidate["status"] = "detail_fetched"
	r.positionLog(position.ID, "info", fmt.Sprintf("详情读取：完成，候选人=%s，来源=%s，文本长度=%d", candidateName, detailModeLabel(mode), len([]rune(detailText))))
	return 0, detailSession, nil
}

// setPendingDetailClose 登记当前候选人详情的清理动作，供正常流程和岗位运行收尾共同使用。
func (r *Runner) setPendingDetailClose(positionID string, closeFn func(context.Context) error) {
	r.mu.Lock()
	if state := r.running[strings.TrimSpace(positionID)]; state != nil {
		state.pendingDetailClose = closeFn
	}
	r.mu.Unlock()
}

// closeCandidateDetailNow 执行当前候选人详情清理，成功后清除待处理标记。
func (r *Runner) closeCandidateDetailNow(ctx context.Context, positionID string, candidateName string, operation string, fallback func(context.Context) error) error {
	r.mu.Lock()
	state := r.running[strings.TrimSpace(positionID)]
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
	err := r.withOperationTimeout(ctx, positionID, candidateName, operation, detailCloseTimeout, closeFn)
	if err == nil {
		r.mu.Lock()
		if state := r.running[strings.TrimSpace(positionID)]; state != nil {
			state.pendingDetailClose = nil
		}
		r.mu.Unlock()
	}
	return err
}

// closePendingCandidateDetail 在岗位运行退出前补关仍可能打开的候选人详情。
func (r *Runner) closePendingCandidateDetail(positionID string) {
	r.mu.Lock()
	state := r.running[strings.TrimSpace(positionID)]
	hasPending := state != nil && state.pendingDetailClose != nil
	r.mu.Unlock()
	if !hasPending {
		return
	}
	r.positionLog(positionID, "info", "岗位运行收尾：检测到候选人详情可能仍打开，执行最后关闭")
	if err := r.closeCandidateDetailNow(context.Background(), positionID, "当前候选人", "岗位运行收尾关闭详情页", nil); err != nil {
		r.positionLog(positionID, "warning", "岗位运行收尾：关闭候选人详情失败，错误="+err.Error())
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

// shouldFetchDetail 判断岗位运行是否需要读取候选人详情。
// position 为岗位运行记录。
func shouldFetchDetail(position localdb.Position) bool {
	return detailMode(position) != ""
}

// detailMode 返回详情读取模式。
// position 为岗位运行记录，支持 dom、ocr 和 ai。
func detailMode(position localdb.Position) string {
	commonConfig := mapValue(position.PositionSnapshot["common_config"])
	keywordConfig := mapValue(position.PositionSnapshot["keyword_config"])
	mode := strings.ToLower(firstNonEmptyString(
		stringFromMap(commonConfig, "detail_mode"),
		stringFromMap(keywordConfig, "detail_mode"),
	))
	if mode == "ocr" || mode == "dom" || mode == "ai" {
		return mode
	}
	if positionMode(position) == "ai" {
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
