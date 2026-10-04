// Package httpapi 本文件负责异步生成候选人推荐报告，并可靠发送岗位创建人通知邮件。
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"os"
	"strings"
	"time"
)

const recommendationSystemPrompt = `你是资深招聘顾问，正在为面试官生成一份候选人推荐报告。
必须遵守：
1. 只依据输入中的岗位、公司、完整简历、条件状态和真实沟通记录，不得编造。
2. 岗位 required 和 confirm 条件已经由业务流程确认；重点解释匹配依据、优势、风险和面试核验点。
3. 加分项权重低，不能掩盖风险。资料没有提到的内容只能写“未确认”，不能猜测。
4. 输出是给面试官看的专业摘要，不要写系统过程、AI过程或隐藏思考。
5. 只输出一个 JSON 对象，字段必须严格为：match_score、recommendation_level、executive_summary、strengths、risks、bonus_items、unconfirmed_items、job_intent_summary、stability_summary、communication_summary、interview_focus、suggested_questions。
6. match_score 是 0-100 数字。recommendation_level 使用“优先推荐”“建议推进”或“谨慎推进”。
7. strengths、risks、bonus_items、unconfirmed_items、interview_focus 都是数组，每项字段为 title、detail、severity、evidence；evidence 是可复核原文或字段数组。
8. suggested_questions 是最多 8 条的字符串数组。所有文字简洁、明确。`

const maxRecommendationAIAttempts = 3

// recommendationAIOutput 表示 AI 只负责生成的分析字段，业务快照由程序补齐。
type recommendationAIOutput struct {
	MatchScore           float64               `json:"match_score"`
	RecommendationLevel  string                `json:"recommendation_level"`
	ExecutiveSummary     string                `json:"executive_summary"`
	Strengths            []RecommendationPoint `json:"strengths"`
	Risks                []RecommendationPoint `json:"risks"`
	BonusItems           []RecommendationPoint `json:"bonus_items"`
	UnconfirmedItems     []RecommendationPoint `json:"unconfirmed_items"`
	JobIntentSummary     string                `json:"job_intent_summary"`
	StabilitySummary     string                `json:"stability_summary"`
	CommunicationSummary string                `json:"communication_summary"`
	InterviewFocus       []RecommendationPoint `json:"interview_focus"`
	SuggestedQuestions   []string              `json:"suggested_questions"`
}

// StartRecommendationWorker 启动推荐报告数据库任务和邮件通知轮询；重复调用只启动一次。
func (s *AutoReplyService) StartRecommendationWorker() {
	if s == nil || s.store == nil || s.aiConfigs == nil {
		return
	}
	s.recommendationWorkerOnce.Do(func() {
		go s.runRecommendationWorker()
	})
}

// runRecommendationWorker 持续领取一条生成任务和一条待发送邮件，单次失败不会退出后台循环。
func (s *AutoReplyService) runRecommendationWorker() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		if err := s.processOneRecommendation(ctx); err != nil {
			log.Printf("[候选人推荐] 处理生成任务失败 err=%v", err)
		}
		if err := s.processOneRecommendationNotification(ctx); err != nil {
			log.Printf("[候选人推荐] 处理通知邮件失败 err=%v", err)
		}
		cancel()
		<-ticker.C
	}
}

// processOneRecommendation 领取并完成一条推荐报告任务，输入变化时自动改用最新快照。
func (s *AutoReplyService) processOneRecommendation(ctx context.Context) error {
	job, claimed, err := s.store.ClaimRecommendationJob(ctx)
	if err != nil || !claimed {
		return err
	}
	source, err := s.store.LoadRecommendationGenerationSource(ctx, job.ReviewID)
	if err != nil {
		_ = s.store.FailRecommendationJob(context.WithoutCancel(ctx), job, err)
		return err
	}
	if source.InputHash != job.InputHash {
		return s.store.RefreshRecommendationJob(context.WithoutCancel(ctx), job, source)
	}
	report, model, tokenUsage, err := s.generateRecommendationReport(ctx, source)
	if err != nil {
		_ = s.store.FailRecommendationJob(context.WithoutCancel(ctx), job, err)
		return err
	}
	publicID, err := newRecommendationPublicID()
	if err != nil {
		_ = s.store.FailRecommendationJob(context.WithoutCancel(ctx), job, err)
		return err
	}
	_, err = s.store.CompleteRecommendationJob(context.WithoutCancel(ctx), job, publicID, model, tokenUsage, report)
	return err
}

// generateRecommendationReport 调用岗位创建人的 AI 配置，并把分析结果与可信业务快照组合为报告。
func (s *AutoReplyService) generateRecommendationReport(ctx context.Context, source recommendationGenerationSource) (CandidateRecommendationReport, string, int, error) {
	config, err := s.aiConfigs.UserConfig(source.CreatorEmail)
	if err != nil {
		return CandidateRecommendationReport{}, "", 0, fmt.Errorf("读取岗位创建人的 AI 配置失败：%w", err)
	}
	if err = validateAIConfigTestRequest(aiConfigRequest{BaseURL: config.BaseURL, Model: config.Model, APIKey: config.APIKey}); err != nil {
		return CandidateRecommendationReport{}, "", 0, fmt.Errorf("岗位创建人的 AI 配置暂时不能生成推荐报告：%w", err)
	}
	dynamic, err := json.Marshal(struct {
		Candidate     RecommendationCandidateSnapshot      `json:"candidate"`
		Position      RecommendationPositionSnapshot       `json:"position"`
		Conditions    []RecommendationConditionSnapshot    `json:"conditions"`
		Conversations []RecommendationConversationSnapshot `json:"conversations"`
	}{source.Candidate, source.Position, source.Conditions, source.Conversations})
	if err != nil {
		return CandidateRecommendationReport{}, "", 0, fmt.Errorf("编码推荐报告输入失败：%w", err)
	}
	messages := []AIMsg{{Role: "system", Content: recommendationSystemPrompt}, {Role: "user", Content: string(dynamic)}}
	totalTokens := 0
	var lastErr error
	for attempt := 1; attempt <= maxRecommendationAIAttempts; attempt++ {
		content, tokens, callErr := s.callCloudResumeAI(ctx, config, messages)
		totalTokens += tokens
		if callErr != nil {
			return CandidateRecommendationReport{}, config.Model, totalTokens, callErr
		}
		output, parseErr := parseRecommendationAIOutput(content)
		if parseErr == nil {
			now := time.Now().UTC()
			return CandidateRecommendationReport{
				Candidate: source.Candidate, Position: source.Position, Conditions: safeSlice(source.Conditions),
				MatchScore: output.MatchScore, RecommendationLevel: output.RecommendationLevel,
				ExecutiveSummary: output.ExecutiveSummary, Strengths: output.Strengths, Risks: output.Risks,
				BonusItems: output.BonusItems, UnconfirmedItems: output.UnconfirmedItems,
				JobIntentSummary: output.JobIntentSummary, StabilitySummary: output.StabilitySummary,
				CommunicationSummary: output.CommunicationSummary, InterviewFocus: output.InterviewFocus,
				SuggestedQuestions: output.SuggestedQuestions, SourceCutoffAt: source.SourceCutoff, GeneratedAt: now,
			}, strings.TrimSpace(config.Model), totalTokens, nil
		}
		lastErr = parseErr
		messages = append(messages,
			AIMsg{Role: "assistant", Content: content},
			AIMsg{Role: "user", Content: "上一次 JSON 不符合约定：" + truncateAutoReplyText(parseErr.Error(), 500) + "。请只修正字段和内容后重新返回完整 JSON。"},
		)
	}
	return CandidateRecommendationReport{}, strings.TrimSpace(config.Model), totalTokens, lastErr
}

// parseRecommendationAIOutput 清理并严格校验 AI 推荐分析字段。
func parseRecommendationAIOutput(raw string) (recommendationAIOutput, error) {
	var output recommendationAIOutput
	if err := json.Unmarshal([]byte(cleanAITextOutput(raw)), &output); err != nil {
		return output, fmt.Errorf("推荐报告不是有效 JSON：%w", err)
	}
	if output.MatchScore < 0 || output.MatchScore > 100 {
		return output, errors.New("match_score 必须在 0 到 100 之间")
	}
	output.RecommendationLevel = strings.TrimSpace(output.RecommendationLevel)
	if output.RecommendationLevel != "优先推荐" && output.RecommendationLevel != "建议推进" && output.RecommendationLevel != "谨慎推进" {
		return output, errors.New("recommendation_level 不在允许范围内")
	}
	output.ExecutiveSummary = truncateAutoReplyText(output.ExecutiveSummary, 1000)
	if output.ExecutiveSummary == "" {
		return output, errors.New("executive_summary 不能为空")
	}
	output.Strengths = cleanRecommendationPoints(output.Strengths, 8)
	output.Risks = cleanRecommendationPoints(output.Risks, 8)
	output.BonusItems = cleanRecommendationPoints(output.BonusItems, 8)
	output.UnconfirmedItems = cleanRecommendationPoints(output.UnconfirmedItems, 8)
	output.InterviewFocus = cleanRecommendationPoints(output.InterviewFocus, 8)
	output.JobIntentSummary = truncateAutoReplyText(output.JobIntentSummary, 500)
	output.StabilitySummary = truncateAutoReplyText(output.StabilitySummary, 500)
	output.CommunicationSummary = truncateAutoReplyText(output.CommunicationSummary, 500)
	output.SuggestedQuestions = cleanRecommendationStrings(output.SuggestedQuestions, 8, 300)
	return output, nil
}

// cleanRecommendationPoints 清理推荐报告列表，删除空项并限制长度。
func cleanRecommendationPoints(items []RecommendationPoint, limit int) []RecommendationPoint {
	result := make([]RecommendationPoint, 0, min(len(items), limit))
	for _, item := range items {
		item.Title = truncateAutoReplyText(item.Title, 100)
		item.Detail = truncateAutoReplyText(item.Detail, 800)
		item.Severity = truncateAutoReplyText(item.Severity, 30)
		item.Evidence = cleanRecommendationStrings(item.Evidence, 6, 500)
		if item.Title == "" && item.Detail == "" {
			continue
		}
		result = append(result, item)
		if len(result) == limit {
			break
		}
	}
	return result
}

// cleanRecommendationStrings 清理字符串数组、去重并限制数量和单项长度。
func cleanRecommendationStrings(items []string, limit int, maxLength int) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, min(len(items), limit))
	for _, item := range items {
		item = truncateAutoReplyText(item, maxLength)
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
		if len(result) == limit {
			break
		}
	}
	return result
}

// newRecommendationPublicID 生成永久公开链接使用的不可猜测随机编号。
func newRecommendationPublicID() (string, error) {
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("生成推荐公开编号失败：%w", err)
	}
	return "rec_" + hex.EncodeToString(buffer), nil
}

// processOneRecommendationNotification 领取并发送一封推荐成功邮件。
func (s *AutoReplyService) processOneRecommendationNotification(ctx context.Context) error {
	item, recipient, claimed, err := s.store.ClaimRecommendationNotification(ctx)
	if err != nil || !claimed {
		return err
	}
	if recipient == "" {
		err = errors.New("岗位创建人和团队负责人都没有可用邮箱")
	} else {
		err = s.mailer.SendCustomHTML(recipient, "GoodHR 候选人已满足条件："+item.Report.Candidate.Name, recommendationEmailHTML(item), recommendationEmailPlainText(item))
	}
	finishErr := s.store.FinishRecommendationNotification(context.WithoutCancel(ctx), item.ID, err)
	if err != nil {
		return err
	}
	return finishErr
}

// recommendationEmailHTML 生成岗位创建人收到的简洁 HTML 推荐邮件。
func recommendationEmailHTML(item CandidateRecommendation) string {
	name := template.HTMLEscapeString(item.Report.Candidate.Name)
	position := template.HTMLEscapeString(item.Report.Position.Name)
	summary := template.HTMLEscapeString(item.Report.ExecutiveSummary)
	level := template.HTMLEscapeString(item.RecommendationLevel)
	url := template.HTMLEscapeString(recommendationPublicWebBaseURL() + "/recommendations/" + item.PublicID)
	return `<div style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:#26332d;line-height:1.7">` +
		`<h2 style="margin:0 0 12px">这位候选人已经确认得差不多啦</h2>` +
		`<p style="margin:0 0 8px"><strong>` + name + `</strong> · ` + position + `</p>` +
		`<p style="margin:0 0 8px">匹配分：<strong>` + fmt.Sprintf("%.0f", item.MatchScore) + `</strong> · ` + level + `</p>` +
		`<p style="margin:0 0 20px;color:#526158">` + summary + `</p>` +
		`<a href="` + url + `" style="display:inline-block;padding:10px 18px;border-radius:8px;background:#2f6f4e;color:#fff;text-decoration:none">查看完整推荐报告</a>` +
		`<p style="margin:18px 0 0;color:#7b8781;font-size:12px">我先把重点整理好了，面试前扫一眼就行。</p></div>`
}

// recommendationEmailPlainText 生成不支持 HTML 的邮箱使用的纯文本内容。
func recommendationEmailPlainText(item CandidateRecommendation) string {
	return fmt.Sprintf("候选人：%s\n岗位：%s\n匹配分：%.0f\n推荐结论：%s\n查看完整推荐报告：%s/recommendations/%s",
		item.Report.Candidate.Name, item.Report.Position.Name, item.MatchScore,
		item.RecommendationLevel, recommendationPublicWebBaseURL(), item.PublicID)
}

// recommendationPublicWebBaseURL 返回推荐页面的前端地址，开发环境默认使用 5173 端口。
func recommendationPublicWebBaseURL() string {
	for _, key := range []string{"GOODHR_PUBLIC_WEB_BASE_URL", "GOODHR_PUBLIC_BASE_URL"} {
		if value := strings.TrimRight(strings.TrimSpace(os.Getenv(key)), "/"); value != "" {
			return value
		}
	}
	return "http://localhost:5173"
}
