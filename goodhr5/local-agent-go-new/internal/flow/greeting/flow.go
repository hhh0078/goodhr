// Package greeting 实现主动打招呼主流程，并在一个顶层步骤列表中展示完整顺序。
package greeting

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/integration/ocr"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/profile"
	"goodhr5/local-agent-go-new/internal/storage"
	"goodhr5/local-agent-go-new/internal/system/notification"
)

// Flow 组装主动打招呼流程依赖。
type Flow struct {
	Browser        *client.Client
	AI             *ai.Client
	OCR            *ocr.Client
	Store          *storage.Store
	Cloud          *cloud.Client
	Notifier       *notification.Notifier
	Logger         shared.Logger
	ScreenshotsDir string
	DownloadsDir   string
	ExtensionPaths func() []string
	screenshotMu   sync.Mutex
	screenshotSeq  int
}

type flowStep struct {
	name     string
	label    string
	optional bool
	run      func(context.Context) error
}

// Run 按平铺步骤启动浏览器、准备平台、处理候选人并同步摘要。
func (f *Flow) Run(ctx context.Context, prepared shared.PreparedTask, runtime model.Runtime) (shared.Stats, error) {
	stats := shared.Stats{}
	position := model.Position{
		ID: prepared.Position.ID, Name: prepared.Position.Name, Keyword: prepared.Position.Keyword,
		RequestPhone: prepared.Position.RequestPhone, RequestWechat: prepared.Position.RequestWechat,
		RequestResume:             prepared.Position.RequestResume,
		HLiepinShortcutSearchName: prepared.Position.CommonConfig.HLiepinShortcutSearchName,
	}
	steps := []flowStep{
		{name: "prepare_screenshot_workspace", label: "准备本次截图目录", run: func(context.Context) error {
			return f.prepareScreenshotWorkspace()
		}},
		{name: "start_browser", label: "启动增强浏览器", run: func(ctx context.Context) error { return f.startBrowser(ctx, prepared) }},
		{name: "open_greeting_page", label: "打开打招呼页面", run: func(ctx context.Context) error {
			return runtime.OpenGreetingPage(ctx, f.Browser, prepared.Platform)
		}},
		{name: "initialize_greeting_page", label: "整理打招呼页面", run: func(ctx context.Context) error {
			return runtime.InitializeGreetingPage(ctx, f.Browser, prepared.Platform)
		}},
		{name: "select_position", label: "选择岗位", run: func(ctx context.Context) error {
			return runtime.SelectPosition(ctx, f.Browser, prepared.Platform, position)
		}},
		{name: "apply_basic_filters", label: "应用基础筛选", run: func(ctx context.Context) error {
			return runtime.ApplyBasicFilters(ctx, f.Browser, prepared.Platform, position)
		}},
		{name: "scan_decide_and_greet", label: "处理候选人并打招呼", run: func(ctx context.Context) error {
			return f.processBatches(ctx, prepared, runtime, &stats)
		}},
	}
	for _, step := range steps {
		if shared.GracefulStopRequested(ctx) {
			return stats, nil
		}
		startedAt := time.Now()
		f.log(prepared.Request.TaskID, step.name, "start", startedAt, nil)
		if err := step.run(ctx); err != nil {
			if step.optional {
				f.log(prepared.Request.TaskID, step.name, "warning", startedAt, err)
				continue
			}
			f.log(prepared.Request.TaskID, step.name, "failed", startedAt, err)
			return stats, fmt.Errorf("%s没处理成功：%w", step.label, err)
		}
		f.log(prepared.Request.TaskID, step.name, "success", startedAt, nil)
	}
	return stats, nil
}

// startBrowser 使用当前 Profile 启动或复用 Camoufox。
func (f *Flow) startBrowser(ctx context.Context, prepared shared.PreparedTask) error {
	headless := prepared.Request.Headless
	humanize := true
	_, err := f.Browser.StartBrowser(ctx, contract.BrowserStartRequest{
		UserDataDir:    prepared.ProfilePath,
		DownloadsPath:  f.DownloadsDir,
		Headless:       &headless,
		Humanize:       &humanize,
		Locale:         "zh-CN",
		Timezone:       "Asia/Shanghai",
		ExtensionPaths: profile.WithBookmarkExtension(prepared.ProfilePath, f.extensionPaths()),
	})
	return err
}

// extensionPaths 返回本次启动浏览器时发现的有效扩展目录。
func (f *Flow) extensionPaths() []string {
	if f.ExtensionPaths == nil {
		return nil
	}
	return f.ExtensionPaths()
}

// processBatches 按配置批次扫描、判断和处理候选人。
func (f *Flow) processBatches(ctx context.Context, prepared shared.PreparedTask, runtime model.Runtime, stats *shared.Stats) error {
	maxBatches := prepared.Position.MaxBatches
	errorPolicy := &shared.ConsecutiveErrorPolicy{}
	seen := make(map[string]struct{})
	rest := newRestSchedule(prepared.Preferences)
	candidateListOptions := model.CandidateListOptions{
		ReadConcurrency: prepared.Preferences.CandidateReadConcurrency,
		Cursor:          &model.CandidateListCursor{},
	}
	for batch := 0; ; batch++ {
		if shared.GracefulStopRequested(ctx) {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := shared.EnsureCloudSession(ctx, f.Cloud, prepared.Request.Token, prepared.Request.TaskID, "greeting", f.Logger); err != nil {
			return err
		}
		if err := waitRandomSeconds(
			ctx, f.Logger, prepared.Request.TaskID, "list_view",
			prepared.Preferences.ListViewDelayMin, prepared.Preferences.ListViewDelayMax,
		); err != nil {
			return err
		}
		candidates, err := runtime.FindCandidates(ctx, f.Browser, prepared.Platform, candidateListOptions)
		if err != nil {
			return err
		}
		if len(candidates) == 0 {
			shared.ReportProgress(f.Logger, prepared.Request.TaskID, "当前页面已经没有候选人，这轮任务完成了")
			return nil
		}
		newCandidates := make([]model.Candidate, 0, len(candidates))
		for _, candidate := range candidates {
			if strings.TrimSpace(candidate.Fingerprint) == "" {
				stats.Processed++
				stats.Skipped++
				f.log(
					prepared.Request.TaskID,
					"candidate_fingerprint",
					"warning",
					time.Now(),
					fmt.Errorf("候选人缺少稳定编号，本轮跳过，避免重复打招呼"),
				)
				continue
			}
			if _, exists := seen[candidate.Fingerprint]; exists {
				stats.Skipped++
				continue
			}
			seen[candidate.Fingerprint] = struct{}{}
			newCandidates = append(newCandidates, candidate)
		}
		if len(newCandidates) > 0 {
			if err := f.Cloud.AddProcessedResumes(
				ctx,
				prepared.Request.Token,
				prepared.Position.ID,
				len(newCandidates),
			); err != nil {
				f.log(prepared.Request.TaskID, "sync_processed_resumes", "warning", time.Now(), err)
			}
		}
		previewCtx, cancelPreviews := context.WithCancel(ctx)
		previews := f.candidatePreviews(previewCtx, prepared, newCandidates)
		for {
			item, previewOpen, previewErr := waitCandidatePreview(
				previewCtx,
				shared.GracefulStopSignal(ctx),
				previews,
			)
			if previewErr != nil {
				cancelPreviews()
				return previewErr
			}
			if shared.GracefulStopRequested(ctx) {
				cancelPreviews()
				return nil
			}
			if !previewOpen {
				break
			}
			candidate := item.Candidate
			if err := ctx.Err(); err != nil {
				cancelPreviews()
				return err
			}
			candidateName := strings.TrimSpace(candidate.Name)
			if candidateName == "" {
				candidateName = fmt.Sprintf("第 %d 位候选人", candidate.Index+1)
			}
			if item.Decision != nil {
				result := reportAIResult(
					f.Logger,
					prepared.Request.TaskID,
					candidate,
					*item.Decision,
					previewScoreThreshold(prepared.Position),
					"preview",
					!item.Decision.Accepted,
				)
				f.recordScreeningResult(ctx, prepared, stats, result)
			} else if item.Err != nil {
				reportAIError(f.Logger, prepared.Request.TaskID, candidate, item.Err)
			}
			shared.ReportCandidateActivity(f.Logger, prepared.Request.TaskID, candidateName, "正在查看候选人列表", "running", nil)
			candidateStartedAt := time.Now()
			candidateCtx, cancelCandidate := context.WithTimeoutCause(ctx, candidateTimeout, errCandidateProcessingTimeout)
			candidateErr := item.Err
			if candidateErr == nil && item.Decision != nil && !item.Decision.Accepted {
				stats.Processed++
				stats.Skipped++
				f.log(
					prepared.Request.TaskID,
					"detail_precheck",
					"skipped",
					time.Now(),
					fmt.Errorf(
						"候选人“%s”基础评分 %.1f，暂不打开详情：%s",
						candidate.Name,
						item.Decision.Score,
						item.Decision.Reason,
					),
				)
				f.saveCandidate(
					candidateCtx,
					prepared,
					candidate,
					"detail_precheck",
					"skipped",
					item.Decision.Reason,
				)
				shared.ReportCandidateActivity(f.Logger, prepared.Request.TaskID, candidateName, "基础信息不匹配，已跳过", "skipped", nil)
			} else if candidateErr == nil {
				if item.Decision != nil {
					f.log(
						prepared.Request.TaskID,
						"detail_precheck",
						"success",
						time.Now(),
						nil,
					)
				}
				candidateErr = f.processCandidate(candidateCtx, prepared, runtime, candidate, stats)
			}
			candidateErr = f.finishCandidateProcessing(candidateCtx, prepared.Request.TaskID, candidateStartedAt, candidateErr)
			cancelCandidate()
			if candidateErr != nil {
				if errors.Is(candidateErr, model.ErrCandidateDetailUnavailable) {
					f.log(prepared.Request.TaskID, "candidate_detail_unavailable", "skipped", time.Now(), candidateErr)
					shared.ReportCandidateActivity(f.Logger, prepared.Request.TaskID, candidateName, "详情暂时无法查看，已跳过", "skipped", nil)
				} else {
					f.log(prepared.Request.TaskID, "candidate_operation", "failed", time.Now(), candidateErr)
				}
				if stopErr := errorPolicy.Record(candidateErr); stopErr != nil {
					shared.ClearCandidateActivity(f.Logger, prepared.Request.TaskID)
					cancelPreviews()
					return stopErr
				}
			} else {
				errorPolicy.Reset()
			}
			shared.ClearCandidateActivity(f.Logger, prepared.Request.TaskID)
			if shared.GracefulStopRequested(ctx) {
				cancelPreviews()
				return nil
			}
			if restErr := rest.afterCandidate(ctx, f.Logger, prepared.Request.TaskID, prepared.Preferences); restErr != nil {
				cancelPreviews()
				return restErr
			}
			if shared.GracefulStopRequested(ctx) {
				cancelPreviews()
				return nil
			}
			if candidateErr != nil {
				continue
			}
			if prepared.Position.MatchLimit > 0 && stats.Succeeded >= prepared.Position.MatchLimit {
				cancelPreviews()
				shared.ReportProgress(
					f.Logger,
					prepared.Request.TaskID,
					fmt.Sprintf("本次已打招呼 %d 人，达到岗位上限，任务完成了", stats.Succeeded),
				)
				return nil
			}
		}
		cancelPreviews()
		if shared.GracefulStopRequested(ctx) {
			return nil
		}
		if candidateBatchLimitReached(maxBatches, batch+1) {
			shared.ReportProgress(
				f.Logger,
				prepared.Request.TaskID,
				fmt.Sprintf("已处理配置的 %d 批候选人，这轮任务完成了", maxBatches),
			)
			return nil
		}
		shared.ReportProgress(
			f.Logger,
			prepared.Request.TaskID,
			fmt.Sprintf("第 %d 批处理完了，正在加载更多候选人", batch+1),
		)
		advanced, err := runtime.AdvanceCandidateList(ctx, f.Browser, prepared.Platform, candidates, candidateListOptions)
		if err != nil {
			return err
		}
		if !advanced {
			shared.ReportProgress(f.Logger, prepared.Request.TaskID, "平台已经没有更多候选人，这轮任务完成了")
			return nil
		}
		if err := waitRandomSeconds(
			ctx, f.Logger, prepared.Request.TaskID, "after_scroll",
			float64(prepared.Preferences.ScrollDelayMin), float64(prepared.Preferences.ScrollDelayMax),
		); err != nil {
			return err
		}
	}
}

// candidateBatchLimitReached 判断明确配置的候选人批数是否已经处理完，零值表示不限制批数。
func candidateBatchLimitReached(maxBatches int, completedBatches int) bool {
	return maxBatches > 0 && completedBatches >= maxBatches
}

// processCandidate 平铺执行基础过滤、详情、OCR、AI、收藏、关闭详情、打招呼和保存结果。
func (f *Flow) processCandidate(ctx context.Context, prepared shared.PreparedTask, runtime model.Runtime, candidate model.Candidate, stats *shared.Stats) (candidateErr error) {
	stats.Processed++
	if reason := strings.TrimSpace(candidate.SkipReason); reason != "" {
		stats.Skipped++
		f.log(prepared.Request.TaskID, "platform_skip", "skipped", time.Now(), nil)
		f.saveCandidate(ctx, prepared, candidate, "platform_skip", "skipped", reason)
		shared.ReportCandidateActivity(f.Logger, prepared.Request.TaskID, candidateDisplayName(candidate), reason, "skipped", nil)
		return nil
	}
	detailMode := strings.ToLower(strings.TrimSpace(prepared.Position.CommonConfig.DetailMode))
	needsDetail := needsCandidateDetail(prepared)
	deferKeywordDecision := isKeywordMode(prepared.Position) && needsDetail
	if !deferKeywordDecision {
		keywordMatch := matchKeywords(candidate, "", prepared.Position)
		if isKeywordMode(prepared.Position) {
			result := reportKeywordMatch(f.Logger, prepared.Request.TaskID, candidate, keywordMatch, !keywordMatch.Accepted || !prepared.Position.RequiresAI)
			f.recordScreeningResult(ctx, prepared, stats, result)
		}
		if !keywordMatch.Accepted {
			stats.Skipped++
			f.saveCandidate(ctx, prepared, candidate, "filter", "skipped", keywordMatch.Reason)
			shared.ReportCandidateActivity(f.Logger, prepared.Request.TaskID, candidateDisplayName(candidate), "关键词不匹配，已跳过", "skipped", nil)
			return nil
		}
	}
	if needsDetail && !shouldOpenDetail(prepared) {
		stats.Skipped++
		f.saveCandidate(ctx, prepared, candidate, "detail_probability", "skipped", "本次未命中个人配置的详情打开概率")
		shared.ReportCandidateActivity(f.Logger, prepared.Request.TaskID, candidateDisplayName(candidate), "本次未查看详情，已跳过", "skipped", nil)
		return nil
	}
	if err := runtime.ScrollToCandidate(ctx, f.Browser, prepared.Platform, candidate); err != nil {
		f.reportPageDiagnostics(ctx, prepared.Request.TaskID, "滚动到候选人失败")
		stats.Failed++
		f.saveCandidate(ctx, prepared, candidate, "scroll_to_candidate", "failed", err.Error())
		return fmt.Errorf("滚动到当前候选人失败：%w", err)
	}
	detail := model.CandidateDetail{}
	var detailImages [][]byte
	detailOpened := false
	defer func() {
		if detailOpened {
			// 收藏或其他详情步骤失败时仍补关；独立预算避免候选人超时让清理直接取消。
			cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
			defer cleanupCancel()
			startedAt := time.Now()
			f.log(prepared.Request.TaskID, "cleanup_candidate_detail", "start", startedAt, nil)
			if err := runtime.CloseCandidateDetail(cleanupCtx, f.Browser, prepared.Platform, candidate); err != nil {
				f.log(prepared.Request.TaskID, "cleanup_candidate_detail", "failed", startedAt, err)
				f.saveCandidate(cleanupCtx, prepared, candidate, "close_detail", "failed", err.Error())
				if candidateErr == nil {
					stats.Failed++
				}
				candidateErr = errors.Join(candidateErr, fmt.Errorf("%w：%w", model.ErrCandidateDetailCleanupFailed, err))
			} else {
				f.log(prepared.Request.TaskID, "cleanup_candidate_detail", "success", startedAt, nil)
			}
		}
	}()
	if needsDetail {
		if err := waitRandomSeconds(
			ctx, f.Logger, prepared.Request.TaskID, "before_detail_open",
			prepared.Preferences.DetailOpenDelayMin, prepared.Preferences.DetailOpenDelayMax,
		); err != nil {
			return err
		}
		shared.ReportCandidateActivity(f.Logger, prepared.Request.TaskID, candidateDisplayName(candidate), "正在打开详情页", "running", nil)
		if err := runtime.OpenCandidateDetail(ctx, f.Browser, prepared.Platform, candidate); err != nil {
			if errors.Is(err, model.ErrCandidateDetailUnavailable) {
				stats.Skipped++
				f.saveCandidate(ctx, prepared, candidate, "open_detail", "skipped", err.Error())
				return err
			}
			f.reportPageDiagnostics(ctx, prepared.Request.TaskID, "打开候选人详情失败")
			stats.Failed++
			f.saveCandidate(ctx, prepared, candidate, "open_detail", "failed", err.Error())
			return fmt.Errorf("打开候选人详情失败：%w", err)
		}
		detailOpened = true
		shared.ReportCandidateActivity(f.Logger, prepared.Request.TaskID, candidateDisplayName(candidate), "正在查看详情页", "running", nil)
		// 截图模式自身从顶部完整滚动；DOM 模式先浏览以加载详情，再读取正文。
		if detailMode != "ai" && !prepared.Position.RequiresOCR {
			if detailBrowser, ok := runtime.(model.DetailBrowser); ok {
				startedAt := time.Now()
				f.log(prepared.Request.TaskID, "browse_candidate_detail", "start", startedAt, nil)
				if err := detailBrowser.BrowseCandidateDetail(ctx, f.Browser, prepared.Platform, candidate); err != nil {
					f.log(prepared.Request.TaskID, "browse_candidate_detail", "failed", startedAt, err)
					stats.Failed++
					return fmt.Errorf("浏览候选人详情中断：%w", err)
				}
				f.log(prepared.Request.TaskID, "browse_candidate_detail", "success", startedAt, nil)
			}
		}
		var err error
		// 图片详情没有 DOM 正文；OCR 和 AI 看图直接读取截图，避免被空正文提前拦截。
		if detailMode != "ai" && !prepared.Position.RequiresOCR {
			detail, err = runtime.ExtractCandidateDetail(ctx, f.Browser, prepared.Platform, candidate)
			if err != nil {
				stats.Failed++
				f.saveCandidate(ctx, prepared, candidate, "detail", "failed", err.Error())
				return fmt.Errorf("读取候选人详情失败：%w", err)
			}
			detail.Text = runtime.CleanCandidateDetailText(detail.Text)
		}
		if prepared.Position.RequiresOCR {
			ocrStartedAt := time.Now()
			f.log(prepared.Request.TaskID, "ocr", "start", ocrStartedAt, nil)
			detail, err = f.readDetailWithOCR(ctx, prepared, candidate, detail, runtime.CleanCandidateDetailText)
			if err != nil {
				f.log(prepared.Request.TaskID, "ocr", "failed", ocrStartedAt, err)
				if ocr.IsNoText(err) {
					stats.Skipped++
					f.saveCandidate(ctx, prepared, candidate, "ocr", "skipped", err.Error())
					return nil
				}
				stats.Failed++
				f.saveCandidate(ctx, prepared, candidate, "ocr", "failed", err.Error())
				return fmt.Errorf("识别候选人详情失败：%w", err)
			}
			f.log(prepared.Request.TaskID, "ocr", "success", ocrStartedAt, nil)
		}
		if deferKeywordDecision && detailMode == "ai" {
			startedAt := time.Now()
			f.log(prepared.Request.TaskID, "vision_keyword_text", "start", startedAt, nil)
			detailImages, err = f.readDetailImages(ctx, prepared, candidate)
			if err == nil {
				detail.Text, err = f.AI.ReadDetailTextVision(ctx, prepared.Position.AI, detailImages)
			}
			if err != nil {
				f.log(prepared.Request.TaskID, "vision_keyword_text", "failed", startedAt, err)
				return fmt.Errorf("读取 AI 详情关键词文字失败：%w", err)
			}
			detail.Text = runtime.CleanCandidateDetailText(detail.Text)
			f.log(prepared.Request.TaskID, "vision_keyword_text", "success", startedAt, nil)
			shared.ReportProgress(f.Logger, prepared.Request.TaskID, fmt.Sprintf("AI 详情已读取 %d 字，开始核对关键词", len([]rune(detail.Text))))
		}
		if err := waitRandomSeconds(
			ctx, f.Logger, prepared.Request.TaskID, "detail_view",
			prepared.Preferences.DetailViewDelayMin, prepared.Preferences.DetailViewDelayMax,
		); err != nil {
			return err
		}
		if strings.TrimSpace(detail.Text) == "" && detailMode != "ai" {
			stats.Skipped++
			f.saveCandidate(ctx, prepared, candidate, "detail", "skipped", "候选人详情为空")
			return nil
		}
	}
	if deferKeywordDecision {
		keywordMatch := matchKeywords(candidate, detail.Text, prepared.Position)
		result := reportKeywordMatch(f.Logger, prepared.Request.TaskID, candidate, keywordMatch, !keywordMatch.Accepted || !prepared.Position.RequiresAI)
		f.recordScreeningResult(ctx, prepared, stats, result)
		if !keywordMatch.Accepted {
			stats.Skipped++
			f.saveCandidate(ctx, prepared, candidate, "filter", "skipped", keywordMatch.Reason)
			shared.ReportCandidateActivity(f.Logger, prepared.Request.TaskID, candidateDisplayName(candidate), "详情不匹配，已跳过", "skipped", nil)
			return nil
		}
	}
	accepted := true
	score := 0.0
	hasScore := false
	reason := "基础规则通过"
	evaluation := ai.Evaluation{}
	evaluationStatus := "failed"
	var evaluationCancel context.CancelFunc
	defer func() {
		if evaluationCancel != nil {
			f.finishCandidateEvaluationAsync(
				prepared,
				candidate,
				evaluationStatus,
				evaluation,
				evaluationCancel,
			)
		}
	}()
	if prepared.Position.RequiresAI {
		shared.ReportCandidateActivity(f.Logger, prepared.Request.TaskID, candidateDisplayName(candidate), "正在判断是否适合打招呼", "running", nil)
		var decision ai.Decision
		var decisionErr error
		decisionStartedAt := time.Now()
		reportAILoading(
			f.Logger,
			prepared.Request.TaskID,
			candidateDisplayName(candidate),
			"final",
			"AI 正在读取详情并判断匹配度",
		)
		f.log(prepared.Request.TaskID, "ai_decision", "start", decisionStartedAt, nil)
		evaluationCtx, cancelEvaluation := context.WithTimeout(context.WithoutCancel(ctx), candidateTimeout)
		evaluationCancel = cancelEvaluation
		// 等待评分时遵守候选人总时限；评分已返回后撤销关联，保留后台完整输出。
		stopEvaluationOnCandidateEnd := context.AfterFunc(ctx, cancelEvaluation)
		if detailMode == "ai" {
			images := detailImages
			var imageErr error
			if len(images) == 0 {
				images, imageErr = f.readDetailImages(ctx, prepared, candidate)
			}
			if imageErr != nil {
				decisionErr = imageErr
			} else {
				evaluation, decisionErr = f.AI.EvaluateCandidateVisionEarly(
					evaluationCtx, prepared.Position.AI, prepared.Position, candidate, detail, images,
				)
			}
		} else {
			evaluation, decisionErr = f.AI.EvaluateCandidateEarly(
				evaluationCtx, prepared.Position.AI, prepared.Position, candidate, detail,
			)
		}
		stopEvaluationOnCandidateEnd()
		if decisionErr != nil {
			reportAIError(f.Logger, prepared.Request.TaskID, candidate, decisionErr)
			evaluationCancel()
			evaluationCancel = nil
			f.log(prepared.Request.TaskID, "ai_decision", "failed", decisionStartedAt, decisionErr)
			stats.Failed++
			f.saveCandidate(ctx, prepared, candidate, "ai_decision", "failed", decisionErr.Error())
			return fmt.Errorf("判断候选人匹配度失败：%w", decisionErr)
		}
		decision = evaluation.Decision
		result := reportAIResult(
			f.Logger,
			prepared.Request.TaskID,
			candidate,
			decision,
			greetScoreThreshold(prepared.Position),
			"final",
			true,
		)
		f.recordScreeningResult(ctx, prepared, stats, result)
		f.log(prepared.Request.TaskID, "ai_decision", "success", decisionStartedAt, nil)
		accepted = decision.Accepted
		score = decision.Score
		hasScore = true
		reason = decision.Reason
	}
	// 收藏只使用已经打开详情后的 AI 分数，与招呼开关及招呼是否达标分别判断。
	if candidateFavoriteAllowed(detailOpened && hasScore && !isKeywordMode(prepared.Position), score, prepared.Position.AIOptions.FavoriteScoreThreshold) {
		startedAt := time.Now()
		f.log(prepared.Request.TaskID, "favorite_candidate", "start", startedAt, nil)
		shared.ReportCandidateActivity(f.Logger, prepared.Request.TaskID, candidateDisplayName(candidate), "正在收藏候选人", "running", nil)
		if err := runtime.FavoriteCandidate(ctx, f.Browser, prepared.Platform, candidate); err != nil {
			f.log(prepared.Request.TaskID, "favorite_candidate", "failed", startedAt, err)
			stats.Failed++
			f.saveCandidate(ctx, prepared, candidate, "favorite", "failed", err.Error())
			shared.ReportCandidateActivity(f.Logger, prepared.Request.TaskID, candidateDisplayName(candidate), "收藏没成功，详情先收好", "failed", err)
			return fmt.Errorf("收藏候选人失败：%w", err)
		}
		f.log(prepared.Request.TaskID, "favorite_candidate", "success", startedAt, nil)
		f.saveCandidate(ctx, prepared, candidate, "favorite", "success", fmt.Sprintf("收藏步骤已处理，详情 AI 评分 %.1f 严格大于收藏阈值 %.1f", score, prepared.Position.AIOptions.FavoriteScoreThreshold))
		shared.ReportCandidateActivity(f.Logger, prepared.Request.TaskID, candidateDisplayName(candidate), "收藏步骤已处理", "success", nil)
	}
	if detailOpened {
		if err := waitRandomSeconds(
			ctx, f.Logger, prepared.Request.TaskID, "before_detail_close",
			prepared.Preferences.DetailCloseDelayMin, prepared.Preferences.DetailCloseDelayMax,
		); err != nil {
			return err
		}
		closeDetailErr := runtime.CloseCandidateDetail(ctx, f.Browser, prepared.Platform, candidate)
		if closeDetailErr != nil {
			f.reportPageDiagnostics(ctx, prepared.Request.TaskID, "关闭候选人详情失败")
			stats.Failed++
			f.saveCandidate(ctx, prepared, candidate, "close_detail", "failed", closeDetailErr.Error())
			return fmt.Errorf("关闭候选人详情失败：%w", closeDetailErr)
		}
		detailOpened = false
	}
	if !accepted {
		evaluationStatus = "skipped"
		stats.Skipped++
		f.saveCandidate(ctx, prepared, candidate, "decision", "skipped", reason)
		shared.ReportCandidateActivity(f.Logger, prepared.Request.TaskID, candidateDisplayName(candidate), "匹配度未达标，已跳过", "skipped", nil)
		return nil
	}
	if prepared.Request.EnableGreet != nil && !*prepared.Request.EnableGreet {
		evaluationStatus = "scanned"
		stats.Skipped++
		f.saveCandidate(ctx, prepared, candidate, "check_only", "skipped", "本次只检查，未打招呼")
		shared.ReportCandidateActivity(f.Logger, prepared.Request.TaskID, candidateDisplayName(candidate), "本次只检查，未打招呼", "skipped", nil)
		return nil
	}
	if err := waitRandomSeconds(
		ctx, f.Logger, prepared.Request.TaskID, "before_greet",
		prepared.Preferences.GreetBeforeDelayMin, prepared.Preferences.GreetBeforeDelayMax,
	); err != nil {
		return err
	}
	infoRequest := model.CandidateInfoRequest{
		RequestPhone: prepared.Position.RequestPhone, RequestWechat: prepared.Position.RequestWechat,
		RequestResume: prepared.Position.RequestResume,
	}
	greetMessage := strings.TrimSpace(prepared.Position.GreetMessage)
	requestInfo := false
	if candidateInfoRequestConfigured(infoRequest) {
		threshold := requestScoreThreshold(prepared.Position)
		switch {
		case !hasScore:
			f.log(
				prepared.Request.TaskID, "request_candidate_info", "skipped", time.Now(),
				fmt.Errorf("没有最终 AI 评分，索要阈值为 %.1f", threshold),
			)
		case score <= threshold:
			f.log(
				prepared.Request.TaskID, "request_candidate_info", "skipped", time.Now(),
				fmt.Errorf("最终 AI 评分 %.1f 没有严格大于索要阈值 %.1f", score, threshold),
			)
		default:
			requestInfo = candidateInfoAllowed(hasScore, score, threshold)
		}
	}
	shared.ReportCandidateActivity(f.Logger, prepared.Request.TaskID, candidateDisplayName(candidate), "正在点击打招呼按钮", "running", nil)
	greetErr := runtime.GreetCandidate(ctx, f.Browser, prepared.Platform, candidate, model.GreetRequest{
		KeepConversationOpen: requestInfo || greetMessage != "",
	})
	if greetErr != nil {
		if errors.Is(greetErr, model.ErrCandidateAlreadyContacted) {
			evaluationStatus = "skipped"
			stats.Skipped++
			f.log(prepared.Request.TaskID, "greet", "skipped", time.Now(), greetErr)
			f.saveCandidate(ctx, prepared, candidate, "greet", "skipped", "候选人已经沟通过，本轮不重复打招呼")
			shared.ReportCandidateActivity(f.Logger, prepared.Request.TaskID, candidateDisplayName(candidate), "此前已沟通过，已跳过", "skipped", nil)
			return nil
		}
		f.reportPageDiagnostics(ctx, prepared.Request.TaskID, "打招呼失败")
		evaluationStatus = "failed"
		stats.Failed++
		f.saveCandidate(ctx, prepared, candidate, "greet", "failed", greetErr.Error())
		return fmt.Errorf("打招呼失败：%w", greetErr)
	}
	// 招呼已确认发送即保存成功；后续索要、发消息或清理失败不得把它变成未发送。
	evaluationStatus = "greeted"
	stats.Succeeded++
	f.saveCandidate(ctx, prepared, candidate, "greet", "success", reason)
	shared.ReportCandidateActivity(f.Logger, prepared.Request.TaskID, candidateDisplayName(candidate), "已成功打招呼", "success", nil)
	if prepared.Position.EnableSound && f.Notifier != nil {
		soundCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		if err := f.Notifier.PlaySuccess(soundCtx); err != nil {
			f.log(prepared.Request.TaskID, "play_success_sound", "warning", time.Now(), err)
		}
		cancel()
	}
	if requestInfo || greetMessage != "" {
		conversationReady := true
		startedAt := time.Now()
		if err := runtime.EnsureCandidateConversation(ctx, f.Browser, prepared.Platform, candidate); err != nil {
			if shared.ShouldStopTaskImmediately(err) {
				return err
			}
			conversationReady = false
			f.log(prepared.Request.TaskID, "ensure_candidate_conversation", "warning", startedAt, err)
			f.reportPageDiagnostics(ctx, prepared.Request.TaskID, "确认候选人聊天框失败")
		} else {
			f.log(prepared.Request.TaskID, "ensure_candidate_conversation", "success", startedAt, nil)
		}
		if conversationReady && requestInfo {
			startedAt = time.Now()
			if err := runtime.RequestCandidateInfo(ctx, f.Browser, prepared.Platform, candidate, infoRequest); err != nil {
				if shared.ShouldStopTaskImmediately(err) {
					return err
				}
				conversationReady = false
				f.log(prepared.Request.TaskID, "request_candidate_info", "warning", startedAt, err)
				f.reportPageDiagnostics(ctx, prepared.Request.TaskID, "索要候选人资料失败")
			} else {
				f.log(prepared.Request.TaskID, "request_candidate_info", "success", startedAt, nil)
			}
		}
		if conversationReady && greetMessage != "" {
			shared.ReportCandidateActivity(f.Logger, prepared.Request.TaskID, candidateDisplayName(candidate), "正在发送打招呼消息", "running", nil)
			startedAt = time.Now()
			if err := runtime.SendCandidateMessage(ctx, f.Browser, prepared.Platform, candidate, greetMessage); err != nil {
				if shared.ShouldStopTaskImmediately(err) {
					return err
				}
				f.log(prepared.Request.TaskID, "send_candidate_message", "failed", startedAt, err)
				shared.ReportCandidateActivity(f.Logger, prepared.Request.TaskID, candidateDisplayName(candidate), "打招呼消息没发成功，请检查当前聊天", "failed", err)
				f.reportPageDiagnostics(ctx, prepared.Request.TaskID, "发送候选人消息失败")
			} else {
				f.log(prepared.Request.TaskID, "send_candidate_message", "success", startedAt, nil)
				shared.ReportCandidateActivity(f.Logger, prepared.Request.TaskID, candidateDisplayName(candidate), "打招呼消息已发送", "success", nil)
			}
		}
	}
	// 每次成功发送都清理弹层，即使没有索要资料或追加消息；多层关闭共用完整预算。
	cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
	startedAt := time.Now()
	if err := runtime.CloseCandidateConversation(cleanupCtx, f.Browser, prepared.Platform, candidate); err != nil {
		if shared.ShouldStopTaskImmediately(err) {
			cleanupCancel()
			return err
		}
		f.log(prepared.Request.TaskID, "close_candidate_conversation", "warning", startedAt, err)
		f.reportPageDiagnostics(cleanupCtx, prepared.Request.TaskID, "关闭候选人聊天框失败")
	} else {
		f.log(prepared.Request.TaskID, "close_candidate_conversation", "success", startedAt, nil)
	}
	cleanupCancel()
	return nil
}

// saveCandidate 保存不含候选人详情的动作摘要。
func (f *Flow) saveCandidate(ctx context.Context, prepared shared.PreparedTask, candidate model.Candidate, action string, result string, reason string) {
	if err := f.Store.SaveCandidate(context.WithoutCancel(ctx), storage.CandidateRecord{
		TaskID: prepared.Request.TaskID, Fingerprint: candidate.Fingerprint,
		PlatformID: prepared.Position.PlatformID, DisplayName: candidate.Name,
		Action: action, Result: result, Reason: reason,
	}); err != nil {
		f.log(prepared.Request.TaskID, "save_candidate", "warning", time.Now(), err)
	}
}

// isKeywordMode 判断岗位是否使用免费关键词筛选模式。
func isKeywordMode(position cloud.PositionSnapshot) bool {
	return strings.EqualFold(strings.TrimSpace(position.CommonConfig.ModeDefault), "keyword")
}

// log 输出主动打招呼步骤日志。
func (f *Flow) log(taskID string, step string, status string, startedAt time.Time, err error) {
	if f.Logger != nil {
		f.Logger.Step(taskID, "greeting", step, status, startedAt, err)
	}
}
