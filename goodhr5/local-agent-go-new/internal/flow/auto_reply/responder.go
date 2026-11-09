// Package auto_reply 本文件实现自动回复 AI 工具循环、调用上限、参数修正和服务器审计记录。
package auto_reply

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/resume_import"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/system/failurediag"
)

const (
	maxAutoReplyToolCalls       = 8
	maxAutoReplyArgumentRepairs = 2
	maxAutoReplyProtocolRepairs = 2
)

// AIResponder 组装 AI 客户端、服务器审计记录和运行进度。
type AIResponder struct {
	AI               *ai.Client
	Cloud            *cloud.Client
	Logger           shared.Logger
	OCR              resume_import.OCRReader
	recommendationMu sync.Mutex
}

type aiReplyAuditOutput struct {
	FailureDiagnostics []failurediag.Detail `json:"failure_diagnostics,omitempty"`
	Decision           ReplyDecision        `json:"decision"`
	AssistantMessages  []ai.ToolMessage     `json:"assistant_messages"`
}

type toolErrorResult struct {
	OK          bool   `json:"ok"`
	Correctable bool   `json:"correctable"`
	Error       string `json:"error"`
}

// StructureResume 在服务器从已归档的附件提取正文，再调用 AI 整理简历。
func (r *AIResponder) StructureResume(ctx context.Context, input ResumeStructureContext) (StructuredResume, error) {
	if r == nil || r.Cloud == nil || r.AI == nil {
		return StructuredResume{}, fmt.Errorf("简历整理暂时不可用，请稍后再试")
	}
	ids := make([]string, 0, len(input.Attachments))
	for _, attachment := range input.Attachments {
		ids = append(ids, attachment.ID)
	}
	paths, err := r.Cloud.LocalAutoReplyAttachmentPaths(ctx, input.Credentials, input.Conversation.ID, ids)
	if err != nil {
		return StructuredResume{}, err
	}
	parts := []string{}
	for _, path := range paths {
		extractor := resume_import.Extractor{Root: filepath.Dir(path), OCR: r.OCR}
		text, extractErr := extractor.Extract(ctx, path)
		if extractErr != nil {
			return StructuredResume{}, extractErr
		}
		parts = append(parts, text)
	}
	if input.Resume.OnlineResumeText != "" {
		parts = append(parts, input.Resume.OnlineResumeText)
	}
	r.reportResume(input, "loading", "AI 正在整理候选人简历", nil)
	text := strings.Join(parts, "\n\n")
	billingCtx := shared.WithAIBilling(ctx, r.Logger, input.TaskID, input.Position.Position.ID, input.PageSnapshot.CandidateName, "AI 简历整理")
	began := time.Now()
	candidate, err := r.AI.ParseAutoReplyResume(billingCtx, input.AIConfig, text, input.Position.Config.Prompts.Resume)
	elapsedMS := time.Since(began).Milliseconds()
	if err != nil {
		if details := failurediag.Details(err); len(details) > 0 {
			shared.ReportAnalysis(r.Logger, input.TaskID, shared.AnalysisStatus{Kind: "auto_reply", PositionID: input.Position.Position.ID, Phase: "error", Stage: "resume_ai", DurationMS: &elapsedMS, Reason: failurediag.Summary(details), FailureDiagnostics: details, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
		}
		return StructuredResume{}, err
	}
	if !hasAutoReplyResumeEvidence(candidate, text) {
		return StructuredResume{}, fmt.Errorf("附件里还没核实到教育、工作或项目等履历，已保留原件，请核对后重试")
	}
	birthYM, birthPrecision := explicitResumeBirthMonth(candidate.BirthYM, text)
	candidate.BirthYM = birthYM
	r.reportResume(input, "result", "简历字段已提取，正在保存", &elapsedMS)
	return StructuredResume{Candidate: candidate.StructuredCandidate, Gender: candidate.Gender, BirthYMPrecision: birthPrecision, Wechat: candidate.Wechat, RawText: text}, nil
}

// hasAutoReplyResumeEvidence 要求本次提取原文包含本次模型返回的一项实际履历；旧模型未返回分类时仍按原文校验。
func hasAutoReplyResumeEvidence(candidate cloud.LibraryCandidate, source string) bool {
	if candidate.IsResume != nil && !*candidate.IsResume {
		return false
	}
	text := normalizeConfirmationContent(source)
	identity := map[string]bool{
		normalizeConfirmationContent(candidate.CandidateName): true,
		normalizeConfirmationContent(candidate.Email):         true,
		normalizeConfirmationContent(candidate.Phone):         true,
		normalizeConfirmationContent(candidate.Wechat):        true,
	}
	inSource := func(values ...string) bool {
		for _, value := range values {
			fact := strings.Trim(normalizeConfirmationContent(value), "【】[]<>《》_—-*：:")
			switch fact {
			case "", "暂无", "未知", "未填写", "无", "简历", "个人简历", "工作经历", "教育经历", "项目经历", "学历", "专业", "学校", "学校名称", "专业名称", "公司名称", "职位名称", "岗位名称", "项目名称", "项目描述", "工作内容", "证书名称", "荣誉名称":
				continue
			}
			if !identity[fact] && len([]rune(fact)) >= 2 && strings.Contains(text, fact) {
				return true
			}
		}
		return false
	}
	for _, education := range candidate.Educations {
		if inSource(education.SchoolName, education.MajorName, education.EducationLevel) {
			return true
		}
	}
	for _, job := range candidate.WorkExperiences {
		if inSource(job.CompanyName, job.PositionName, job.Content) {
			return true
		}
	}
	for _, project := range candidate.ProjectExperiences {
		if inSource(project.ProjectName, project.Content) {
			return true
		}
	}
	for _, certificate := range candidate.Certificates {
		if inSource(certificate.CertificateName) {
			return true
		}
	}
	for _, honor := range candidate.Honors {
		if inSource(honor.HonorName) {
			return true
		}
	}
	return inSource(candidate.EducationLevel, candidate.CurrentPosition, candidate.WorkYears)
}

// 生日标签允许 PDF 的中文排版空格；年月必须与标签相邻，不跨行搜索其他日期。
const resumeBirthLabel = `(?:出[\t\p{Zs}]*生(?:年月日|年月|日期|时间|于)?|生[\t\p{Zs}]*日|生于|date[ \t]+of[ \t]+birth|birth(?:day|date)?|dob)`
const resumeBirthDate = `([0-9]{4})[\t\p{Zs}]*(?:[-./]|年)[\t\p{Zs}]*(0?[1-9]|1[0-2])(?:[\t\p{Zs}]*月)?`

var resumeBirthPrefix = regexp.MustCompile(`(?i)` + resumeBirthLabel + `[\t\p{Zs}]*[:：]?[\t\p{Zs}]*` + resumeBirthDate + `(?:[^0-9]|$)`)
var resumeBirthSuffix = regexp.MustCompile(`(?:^|[^0-9])` + resumeBirthDate + `[\t\p{Zs}]*(?:出[\t\p{Zs}]*生|生(?:[，。;；\s]|$))`)

// explicitResumeBirthMonth 提取原文唯一明确标注的出生年月；模型和原文冲突或出现不同生日时留空。
func explicitResumeBirthMonth(value, source string) (string, string) {
	value = strings.TrimSpace(value)
	month := ""
	for _, pattern := range []*regexp.Regexp{resumeBirthPrefix, resumeBirthSuffix} {
		for _, match := range pattern.FindAllStringSubmatch(source, -1) {
			number, _ := strconv.Atoi(match[2])
			date := fmt.Sprintf("%s-%02d", match[1], number)
			if month != "" && month != date || value != "" && value != date {
				return "", ""
			}
			month = date
		}
	}
	if month == "" {
		return "", ""
	}
	return month, "month"
}

// reportResume 把简历结构化请求状态同步到当前运行小窗。
func (r *AIResponder) reportResume(input ResumeStructureContext, phase string, reason string, elapsedMS *int64) {
	shared.ReportAnalysis(r.Logger, input.TaskID, shared.AnalysisStatus{
		Kind: "auto_reply", PositionID: input.Position.Position.ID, Phase: phase, Stage: "resume_ai", DurationMS: elapsedMS,
		CandidateName: input.PageSnapshot.CandidateName, Reason: reason,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
}

// Reply 执行最多8次工具调用并只返回待发送消息或明确的人工接管决定。
func (r *AIResponder) Reply(ctx context.Context, input ReplyContext) (ReplyDecision, error) {
	if r == nil || r.AI == nil || r.Cloud == nil {
		return ReplyDecision{}, fmt.Errorf("自动回复 AI 处理器没有准备完整")
	}
	if strings.TrimSpace(input.Conversation.ID) == "" || strings.TrimSpace(input.BasedOnMessageKey) == "" {
		return ReplyDecision{}, fmt.Errorf("自动回复 AI 缺少会话或候选人消息编号")
	}
	input, err := boundedReplyContext(input)
	if err != nil {
		return ReplyDecision{}, err
	}
	messages, err := initialToolMessages(input)
	if err != nil {
		return ReplyDecision{}, err
	}
	traceID, err := newAutoReplyTraceID(input.TaskID)
	if err != nil {
		return ReplyDecision{}, err
	}
	inputJSON, err := json.Marshal(messages)
	if err != nil {
		return ReplyDecision{}, fmt.Errorf("编码 AI 总记录输入失败：%w", err)
	}
	run, err := r.Cloud.StartAutoReplyAIRun(ctx, input.Credentials, cloud.AutoReplyAIRun{
		ConversationID: input.Conversation.ID, CandidateID: input.Conversation.CandidateID,
		PositionID: input.Position.Position.ID, TraceID: traceID, Model: input.AIConfig.Model,
		BasedOnMessageKey: input.BasedOnMessageKey, InputMessages: inputJSON,
	})
	if err != nil {
		return ReplyDecision{}, fmt.Errorf("创建 AI 总记录失败：%w", err)
	}

	state := &toolExecutionState{
		input:         input,
		confirmations: append([]cloud.CandidateConfirmationItem(nil), input.ConfirmationItems...),
	}
	assistantMessages := make([]ai.ToolMessage, 0, 4)
	totalTokens := 0
	toolCount := 0
	argumentRepairs := 0
	protocolRepairs := 0
	for round := 1; ; round++ {
		encodedContext, encodeErr := json.Marshal(messages)
		if encodeErr != nil || len(encodedContext) > maxReplyContextBytes {
			return ReplyDecision{}, r.failRun(input, run, assistantMessages, totalTokens, "AI_CONTEXT_LIMIT", fmt.Errorf("本轮模型上下文已达到预算，请人工处理"))
		}
		r.report(input, "loading", "ai", fmt.Sprintf("AI 正在分析第%d轮", round), false)
		billingCtx := shared.WithAIBilling(ctx, r.Logger, input.TaskID, input.Position.Position.ID, input.PageSnapshot.CandidateName, fmt.Sprintf("自动回复 AI 第%d轮", round))
		result, chatErr := r.AI.ChatWithTools(billingCtx, input.AIConfig, ai.ToolChatRequest{
			Messages: messages, Tools: autoReplyToolsForContext(input), EnableThinking: input.EnableThinking,
		})
		if chatErr != nil {
			return ReplyDecision{}, r.failRun(input, run, assistantMessages, totalTokens, "AI_TOOL_REQUEST_FAILED", chatErr)
		}
		totalTokens += result.TokenUsage
		message := normalizeAssistantToolCalls(result.Message, traceID, toolCount)
		messages = append(messages, message)
		assistantMessages = append(assistantMessages, message)
		if len(message.ToolCalls) == 0 {
			protocolErr := fmt.Errorf("AI 没有按标准 tool_calls 返回发送或转人工动作，请检查当前模型是否支持工具调用")
			protocolRepairs++
			if protocolRepairs > maxAutoReplyProtocolRepairs {
				return ReplyDecision{}, r.failRun(input, run, assistantMessages, totalTokens, "AI_TOOL_ACTION_MISSING", protocolErr)
			}
			correction := "请根据错误重新处理，禁止直接返回普通文本；有证据变化才调用 upsert_confirmation_items，再调用 send_messages 准备回复，或调用 notify_hr 请求人工协助。"
			if input.ConditionsOnly {
				correction = "请仅使用 upsert_confirmation_items 提议有证据的条件变化，再调用 finish_review，不准备候选人回复。"
			}
			messages = append(messages, ai.ToolMessage{
				Role: "user",
				Content: "上一次输出没有通过工具协议校验，错误信息：" + protocolErr.Error() +
					"。" + correction,
			})
			continue
		}
		if toolCount+len(message.ToolCalls) > maxAutoReplyToolCalls {
			decision := ReplyDecision{ManualReason: "AI 工具调用超过8次，我没敢继续自动回复", ReasonKey: "ai_tool_limit"}
			return r.completeRun(input, run, assistantMessages, totalTokens, decision)
		}
		for _, call := range message.ToolCalls {
			toolCount++
			r.report(input, "loading", "tool", "AI 调用工具："+toolDisplayName(call.Function.Name), false)
			resultJSON, toolErr := r.executeAuditedTool(ctx, input, run, state, call, toolCount)
			if toolErr != nil {
				var argumentErr *toolArgumentError
				if !errors.As(toolErr, &argumentErr) {
					return ReplyDecision{}, r.failRun(input, run, assistantMessages, totalTokens, "AI_TOOL_EXECUTION_FAILED", toolErr)
				}
				argumentRepairs++
				if argumentRepairs > maxAutoReplyArgumentRepairs {
					decision := ReplyDecision{ManualReason: "AI 工具参数连续修正后还是不对，我没敢自动回复", ReasonKey: "ai_tool_arguments"}
					return r.completeRun(input, run, assistantMessages, totalTokens, decision)
				}
			}
			messages = append(messages, ai.ToolMessage{
				Role: "tool", ToolCallID: call.ID, Name: call.Function.Name, Content: string(resultJSON),
			})
		}
		if state.reviewFinished {
			return r.completeRun(input, run, assistantMessages, totalTokens, ReplyDecision{ConfirmationUpdates: state.confirmationUpdates, Messages: state.pendingMessages})
		}
		if len(state.pendingMessages) > 0 {
			decision := ReplyDecision{Messages: append([]ReplyMessage(nil), state.pendingMessages...), ConfirmationUpdates: state.confirmationUpdates, Suggestions: state.suggestions}
			return r.completeRun(input, run, assistantMessages, totalTokens, decision)
		}
		if state.manualReason != "" {
			decision := ReplyDecision{HandoffFollowup: state.manualFollowup, HandoffQuestions: state.handoffQuestions, KnownAnswer: state.knownAnswer, ManualReason: state.manualReason, ReasonKey: state.manualKey, ConfirmationUpdates: state.confirmationUpdates, Suggestions: state.suggestions}
			return r.completeRun(input, run, assistantMessages, totalTokens, decision)
		}
	}
}

// executeAuditedTool 先保存工具开始记录，再执行并保存强类型结果或错误。
func (r *AIResponder) executeAuditedTool(ctx context.Context, input ReplyContext, run cloud.AutoReplyAIRun, state *toolExecutionState, call ai.ToolCall, sequence int) (json.RawMessage, error) {
	now := time.Now().UTC()
	arguments := validAuditArguments(call.Function.Arguments)
	toolName := firstNonEmpty(call.Function.Name, "unknown")
	started := cloud.AutoReplyToolCall{
		AIRunID: run.ID, ToolCallID: call.ID, SequenceNo: sequence,
		ToolName: toolName, ArgumentsJSON: arguments,
		ResultJSON: json.RawMessage(`{}`), Status: "running", StartedAt: now, CreatedAt: now,
	}
	if _, err := r.Cloud.SaveAutoReplyToolCall(ctx, input.Credentials, started); err != nil {
		return nil, fmt.Errorf("保存 AI 工具开始记录失败：%w", err)
	}
	result, executeErr := state.execute(ctx, call)
	finished := started
	completedAt := time.Now().UTC()
	finished.CompletedAt = &completedAt
	finished.Status = "completed"
	finished.ResultJSON = result
	if executeErr != nil {
		finished.Status = "failed"
		finished.ErrorMessage = executeErr.Error()
		if evidence := failurediag.Summary(failurediag.Details(executeErr)); evidence != "" {
			finished.ErrorMessage = evidence + "；" + finished.ErrorMessage
		}
		var argumentErr *toolArgumentError
		if errors.As(executeErr, &argumentErr) {
			finished.ErrorCode = "INVALID_TOOL_ARGUMENTS"
			result, _ = json.Marshal(toolErrorResult{OK: false, Correctable: true, Error: executeErr.Error()})
			finished.ResultJSON = result
		} else {
			finished.ErrorCode = "TOOL_EXECUTION_FAILED"
			result, _ = json.Marshal(toolErrorResult{OK: false, Correctable: false, Error: executeErr.Error()})
			finished.ResultJSON = result
		}
	}
	if _, err := r.Cloud.SaveAutoReplyToolCall(context.WithoutCancel(ctx), input.Credentials, finished); err != nil {
		return nil, fmt.Errorf("保存 AI 工具完成记录失败：%w", err)
	}
	if executeErr != nil {
		elapsed := completedAt.Sub(now).Milliseconds()
		shared.ReportAnalysis(r.Logger, input.TaskID, shared.AnalysisStatus{
			Kind: "auto_reply", PositionID: input.Position.Position.ID, CandidateName: input.PageSnapshot.CandidateName, Phase: "error", Stage: "tool",
			DurationMS: &elapsed, FailureDiagnostics: failurediag.Details(executeErr), UpdatedAt: completedAt.Format(time.RFC3339Nano),
			Reason: "AI 工具失败：" + toolDisplayName(toolName) + "，" + truncateRunText(finished.ErrorMessage, 120),
		})
	} else {
		r.report(input, "loading", "tool", "AI 工具完成："+toolDisplayName(toolName)+"，结果 "+truncateRunText(string(result), 160), false)
	}
	return result, executeErr
}

// completeRun 完成 AI 总记录并向运行小窗展示回复或转人工原因。
func (r *AIResponder) completeRun(input ReplyContext, run cloud.AutoReplyAIRun, assistantMessages []ai.ToolMessage, totalTokens int, decision ReplyDecision) (ReplyDecision, error) {
	output, err := json.Marshal(aiReplyAuditOutput{Decision: decision, AssistantMessages: assistantMessages})
	if err != nil {
		return ReplyDecision{}, fmt.Errorf("编码 AI 总记录输出失败：%w", err)
	}
	run.Status = "completed"
	run.OutputMessage = output
	run.TokenUsage = totalTokens
	if _, err = r.Cloud.FinishAutoReplyAIRun(context.Background(), input.Credentials, run); err != nil {
		return ReplyDecision{}, fmt.Errorf("保存 AI 总记录完成状态失败：%w", err)
	}
	if input.ConditionsOnly {
		r.report(input, "result", "resume_review", "简历补审已完成，没有重复发送聊天", true)
	} else if len(decision.Messages) > 0 {
		parts := make([]string, 0, len(decision.Messages))
		for _, item := range decision.Messages {
			parts = append(parts, item.Content)
		}
		r.report(input, "result", "reply", "AI 准备回复："+truncateRunText(strings.Join(parts, " / "), 160), true)
	} else {
		r.report(input, "result", "manual", "转人工："+truncateRunText(decision.ManualReason, 120), true)
	}
	return decision, nil
}

// failRun 尽力保存 AI 失败总记录，并保留最先发生的业务错误。
func (r *AIResponder) failRun(input ReplyContext, run cloud.AutoReplyAIRun, assistantMessages []ai.ToolMessage, totalTokens int, code string, cause error) error {
	details := failurediag.Details(cause)
	output, _ := json.Marshal(aiReplyAuditOutput{AssistantMessages: assistantMessages, FailureDiagnostics: details})
	run.Status = "failed"
	run.OutputMessage = output
	run.ErrorCode = code
	run.ErrorMessage = truncateRunText(cause.Error(), 1000)
	if len(details) > 0 {
		run.ErrorMessage = failurediag.Summary(details)
	}
	run.TokenUsage = totalTokens
	_, finishErr := r.Cloud.FinishAutoReplyAIRun(context.Background(), input.Credentials, run)
	if finishErr != nil {
		return fmt.Errorf("%w；AI 总记录也没保存完整：%v", cause, finishErr)
	}
	shared.ReportAnalysis(r.Logger, input.TaskID, shared.AnalysisStatus{
		Kind: "auto_reply", PositionID: input.Position.Position.ID, Phase: "error", Stage: "ai", Terminal: true, CandidateName: input.PageSnapshot.CandidateName,
		Reason: "AI 没处理成功：" + truncateRunText(run.ErrorMessage, 120), FailureDiagnostics: details, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	return cause
}

// report 记录当前 AI 轮次、工具和结果，供运行进度展示。
func (r *AIResponder) report(input ReplyContext, phase string, stage string, reason string, terminal bool) {
	shared.ReportAnalysis(r.Logger, input.TaskID, shared.AnalysisStatus{
		Kind: "auto_reply", PositionID: input.Position.Position.ID, Phase: phase, Stage: stage, Terminal: terminal,
		CandidateName: input.PageSnapshot.CandidateName, Reason: reason,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
}

// newAutoReplyTraceID 使用任务编号和随机字节生成不可冲突的 AI 审计追踪编号。
func newAutoReplyTraceID(taskID string) (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("生成 AI 追踪编号失败：%w", err)
	}
	prefix := strings.TrimSpace(taskID)
	if prefix == "" {
		prefix = "auto-reply"
	}
	return prefix + "-ai-" + hex.EncodeToString(random), nil
}

// normalizeAssistantToolCalls 补齐兼容接口偶尔缺少的角色、类型和调用编号。
func normalizeAssistantToolCalls(message ai.ToolMessage, traceID string, alreadyUsed int) ai.ToolMessage {
	if strings.TrimSpace(message.Role) == "" {
		message.Role = "assistant"
	}
	for index := range message.ToolCalls {
		if strings.TrimSpace(message.ToolCalls[index].ID) == "" {
			message.ToolCalls[index].ID = fmt.Sprintf("%s-call-%d", traceID, alreadyUsed+index+1)
		}
		if strings.TrimSpace(message.ToolCalls[index].Type) == "" {
			message.ToolCalls[index].Type = "function"
		}
	}
	return message
}

// validAuditArguments 把无效 JSON 包装后保存，确保审计接口永远收到合法 JSON。
func validAuditArguments(value string) json.RawMessage {
	trimmed := strings.TrimSpace(value)
	if json.Valid([]byte(trimmed)) && trimmed != "null" {
		return json.RawMessage(trimmed)
	}
	encoded, _ := json.Marshal(struct {
		RawArguments string `json:"raw_arguments"`
	}{RawArguments: value})
	return encoded
}

// toolDisplayName 返回适合用户查看的工具中文名称。
func toolDisplayName(name string) string {
	labels := map[string]string{
		toolGetContext: "查看岗位和候选人", toolGetChatHistory: "查看聊天记录",
		toolGetResume: "查看简历", toolGetConfirmations: "查看确认项",
		toolUpsertConfirmations: "更新确认项", toolRequestResume: "核对简历状态",
		toolArchiveConfirmations: "删除沟通目标",
		toolSendMessages:         "准备回复", toolSuggestConfig: "提交资料建议", toolNotifyHR: "转人工",
		toolFinishReview: "完成简历补审",
	}
	if label := labels[strings.TrimSpace(name)]; label != "" {
		return label
	}
	return firstNonEmpty(name, "未知工具")
}

// truncateRunText 截断运行小窗和审计错误中的长文本。
func truncateRunText(value string, limit int) string {
	chars := []rune(strings.TrimSpace(value))
	if len(chars) <= limit {
		return string(chars)
	}
	return string(chars[:limit]) + "…"
}
