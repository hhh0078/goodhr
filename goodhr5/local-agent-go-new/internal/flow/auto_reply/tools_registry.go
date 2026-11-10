// Package auto_reply 本文件定义自动回复动作提议工具及其强类型参数校验；业务变更由固定流程统一提交。
package auto_reply

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

const (
	maxAutoReplyMessageRunes = 200

	toolGetContext           = "get_context"
	toolGetChatHistory       = "get_chat_history"
	toolGetResume            = "get_resume"
	toolGetConfirmations     = "get_confirmation_items"
	toolUpsertConfirmations  = "upsert_confirmation_items"
	toolArchiveConfirmations = "archive_confirmation_items"
	toolRequestResume        = "request_resume"
	toolSendMessages         = "send_messages"
	toolSuggestConfig        = "suggest_config_change"
	toolNotifyHR             = "notify_hr"
	toolFinishReview         = "finish_review"
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
	Messages     []ReplyMessage             `json:"messages"`
	InfoRequests model.CandidateInfoRequest `json:"info_requests,omitempty"`
}

type suggestionToolArgs struct {
	SuggestionType string          `json:"suggestion_type"`
	Operation      string          `json:"operation"`
	TargetID       string          `json:"target_id"`
	ProposedValue  json.RawMessage `json:"proposed_value"`
	Reason         string          `json:"reason"`
}

type notifyHRToolArgs struct {
	Followup           *goalFollowupToolItem `json:"followup,omitempty"`
	Question           string                `json:"question,omitempty"`
	ExistingQuestionID string                `json:"existing_question_id,omitempty"`
	Questions          []handoffQuestion     `json:"questions,omitempty"`
	Reason             string                `json:"reason"`
	ReasonKey          string                `json:"reason_key"`
	KnownAnswer        string                `json:"known_answer,omitempty"`
}

type toolExecutionState struct {
	manualFollowup      *ReplyMessage
	handoffQuestions    []handoffQuestion
	input               ReplyContext
	confirmations       []cloud.CandidateConfirmationItem
	pendingMessages     []ReplyMessage
	manualReason        string
	manualKey           string
	knownAnswer         string
	confirmationUpdates []cloud.CandidateConfirmationItem
	suggestions         []cloud.AutoReplyConfigSuggestion
	reviewFinished      bool
	messagesPrepared    bool
	textSuppressed      bool
	infoRequests        model.CandidateInfoRequest
}

// autoReplyToolsForContext 复用目标工具；新简历事件可提议一次追问，普通内部复核仍不暴露发送能力。
func autoReplyToolsForContext(input ReplyContext) []ai.ToolDefinition {
	tools := autoReplyToolDefinitions()
	if !input.ConditionsOnly {
		return tools
	}
	result := []ai.ToolDefinition{tools[0], tools[len(tools)-1], newToolDefinition(toolFinishReview, "结束本次目标复核；没有需要追问的信息时不发送任何消息。", `{"type":"object","properties":{},"additionalProperties":false}`)}
	if input.AllowGoalFollowup {
		result = append(result, tools[1])
	}
	return result
}

// autoReplyToolDefinitions 返回只产生提议的动作工具，不重复提供已有上下文的查询工具。
func autoReplyToolDefinitions() []ai.ToolDefinition {
	return []ai.ToolDefinition{
		newToolDefinition(toolUpsertConfirmations, "维护沟通目标：已有目标按id更新；新增目标id留空、item_type=confirm、status=pending，并用source_ref及evidence_text引用岗位position、在线资料online_profile、简历resume或真实来信的建档依据。AI自建目标可改写，HR岗位条件只能更新有证据的判断，不能改正文、类型或放宽。先查已有目标避免同义重复。每批最多50项。matched/unmatched必须有候选人或简历的真实证据。", `{"type":"object","properties":{"items":{"type":"array","minItems":1,"maxItems":50,"items":{"type":"object","properties":{"id":{"type":"string","maxLength":128},"position_condition_id":{"type":"string","maxLength":128},"item_type":{"type":"string","enum":["required","confirm","bonus"]},"content":{"type":"string","minLength":1,"maxLength":1000},"status":{"type":"string","enum":["pending","matched","unmatched"]},"status_reason":{"type":"string","minLength":1,"maxLength":500},"source_ref":{"type":"string","maxLength":256},"evidence_text":{"type":"string","maxLength":2000}},"required":["id","item_type","content","status","status_reason","source_ref","evidence_text"],"additionalProperties":false}}},"required":["items"],"additionalProperties":false}`),
		newToolDefinition(toolSendMessages, "准备最多两条候选人消息：先回答再询问一个目标。适用配置话术时必须绑定phrase_id；空或已删除场景传该id和空content，正常静默完成，不得换词补发。native_info_requests=true时普通答复不能混入索要资料；确需电话或微信时可附info_requests单独提议，系统在答案送达及平台条件满足后操作原生按钮。其他平台可按需提议附件，正文原已索要时保持requests_resume原约定。信息不足时改用 notify_hr。", `{"type":"object","properties":{"info_requests":{"type":"object","properties":{"request_phone":{"type":"boolean"},"request_wechat":{"type":"boolean"},"request_resume":{"type":"boolean"}},"additionalProperties":false},"messages":{"type":"array","minItems":1,"maxItems":2,"items":{"type":"object","properties":{"type":{"type":"string","enum":["answer","confirmation"]},"content":{"type":"string","maxLength":200},"phrase_id":{"type":"string","maxLength":80},"confirmation_item_id":{"type":"string","maxLength":128},"requests_resume":{"type":"boolean"}},"required":["type","content","requests_resume"],"additionalProperties":false}}},"required":["messages"],"additionalProperties":false}`),
		newToolDefinition(toolSuggestConfig, "提交岗位或公司资料的新增、修改或删除建议，等待 HR 审核。", `{"type":"object","properties":{"suggestion_type":{"type":"string","enum":["position","company"]},"operation":{"type":"string","enum":["create","update","delete"]},"target_id":{"type":"string","maxLength":128},"proposed_value":{"type":"object"},"reason":{"type":"string","minLength":1,"maxLength":1000}},"required":["suggestion_type","operation","proposed_value","reason"],"additionalProperties":false}`),
		newToolDefinition(toolNotifyHR, "没有可靠答案或问题与招聘无关时，通知当前招聘团队人工接管；missing_recruitment_info 表示需要核实的招聘问题，可附上混合问题中有依据的 known_answer。系统登记人工待办后才可以按配置发回执，技术问题或无关问题只作内部通知。登记缺失招聘信息时，如另有尚未问过的候选人目标，可附followup（content与confirmation_item_id），系统会先发已登记的回执再最多追问一次。", `{"type":"object","properties":{"followup":{"type":"object","properties":{"content":{"type":"string","maxLength":200},"phrase_id":{"type":"string","maxLength":80},"confirmation_item_id":{"type":"string","minLength":1,"maxLength":128}},"required":["content","confirmation_item_id"],"additionalProperties":false},"reason":{"type":"string","minLength":1,"maxLength":500},"reason_key":{"type":"string","minLength":1,"maxLength":100},"known_answer":{"type":"string","maxLength":200},"question":{"type":"string","maxLength":300},"existing_question_id":{"type":"string","maxLength":128},"questions":{"type":"array","maxItems":8,"items":{"type":"object","properties":{"question":{"type":"string","maxLength":300},"existing_question_id":{"type":"string","maxLength":128}},"required":["question"],"additionalProperties":false}}},"required":["reason","reason_key"],"additionalProperties":false}`),
		newToolDefinition(toolArchiveConfirmations, "删除已不再需要或被其他任务替代的AI自建沟通目标，保留审计；不能删除HR岗位必要条件。请列出已有id及原因，不要删除后用同义问题重新催问。", `{"type":"object","properties":{"ids":{"type":"array","minItems":1,"maxItems":50,"items":{"type":"string","minLength":1,"maxLength":128}},"reason":{"type":"string","minLength":1,"maxLength":500}},"required":["ids","reason"],"additionalProperties":false}`),
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
	if s.input.ConditionsOnly && call.Function.Name != toolUpsertConfirmations && call.Function.Name != toolArchiveConfirmations && call.Function.Name != toolFinishReview && !(s.input.AllowGoalFollowup && call.Function.Name == toolSendMessages) {
		return nil, argumentError("本轮只复核内部条件，不允许准备消息或通知，请使用 finish_review 结束")
	}
	switch strings.TrimSpace(call.Function.Name) {
	case toolUpsertConfirmations:
		return s.upsertConfirmations(ctx, call)
	case toolArchiveConfirmations:
		return s.archiveGoals(call)
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
func (s *toolExecutionState) upsertConfirmations(_ context.Context, call ai.ToolCall) (json.RawMessage, error) {
	return s.proposeGoalUpdates(call)
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
	_, err := s.normalizeConfirmationSource(item.SourceRef, item.EvidenceText, false)
	return err
}

// normalizeConfirmationContent 生成仅用于本轮匹配的稳定确认项正文。
func normalizeConfirmationContent(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), ""))
}

// replaceConfirmation 用已验证的提议更新本轮确认项快照。
func (s *toolExecutionState) replaceConfirmation(saved cloud.CandidateConfirmationItem) {
	for index, item := range s.confirmations {
		if item.ID == saved.ID || (item.DedupeKey != "" && item.DedupeKey == saved.DedupeKey) {
			if item.Content != saved.Content || saved.Status != "pending" || saved.ArchivedAt != nil {
				if s.manualFollowup != nil && s.manualFollowup.ConfirmationItemID == item.ID {
					s.manualFollowup = nil
				}
				kept := s.pendingMessages[:0]
				for _, message := range s.pendingMessages {
					if message.Type != "confirmation" || message.ConfirmationItemID != item.ID {
						kept = append(kept, message)
					}
				}
				s.pendingMessages = kept
			}
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
	if err := validateNativeInfoProposal(s.input, args.InfoRequests, args.Messages); err != nil {
		return nil, err
	}
	if s.input.ConditionsOnly && (!s.input.AllowGoalFollowup || len(args.Messages) != 1 || args.Messages[0].Type != "confirmation") {
		return nil, argumentError("简历新证据轮只可追问一个尚未问过的目标，不能补答历史消息")
	}
	if s.manualReason != "" || s.messagesPrepared || len(s.pendingMessages) > 0 {
		return nil, argumentError("已经选择转人工，不能同时发送消息")
	}
	confirmationCount := 0
	prepared := make([]ReplyMessage, 0, len(args.Messages))
	for index, item := range args.Messages {
		item.Type = strings.TrimSpace(item.Type)
		item.Content = strings.TrimSpace(item.Content)
		item.ConfirmationItemID = strings.TrimSpace(item.ConfirmationItemID)
		if !oneOf(item.Type, "answer", "confirmation") {
			return nil, argumentError(fmt.Sprintf("第%d条消息类型不支持", index+1))
		}
		var allowed bool
		item, allowed = applyReplyScenePolicy(s.input, item)
		if !allowed {
			s.textSuppressed = true
			continue
		}
		if item.Content == "" || len([]rune(item.Content)) > maxAutoReplyMessageRunes {
			return nil, argumentError(fmt.Sprintf("第%d条消息类型不支持、内容为空或超过200字", index+1))
		}
		if strings.ContainsAny(item.Content, "{}｛｝") {
			return nil, argumentError("话术还有未填写的变量，请用已核实的信息替换，或转人工。")
		}
		if item.RequestsResume && (s.input.Conversation.Status == "waiting_resume" || s.input.CandidateState.HasResumeAttachment || s.input.PageSnapshot.ResumeCardAvailable) {
			return nil, argumentError("已经索要或收到简历，不要再次索要；请只回答当前问题。")
		}
		if err := checkPhoneReply(s.input.PhoneContact, s.input.Position.Config, item.Content); err != nil {
			return nil, err
		}
		if item.Type == "answer" {
			if index > 0 {
				return nil, argumentError("回答消息只能放在第一条")
			}
			item.Content = normalizeSimpleCandidateReply(s.input, item.Content)
			if err := checkReplyFollowThrough(s.input.Position, item.Content); err != nil {
				short := verifiedLocationAnswer(s.input, item.Content)
				if short == "" {
					return nil, err
				}
				item.Content = short
				item.RequestsResume = false
			}
			item.ConfirmationItemID = ""
		} else {
			confirmationCount++
			if confirmationCount > 1 || (len(args.Messages) == 2 && index != 1) {
				return nil, argumentError("每轮最多询问一个条件，两条消息时确认问题必须放在第二条")
			}
			if err := s.validateGoalQuestion(item); err != nil {
				return nil, err
			}
			goal, _ := s.confirmationByID(item.ConfirmationItemID)
			item.GoalContentKey = cloud.AutoReplyGoalContentKey(goal)
		}
		prepared = append(prepared, item)
	}
	s.pendingMessages = prepared
	s.messagesPrepared = true
	s.infoRequests = args.InfoRequests
	return marshalToolResult(struct {
		Prepared   int  `json:"prepared"`
		Suppressed bool `json:"text_suppressed,omitempty"`
	}{len(prepared), s.textSuppressed})
}

// explicitWorkplace 匹配岗位原文中独立标明的简短工作地点；多个可能地点或复杂安排留给正常问答。
var explicitWorkplace = regexp.MustCompile(`(?:工作地点|工作城市|上班地点)[：:是为在于\s]*([\p{Han}]{2,12})(?:[，,。；;\n]|$)`)

// verifiedLocationAnswer 仅当候选人确实问过地点、草稿的首句和岗位明示地点完全相符时，保留这句可独立发送的答案。
func verifiedLocationAnswer(input ReplyContext, draft string) string {
	asked := false
	for _, m := range input.PendingMessages {
		if m.Direction == "candidate" && containsAnyText(m.TextContent, "哪个城市", "哪座城市", "工作地点", "上班地点", "在哪里", "在哪儿", "在哪个区") {
			asked = true
			break
		}
	}
	if !asked {
		return ""
	}
	matches := explicitWorkplace.FindAllStringSubmatch(input.Position.Config.PositionDescription, -1)
	if len(matches) != 1 || containsAnyText(matches[0][1], "待定", "未定", "待确认", "不限", "未确认", "另议", "或者", "或", "以及", "待分配") {
		return ""
	}
	location := matches[0][1]
	clauses := strings.FieldsFunc(draft, func(r rune) bool { return strings.ContainsRune("，,。；;！!？?～~\n", r) })
	if len(clauses) < 2 {
		return ""
	}
	first := strings.TrimRight(strings.TrimSpace(clauses[0]), "哦呀呢哟")
	for _, prefix := range []string{"岗位在", "这份岗位在", "这个岗位在", "工作地点在", "这份岗位的工作地点在", "岗位的工作地点在", "工作地点是", "工作城市是"} {
		if first == prefix+location {
			return "这份岗位的工作地点在" + location + "。"
		}
	}
	return ""
}

// checkReplyFollowThrough 拦下把未知信息说成正在办理或承诺未来联络的回答，让既有工具参数纠错只重写这次草稿。
func checkReplyFollowThrough(position cloud.AutoReplyPositionSnapshot, answer string) error {
	facts := strings.Join([]string{position.Config.PositionDescription, position.CompanyProfile.Address,
		position.CompanyProfile.Overview, position.CompanyProfile.ExtraInfo}, "\n")
	for _, clause := range strings.FieldsFunc(answer, func(r rune) bool {
		return strings.ContainsRune("，,。；;！!？?～~\n", r)
	}) {
		clause = strings.TrimSpace(clause)
		waiting := containsAnyText(clause, "还在确认中", "正在确认中", "还在核实中", "正在核实中")
		followUp := containsAnyText(clause, "后续", "稍后", "之后", "到时", "有消息", "有结果", "有准确信息", "确认后", "核实后") &&
			containsAnyText(clause, "同步", "通知您", "联系您", "回复您", "给您反馈", "告知您", "告诉您", "跟您反馈")
		if (waiting || followUp) && !strings.Contains(facts, clause) {
			return argumentError("请删掉没有岗位或公司原文依据的办理进度和后续联络承诺，只简短回答已知事实。待处理问题都缺少依据时用 notify_hr。")
		}
	}
	return nil
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
	if input.Position.Config.Phrases != nil || input.Position.Config.PhrasesVersion != "" {
		// 新岗位的问候、澄清等以云端话术为准，保留带有实际问题的模型答复。
		return strings.TrimSpace(proposed)
	}
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
	answer := strings.TrimSpace(args.KnownAnswer)
	if answer != "" {
		if key != "missing_recruitment_info" || len([]rune(answer)) > maxAutoReplyMessageRunes || strings.ContainsAny(answer, "{}｛｝") {
			return nil, argumentError("只有需要核实的招聘问题可以附上有依据的简短回答")
		}
		if err := checkPhoneReply(s.input.PhoneContact, s.input.Position.Config, answer); err != nil {
			return nil, err
		}
		if err := checkReplyFollowThrough(s.input.Position, answer); err != nil {
			return nil, err
		}
	}
	qs, err := verifiedHandoffQuestions(s.input, args)
	if err != nil {
		return nil, err
	}
	if args.Followup != nil {
		if key != "missing_recruitment_info" {
			return nil, argumentError("只有登记招聘待确认问题时可以附带一个独立沟通目标追问")
		}
		question := ReplyMessage{Type: "confirmation", Content: strings.TrimSpace(args.Followup.Content), PhraseID: args.Followup.PhraseID, ConfirmationItemID: strings.TrimSpace(args.Followup.ConfirmationItemID)}
		if question, allowed := applyReplyScenePolicy(s.input, question); allowed {
			if err = s.validateGoalQuestion(question); err != nil {
				return nil, err
			}
			goal, _ := s.confirmationByID(question.ConfirmationItemID)
			question.GoalContentKey = cloud.AutoReplyGoalContentKey(goal)
			s.manualFollowup = &question
		}
	}
	s.handoffQuestions = qs
	s.knownAnswer = answer
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
		return call.WithArgumentParseDiagnostic(argumentError("参数 JSON 不正确：" + err.Error()))
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return argumentError("参数只能包含一个 JSON 对象")
		}
		return call.WithArgumentParseDiagnostic(argumentError("参数 JSON 不正确：" + err.Error()))
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
