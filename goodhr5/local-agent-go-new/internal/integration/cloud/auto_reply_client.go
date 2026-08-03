// Package cloud 本文件提供本地自动回复流程访问云端业务数据的强类型客户端。
package cloud

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const autoReplyAgentBasePath = "/api/auto-reply/agent"

// AutoReplySnapshot 读取岗位、公司、条件和会员权限的自动回复快照。
func (c *Client) AutoReplySnapshot(ctx context.Context, credentials AgentCredentials, positionID string) (AutoReplyPositionSnapshot, error) {
	if err := validateAutoReplyCredentials(credentials); err != nil {
		return AutoReplyPositionSnapshot{}, err
	}
	positionID = strings.TrimSpace(positionID)
	if positionID == "" {
		return AutoReplyPositionSnapshot{}, fmt.Errorf("岗位编号不能为空")
	}
	var result AutoReplyPositionSnapshot
	path := autoReplyAgentBasePath + "/positions/" + url.PathEscape(positionID) + "/snapshot"
	if err := c.doWithMachineID(ctx, http.MethodGet, path, credentials.Token, credentials.MachineID, nil, &result); err != nil {
		return AutoReplyPositionSnapshot{}, err
	}
	if result.Position.ID == "" || result.Config.PositionID == "" || result.CompanyProfile.ID == "" {
		return AutoReplyPositionSnapshot{}, fmt.Errorf("云端自动回复岗位快照不完整")
	}
	return result, nil
}

// AutoReplyStatus 读取岗位自动回复实时开关和当前会员权限。
func (c *Client) AutoReplyStatus(ctx context.Context, credentials AgentCredentials, positionID string) (AutoReplyPositionStatus, error) {
	if err := validateAutoReplyCredentials(credentials); err != nil {
		return AutoReplyPositionStatus{}, err
	}
	positionID = strings.TrimSpace(positionID)
	if positionID == "" {
		return AutoReplyPositionStatus{}, fmt.Errorf("岗位编号不能为空")
	}
	var result AutoReplyPositionStatus
	path := "/api/auto-reply/positions/" + url.PathEscape(positionID) + "/status"
	if err := c.doWithMachineID(ctx, http.MethodGet, path, credentials.Token, credentials.MachineID, nil, &result); err != nil {
		return AutoReplyPositionStatus{}, err
	}
	return result, nil
}

// AutoReplyCandidateState 按平台候选人编号优先、会话编号或手机号后备读取候选人状态。
func (c *Client) AutoReplyCandidateState(ctx context.Context, credentials AgentCredentials, lookup AutoReplyCandidateLookup) (AutoReplyCandidateState, error) {
	if err := validateAutoReplyCredentials(credentials); err != nil {
		return AutoReplyCandidateState{}, err
	}
	if strings.TrimSpace(lookup.PlatformID) == "" {
		return AutoReplyCandidateState{}, fmt.Errorf("招聘平台编号不能为空")
	}
	if strings.TrimSpace(lookup.PlatformCandidateID) == "" && strings.TrimSpace(lookup.PlatformThreadID) == "" && strings.TrimSpace(lookup.Phone) == "" {
		return AutoReplyCandidateState{}, fmt.Errorf("候选人编号、会话编号和手机号至少需要一个")
	}
	query := url.Values{}
	query.Set("platform_id", strings.TrimSpace(lookup.PlatformID))
	query.Set("platform_account_id", strings.TrimSpace(lookup.PlatformAccountID))
	query.Set("platform_candidate_id", strings.TrimSpace(lookup.PlatformCandidateID))
	query.Set("platform_thread_id", strings.TrimSpace(lookup.PlatformThreadID))
	query.Set("phone", strings.TrimSpace(lookup.Phone))
	var result AutoReplyCandidateState
	path := autoReplyAgentBasePath + "/candidate-state?" + query.Encode()
	if err := c.doWithMachineID(ctx, http.MethodGet, path, credentials.Token, credentials.MachineID, nil, &result); err != nil {
		return AutoReplyCandidateState{}, err
	}
	return result, nil
}

// SaveAutoReplyCandidate 保存经过本地清洗和校验的正式候选人简历。
func (c *Client) SaveAutoReplyCandidate(ctx context.Context, credentials AgentCredentials, candidate AutoReplyCandidateInput) (AutoReplyCandidateSaveResult, error) {
	if err := validateAutoReplyCredentials(credentials); err != nil {
		return AutoReplyCandidateSaveResult{}, err
	}
	if strings.TrimSpace(candidate.PositionID) == "" || strings.TrimSpace(candidate.PlatformID) == "" {
		return AutoReplyCandidateSaveResult{}, fmt.Errorf("正式简历缺少岗位或招聘平台")
	}
	if strings.TrimSpace(candidate.Phone) == "" {
		return AutoReplyCandidateSaveResult{}, fmt.Errorf("没有手机号时只能保存临时会话，暂时不能进入正式简历库")
	}
	var result AutoReplyCandidateSaveResult
	if err := c.doWithMachineID(ctx, http.MethodPost, autoReplyAgentBasePath+"/candidates", credentials.Token, credentials.MachineID, candidate, &result); err != nil {
		return AutoReplyCandidateSaveResult{}, err
	}
	if result.CandidateID == "" {
		return AutoReplyCandidateSaveResult{}, fmt.Errorf("云端没有确认正式候选人保存结果")
	}
	return result, nil
}

// SaveAutoReplyIdentity 保存一个可稳定识别的平台候选人身份。
func (c *Client) SaveAutoReplyIdentity(ctx context.Context, credentials AgentCredentials, identity CandidatePlatformIdentity) (CandidatePlatformIdentity, error) {
	if err := validateAutoReplyCredentials(credentials); err != nil {
		return CandidatePlatformIdentity{}, err
	}
	if strings.TrimSpace(identity.PlatformID) == "" || strings.TrimSpace(identity.PlatformCandidateID) == "" {
		return CandidatePlatformIdentity{}, fmt.Errorf("保存平台身份需要招聘平台和候选人编号")
	}
	var response struct {
		Identity CandidatePlatformIdentity `json:"identity"`
	}
	if err := c.doWithMachineID(ctx, http.MethodPost, autoReplyAgentBasePath+"/identities", credentials.Token, credentials.MachineID, identity, &response); err != nil {
		return CandidatePlatformIdentity{}, err
	}
	if response.Identity.ID == "" {
		return CandidatePlatformIdentity{}, fmt.Errorf("云端没有确认平台候选人身份保存结果")
	}
	return response.Identity, nil
}

// SaveAutoReplyConversation 保存可以先于正式简历存在的平台会话。
func (c *Client) SaveAutoReplyConversation(ctx context.Context, credentials AgentCredentials, conversation AutoReplyConversation) (AutoReplyConversation, error) {
	if err := validateAutoReplyCredentials(credentials); err != nil {
		return AutoReplyConversation{}, err
	}
	if strings.TrimSpace(conversation.PlatformID) == "" || strings.TrimSpace(conversation.PlatformThreadID) == "" {
		return AutoReplyConversation{}, fmt.Errorf("保存会话需要招聘平台和平台会话编号")
	}
	var response struct {
		Conversation AutoReplyConversation `json:"conversation"`
	}
	if err := c.doWithMachineID(ctx, http.MethodPost, autoReplyAgentBasePath+"/conversations", credentials.Token, credentials.MachineID, conversation, &response); err != nil {
		return AutoReplyConversation{}, err
	}
	if response.Conversation.ID == "" {
		return AutoReplyConversation{}, fmt.Errorf("云端没有确认候选人会话保存结果")
	}
	return response.Conversation, nil
}

// SyncAutoReplyMessages 幂等同步首次聊天记录或后续差量消息。
func (c *Client) SyncAutoReplyMessages(ctx context.Context, credentials AgentCredentials, request AutoReplyMessageSyncRequest) (AutoReplyMessageSyncResult, error) {
	if err := validateAutoReplyCredentials(credentials); err != nil {
		return AutoReplyMessageSyncResult{}, err
	}
	if strings.TrimSpace(request.ConversationID) == "" {
		return AutoReplyMessageSyncResult{}, fmt.Errorf("会话编号不能为空")
	}
	if len(request.Messages) > AutoReplyMaxHistoryMessages {
		return AutoReplyMessageSyncResult{}, fmt.Errorf("首次聊天记录最多同步%d条", AutoReplyMaxHistoryMessages)
	}
	var response struct {
		Sync AutoReplyMessageSyncResult `json:"sync"`
	}
	if err := c.doWithMachineID(ctx, http.MethodPost, autoReplyAgentBasePath+"/messages/sync", credentials.Token, credentials.MachineID, request, &response); err != nil {
		return AutoReplyMessageSyncResult{}, err
	}
	return response.Sync, nil
}

// AutoReplyMessages 读取当前会话最多5000条标准化聊天记录。
func (c *Client) AutoReplyMessages(ctx context.Context, credentials AgentCredentials, conversationID string) ([]AutoReplyMessage, error) {
	if err := validateAutoReplyCredentials(credentials); err != nil {
		return nil, err
	}
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return nil, fmt.Errorf("会话编号不能为空")
	}
	var response struct {
		Messages []AutoReplyMessage `json:"messages"`
	}
	path := autoReplyAgentBasePath + "/messages?conversation_id=" + url.QueryEscape(conversationID)
	if err := c.doWithMachineID(ctx, http.MethodGet, path, credentials.Token, credentials.MachineID, nil, &response); err != nil {
		return nil, err
	}
	return response.Messages, nil
}

// AutoReplyConfirmationItems 读取当前候选人和岗位之间的结构化确认项。
func (c *Client) AutoReplyConfirmationItems(ctx context.Context, credentials AgentCredentials, conversationID string) ([]CandidateConfirmationItem, error) {
	if err := validateAutoReplyCredentials(credentials); err != nil {
		return nil, err
	}
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return nil, fmt.Errorf("会话编号不能为空")
	}
	var response struct {
		Items []CandidateConfirmationItem `json:"confirmation_items"`
	}
	path := autoReplyAgentBasePath + "/confirmations?conversation_id=" + url.QueryEscape(conversationID)
	if err := c.doWithMachineID(ctx, http.MethodGet, path, credentials.Token, credentials.MachineID, nil, &response); err != nil {
		return nil, err
	}
	return response.Items, nil
}

// SaveAutoReplyConfirmationItem 新增或更新一条候选人确认项。
func (c *Client) SaveAutoReplyConfirmationItem(ctx context.Context, credentials AgentCredentials, item CandidateConfirmationItem) (CandidateConfirmationItem, error) {
	if err := validateAutoReplyCredentials(credentials); err != nil {
		return CandidateConfirmationItem{}, err
	}
	if strings.TrimSpace(item.ConversationID) == "" || strings.TrimSpace(item.Content) == "" {
		return CandidateConfirmationItem{}, fmt.Errorf("候选人确认项缺少会话或内容")
	}
	var response struct {
		Item CandidateConfirmationItem `json:"confirmation_item"`
	}
	if err := c.doWithMachineID(ctx, http.MethodPut, autoReplyAgentBasePath+"/confirmations", credentials.Token, credentials.MachineID, item, &response); err != nil {
		return CandidateConfirmationItem{}, err
	}
	if response.Item.ID == "" {
		return CandidateConfirmationItem{}, fmt.Errorf("云端没有确认候选人确认项保存结果")
	}
	return response.Item, nil
}

// AutoReplyAttachments 读取正式候选人或临时会话已有的简历附件元数据。
func (c *Client) AutoReplyAttachments(ctx context.Context, credentials AgentCredentials, candidateID string, conversationID string) ([]StoredResumeAttachment, error) {
	if err := validateAutoReplyCredentials(credentials); err != nil {
		return nil, err
	}
	if strings.TrimSpace(candidateID) == "" && strings.TrimSpace(conversationID) == "" {
		return nil, fmt.Errorf("读取简历附件需要候选人或会话编号")
	}
	query := url.Values{}
	query.Set("candidate_id", strings.TrimSpace(candidateID))
	query.Set("conversation_id", strings.TrimSpace(conversationID))
	var response struct {
		Attachments []StoredResumeAttachment `json:"attachments"`
	}
	path := autoReplyAgentBasePath + "/attachments?" + query.Encode()
	if err := c.doWithMachineID(ctx, http.MethodGet, path, credentials.Token, credentials.MachineID, nil, &response); err != nil {
		return nil, err
	}
	return response.Attachments, nil
}

// StartAutoReplyAIRun 创建一条供运行小窗和排障查看的 AI 总记录。
func (c *Client) StartAutoReplyAIRun(ctx context.Context, credentials AgentCredentials, run AutoReplyAIRun) (AutoReplyAIRun, error) {
	return c.saveAutoReplyAIRun(ctx, credentials, "/ai-runs/start", run)
}

// FinishAutoReplyAIRun 保存 AI 最终返回、错误和 Token 使用量。
func (c *Client) FinishAutoReplyAIRun(ctx context.Context, credentials AgentCredentials, run AutoReplyAIRun) (AutoReplyAIRun, error) {
	return c.saveAutoReplyAIRun(ctx, credentials, "/ai-runs/finish", run)
}

// SaveAutoReplyToolCall 幂等保存一次 AI 工具调用及其执行结果。
func (c *Client) SaveAutoReplyToolCall(ctx context.Context, credentials AgentCredentials, call AutoReplyToolCall) (AutoReplyToolCall, error) {
	if err := validateAutoReplyCredentials(credentials); err != nil {
		return AutoReplyToolCall{}, err
	}
	if strings.TrimSpace(call.AIRunID) == "" || strings.TrimSpace(call.ToolCallID) == "" || strings.TrimSpace(call.ToolName) == "" {
		return AutoReplyToolCall{}, fmt.Errorf("AI 工具记录缺少运行编号、调用编号或工具名称")
	}
	var response struct {
		ToolCall AutoReplyToolCall `json:"tool_call"`
	}
	if err := c.doWithMachineID(ctx, http.MethodPost, autoReplyAgentBasePath+"/tool-calls", credentials.Token, credentials.MachineID, call, &response); err != nil {
		return AutoReplyToolCall{}, err
	}
	if response.ToolCall.ID == "" {
		return AutoReplyToolCall{}, fmt.Errorf("云端没有确认 AI 工具记录保存结果")
	}
	return response.ToolCall, nil
}

// SaveAutoReplySuggestion 保存 AI 学到但必须由 HR 审核的岗位或公司资料建议。
func (c *Client) SaveAutoReplySuggestion(ctx context.Context, credentials AgentCredentials, suggestion AutoReplyConfigSuggestion) (AutoReplyConfigSuggestion, error) {
	if err := validateAutoReplyCredentials(credentials); err != nil {
		return AutoReplyConfigSuggestion{}, err
	}
	if strings.TrimSpace(suggestion.SuggestionType) == "" || strings.TrimSpace(suggestion.Operation) == "" {
		return AutoReplyConfigSuggestion{}, fmt.Errorf("配置建议缺少类型或操作")
	}
	var response struct {
		Suggestion AutoReplyConfigSuggestion `json:"suggestion"`
	}
	if err := c.doWithMachineID(ctx, http.MethodPost, autoReplyAgentBasePath+"/suggestions", credentials.Token, credentials.MachineID, suggestion, &response); err != nil {
		return AutoReplyConfigSuggestion{}, err
	}
	if response.Suggestion.ID == "" {
		return AutoReplyConfigSuggestion{}, fmt.Errorf("云端没有确认配置建议保存结果")
	}
	return response.Suggestion, nil
}

// NotifyAutoReplyManualHandoff 幂等通知 HR 人工接管，邮件失败只通过返回值提示。
func (c *Client) NotifyAutoReplyManualHandoff(ctx context.Context, credentials AgentCredentials, notification AutoReplyNotification) (AutoReplyNotificationResult, error) {
	if err := validateAutoReplyCredentials(credentials); err != nil {
		return AutoReplyNotificationResult{}, err
	}
	if strings.TrimSpace(notification.BasedOnMessageKey) == "" || strings.TrimSpace(notification.ReasonKey) == "" || strings.TrimSpace(notification.Reason) == "" {
		return AutoReplyNotificationResult{}, fmt.Errorf("人工接管通知缺少消息、原因编号或原因")
	}
	var result AutoReplyNotificationResult
	if err := c.doWithMachineID(ctx, http.MethodPost, autoReplyAgentBasePath+"/notifications", credentials.Token, credentials.MachineID, notification, &result); err != nil {
		return AutoReplyNotificationResult{}, err
	}
	return result, nil
}

// saveAutoReplyAIRun 复用 AI 总记录创建和完成接口的相同协议。
func (c *Client) saveAutoReplyAIRun(ctx context.Context, credentials AgentCredentials, path string, run AutoReplyAIRun) (AutoReplyAIRun, error) {
	if err := validateAutoReplyCredentials(credentials); err != nil {
		return AutoReplyAIRun{}, err
	}
	if strings.TrimSpace(run.ConversationID) == "" || strings.TrimSpace(run.TraceID) == "" || strings.TrimSpace(run.BasedOnMessageKey) == "" {
		return AutoReplyAIRun{}, fmt.Errorf("AI 总记录缺少会话、追踪编号或消息编号")
	}
	var response struct {
		Run AutoReplyAIRun `json:"ai_run"`
	}
	if err := c.doWithMachineID(ctx, http.MethodPost, autoReplyAgentBasePath+path, credentials.Token, credentials.MachineID, run, &response); err != nil {
		return AutoReplyAIRun{}, err
	}
	if response.Run.ID == "" {
		return AutoReplyAIRun{}, fmt.Errorf("云端没有确认 AI 总记录保存结果")
	}
	return response.Run, nil
}

// validateAutoReplyCredentials 校验自动回复敏感接口所需的登录凭证和稳定设备编号。
func validateAutoReplyCredentials(credentials AgentCredentials) error {
	if strings.TrimSpace(credentials.Token) == "" {
		return fmt.Errorf("登录凭证不能为空")
	}
	if strings.TrimSpace(credentials.MachineID) == "" {
		return fmt.Errorf("设备编号不能为空")
	}
	return nil
}
