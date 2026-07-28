// Package greeting 实现主动打招呼主流程，并在一个顶层步骤列表中展示完整顺序。
package greeting

import (
	"context"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/integration/ocr"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// Flow 组装主动打招呼流程依赖。
type Flow struct {
	Browser        *client.Client
	AI             *ai.Client
	OCR            *ocr.Client
	Store          *storage.Store
	Cloud          *cloud.Client
	Logger         shared.Logger
	ScreenshotsDir string
	DownloadsDir   string
}

type flowStep struct {
	name string
	run  func(context.Context) error
}

// Run 按平铺步骤启动浏览器、准备平台、处理候选人并同步摘要。
func (f *Flow) Run(ctx context.Context, prepared shared.PreparedTask, runtime model.Runtime) (shared.Stats, error) {
	stats := shared.Stats{}
	steps := []flowStep{
		{name: "start_browser", run: func(ctx context.Context) error { return f.startBrowser(ctx, prepared) }},
		{name: "prepare_platform", run: func(ctx context.Context) error {
			return runtime.PrepareGreeting(ctx, f.Browser, prepared.Platform, model.Position{
				ID: prepared.Position.ID, Name: prepared.Position.Name, Keyword: prepared.Position.Keyword,
			})
		}},
		{name: "scan_decide_and_greet", run: func(ctx context.Context) error {
			return f.processBatches(ctx, prepared, runtime, &stats)
		}},
		{name: "sync_summary", run: func(ctx context.Context) error {
			return f.syncSummary(ctx, prepared, stats, "completed", "", "")
		}},
	}
	for _, step := range steps {
		startedAt := time.Now()
		f.log(prepared.Request.TaskID, step.name, "start", startedAt, nil)
		if err := step.run(ctx); err != nil {
			f.log(prepared.Request.TaskID, step.name, "failed", startedAt, err)
			_ = f.syncSummary(context.WithoutCancel(ctx), prepared, stats, "failed", "FLOW_STEP_FAILED", err.Error())
			return stats, fmt.Errorf("%s 失败：%w", step.name, err)
		}
		f.log(prepared.Request.TaskID, step.name, "success", startedAt, nil)
	}
	return stats, nil
}

// startBrowser 使用当前 Profile 启动或复用 CloakBrowser。
func (f *Flow) startBrowser(ctx context.Context, prepared shared.PreparedTask) error {
	headless := prepared.Request.Headless
	humanize := true
	_, err := f.Browser.StartBrowser(ctx, contract.BrowserStartRequest{
		UserDataDir:   prepared.ProfilePath,
		DownloadsPath: f.DownloadsDir,
		Headless:      &headless,
		Humanize:      &humanize,
		Locale:        "zh-CN",
		Timezone:      "Asia/Shanghai",
	})
	return err
}

// processBatches 按配置批次扫描、判断和处理候选人。
func (f *Flow) processBatches(ctx context.Context, prepared shared.PreparedTask, runtime model.Runtime, stats *shared.Stats) error {
	maxBatches := prepared.Position.MaxBatches
	if maxBatches <= 0 {
		maxBatches = 1
	}
	for batch := 0; batch < maxBatches; batch++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		candidates, err := runtime.ScanCandidates(ctx, f.Browser, prepared.Platform)
		if err != nil {
			return err
		}
		if len(candidates) == 0 {
			return nil
		}
		for _, candidate := range candidates {
			if err := ctx.Err(); err != nil {
				return err
			}
			f.processCandidate(ctx, prepared, runtime, candidate, stats)
			if prepared.Position.MatchLimit > 0 && stats.Succeeded >= prepared.Position.MatchLimit {
				return nil
			}
		}
		if batch+1 < maxBatches {
			if err := runtime.ScrollCandidates(ctx, f.Browser, prepared.Platform); err != nil {
				return err
			}
		}
	}
	return nil
}

// processCandidate 平铺执行基础过滤、详情、OCR、AI、打招呼、关闭详情和保存结果。
func (f *Flow) processCandidate(ctx context.Context, prepared shared.PreparedTask, runtime model.Runtime, candidate model.Candidate, stats *shared.Stats) {
	stats.Processed++
	if !matchesKeyword(candidate, prepared.Position) {
		stats.Skipped++
		f.saveCandidate(ctx, prepared, candidate, "filter", "skipped", "基础关键词不匹配")
		return
	}
	detail, err := runtime.ReadCandidateDetail(ctx, f.Browser, prepared.Platform, candidate)
	if err != nil {
		stats.Failed++
		f.saveCandidate(ctx, prepared, candidate, "detail", "failed", err.Error())
		return
	}
	detailClosed := false
	defer func() {
		if !detailClosed {
			_ = runtime.CloseCandidateDetail(context.WithoutCancel(ctx), f.Browser, prepared.Platform)
		}
	}()
	if prepared.Position.RequiresOCR {
		detail, err = f.readDetailWithOCR(ctx, prepared, candidate, detail)
		if err != nil {
			stats.Failed++
			f.saveCandidate(ctx, prepared, candidate, "ocr", "failed", err.Error())
			return
		}
	}
	accepted := true
	reason := "基础规则通过"
	if prepared.Position.RequiresAI {
		decision, decisionErr := f.AI.EvaluateCandidate(ctx, prepared.Position.AI, prepared.Position, candidate, detail)
		if decisionErr != nil {
			stats.Failed++
			f.saveCandidate(ctx, prepared, candidate, "ai_decision", "failed", decisionErr.Error())
			return
		}
		accepted = decision.Accepted
		reason = decision.Reason
	}
	if !accepted {
		stats.Skipped++
		f.saveCandidate(ctx, prepared, candidate, "decision", "skipped", reason)
		return
	}
	if err := runtime.CloseCandidateDetail(ctx, f.Browser, prepared.Platform); err != nil {
		stats.Failed++
		f.saveCandidate(ctx, prepared, candidate, "close_detail", "failed", err.Error())
		return
	}
	detailClosed = true
	if err := runtime.GreetCandidate(ctx, f.Browser, prepared.Platform, candidate, prepared.Position.GreetMessage); err != nil {
		stats.Failed++
		f.saveCandidate(ctx, prepared, candidate, "greet", "failed", err.Error())
		return
	}
	stats.Succeeded++
	f.saveCandidate(ctx, prepared, candidate, "greet", "success", reason)
}

// readDetailWithOCR 截取当前详情页并在本地识别文字。
func (f *Flow) readDetailWithOCR(ctx context.Context, prepared shared.PreparedTask, candidate model.Candidate, detail model.CandidateDetail) (model.CandidateDetail, error) {
	filename := fmt.Sprintf("%s-%s.png", prepared.Request.TaskID, candidate.Fingerprint)
	var target *contract.SelectorSpec
	if selector, ok := prepared.Platform.Selectors["candidate.detail"]; ok {
		target = &selector
	}
	screenshot, err := f.Browser.Screenshot(ctx, contract.ScreenshotRequest{
		Directory: f.ScreenshotsDir,
		Filename:  filename,
		Target:    target,
	})
	if err != nil {
		return detail, err
	}
	result, err := f.OCR.Recognize(ctx, screenshot.Path)
	if err != nil {
		return detail, err
	}
	detail.Text = strings.TrimSpace(detail.Text + "\n" + result.Text)
	return detail, nil
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

// syncSummary 同步不含敏感内容的任务统计。
func (f *Flow) syncSummary(ctx context.Context, prepared shared.PreparedTask, stats shared.Stats, status string, errorCode string, errorMessage string) error {
	return f.Cloud.SyncSummary(ctx, prepared.Request.Token, cloud.TaskSummary{
		TaskID: prepared.Request.TaskID, PositionID: prepared.Position.ID,
		Status: status, Processed: stats.Processed, Succeeded: stats.Succeeded,
		Failed: stats.Failed, ErrorCode: errorCode, ErrorMessage: errorMessage,
	})
}

// matchesKeyword 执行不区分大小写的基础关键词过滤。
func matchesKeyword(candidate model.Candidate, position cloud.PositionSnapshot) bool {
	parts := []string{candidate.Name, candidate.Summary}
	for _, value := range candidate.Fields {
		parts = append(parts, value)
	}
	content := strings.ToLower(strings.Join(parts, "\n"))
	for _, keyword := range position.ExcludeKeywords {
		if keyword = strings.ToLower(strings.TrimSpace(keyword)); keyword != "" && strings.Contains(content, keyword) {
			return false
		}
	}
	keywords := position.Keywords
	if len(keywords) == 0 && strings.TrimSpace(position.Keyword) != "" {
		keywords = []string{position.Keyword}
	}
	if len(keywords) == 0 {
		return true
	}
	matched := 0
	valid := 0
	for _, keyword := range keywords {
		keyword = strings.ToLower(strings.TrimSpace(keyword))
		if keyword == "" {
			continue
		}
		valid++
		if strings.Contains(content, keyword) {
			matched++
		}
	}
	if valid == 0 {
		return true
	}
	if position.IsAndMode {
		return matched == valid
	}
	return matched > 0
}

// log 输出主动打招呼步骤日志。
func (f *Flow) log(taskID string, step string, status string, startedAt time.Time, err error) {
	if f.Logger != nil {
		f.Logger.Step(taskID, "greeting", step, status, startedAt, err)
	}
}
