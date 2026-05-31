// 本文件负责云端任务执行编排，按平台配置调用 Local Agent API 完成候选人筛选流程。
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

type claimedTaskCookie struct {
	CookieID      string
	EncryptedData string
	EncryptedKeys map[string]string
	DisplayName   string
}

// TaskExecutor 负责任务的云端编排执行。
type TaskExecutor struct {
	task           TaskRun
	platformCfg    PlatformConfig
	filter         *KeywordFilter
	position       map[string]any
	aiConfig       AIConfig
	defaultPrompts DefaultPrompts
	userPrefs      UserPreferences
	agentWS        *AgentWSHub
	httpClient     *http.Client
	logCallback    func(level, message string)
	countCallback  func(scanned, greeted, skipped, failed int)
	cookies        []map[string]any
	claimedCookie  *claimedTaskCookie
	candidateStore CandidateStore
	seenCandidates map[string]struct{}
	scannedCount   int
	greetedCount   int
	skippedCount   int
	failedCount    int
}

// NewTaskExecutor 创建任务编排器实例。
func NewTaskExecutor(
	task TaskRun,
	platformCfg PlatformConfig,
	position map[string]any,
	agentWS *AgentWSHub,
	aiConfig AIConfig,
	defaultPrompts DefaultPrompts,
	userPrefs UserPreferences,
	claimedCookie *claimedTaskCookie,
	candidateStore CandidateStore,
	logCallback func(level, message string),
	countCallback func(scanned, greeted, skipped, failed int),
) *TaskExecutor {
	var filter *KeywordFilter
	if task.Mode != "ai" && position != nil {
		keywords := toStringSlice(position["keywords"])
		exclude := toStringSlice(position["exclude"])
		isAndMode := false
		if v, ok := position["is_and_mode"].(bool); ok {
			isAndMode = v
		}
		filter = NewKeywordFilter(keywords, exclude, isAndMode, 7)
	}

	return &TaskExecutor{
		task:           task,
		platformCfg:    platformCfg,
		filter:         filter,
		position:       position,
		aiConfig:       aiConfig,
		defaultPrompts: defaultPrompts,
		userPrefs:      userPrefs,
		agentWS:        agentWS,
		httpClient:     &http.Client{Timeout: 120 * time.Second},
		logCallback:    logCallback,
		countCallback:  countCallback,
		claimedCookie:  claimedCookie,
		candidateStore: candidateStore,
		seenCandidates: make(map[string]struct{}),
	}
}

// Run 执行任务编排主流程。
func (e *TaskExecutor) Run(ctx context.Context) error {
	e.log("info", "任务执行开始")

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.prepareCookies(); err != nil {
		return fmt.Errorf("准备 cookie 失败: %w", err)
	}

	// 1. 启动浏览器
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.startBrowser(); err != nil {
		return fmt.Errorf("启动浏览器失败: %w", err)
	}
	defer e.stopBrowser()

	// 2. 打开平台推荐页
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.openPage(); err != nil {
		return fmt.Errorf("打开页面失败: %w", err)
	}

	// 3. 先处理当前可见候选人，处理完后再滚动下一屏
	idleRounds := 0
	for round := 1; ; round++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if e.reachedMatchLimit() {
			e.log("info", fmt.Sprintf("已达到任务上限 %d，停止继续处理", e.task.MatchLimit))
			break
		}

		e.log("info", fmt.Sprintf("开始处理第 %d 轮当前可见候选人", round))
		candidates, err := e.extractCandidates()
		if err != nil {
			return fmt.Errorf("提取候选人失败: %w", err)
		}
		if len(candidates) == 0 {
			e.log("warn", "当前可见区域未找到候选人")
		}
		newCandidates := e.filterNewCandidates(candidates)
		if len(newCandidates) == 0 {
			idleRounds++
			if idleRounds >= 2 {
				e.log("info", "连续两轮都没有新的可见候选人，结束本次任务")
				break
			}
			e.log("info", fmt.Sprintf("第 %d 轮没有新的可见候选人，准备滚动下一屏", round))
			if err := e.scrollPage(); err != nil {
				return fmt.Errorf("滚动加载失败: %w", err)
			}
			continue
		}

		idleRounds = 0
		e.log("info", fmt.Sprintf("第 %d 轮提取到 %d 个候选人，其中 %d 个为新候选人", round, len(candidates), len(newCandidates)))
		if err := e.processCandidates(ctx, newCandidates); err != nil {
			return fmt.Errorf("处理候选人失败: %w", err)
		}
		if e.reachedMatchLimit() {
			e.log("info", fmt.Sprintf("已达到任务上限 %d，停止继续处理", e.task.MatchLimit))
			break
		}
		e.log("info", fmt.Sprintf("第 %d 轮当前可见候选人处理完成，准备滚动下一屏", round))
		if err := e.scrollPage(); err != nil {
			return fmt.Errorf("滚动加载失败: %w", err)
		}
	}

	e.log("info", "任务执行完成")
	return nil
}

// startBrowser 调用 Local Agent 启动 CloakBrowser。
func (e *TaskExecutor) startBrowser() error {
	e.log("info", "正在启动浏览器")
	body := map[string]any{
		"persistent":    true,
		"user_data_dir": e.task.PlatformAccountID,
		"headless":      false,
		"humanize":      true,
	}
	if len(e.cookies) > 0 {
		e.log("info", fmt.Sprintf("启动浏览器时注入 %d 条 cookie", len(e.cookies)))
		body["cookies"] = e.cookies
	}
	var resp struct {
		Ok     bool   `json:"ok"`
		Status string `json:"status"`
	}
	if err := e.post("/api/v1/browser/start", body, &resp); err != nil {
		return err
	}
	if !resp.Ok {
		return fmt.Errorf("启动失败: %s", resp.Status)
	}
	return nil
}

// stopBrowser 调用 Local Agent 关闭浏览器。
func (e *TaskExecutor) stopBrowser() {
	e.log("info", "正在关闭浏览器")
	_ = e.post("/api/v1/browser/stop", nil, nil)
}

// openPage 打开平台推荐页。
func (e *TaskExecutor) openPage() error {
	return e.platformCfg.OpenEntryPage(e, e.cookies)
}

func (e *TaskExecutor) prepareCookies() error {
	if e.claimedCookie == nil {
		e.log("warn", "当前任务未绑定平台账号 cookie，将按未登录状态继续执行")
		return nil
	}
	e.log("info", fmt.Sprintf("准备解密任务 cookie：账号=%s cookie=%s", e.claimedCookie.DisplayName, e.claimedCookie.CookieID))
	var resp struct {
		Ok      bool             `json:"ok"`
		Cookies []map[string]any `json:"cookies"`
		Count   int              `json:"count"`
	}
	if err := e.post("/api/v1/cookies/decrypt", map[string]any{
		"encrypted_data": e.claimedCookie.EncryptedData,
		"encrypted_keys": e.claimedCookie.EncryptedKeys,
	}, &resp); err != nil {
		return err
	}
	if !resp.Ok {
		return fmt.Errorf("本地程序未返回成功状态")
	}
	e.cookies = resp.Cookies
	e.log("info", fmt.Sprintf("任务 cookie 解密成功，共 %d 条", len(e.cookies)))
	return nil
}

// scrollPage 滚动加载候选人列表。
func (e *TaskExecutor) scrollPage() error {
	return e.platformCfg.ScrollCandidateList(e, e.userPrefs)
}

// extractCandidates 从页面提取候选人卡片。
func (e *TaskExecutor) extractCandidates() ([]Candidate, error) {
	return e.platformCfg.ListVisibleCandidates(e)
}

// candidatePersistenceContext 保存当前候选人的持久化上下文。
type candidatePersistenceContext struct {
	Profile    TaskCandidate
	Engagement CandidateEngagement
}

// processCandidates 逐候选人筛选和打招呼。
func (e *TaskExecutor) processCandidates(ctx context.Context, candidates []Candidate) error {
	for i := range candidates {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if e.reachedMatchLimit() {
			e.log("info", fmt.Sprintf("已达到任务上限 %d，本轮停止继续处理候选人", e.task.MatchLimit))
			return nil
		}

		candidate := candidates[i]
		candidateName := candidate.DisplayName()
		e.log("info", fmt.Sprintf("处理候选人 %s（%d/%d）", candidateName, i+1, len(candidates)))
		e.incrementCounts(1, 0, 0, 0)

		baseText := strings.TrimSpace(e.platformCfg.CandidateFilterText(candidate))
		persistence, err := e.prepareCandidatePersistence(candidate, baseText, baseText, "")
		if err != nil {
			e.log("warn", fmt.Sprintf("候选人 %s 初始化简历库记录失败: %v", candidateName, err))
		}
		shouldOpenDetail, detailScoreDecision, err := e.decideOpenDetail(baseText)
		if err != nil {
			e.log("error", fmt.Sprintf("候选人 %s 详情决策失败: %v", candidateName, err))
			e.incrementCounts(0, 0, 0, 1)
			continue
		}
		candidate.AI.Detail.Score = float64Ptr(detailScoreDecision.Score)
		candidate.AI.Detail.Reason = strings.TrimSpace(detailScoreDecision.Reason)
		e.saveCandidateEvent(persistence, CandidateEvent{
			EventType:  "detail_analysis",
			Score:      float64Ptr(detailScoreDecision.Score),
			Reason:     strings.TrimSpace(detailScoreDecision.Reason),
			InputText:  baseText,
			OutputText: scoreDecisionOutput(detailScoreDecision),
			Model:      e.aiConfig.Model,
			TokenUsage: detailScoreDecision.TokenUsage,
			Metadata: map[string]any{
				"should_open_detail": shouldOpenDetail,
				"threshold":          e.detailThreshold(),
			},
		})
		if detailScoreDecision.Reason != "" {
			e.log("info", fmt.Sprintf("候选人 %s 看详情评分: %.1f，原因: %s（token=%d）", candidateName, detailScoreDecision.Score, detailScoreDecision.Reason, detailScoreDecision.TokenUsage))
		}
		detailFetchedAt := (*time.Time)(nil)
		detailText := ""
		if shouldOpenDetail {
			detailText, err = e.platformCfg.FetchCandidateDetailText(e, e.userPrefs, candidate, e.positionDetailMode())
			if err != nil {
				e.log("error", fmt.Sprintf("候选人 %s 详情提取失败: %v", candidateName, err))
				e.incrementCounts(0, 0, 0, 1)
				continue
			}
			e.log("info", fmt.Sprintf("候选人 %s 详情文本: %s", candidateName, previewDetailLog(detailText, 800)))
			now := time.Now().UTC()
			detailFetchedAt = &now
			e.saveCandidateEvent(persistence, CandidateEvent{
				EventType:   "detail_fetched",
				InputText:   baseText,
				OutputText:  detailText,
				MessageText: previewDetailLog(detailText, 300),
				Metadata: map[string]any{
					"detail_mode": e.positionDetailMode(),
				},
			})
		}
		filterText := e.mergeCandidateTexts(baseText, detailText)
		if updated, err := e.prepareCandidatePersistence(candidate, baseText, filterText, detailText); err == nil {
			persistence = updated
		}
		if persistence != nil && detailFetchedAt != nil {
			_ = e.candidateStore.UpdateCandidateEngagementStatus(persistence.Engagement.ID, "analyzed", detailFetchedAt, nil)
		}

		// 筛选逻辑
		if e.task.Mode == "ai" {
			greetDecision, err := e.callGreetScoreAI(e.positionDescription(), filterText)
			if err != nil {
				e.log("error", fmt.Sprintf("AI 筛选失败: %v", err))
				e.incrementCounts(0, 0, 0, 1)
				continue
			}
			candidate.AI.Greet.Score = float64Ptr(greetDecision.Score)
			candidate.AI.Greet.Reason = strings.TrimSpace(greetDecision.Reason)
			e.saveCandidateEvent(persistence, CandidateEvent{
				EventType:  "greet_analysis",
				Score:      float64Ptr(greetDecision.Score),
				Reason:     strings.TrimSpace(greetDecision.Reason),
				InputText:  filterText,
				OutputText: scoreDecisionOutput(greetDecision),
				Model:      e.aiConfig.Model,
				TokenUsage: greetDecision.TokenUsage,
				Metadata: map[string]any{
					"threshold": e.greetThreshold(),
				},
			})
			e.log("info", fmt.Sprintf("候选人 %s 打招呼评分: %.1f，原因: %s", candidateName, greetDecision.Score, greetDecision.Reason))
			shouldGreet, finalGreetScore, finalGreetReason, reviewDecision, usedReview := e.evaluateGreetScore(greetDecision, filterText)
			if usedReview {
				candidate.AI.Review.Score = float64Ptr(reviewDecision.Score)
				candidate.AI.Review.Reason = strings.TrimSpace(reviewDecision.Reason)
				e.saveCandidateEvent(persistence, CandidateEvent{
					EventType:  "review_analysis",
					Score:      float64Ptr(reviewDecision.Score),
					Reason:     strings.TrimSpace(reviewDecision.Reason),
					InputText:  filterText,
					OutputText: scoreDecisionOutput(reviewDecision),
					Model:      e.aiConfig.Model,
					TokenUsage: reviewDecision.TokenUsage,
					Metadata: map[string]any{
						"threshold": e.greetThreshold(),
					},
				})
				e.log("info", fmt.Sprintf("候选人 %s 复核评分: %.1f，原因: %s（token=%d）", candidateName, reviewDecision.Score, reviewDecision.Reason, reviewDecision.TokenUsage))
			}
			if !shouldGreet {
				e.log("info", fmt.Sprintf("候选人 %s AI 筛选跳过: %s（最终评分=%.1f，阈值=%.1f）", candidateName, finalGreetReason, finalGreetScore, e.greetThreshold()))
				e.saveCandidateEvent(persistence, CandidateEvent{
					EventType: "candidate_skipped",
					Score:     float64Ptr(finalGreetScore),
					Reason:    finalGreetReason,
					InputText: filterText,
					Metadata: map[string]any{
						"mode":      "ai",
						"threshold": e.greetThreshold(),
					},
				})
				e.updateEngagementStatus(persistence, "skipped", nil, nil)
				e.incrementCounts(0, 0, 1, 0)
				continue
			}
			e.log("info", fmt.Sprintf("候选人 %s AI 通过: %s（最终评分=%.1f，阈值=%.1f）", candidateName, finalGreetReason, finalGreetScore, e.greetThreshold()))
		} else if e.filter != nil {
			result := e.filter.Filter(filterText)
			if !result.Passed {
				e.log("info", fmt.Sprintf("候选人 %s 被筛选跳过: %s", candidateName, result.Reason))
				e.saveCandidateEvent(persistence, CandidateEvent{
					EventType: "candidate_skipped",
					Reason:    result.Reason,
					InputText: filterText,
					Metadata:  map[string]any{"mode": "keyword"},
				})
				e.updateEngagementStatus(persistence, "skipped", nil, nil)
				e.incrementCounts(0, 0, 1, 0)
				continue
			}
			e.log("info", fmt.Sprintf("候选人 %s 通过筛选: %s", candidateName, result.Reason))
		}

		// 打招呼：交由平台动作实现
		if err := e.platformCfg.GreetCandidate(e, e.userPrefs, candidate, e.positionGreetMessage()); err != nil {
			e.log("error", fmt.Sprintf("候选人 %s 打招呼失败: %v", candidateName, err))
			e.incrementCounts(0, 0, 0, 1)
			continue
		}
		e.log("info", fmt.Sprintf("候选人 %s 打招呼成功", candidateName))
		now := time.Now().UTC()
		e.saveCandidateEvent(persistence, CandidateEvent{
			EventType:   "greet_success",
			Reason:      "打招呼成功",
			InputText:   filterText,
			MessageText: e.positionGreetMessage(),
			Metadata: map[string]any{
				"candidate_name": candidateName,
			},
		})
		e.updateEngagementStatus(persistence, "greeted", detailFetchedAt, &now)
		e.incrementCounts(0, 1, 0, 0)
		if e.task.EnableSound {
			if err := e.playSuccessSound(); err != nil {
				e.log("warn", fmt.Sprintf("播放成功提示音失败: %v", err))
			}
		}
	}
	return nil
}

// positionGreetMessage 返回岗位模板配置的打招呼语。
func (e *TaskExecutor) positionGreetMessage() string {
	if e.position == nil {
		return ""
	}
	if message, ok := e.position["greet_message"].(string); ok {
		return strings.TrimSpace(message)
	}
	return ""
}

// previewDetailLog 生成详情文本日志预览，避免日志过长。
func previewDetailLog(text string, maxRunes int) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "（空）"
	}
	runes := []rune(trimmed)
	if maxRunes <= 0 || len(runes) <= maxRunes {
		return fmt.Sprintf("len=%d, content=%s", len(runes), trimmed)
	}
	return fmt.Sprintf("len=%d, preview=%s...(已截断)", len(runes), string(runes[:maxRunes]))
}

// decideOpenDetail 根据任务模式决定本次是否需要打开详情。
func (e *TaskExecutor) decideOpenDetail(baseText string) (bool, AIScoreDecision, error) {
	if strings.TrimSpace(baseText) == "" {
		return false, AIScoreDecision{Score: 0, Reason: "基础信息为空，跳过详情", TokenUsage: 0}, nil
	}
	if e.task.Mode == "ai" {
		decision, err := e.callOpenDetailScoreAI(e.positionDescription(), baseText)
		if err != nil {
			return false, AIScoreDecision{}, err
		}
		threshold := e.detailThreshold()
		return decision.Score >= threshold, decision, nil
	}
	shouldOpen, reason, token, err := rollDetailOpenByProbability(e.userPrefs.DetailOpenProbability)
	return shouldOpen, AIScoreDecision{Score: 0, Reason: reason, TokenUsage: token}, err
}

// mergeCandidateTexts 合并候选人基础信息和详情文本，供筛选流程使用。
func (e *TaskExecutor) mergeCandidateTexts(baseText, detailText string) string {
	base := strings.TrimSpace(baseText)
	detail := strings.TrimSpace(detailText)
	if detail == "" {
		return base
	}
	if base == "" {
		return detail
	}
	return base + "\n详情信息：\n" + detail
}

// positionDetailMode 返回岗位模板配置的详情读取模式。
func (e *TaskExecutor) positionDetailMode() string {
	if e.position == nil {
		return "dom"
	}
	common, _ := e.position["common_config"].(map[string]any)
	if mode, ok := common["detail_mode"].(string); ok && strings.TrimSpace(mode) != "" {
		return strings.TrimSpace(mode)
	}
	return "dom"
}

func (e *TaskExecutor) incrementCounts(scanned, greeted, skipped, failed int) {
	e.scannedCount += scanned
	e.greetedCount += greeted
	e.skippedCount += skipped
	e.failedCount += failed
	if e.countCallback != nil {
		e.countCallback(scanned, greeted, skipped, failed)
	}
}

// reachedMatchLimit 判断当前任务是否已经达到打招呼上限。
func (e *TaskExecutor) reachedMatchLimit() bool {
	if e.task.MatchLimit <= 0 {
		return false
	}
	return e.greetedCount >= e.task.MatchLimit
}

// filterNewCandidates 过滤掉当前任务轮次里已经处理过的候选人。
func (e *TaskExecutor) filterNewCandidates(candidates []Candidate) []Candidate {
	result := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		key := e.platformCfg.CandidateFingerprint(candidate)
		if key == "" {
			result = append(result, candidate)
			continue
		}
		if _, exists := e.seenCandidates[key]; exists {
			continue
		}
		e.seenCandidates[key] = struct{}{}
		result = append(result, candidate)
	}
	return result
}

// prepareCandidatePersistence 保存候选人主体并创建本次触达上下文。
// candidate 为平台候选人对象，baseText/filterText/detailText 为本次抽取文本。
func (e *TaskExecutor) prepareCandidatePersistence(candidate Candidate, baseText, filterText, detailText string) (*candidatePersistenceContext, error) {
	if e.candidateStore == nil {
		return nil, nil
	}
	if strings.TrimSpace(candidate.Detail.Text) == "" && strings.TrimSpace(detailText) != "" {
		candidate.Detail.Text = strings.TrimSpace(detailText)
	}
	profile, err := e.candidateStore.SaveCandidateProfile(CandidateProfileInput{
		UserEmail:           e.task.UserEmail,
		PlatformID:          e.task.PlatformID,
		PlatformCandidateID: strings.TrimSpace(candidate.PlatformCandidateID),
		CandidateName:       candidate.DisplayName(),
		BirthYM:             strings.TrimSpace(candidate.BasicProfile.BirthYM),
		Phone:               strings.TrimSpace(candidate.BasicProfile.Phone),
		Email:               strings.TrimSpace(candidate.BasicProfile.Email),
		WorkRegion:          strings.TrimSpace(candidate.BasicProfile.WorkRegion),
		WorkYears:           strings.TrimSpace(candidate.BasicProfile.WorkYears),
		ExpectedSalaryMin:   candidate.BasicProfile.ExpectedSalary.Min,
		ExpectedSalaryMax:   candidate.BasicProfile.ExpectedSalary.Max,
		BasicInfo:           strings.TrimSpace(firstNonEmpty(candidate.BasicInfo, baseText)),
		EducationLevel:      strings.TrimSpace(candidate.EducationLevel),
		ExpectedPosition:    strings.TrimSpace(candidate.BasicProfile.ExpectedPosition),
		OnlineStatus:        strings.TrimSpace(candidate.BasicProfile.OnlineStatus),
		PersonalDescription: strings.TrimSpace(candidate.PersonalDescription),
		RawText:             strings.TrimSpace(firstNonEmpty(candidate.RawText, baseText)),
		FilterText:          strings.TrimSpace(firstNonEmpty(candidate.FilterText, filterText)),
		WorkExperiences:     candidate.BasicProfile.WorkExperiences,
		Educations:          candidate.BasicProfile.Educations,
		Certificates:        candidate.BasicProfile.Certificates,
		Honors:              candidate.BasicProfile.Honors,
		ProjectExperiences:  candidate.BasicProfile.ProjectExperiences,
		Communications:      candidate.BasicProfile.ColleagueCommunications,
		ResumeURL:           strings.TrimSpace(candidate.ResumeAttachment.URL),
		ResumeText:          strings.TrimSpace(firstNonEmpty(candidate.ResumeAttachment.ExtractedText, detailText)),
		Ext:                 candidate.Ext,
		FirstSeenAt:         parseOptionalRFC3339(candidate.Timestamps.FirstSeenAt),
	})
	if err != nil {
		return nil, err
	}
	engagement, err := e.candidateStore.UpsertCandidateEngagement(CandidateEngagement{
		CandidateID:       profile.ID,
		UserEmail:         e.task.UserEmail,
		TaskID:            e.task.ID,
		PositionID:        e.task.PositionID,
		PlatformAccountID: e.task.PlatformAccountID,
		PlatformID:        e.task.PlatformID,
		Status:            "created",
		FirstSeenAt:       profile.FirstSeenAt,
	})
	if err != nil {
		return nil, err
	}
	return &candidatePersistenceContext{Profile: profile, Engagement: engagement}, nil
}

// saveCandidateEvent 保存候选人事件流水。
// persistence 为当前候选人上下文，event 为要写入的事件。
func (e *TaskExecutor) saveCandidateEvent(persistence *candidatePersistenceContext, event CandidateEvent) {
	if e.candidateStore == nil || persistence == nil {
		return
	}
	event.CandidateID = persistence.Profile.ID
	event.EngagementID = persistence.Engagement.ID
	event.TaskID = e.task.ID
	event.PositionID = e.task.PositionID
	event.PlatformAccountID = e.task.PlatformAccountID
	event.PlatformID = e.task.PlatformID
	if _, err := e.candidateStore.SaveCandidateEvent(event); err != nil {
		e.log("warn", fmt.Sprintf("候选人事件保存失败 type=%s candidate=%s err=%v", event.EventType, persistence.Profile.CandidateName, err))
	}
}

// updateEngagementStatus 更新触达上下文状态。
// persistence 为当前候选人上下文，status 为目标状态。
func (e *TaskExecutor) updateEngagementStatus(persistence *candidatePersistenceContext, status string, detailFetchedAt *time.Time, greetedAt *time.Time) {
	if e.candidateStore == nil || persistence == nil {
		return
	}
	if err := e.candidateStore.UpdateCandidateEngagementStatus(persistence.Engagement.ID, status, detailFetchedAt, greetedAt); err != nil {
		e.log("warn", fmt.Sprintf("候选人触达状态更新失败 engagement=%s status=%s err=%v", persistence.Engagement.ID, status, err))
	}
}

// scoreDecisionOutput 将 AI 评分结果转换成 JSON 文本。
// decision 为评分结果，返回用于事件流水保存的输出文本。
func scoreDecisionOutput(decision AIScoreDecision) string {
	raw, err := json.Marshal(map[string]any{
		"score":       decision.Score,
		"reason":      decision.Reason,
		"token_usage": decision.TokenUsage,
	})
	if err != nil {
		return ""
	}
	return string(raw)
}

func firstNonEmpty(primary, fallback string) string {
	text := strings.TrimSpace(primary)
	if text != "" {
		return text
	}
	return strings.TrimSpace(fallback)
}

func parseOptionalRFC3339(raw string) *time.Time {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil
	}
	value, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return nil
	}
	return &value
}

func (e *TaskExecutor) playSuccessSound() error {
	return e.post("/api/v1/sound/play", map[string]any{
		"kind": "success",
	}, nil)
}

// ---------- Local Agent WebSocket 客户端 ----------

// post 通过 WebSocket 向 Local Agent 发送浏览器操作请求。
func (e *TaskExecutor) post(path string, body any, result any) error {
	if e.agentWS == nil {
		return fmt.Errorf("Local Agent WebSocket 未初始化")
	}
	e.log("info", fmt.Sprintf("正在请求本地程序：%s", path))
	payload := map[string]any{
		"path": path,
		"body": body,
	}
	resp, err := e.agentWS.SendCommand(e.task.UserEmail, AgentWSMessage{
		Type:    "local.http.post",
		TaskID:  e.task.ID,
		Payload: payload,
	}, 3)
	if err != nil {
		e.log("error", fmt.Sprintf("本地程序请求失败：%s，err=%v", path, err))
		if payloadJSON, marshalErr := json.Marshal(payload); marshalErr == nil {
			e.log("error", fmt.Sprintf("本地程序失败请求参数：%s", string(payloadJSON)))
		} else {
			e.log("error", fmt.Sprintf("本地程序失败请求参数序列化失败：%v", marshalErr))
		}
		if detail := localAgentReplyDetail(resp); detail != "" {
			e.log("error", fmt.Sprintf("本地程序详细错误：%s", detail))
		}
		return fmt.Errorf("请求 Local Agent 失败 (%s): %w", path, err)
	}
	e.log("info", fmt.Sprintf("本地程序响应成功：%s", path))

	if result != nil {
		respBytes, err := json.Marshal(resp.Payload)
		if err != nil {
			return fmt.Errorf("序列化 Local Agent 响应失败: %w", err)
		}
		if err := json.Unmarshal(respBytes, result); err != nil {
			return fmt.Errorf("解析响应失败: %w", err)
		}
	}

	return nil
}

func localAgentReplyDetail(resp AgentWSMessage) string {
	if len(resp.Payload) == 0 {
		return ""
	}
	if traceback, ok := resp.Payload["traceback"].(string); ok && strings.TrimSpace(traceback) != "" {
		return strings.TrimSpace(traceback)
	}
	if detail, ok := resp.Payload["detail"].(string); ok && strings.TrimSpace(detail) != "" {
		return strings.TrimSpace(detail)
	}
	return ""
}

// log 记录任务执行日志。
func (e *TaskExecutor) log(level, message string) {
	if e.logCallback != nil {
		e.logCallback(level, message)
	}
}

// toStringSlice 将 interface{} 转为 []string。
func toStringSlice(v any) []string {
	if v == nil {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func greetDelayBefore(prefs UserPreferences) float64 {
	if prefs.GreetDelayMax > prefs.GreetDelayMin && prefs.GreetDelayMin >= 0 {
		return (prefs.GreetDelayMin + prefs.GreetDelayMax) / 2
	}
	if prefs.GreetDelayMin >= 0 {
		return prefs.GreetDelayMin
	}
	return 1
}

func detailDelayBefore(prefs UserPreferences) float64 {
	if prefs.DetailViewDelayMax > prefs.DetailViewDelayMin && prefs.DetailViewDelayMin >= 0 {
		return (prefs.DetailViewDelayMin + prefs.DetailViewDelayMax) / 2
	}
	if prefs.DetailViewDelayMin >= 0 {
		return prefs.DetailViewDelayMin
	}
	return 1
}

// ---------- AI 筛选 ----------

type AIRequest struct {
	Model          string            `json:"model"`
	Messages       []AIMsg           `json:"messages"`
	Temperature    float64           `json:"temperature"`
	ResponseFormat map[string]string `json:"response_format,omitempty"`
}
type AIMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type AIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}
type AIScoreDecision struct {
	Score      float64 `json:"score"`
	Reason     string  `json:"reason"`
	TokenUsage int     `json:"token_usage"`
}

const defaultAIGreetScorePrompt = `你是一个资深的HR专家。请根据岗位要求给候选人打“打招呼建议分”。

重要提示：
1. 仅输出 JSON，不能输出其它内容。
2. 返回字段必须是 score 和 reason。
3. score 范围是 0-100，可以是小数。
4. reason 控制在30字以内。

岗位要求：
%s

候选人信息：
%s

请返回JSON：{"score": 78, "reason": "匹配核心要求"}`

const defaultOpenDetailScorePrompt = `你是一个资深的HR专家。请根据岗位要求给候选人打“查看详情建议分”。

重要提示：
1. 仅根据候选人基础信息评估是否值得打开详情。
2. 仅输出 JSON，不能输出其它内容。
3. 返回字段必须是 score 和 reason。
4. score 范围是 0-100，可以是小数。
5. reason 控制在30字以内。

岗位要求：
%s

候选人基础信息：
%s

请返回JSON：{"score": 66, "reason": "可进一步确认细节"}`

const defaultAIReviewScorePrompt = `你是一个资深的HR专家。当前候选人分数接近岗位阈值，请做“打招呼前二次复核评分”。

重要提示：
1. 仅输出 JSON，不能输出其它内容。
2. 返回字段必须是 score 和 reason。
3. score 范围是 0-100，可以是小数。
4. reason 控制在30字以内。
5. 评分更关注风险点与关键硬指标。

岗位要求：
%s

候选人信息：
%s

请返回JSON：{"score": 72, "reason": "边界候选人可谨慎通过"}`

// positionDescription 从岗位信息中提取职位要求文本。
func (e *TaskExecutor) positionDescription() string {
	if requirement := e.positionAIConfigString("position_requirement"); requirement != "" {
		return requirement
	}
	if e.position == nil {
		return ""
	}
	if desc, ok := e.position["name"].(string); ok && desc != "" {
		return desc
	}
	return ""
}

// positionAIConfigString 读取岗位模板中的 AI 文本配置。
func (e *TaskExecutor) positionAIConfigString(keys ...string) string {
	if e.position == nil {
		return ""
	}
	aiConfig, _ := e.position["ai_config"].(map[string]any)
	for _, key := range keys {
		if value, ok := aiConfig[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// aiRequestConfig 返回当前任务使用的 AI 请求配置。
func (e *TaskExecutor) aiRequestConfig() (string, string, float64) {
	model := strings.TrimSpace(e.aiConfig.Model)
	baseURL := strings.TrimSpace(e.aiConfig.BaseURL)
	temperature := e.aiConfig.Temperature

	if e.userPrefs.AIModel != "" {
		model = e.userPrefs.AIModel
	}
	return model, baseURL, temperature
}

// doAIChat 调用 AI API，返回原始文本和 token 消耗。
func (e *TaskExecutor) doAIChat(prompt string, forceJSON bool) (string, int, error) {
	model, baseURL, temperature := e.aiRequestConfig()
	if baseURL == "" {
		return "", 0, fmt.Errorf("AI 配置缺少 base_url")
	}
	if model == "" {
		return "", 0, fmt.Errorf("AI 配置缺少 model")
	}
	if e.aiConfig.APIKey == "" {
		return "", 0, fmt.Errorf("AI 配置缺少 API Key")
	}
	reqBody := AIRequest{
		Model:       model,
		Messages:    []AIMsg{{Role: "user", Content: prompt}},
		Temperature: temperature,
	}
	if forceJSON {
		reqBody.ResponseFormat = map[string]string{"type": "json_object"}
	}

	// 输出请求体，便于排查模型入参。
	// if bodyPreview, err := json.Marshal(reqBody); err == nil {
	// 	e.log("info", fmt.Sprintf("AI请求体：%s", string(bodyPreview)))
	// }

	reqBody.Model = model
	data, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest(http.MethodPost, baseURL, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.aiConfig.APIKey)
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("AI API 请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", 0, fmt.Errorf("AI API 错误 %d", resp.StatusCode)
	}
	var aiResp AIResponse
	if err := json.Unmarshal(body, &aiResp); err != nil {
		return "", 0, fmt.Errorf("解析 AI 响应失败: %w", err)
	}
	if len(aiResp.Choices) == 0 {
		return "", aiResp.Usage.TotalTokens, fmt.Errorf("AI 未返回结果")
	}

	// 输出响应体，便于排查模型输出。
	if bodyPreview, err := json.Marshal(aiResp); err == nil {
		e.log("info", fmt.Sprintf("AI响应体：%s", string(bodyPreview)))
	}
	return strings.TrimSpace(aiResp.Choices[0].Message.Content), aiResp.Usage.TotalTokens, nil
}

// decodeJSONWithRetry 解析 AI JSON 输出，失败时要求 AI 重新只输出一次合法 JSON。
func (e *TaskExecutor) decodeJSONWithRetry(raw string, target any) error {
	if tryDecodeJSON(raw, target) == nil {
		return nil
	}
	repairPrompt := fmt.Sprintf(
		"下面这段内容本应是一个合法 JSON，但当前无法解析。请只返回一次合法 JSON，不要添加解释。\n原始输出：\n%s",
		raw,
	)
	repaired, _, err := e.doAIChat(repairPrompt, true)
	if err != nil {
		return err
	}
	if err := tryDecodeJSON(repaired, target); err != nil {
		return fmt.Errorf("AI JSON 解析失败: %w", err)
	}
	return nil
}

// callOpenDetailScoreAI 调用 AI 返回查看详情评分。
func (e *TaskExecutor) callOpenDetailScoreAI(jobDesc, candidateText string) (AIScoreDecision, error) {
	prompt := fmt.Sprintf(defaultOpenDetailScorePrompt, jobDesc, candidateText)
	if customPrompt := e.effectivePrompt(e.defaultPrompts.OpenDetailPrompt, "open_detail_prompt"); customPrompt != "" {
		prompt = buildPromptFromTemplate(customPrompt, jobDesc, candidateText, prompt, "补充要求")
	}
	content, tokens, err := e.doAIChat(prompt, true)
	if err != nil {
		return AIScoreDecision{}, err
	}
	var decision AIScoreDecision
	if err := e.decodeJSONWithRetry(content, &decision); err != nil {
		return AIScoreDecision{}, err
	}
	decision.Score = clampScore(decision.Score)
	decision.Reason = truncateText(strings.TrimSpace(decision.Reason), 30)
	decision.TokenUsage = tokens
	return decision, nil
}

// callGreetScoreAI 调用 AI 返回打招呼评分。
func (e *TaskExecutor) callGreetScoreAI(jobDesc, candidateText string) (AIScoreDecision, error) {
	prompt := fmt.Sprintf(defaultAIGreetScorePrompt, jobDesc, candidateText)
	if customPrompt := e.effectivePrompt(e.defaultPrompts.FilterPrompt, "greet_prompt", "filter_prompt", "click_prompt"); customPrompt != "" {
		prompt = buildPromptFromTemplate(customPrompt, jobDesc, candidateText, prompt, "补充规则")
	}
	content, tokens, err := e.doAIChat(prompt, true)
	if err != nil {
		return AIScoreDecision{}, err
	}
	var decision AIScoreDecision
	if err := e.decodeJSONWithRetry(content, &decision); err != nil {
		return AIScoreDecision{}, err
	}
	decision.Score = clampScore(decision.Score)
	decision.Reason = truncateText(strings.TrimSpace(decision.Reason), 30)
	decision.TokenUsage = tokens
	return decision, nil
}

// callReviewScoreAI 调用 AI 返回临界分复核评分。
func (e *TaskExecutor) callReviewScoreAI(jobDesc, candidateText string) (AIScoreDecision, error) {
	prompt := fmt.Sprintf(defaultAIReviewScorePrompt, jobDesc, candidateText)
	if customPrompt := e.positionAIConfigString("review_prompt"); customPrompt != "" {
		prompt = buildPromptFromTemplate(customPrompt, jobDesc, candidateText, prompt, "复核规则")
	}
	content, tokens, err := e.doAIChat(prompt, true)
	if err != nil {
		return AIScoreDecision{}, err
	}
	var decision AIScoreDecision
	if err := e.decodeJSONWithRetry(content, &decision); err != nil {
		return AIScoreDecision{}, err
	}
	decision.Score = clampScore(decision.Score)
	decision.Reason = truncateText(strings.TrimSpace(decision.Reason), 30)
	decision.TokenUsage = tokens
	return decision, nil
}

// evaluateGreetScore 根据打招呼评分和阈值决定是否打招呼，并在临界区间执行复核。
func (e *TaskExecutor) evaluateGreetScore(initial AIScoreDecision, candidateText string) (bool, float64, string, AIScoreDecision, bool) {
	threshold := e.greetThreshold()
	finalScore := initial.Score
	finalReason := firstNonEmpty(strings.TrimSpace(initial.Reason), "评分低于阈值")
	if e.shouldRunReview(initial.Score, threshold) {
		reviewDecision, err := e.callReviewScoreAI(e.positionDescription(), candidateText)
		if err != nil {
			e.log("warn", fmt.Sprintf("候选人复核评分失败，沿用首次评分：%v", err))
		} else {
			finalScore = reviewDecision.Score
			if strings.TrimSpace(reviewDecision.Reason) != "" {
				finalReason = strings.TrimSpace(reviewDecision.Reason)
			}
			return finalScore >= threshold, finalScore, finalReason, reviewDecision, true
		}
	}
	return finalScore >= threshold, finalScore, finalReason, AIScoreDecision{}, false
}

// shouldRunReview 判断是否需要触发复核评分。
func (e *TaskExecutor) shouldRunReview(score, threshold float64) bool {
	reviewPrompt := strings.TrimSpace(e.positionAIConfigString("review_prompt"))
	if reviewPrompt == "" {
		return false
	}
	delta := score - threshold
	if delta < 0 {
		delta = -delta
	}
	return delta <= 10
}

// effectivePrompt 读取岗位模板提示词，为空时使用系统默认提示词。
func (e *TaskExecutor) effectivePrompt(systemDefault string, keys ...string) string {
	if prompt := e.positionAIConfigString(keys...); prompt != "" {
		return prompt
	}
	return strings.TrimSpace(systemDefault)
}

// buildPromptFromTemplate 根据占位符判断提示词是完整模板还是补充规则。
func buildPromptFromTemplate(template, jobDesc, candidateText, fallback, extraTitle string) string {
	text := strings.TrimSpace(template)
	if text == "" {
		return fallback
	}
	if strings.Contains(text, "${岗位信息}") || strings.Contains(text, "${候选人信息}") {
		text = strings.ReplaceAll(text, "${岗位信息}", jobDesc)
		text = strings.ReplaceAll(text, "${候选人信息}", candidateText)
		return text
	}
	return fallback + "\n\n" + extraTitle + "：\n" + text
}

// tryDecodeJSON 尝试从 AI 文本中解析 JSON。
func tryDecodeJSON(raw string, target any) error {
	text := strings.TrimSpace(raw)
	if text == "" {
		return errors.New("empty json text")
	}
	if err := json.Unmarshal([]byte(text), target); err == nil {
		return nil
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		return json.Unmarshal([]byte(text[start:end+1]), target)
	}
	return errors.New("json block not found")
}

// rollDetailOpenByProbability 用概率决定关键词模式是否打开详情。
func rollDetailOpenByProbability(probability int) (bool, string, int, error) {
	if probability <= 0 {
		return false, "详情概率为0%，跳过详情", 0, nil
	}
	if probability >= 100 {
		return true, "详情概率为100%，打开详情", 0, nil
	}
	roll := rand.New(rand.NewSource(time.Now().UnixNano())).Intn(100) + 1
	shouldOpen := roll <= probability
	decision := "跳过详情"
	if shouldOpen {
		decision = "打开详情"
	}
	return shouldOpen, fmt.Sprintf("详情概率 %d%%，本次随机值 %d，%s", probability, roll, decision), 0, nil
}

// truncateText 按最大长度截断文本。
func truncateText(text string, maxLen int) string {
	value := strings.TrimSpace(text)
	if maxLen <= 0 || len([]rune(value)) <= maxLen {
		return value
	}
	return string([]rune(value)[:maxLen])
}

// detailThreshold 返回岗位模板里的详情查看阈值。
func (e *TaskExecutor) detailThreshold() float64 {
	return e.positionAIConfigNumber(60, "detail_score_threshold")
}

// greetThreshold 返回岗位模板里的打招呼阈值。
func (e *TaskExecutor) greetThreshold() float64 {
	return e.positionAIConfigNumber(70, "greet_score_threshold")
}

// positionAIConfigNumber 从岗位 AI 配置读取数值参数。
func (e *TaskExecutor) positionAIConfigNumber(fallback float64, keys ...string) float64 {
	if e.position == nil {
		return fallback
	}
	aiConfig, _ := e.position["ai_config"].(map[string]any)
	for _, key := range keys {
		value, ok := aiConfig[key]
		if !ok || value == nil {
			continue
		}
		switch v := value.(type) {
		case float64:
			return clampScore(v)
		case int:
			return clampScore(float64(v))
		case string:
			parsed := strings.TrimSpace(v)
			if parsed == "" {
				continue
			}
			var num float64
			if _, err := fmt.Sscanf(parsed, "%f", &num); err == nil {
				return clampScore(num)
			}
		}
	}
	return fallback
}

// clampScore 将分数限制在 0-100 区间。
func clampScore(score float64) float64 {
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

// float64Ptr 返回 float64 的指针。
func float64Ptr(v float64) *float64 {
	value := v
	return &value
}
