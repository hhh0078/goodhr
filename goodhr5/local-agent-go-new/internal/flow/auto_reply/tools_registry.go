// Package auto_reply 本文件定义自动回复九个标准 AI 工具及其强类型参数校验和本地执行。
package auto_reply

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

const (
	maxAutoReplyMessageRunes = 200

	toolGetContext          = "get_context"
	toolGetChatHistory      = "get_chat_history"
	toolGetResume           = "get_resume"
	toolGetConfirmations    = "get_confirmation_items"
	toolUpsertConfirmations = "upsert_confirmation_items"
	toolRequestResume       = "request_resume"
	toolSendMessage         = "send_message"
	toolSuggestConfig       = "suggest_config_change"
	toolNotifyHR            = "notify_hr"
)

// toolArgumentError 表示允许 AI 根据错误信息修正的工具名称或参数错误。
type toolArgumentError struct {
	message string
}

// Error 返回可供 AI 修正工具调用的中文说明。
func (e *toolArgumentError) Error() string {
	return e.message
}

type historyToolArgs struct {
	Limit int `json:"limit"`
}

type confirmationToolItem struct {
	ItemType     string `json:"item_type"`
	Content      string `json:"content"`
	Status       string `json:"status"`
	SourceRef    string `json:"source_ref"`
	EvidenceText string `json:"evidence_text"`
	Summary      string `json:"summary"`
}

type confirmationToolArgs struct {
	Items []confirmationToolItem `json:"items"`
}

type sendMessageToolArgs struct {
	Message string `json:"message"`
}

type suggestionToolArgs struct {
	SuggestionType string          `json:"suggestion_type"`
	Operation      string          `json:"operation"`
	TargetID       string          `json:"target_id"`
	ProposedValue  json.RawMessage `json:"proposed_value"`
	Reason         string          `json:"reason"`
}

type notifyHRToolArgs struct {
	Reason    string `json:"reason"`
	ReasonKey string `json:"reason_key"`
}

type toolExecutionState struct {
	input         ReplyContext
	cloud         *cloud.Client
	confirmations []cloud.CandidateConfirmationItem
	pendingReply  string
	manualReason  string
	manualKey     string
}

// autoReplyToolDefinitions 返回自动回复固定的九个标准 OpenAI 函数工具。
func autoReplyToolDefinitions() []ai.ToolDefinition {
	return []ai.ToolDefinition{
		newToolDefinition(toolGetContext, "查看当前岗位、公司、候选人和会话身份上下文。", `{"type":"object","properties":{},"additionalProperties":false}`),
		newToolDefinition(toolGetChatHistory, "查看按时间排序的聊天记录，limit 默认200，最大5000。", `{"type":"object","properties":{"limit":{"type":"integer","minimum":1,"maximum":5000}},"additionalProperties":false}`),
		newToolDefinition(toolGetResume, "查看云端正式简历、附件元数据和本轮页面简历。", `{"type":"object","properties":{},"additionalProperties":false}`),
		newToolDefinition(toolGetConfirmations, "查看候选人与当前岗位之间的结构化确认项。", `{"type":"object","properties":{},"additionalProperties":false}`),
		newToolDefinition(toolUpsertConfirmations, "批量新增或修改确认项，内容相同会自动去重。", `{"type":"object","properties":{"items":{"type":"array","minItems":1,"maxItems":20,"items":{"type":"object","properties":{"item_type":{"type":"string","enum":["required","confirm","bonus"]},"content":{"type":"string","minLength":1,"maxLength":1000},"status":{"type":"string","enum":["pending","matched","unmatched","not_applicable","conflicted"]},"source_ref":{"type":"string","maxLength":256},"evidence_text":{"type":"string","maxLength":2000},"summary":{"type":"string","maxLength":500}},"required":["item_type","content","status"],"additionalProperties":false}}},"required":["items"],"additionalProperties":false}`),
		newToolDefinition(toolRequestResume, "查询固定流程是否已经索要或取得简历。", `{"type":"object","properties":{},"additionalProperties":false}`),
		newToolDefinition(toolSendMessage, "生成本条候选人消息唯一一条待发送回复；固定流程会在页面复核后发送。", `{"type":"object","properties":{"message":{"type":"string","minLength":1,"maxLength":200}},"required":["message"],"additionalProperties":false}`),
		newToolDefinition(toolSuggestConfig, "提交岗位或公司资料的新增、修改或删除建议，等待 HR 审核。", `{"type":"object","properties":{"suggestion_type":{"type":"string","enum":["position","company"]},"operation":{"type":"string","enum":["create","update","delete"]},"target_id":{"type":"string","maxLength":128},"proposed_value":{"type":"object"},"reason":{"type":"string","minLength":1,"maxLength":1000}},"required":["suggestion_type","operation","proposed_value","reason"],"additionalProperties":false}`),
		newToolDefinition(toolNotifyHR, "无法可靠回答或问题与招聘无关时，请求 HR 人工接管并邮件通知。", `{"type":"object","properties":{"reason":{"type":"string","minLength":1,"maxLength":500},"reason_key":{"type":"string","minLength":1,"maxLength":100}},"required":["reason","reason_key"],"additionalProperties":false}`),
	}
}

// newToolDefinition 从固定 JSON Schema 构造标准函数工具定义。
func newToolDefinition(name string, description string, schema string) ai.ToolDefinition {
	return ai.ToolDefinition{Type: "function", Function: ai.ToolFunction{
		Name: name, Description: description, Parameters: json.RawMessage(schema),
	}}
}

// execute 执行一个经过名称路由的强类型工具，不把页面动作交给 AI。
func (s *toolExecutionState) execute(ctx context.Context, call ai.ToolCall) (json.RawMessage, error) {
	if strings.TrimSpace(call.Type) != "" && call.Type != "function" {
		return nil, argumentError("工具类型只支持 function")
	}
	switch strings.TrimSpace(call.Function.Name) {
	case toolGetContext:
		if err := decodeToolArguments(call, &struct{}{}); err != nil {
			return nil, err
		}
		return marshalToolResult(struct {
			Position     cloud.AutoReplyPosition       `json:"position"`
			Config       cloud.PositionAutoReplyConfig `json:"config"`
			Company      cloud.CompanyProfile          `json:"company"`
			Conversation cloud.AutoReplyConversation   `json:"conversation"`
			Candidate    cloud.AutoReplyCandidateState `json:"candidate"`
		}{s.input.Position.Position, s.input.Position.Config, s.input.Position.CompanyProfile, s.input.Conversation, s.input.CandidateState})
	case toolGetChatHistory:
		var args historyToolArgs
		if err := decodeToolArguments(call, &args); err != nil {
			return nil, err
		}
		if args.Limit == 0 {
			args.Limit = 200
		}
		if args.Limit < 1 || args.Limit > cloud.AutoReplyMaxHistoryMessages {
			return nil, argumentError("limit 需要在1到5000之间")
		}
		start := max(0, len(s.input.Messages)-args.Limit)
		return marshalToolResult(struct {
			Messages  []cloud.AutoReplyMessage `json:"messages"`
			Truncated bool                     `json:"truncated"`
		}{s.input.Messages[start:], start > 0})
	case toolGetResume:
		if err := decodeToolArguments(call, &struct{}{}); err != nil {
			return nil, err
		}
		return marshalToolResult(struct {
			Stored *cloud.AutoReplyStoredCandidate `json:"stored,omitempty"`
			Files  []cloud.StoredResumeAttachment  `json:"attachments"`
			Page   *modelAutoReplyResumeBundle     `json:"page,omitempty"`
		}{s.input.CandidateState.Candidate, s.input.CandidateState.Attachments, resumeForTool(s.input.Resume)})
	case toolGetConfirmations:
		if err := decodeToolArguments(call, &struct{}{}); err != nil {
			return nil, err
		}
		return marshalToolResult(struct {
			Items []cloud.CandidateConfirmationItem `json:"items"`
		}{s.confirmations})
	case toolUpsertConfirmations:
		return s.upsertConfirmations(ctx, call)
	case toolRequestResume:
		if err := decodeToolArguments(call, &struct{}{}); err != nil {
			return nil, err
		}
		available := s.input.Resume != nil || s.input.CandidateState.HasResumeAttachment
		status := "固定流程还没有拿到简历，请转人工核对"
		if available {
			status = "固定流程已经取得简历或附件"
		}
		return marshalToolResult(struct {
			Available bool   `json:"available"`
			Status    string `json:"status"`
		}{available, status})
	case toolSendMessage:
		return s.prepareMessage(call)
	case toolSuggestConfig:
		return s.saveSuggestion(ctx, call)
	case toolNotifyHR:
		return s.prepareManualHandoff(call)
	default:
		return nil, argumentError("不支持的工具：" + strings.TrimSpace(call.Function.Name))
	}
}

// modelAutoReplyResumeBundle 是写入工具结果的只读页面简历摘要，避免暴露本地附件绝对路径。
type modelAutoReplyResumeBundle struct {
	CandidateName    string `json:"candidate_name"`
	Gender           string `json:"gender"`
	Phone            string `json:"phone"`
	Email            string `json:"email"`
	Wechat           string `json:"wechat"`
	BirthYM          string `json:"birth_ym"`
	OnlineResumeText string `json:"online_resume_text"`
	AttachmentCount  int    `json:"attachment_count"`
}

// resumeForTool 删除本地附件绝对路径后返回 AI 可读取的简历摘要。
func resumeForTool(value *model.AutoReplyResumeBundle) *modelAutoReplyResumeBundle {
	if value == nil {
		return nil
	}
	return &modelAutoReplyResumeBundle{
		CandidateName: value.CandidateName, Gender: value.Gender, Phone: value.Phone,
		Email: value.Email, Wechat: value.Wechat, BirthYM: value.BirthYM,
		OnlineResumeText: value.OnlineResumeText, AttachmentCount: len(value.AttachmentPaths),
	}
}

// upsertConfirmations 校验全部确认项后逐条幂等保存，并刷新本轮内存快照。
func (s *toolExecutionState) upsertConfirmations(ctx context.Context, call ai.ToolCall) (json.RawMessage, error) {
	var args confirmationToolArgs
	if err := decodeToolArguments(call, &args); err != nil {
		return nil, err
	}
	if len(args.Items) == 0 || len(args.Items) > 20 {
		return nil, argumentError("items 需要包含1到20条确认项")
	}
	for index, item := range args.Items {
		if err := validateConfirmationToolItem(item); err != nil {
			return nil, argumentError(fmt.Sprintf("第%d条确认项%s", index+1, err.Error()))
		}
	}
	saved := make([]cloud.CandidateConfirmationItem, 0, len(args.Items))
	for _, item := range args.Items {
		result, err := s.cloud.SaveAutoReplyConfirmationItem(ctx, s.input.Credentials, cloud.CandidateConfirmationItem{
			ConversationID: s.input.Conversation.ID, CandidateID: s.input.Conversation.CandidateID,
			PositionID: s.input.Position.Position.ID, ItemType: item.ItemType,
			Content: strings.TrimSpace(item.Content), Status: item.Status, SourceType: "ai",
			SourceRef:    firstNonEmpty(item.SourceRef, s.input.BasedOnMessageKey),
			EvidenceText: strings.TrimSpace(item.EvidenceText), Summary: strings.TrimSpace(item.Summary),
			CreatedByKind: "ai",
		})
		if err != nil {
			return nil, fmt.Errorf("保存候选人确认项失败：%w", err)
		}
		saved = append(saved, result)
		s.replaceConfirmation(result)
	}
	return marshalToolResult(struct {
		Items []cloud.CandidateConfirmationItem `json:"items"`
	}{saved})
}

// validateConfirmationToolItem 校验单条确认项枚举和文字长度。
func validateConfirmationToolItem(item confirmationToolItem) error {
	if !oneOf(item.ItemType, "required", "confirm", "bonus") {
		return fmt.Errorf("类型不支持")
	}
	if strings.TrimSpace(item.Content) == "" || len([]rune(item.Content)) > 1000 {
		return fmt.Errorf("内容为空或超过1000字")
	}
	if !oneOf(item.Status, "pending", "matched", "unmatched", "not_applicable", "conflicted") {
		return fmt.Errorf("状态不支持")
	}
	if len([]rune(item.SourceRef)) > 256 || len([]rune(item.EvidenceText)) > 2000 || len([]rune(item.Summary)) > 500 {
		return fmt.Errorf("证据或摘要过长")
	}
	return nil
}

// replaceConfirmation 用云端返回的去重键更新本轮确认项快照。
func (s *toolExecutionState) replaceConfirmation(saved cloud.CandidateConfirmationItem) {
	for index, item := range s.confirmations {
		if item.ID == saved.ID || (item.DedupeKey != "" && item.DedupeKey == saved.DedupeKey) {
			s.confirmations[index] = saved
			return
		}
	}
	s.confirmations = append(s.confirmations, saved)
}

// prepareMessage 记录唯一一条待发送回复，真正页面发送仍由公共固定流程完成。
func (s *toolExecutionState) prepareMessage(call ai.ToolCall) (json.RawMessage, error) {
	var args sendMessageToolArgs
	if err := decodeToolArguments(call, &args); err != nil {
		return nil, err
	}
	message := strings.TrimSpace(args.Message)
	if message == "" || len([]rune(message)) > maxAutoReplyMessageRunes {
		return nil, argumentError("message 不能为空且不能超过200字")
	}
	if s.manualReason != "" {
		return nil, argumentError("已经选择转人工，不能同时发送消息")
	}
	if s.pendingReply != "" {
		return nil, argumentError("一条候选人新消息最多发送一条回复")
	}
	s.pendingReply = message
	return marshalToolResult(struct {
		Prepared bool `json:"prepared"`
	}{true})
}

// saveSuggestion 校验并保存一条等待 HR 审核的岗位或公司资料建议。
func (s *toolExecutionState) saveSuggestion(ctx context.Context, call ai.ToolCall) (json.RawMessage, error) {
	var args suggestionToolArgs
	if err := decodeToolArguments(call, &args); err != nil {
		return nil, err
	}
	if !oneOf(args.SuggestionType, "position", "company") || !oneOf(args.Operation, "create", "update", "delete") {
		return nil, argumentError("suggestion_type 或 operation 不支持")
	}
	if strings.TrimSpace(args.Reason) == "" || len([]rune(args.Reason)) > 1000 {
		return nil, argumentError("reason 不能为空且不能超过1000字")
	}
	proposed := strings.TrimSpace(string(args.ProposedValue))
	if len(args.ProposedValue) == 0 || !json.Valid(args.ProposedValue) || !strings.HasPrefix(proposed, "{") {
		return nil, argumentError("proposed_value 必须是 JSON 对象")
	}
	companyID := ""
	if args.SuggestionType == "company" {
		companyID = s.input.Position.CompanyProfile.ID
	}
	result, err := s.cloud.SaveAutoReplySuggestion(ctx, s.input.Credentials, cloud.AutoReplyConfigSuggestion{
		ConversationID: s.input.Conversation.ID, PositionID: s.input.Position.Position.ID,
		CompanyProfileID: companyID, SuggestionType: args.SuggestionType,
		Operation: args.Operation, TargetID: strings.TrimSpace(args.TargetID),
		ProposedValue: args.ProposedValue, Reason: strings.TrimSpace(args.Reason),
	})
	if err != nil {
		return nil, fmt.Errorf("保存配置建议失败：%w", err)
	}
	return marshalToolResult(result)
}

// prepareManualHandoff 记录转人工原因，由固定流程统一发送幂等邮件。
func (s *toolExecutionState) prepareManualHandoff(call ai.ToolCall) (json.RawMessage, error) {
	var args notifyHRToolArgs
	if err := decodeToolArguments(call, &args); err != nil {
		return nil, err
	}
	reason := strings.TrimSpace(args.Reason)
	key := strings.TrimSpace(args.ReasonKey)
	if reason == "" || len([]rune(reason)) > 500 || key == "" || len([]rune(key)) > 100 {
		return nil, argumentError("reason 和 reason_key 不能为空，且长度不能超限")
	}
	if s.pendingReply != "" {
		return nil, argumentError("已经准备发送消息，不能同时转人工")
	}
	s.manualReason = reason
	s.manualKey = key
	return marshalToolResult(struct {
		Prepared bool `json:"prepared"`
	}{true})
}

// decodeToolArguments 严格解码单个工具参数，不允许多余字段或多个 JSON 文档。
func decodeToolArguments(call ai.ToolCall, target any) error {
	arguments := strings.TrimSpace(call.Function.Arguments)
	if arguments == "" {
		arguments = "{}"
	}
	decoder := json.NewDecoder(strings.NewReader(arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return argumentError("参数 JSON 不正确：" + err.Error())
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return argumentError("参数只能包含一个 JSON 对象")
		}
		return argumentError("参数 JSON 不正确：" + err.Error())
	}
	return nil
}

// marshalToolResult 把强类型工具结果编码为标准 JSON。
func marshalToolResult(value any) (json.RawMessage, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("编码工具结果失败：%w", err)
	}
	return encoded, nil
}

// argumentError 创建允许 AI 修正的参数错误。
func argumentError(message string) error {
	return &toolArgumentError{message: strings.TrimSpace(message)}
}

// oneOf 判断字符串是否属于允许值。
func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}
