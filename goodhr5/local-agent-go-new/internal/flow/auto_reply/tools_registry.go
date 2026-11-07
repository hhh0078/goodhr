// Package auto_reply 本文件定义自动回复动作提议工具及其强类型参数校验；业务变更由固定流程统一提交。
package auto_reply

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

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
	toolSendMessages        = "send_messages"
	toolSuggestConfig       = "suggest_config_change"
	toolNotifyHR            = "notify_hr"
	toolFinishReview        = "finish_review"
)

// toolArgumentError 表示允许 AI 根据错误信息修正的工具名称或参数错误。
type toolArgumentError struct {
	message string
}

// Error 返回可供 AI 修正工具调用的中文说明。
func (e *toolArgumentError) Error() string {
	return e.message
}

type confirmationToolItem struct {
	ID                  string `json:"id"`
	PositionConditionID string `json:"position_condition_id"`
	ItemType            string `json:"item_type"`
	Content             string `json:"content"`
	Status              string `json:"status"`
	StatusReason        string `json:"status_reason"`
	SourceRef           string `json:"source_ref"`
	EvidenceText        string `json:"evidence_text"`
}

type confirmationToolArgs struct {
	Items []confirmationToolItem `json:"items"`
}

type sendMessagesToolArgs struct {
	Messages []ReplyMessage `json:"messages"`
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
	input               ReplyContext
	confirmations       []cloud.CandidateConfirmationItem
	pendingMessages     []ReplyMessage
	manualReason        string
	manualKey           string
	confirmationUpdates []cloud.CandidateConfirmationItem
	suggestions         []cloud.AutoReplyConfigSuggestion
	reviewFinished      bool
}

// autoReplyToolsForContext 复用同一条件工具；内部补审只允许条件提议和完成信号，不暴露发送能力。
func autoReplyToolsForContext(input ReplyContext) []ai.ToolDefinition {
	tools := autoReplyToolDefinitions()
	if !input.ConditionsOnly {
		return tools
	}
	return []ai.ToolDefinition{tools[0], newToolDefinition(toolFinishReview, "结束本次内部条件复核，不发送任何候选人消息。", `{"type":"object","properties":{},"additionalProperties":false}`)}
}

// autoReplyToolDefinitions 返回只产生提议的四个动作工具，不重复提供已有上下文的查询工具。
func autoReplyToolDefinitions() []ai.ToolDefinition {
	return []ai.ToolDefinition{
		newToolDefinition(toolUpsertConfirmations, "只更新 confirmation_items 中已有 id 的条件状态，不能新增或改变条件。每批最多50项，可分批。matched/unmatched必须提供存在的source_ref和证据原文。", `{"type":"object","properties":{"items":{"type":"array","minItems":1,"maxItems":50,"items":{"type":"object","properties":{"id":{"type":"string","maxLength":128},"position_condition_id":{"type":"string","maxLength":128},"item_type":{"type":"string","enum":["required","confirm","bonus"]},"content":{"type":"string","minLength":1,"maxLength":1000},"status":{"type":"string","enum":["pending","matched","unmatched"]},"status_reason":{"type":"string","minLength":1,"maxLength":500},"source_ref":{"type":"string","maxLength":256},"evidence_text":{"type":"string","maxLength":2000}},"required":["id","item_type","content","status","status_reason","source_ref","evidence_text"],"additionalProperties":false}}},"required":["items"],"additionalProperties":false}`),
		newToolDefinition(toolSendMessages, "准备最多两条候选人消息：可先回答最新问题，再询问一个未确认条件。信息不足时改用 notify_hr。", `{"type":"object","properties":{"messages":{"type":"array","minItems":1,"maxItems":2,"items":{"type":"object","properties":{"type":{"type":"string","enum":["answer","confirmation"]},"content":{"type":"string","minLength":1,"maxLength":200},"confirmation_item_id":{"type":"string","maxLength":128}},"required":["type","content"],"additionalProperties":false}}},"required":["messages"],"additionalProperties":false}`),
		newToolDefinition(toolSuggestConfig, "提交岗位或公司资料的新增、修改或删除建议，等待 HR 审核。", `{"type":"object","properties":{"suggestion_type":{"type":"string","enum":["position","company"]},"operation":{"type":"string","enum":["create","update","delete"]},"target_id":{"type":"string","maxLength":128},"proposed_value":{"type":"object"},"reason":{"type":"string","minLength":1,"maxLength":1000}},"required":["suggestion_type","operation","proposed_value","reason"],"additionalProperties":false}`),
		newToolDefinition(toolNotifyHR, "没有可靠答案或问题与招聘无关时，通知当前招聘团队人工接管；这是内部通知，候选人不会收到任何占位回复。", `{"type":"object","properties":{"reason":{"type":"string","minLength":1,"maxLength":500},"reason_key":{"type":"string","minLength":1,"maxLength":100}},"required":["reason","reason_key"],"additionalProperties":false}`),
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
	if s.input.ConditionsOnly && call.Function.Name != toolUpsertConfirmations && call.Function.Name != toolFinishReview {
		return nil, argumentError("本轮只复核内部条件，不允许准备消息或通知，请使用 finish_review 结束")
	}
	switch strings.TrimSpace(call.Function.Name) {
	case toolUpsertConfirmations:
		return s.upsertConfirmations(ctx, call)
	case toolSendMessages:
		return s.prepareMessages(call)
	case toolSuggestConfig:
		return s.saveSuggestion(ctx, call)
	case toolNotifyHR:
		return s.prepareManualHandoff(call)
	case toolFinishReview:
		if !s.input.ConditionsOnly {
			return nil, argumentError("普通问答不能使用内部复核完成工具")
		}
		var args struct{}
		if err := decodeToolArguments(call, &args); err != nil {
			return nil, err
		}
		s.reviewFinished = true
		return json.RawMessage(`{"ok":true}`), nil
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

// upsertConfirmations 校验增量证据后仅更新本轮提议，消息与配置复核通过前不修改持久状态。
func (s *toolExecutionState) upsertConfirmations(ctx context.Context, call ai.ToolCall) (json.RawMessage, error) {
	var args confirmationToolArgs
	if err := decodeToolArguments(call, &args); err != nil {
		return nil, err
	}
	if len(args.Items) == 0 || len(args.Items) > 50 {
		return nil, argumentError("items 需要包含1到50条确认项")
	}
	saved := make([]cloud.CandidateConfirmationItem, 0, len(args.Items))
	for index, item := range args.Items {
		if err := validateConfirmationToolItem(item); err != nil {
			return nil, argumentError(fmt.Sprintf("第%d条确认项%s", index+1, err.Error()))
		}
		result, found := s.confirmationByID(item.ID)
		if !found || (item.PositionConditionID != "" && item.PositionConditionID != result.PositionConditionID) {
			return nil, argumentError("只能更新 confirmation_items 中已有的条件 id，请忽略未配置的要求")
		}
		if result.ItemType != item.ItemType || strings.TrimSpace(result.Content) != strings.TrimSpace(item.Content) {
			return nil, argumentError("条件类型与正文必须与该 id 的岗位配置一致")
		}
		now := time.Now().UTC()
		result.ConversationID = s.input.Conversation.ID
		result.Status, result.StatusReason = item.Status, strings.TrimSpace(item.StatusReason)
		result.SourceRef, result.EvidenceText = strings.TrimSpace(item.SourceRef), strings.TrimSpace(item.EvidenceText)
		result.Summary, result.LastReviewedAt = result.StatusReason, &now
		if result.Status != "pending" && result.SourceRef == s.input.BasedOnMessageKey {
			result.LastAnsweredAt = &now
		}
		if err := s.validateConfirmationEvidence(result); err != nil {
			return nil, err
		}
		saved = append(saved, result)
	}
	// 整批校验通过才合并到本轮状态，参数修正不会留下半批判断。
	for _, result := range saved {
		s.addConfirmationProposal(result)
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
	if !oneOf(item.Status, "pending", "matched", "unmatched") {
		return fmt.Errorf("状态不支持")
	}
	if strings.TrimSpace(item.StatusReason) == "" || len([]rune(item.StatusReason)) > 500 {
		return fmt.Errorf("状态理由为空或超过500字")
	}
	if len([]rune(item.ID)) > 128 || len([]rune(item.PositionConditionID)) > 128 || len([]rune(item.SourceRef)) > 256 || len([]rune(item.EvidenceText)) > 2000 {
		return fmt.Errorf("证据或摘要过长")
	}
	return nil
}

// addConfirmationProposal 合并同轮同一条件的最后一次有效判断，不写入本地或云端。
func (s *toolExecutionState) addConfirmationProposal(item cloud.CandidateConfirmationItem) {
	for index, current := range s.confirmationUpdates {
		if current.ID == item.ID {
			s.confirmationUpdates[index] = item
			return
		}
	}
	s.confirmationUpdates = append(s.confirmationUpdates, item)
}

// validateConfirmationEvidence 要求确定性判断引用本轮可见真实消息或简历原文，不能凭空标记符合。
func (s *toolExecutionState) validateConfirmationEvidence(item cloud.CandidateConfirmationItem) error {
	if item.Status == "pending" {
		return nil
	}
	evidence := strings.TrimSpace(item.EvidenceText)
	ref := strings.TrimSpace(item.SourceRef)
	if evidence == "" || ref == "" {
		return argumentError("符合或不符合需要真实来源编号和证据原文；没有证据请保持pending")
	}
	for _, message := range s.input.Messages {
		if (ref == message.ID || ref == message.PlatformMessageID || ref == message.Fingerprint) && message.Direction == "candidate" && strings.Contains(message.TextContent, evidence) {
			return nil
		}
	}
	if ref == "resume" && s.input.CandidateState.Candidate != nil && s.input.CandidateState.Candidate.ResumeStructured {
		raw, _ := json.Marshal(s.input.CandidateState.Candidate)
		if strings.Contains(string(raw), evidence) {
			return nil
		}
	}
	if ref == "resume" && s.input.Resume != nil && strings.Contains(s.input.Resume.OnlineResumeText, evidence) {
		return nil
	}
	return argumentError("证据没有对应到本轮可见的候选人消息或简历，请引用真实原文，否则保持pending")
}

// normalizeConfirmationContent 生成仅用于本轮匹配的稳定确认项正文。
func normalizeConfirmationContent(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), ""))
}

// replaceConfirmation 用已验证的提议更新本轮确认项快照。
func (s *toolExecutionState) replaceConfirmation(saved cloud.CandidateConfirmationItem) {
	for index, item := range s.confirmations {
		if item.ID == saved.ID || (item.DedupeKey != "" && item.DedupeKey == saved.DedupeKey) {
			s.confirmations[index] = saved
			return
		}
	}
	s.confirmations = append(s.confirmations, saved)
}

// prepareMessages 记录最多两条待发送消息，真正页面发送仍由公共固定流程完成。
func (s *toolExecutionState) prepareMessages(call ai.ToolCall) (json.RawMessage, error) {
	var args sendMessagesToolArgs
	if err := decodeToolArguments(call, &args); err != nil {
		return nil, err
	}

	if len(args.Messages) == 0 || len(args.Messages) > 2 {
		return nil, argumentError("messages 需要包含1到2条消息")
	}
	if s.manualReason != "" || len(s.pendingMessages) > 0 {
		return nil, argumentError("已经选择转人工，不能同时发送消息")
	}
	confirmationCount := 0
	prepared := make([]ReplyMessage, 0, len(args.Messages))
	for index, item := range args.Messages {
		item.Type = strings.TrimSpace(item.Type)
		item.Content = strings.TrimSpace(item.Content)
		item.ConfirmationItemID = strings.TrimSpace(item.ConfirmationItemID)
		if !oneOf(item.Type, "answer", "confirmation") || item.Content == "" || len([]rune(item.Content)) > maxAutoReplyMessageRunes {
			return nil, argumentError(fmt.Sprintf("第%d条消息类型不支持、内容为空或超过200字", index+1))
		}
		if item.Type == "answer" {
			if index > 0 {
				return nil, argumentError("回答消息只能放在第一条")
			}
			item.Content = normalizeSimpleCandidateReply(s.input, item.Content)
			item.ConfirmationItemID = ""
		} else {
			confirmationCount++
			if confirmationCount > 1 || (len(args.Messages) == 2 && index != 1) {
				return nil, argumentError("每轮最多询问一个条件，两条消息时确认问题必须放在第二条")
			}
			confirmation, found := s.confirmationByID(item.ConfirmationItemID)
			if !found || confirmation.Status != "pending" || !oneOf(confirmation.ItemType, "required", "confirm") {
				return nil, argumentError("确认消息必须关联一条仍未确认的 required 或 confirm 条件")
			}
			if confirmation.AskCount >= 3 {
				return nil, argumentError("这条条件已经询问三次，请先回答问题或请求人工协助")
			}
		}
		prepared = append(prepared, item)
	}
	s.pendingMessages = prepared
	return marshalToolResult(struct {
		Prepared int `json:"prepared"`
	}{len(prepared)})
}

// confirmationByID 按内部编号读取本轮确认项。
func (s *toolExecutionState) confirmationByID(id string) (cloud.CandidateConfirmationItem, bool) {
	for _, item := range s.confirmations {
		if item.ID == strings.TrimSpace(id) {
			return item, true
		}
	}
	return cloud.CandidateConfirmationItem{}, false
}

// normalizeSimpleCandidateReply 防止 AI 对纯问候、致谢或求职兴趣主动扩展岗位和简历信息。
func normalizeSimpleCandidateReply(input ReplyContext, proposed string) string {
	if len(input.PendingMessages) > 1 {
		return strings.TrimSpace(proposed)
	}
	message, found := findBasedOnMessage(input.Messages, input.BasedOnMessageKey)
	latest := strings.TrimSpace(message.TextContent)
	if !found || latest == "" || looksLikeCandidateQuestion(latest) {
		return strings.TrimSpace(proposed)
	}
	if containsAnyText(latest, "谢谢", "感谢", "辛苦了") && len([]rune(latest)) <= 30 {
		return "不客气～"
	}
	if containsAnyText(latest, "感兴趣", "详聊", "沟通", "了解一下", "在看机会", "看看机会") && len([]rune(latest)) <= 80 {
		return "可以的，你想了解岗位哪方面呢？"
	}
	if containsAnyText(latest, "你好", "您好", "嗨", "哈喽", "在吗") && len([]rune(latest)) <= 20 {
		return "你好呀～"
	}
	return strings.TrimSpace(proposed)
}

// looksLikeCandidateQuestion 判断候选人是否提出了需要 AI 正常回答的具体问题。
func looksLikeCandidateQuestion(value string) bool {
	return strings.ContainsAny(value, "?？") || containsAnyText(value,
		"多少", "哪里", "哪儿", "怎么", "如何", "为什么", "是否", "能否", "可以吗", "可不可以", "有没有", "几号", "几点", "什么时候", "什么样",
	)
}

// containsAnyText 判断文字中是否包含任意一个非空关键词。
func containsAnyText(value string, keywords ...string) bool {
	for _, keyword := range keywords {
		if keyword != "" && strings.Contains(value, keyword) {
			return true
		}
	}
	return false
}

// saveSuggestion 校验并暂存一条等待 HR 审核的岗位或公司资料建议。
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
	result := cloud.AutoReplyConfigSuggestion{
		ConversationID: s.input.Conversation.ID, PositionID: s.input.Position.Position.ID,
		CompanyProfileID: companyID, SuggestionType: args.SuggestionType,
		Operation: args.Operation, TargetID: strings.TrimSpace(args.TargetID),
		ProposedValue: args.ProposedValue, Reason: strings.TrimSpace(args.Reason),
	}
	s.suggestions = append(s.suggestions, result)
	return marshalToolResult(struct {
		Prepared bool `json:"prepared"`
	}{true})
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

	if len(s.pendingMessages) > 0 {
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
