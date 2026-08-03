// Package auto_reply 本文件实现自动回复 AI 工具循环、调用上限、参数修正和云端完整审计。
package auto_reply

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

const (
	maxAutoReplyToolCalls       = 8
	maxAutoReplyArgumentRepairs = 2
)

// AIResponder 组装 AI 客户端、云端审计和运行小窗日志。
type AIResponder struct {
	AI     *ai.Client
	Cloud  *cloud.Client
	Logger shared.Logger
}

type aiReplyAuditOutput struct {
	Decision          ReplyDecision    `json:"decision"`
	AssistantMessages []ai.ToolMessage `json:"assistant_messages"`
}

type toolErrorResult struct {
	OK          bool   `json:"ok"`
	Correctable bool   `json:"correctable"`
	Error       string `json:"error"`
}

// Reply 执行最多8次工具调用并只返回待发送消息或明确的人工接管决定。
func (r *AIResponder) Reply(ctx context.Context, input ReplyContext) (ReplyDecision, error) {
	if r == nil || r.AI == nil || r.Cloud == nil {
		return ReplyDecision{}, fmt.Errorf("自动回复 AI 处理器没有准备完整")
	}
	if strings.TrimSpace(input.Conversation.ID) == "" || strings.TrimSpace(input.BasedOnMessageKey) == "" {
		return ReplyDecision{}, fmt.Errorf("自动回复 AI 缺少会话或候选人消息编号")
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
		input: input, cloud: r.Cloud,
		confirmations: append([]cloud.CandidateConfirmationItem(nil), input.ConfirmationItems...),
	}
	assistantMessages := make([]ai.ToolMessage, 0, 4)
	totalTokens := 0
	toolCount := 0
	argumentRepairs := 0
	for round := 1; ; round++ {
		r.report(input, "loading", "ai", fmt.Sprintf("AI 正在分析第%d轮", round), false)
		result, chatErr := r.AI.ChatWithTools(ctx, input.AIConfig, ai.ToolChatRequest{
			Messages: messages, Tools: autoReplyToolDefinitions(), EnableThinking: input.EnableThinking,
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
			return ReplyDecision{}, r.failRun(input, run, assistantMessages, totalTokens, "AI_TOOL_ACTION_MISSING", protocolErr)
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
		if state.pendingReply != "" {
			decision := ReplyDecision{Reply: state.pendingReply}
			return r.completeRun(input, run, assistantMessages, totalTokens, decision)
		}
		if state.manualReason != "" {
			decision := ReplyDecision{ManualReason: state.manualReason, ReasonKey: state.manualKey}
			return r.completeRun(input, run, assistantMessages, totalTokens, decision)
		}
	}
}

// executeAuditedTool 先保存工具开始记录，再执行并保存强类型结果或错误。
func (r *AIResponder) executeAuditedTool(ctx context.Context, input ReplyContext, run cloud.AutoReplyAIRun, state *toolExecutionState, call ai.ToolCall, sequence int) (json.RawMessage, error) {
	arguments := validAuditArguments(call.Function.Arguments)
	toolName := firstNonEmpty(call.Function.Name, "unknown")
	started := cloud.AutoReplyToolCall{
		AIRunID: run.ID, ToolCallID: call.ID, SequenceNo: sequence,
		ToolName: toolName, ArgumentsJSON: arguments,
		ResultJSON: json.RawMessage(`{}`), Status: "running",
	}
	if _, err := r.Cloud.SaveAutoReplyToolCall(ctx, input.Credentials, started); err != nil {
		return nil, fmt.Errorf("保存 AI 工具开始记录失败：%w", err)
	}
	result, executeErr := state.execute(ctx, call)
	finished := started
	finished.Status = "completed"
	finished.ResultJSON = result
	if executeErr != nil {
		finished.Status = "failed"
		finished.ErrorMessage = executeErr.Error()
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
	if decision.Reply != "" {
		r.report(input, "result", "reply", "AI 准备回复："+truncateRunText(decision.Reply, 120), true)
	} else {
		r.report(input, "result", "manual", "转人工："+truncateRunText(decision.ManualReason, 120), true)
	}
	return decision, nil
}

// failRun 尽力保存 AI 失败总记录，并保留最先发生的业务错误。
func (r *AIResponder) failRun(input ReplyContext, run cloud.AutoReplyAIRun, assistantMessages []ai.ToolMessage, totalTokens int, code string, cause error) error {
	output, _ := json.Marshal(aiReplyAuditOutput{AssistantMessages: assistantMessages})
	run.Status = "failed"
	run.OutputMessage = output
	run.ErrorCode = code
	run.ErrorMessage = truncateRunText(cause.Error(), 1000)
	run.TokenUsage = totalTokens
	_, finishErr := r.Cloud.FinishAutoReplyAIRun(context.Background(), input.Credentials, run)
	if finishErr != nil {
		return fmt.Errorf("%w；AI 总记录也没保存完整：%v", cause, finishErr)
	}
	r.report(input, "error", "ai", "AI 没处理成功："+truncateRunText(cause.Error(), 120), true)
	return cause
}

// report 把当前 AI 轮次、工具和结果同步给本地运行小窗。
func (r *AIResponder) report(input ReplyContext, phase string, stage string, reason string, terminal bool) {
	shared.ReportAnalysis(r.Logger, input.TaskID, shared.AnalysisStatus{
		Kind: "auto_reply", Phase: phase, Stage: stage, Terminal: terminal,
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
		toolSendMessage: "准备回复", toolSuggestConfig: "提交资料建议", toolNotifyHR: "转人工",
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
