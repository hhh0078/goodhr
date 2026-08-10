// Package httpapi 本文件负责按需读取候选人的简历附件、沟通记录、确认项和 AI 记录。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// AutoReplyDetail 校验简历查看权限后，只读取前端本次打开的一个关联资料区块。
func (s *CandidateService) AutoReplyDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	candidateID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/candidates/"), "/auto-reply")
	if candidateID == "" || candidateID == r.URL.Path {
		writeError(w, http.StatusBadRequest, "candidate id is required")
		return
	}
	tenant, err := s.tenantStore.GetOrCreateTenant(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get tenant")
		return
	}
	isAdmin, _ := s.tenantStore.IsTenantAdmin(tenant.ID, session.Email)
	if _, err = s.store.GetPositionCandidate(tenant.ID, candidateID, strings.TrimSpace(r.URL.Query().Get("engagement_id")), session.Email, isAdmin); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "candidate not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load candidate")
		return
	}
	section := strings.TrimSpace(r.URL.Query().Get("section"))
	validSections := map[string]struct{}{"attachments": {}, "conversations": {}, "confirmations": {}, "ai_records": {}}
	if _, valid := validSections[section]; !valid {
		writeError(w, http.StatusBadRequest, errCandidateAutoReplySection.Error())
		return
	}
	if s.autoReply == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "auto_reply": emptyCandidateAutoReplySection(section)})
		return
	}
	detail, err := s.loadCandidateAutoReplySection(r.Context(), tenant.ID, candidateID, section)
	if err != nil {
		if errors.Is(err, errCandidateAutoReplySection) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeAutoReplyInternalError(w, "CANDIDATE_ACTIVITY_LOAD_FAILED", "候选人的关联资料暂时没读出来，请稍后再试", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "auto_reply": detail})
}

var errCandidateAutoReplySection = errors.New("关联资料类型不正确")

// loadCandidateAutoReplySection 按区块读取数据，避免打开简历详情时一次加载全部历史。
func (s *CandidateService) loadCandidateAutoReplySection(ctx context.Context, tenantID, candidateID, section string) (map[string]any, error) {
	switch section {
	case "attachments":
		items, err := s.autoReply.ListResumeAttachments(ctx, tenantID, candidateID, "")
		return map[string]any{"attachments": publicCandidateAttachments(items)}, err
	case "conversations":
		conversations, err := s.autoReply.ListCandidateAutoReplyConversations(ctx, tenantID, candidateID)
		if err != nil {
			return nil, err
		}
		result := make([]map[string]any, 0, len(conversations))
		for _, conversation := range conversations {
			messages, messageErr := s.autoReply.ListAutoReplyMessages(ctx, tenantID, conversation.ID, 5000)
			if messageErr != nil {
				return nil, messageErr
			}
			result = append(result, publicCandidateConversationDetail(conversation, messages, nil))
		}
		return map[string]any{"conversations": result}, nil
	case "confirmations":
		conversations, err := s.autoReply.ListCandidateAutoReplyConversations(ctx, tenantID, candidateID)
		if err != nil {
			return nil, err
		}
		result := make([]CandidateConfirmationItem, 0)
		for _, conversation := range conversations {
			items, confirmationErr := s.autoReply.ListConfirmationItems(ctx, tenantID, conversation.ID)
			if confirmationErr != nil {
				return nil, confirmationErr
			}
			result = append(result, items...)
		}
		return map[string]any{"confirmation_items": publicCandidateConfirmationItems(result)}, nil
	case "ai_records":
		items, err := s.autoReply.ListCandidateAutoReplyAudit(ctx, tenantID, candidateID, 100)
		return map[string]any{"ai_records": publicCandidateAIRecords(items)}, err
	default:
		return nil, errCandidateAutoReplySection
	}
}

// emptyCandidateAutoReplySection 在自动回复存储未启用时返回对应区块的空数组。
func emptyCandidateAutoReplySection(section string) map[string]any {
	keys := map[string]string{
		"attachments": "attachments", "conversations": "conversations",
		"confirmations": "confirmation_items", "ai_records": "ai_records",
	}
	if key := keys[section]; key != "" {
		return map[string]any{key: []any{}}
	}
	return map[string]any{}
}

// publicCandidateAttachments 隐藏云端文件系统路径，只返回受保护下载地址和展示元数据。
func publicCandidateAttachments(items []StoredResumeAttachment) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, map[string]any{
			"id": item.ID, "original_name": item.OriginalName, "mime_type": item.MIMEType,
			"size_bytes": item.SizeBytes, "created_at": item.CreatedAt,
			"download_url": "/api/auto-reply/attachments/" + item.ID,
		})
	}
	return result
}

// publicCandidateConversationDetail 返回一段会话、聊天消息和确认项的组合数据。
func publicCandidateConversationDetail(conversation AutoReplyConversation, messages []AutoReplyMessage, confirmations []CandidateConfirmationItem) map[string]any {
	return map[string]any{
		"id": conversation.ID, "position_id": conversation.PositionID,
		"platform_id": conversation.PlatformID, "candidate_name": conversation.CandidateName,
		"gender": conversation.Gender, "page_position_text": conversation.PagePositionText,
		"status": conversation.Status, "history_complete": conversation.HistoryComplete,
		"created_at": conversation.CreatedAt, "updated_at": conversation.UpdatedAt,
		"messages":           publicCandidateMessages(messages),
		"confirmation_items": publicCandidateConfirmationItems(confirmations),
	}
}

// publicCandidateMessages 返回详情页需要的消息方向、类型、正文和时间。
func publicCandidateMessages(items []AutoReplyMessage) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		card := json.RawMessage(item.CardContent)
		if len(card) == 0 {
			card = json.RawMessage(`{}`)
		}
		result = append(result, map[string]any{
			"id": item.ID, "direction": item.Direction, "message_type": item.MessageType,
			"text_content": item.TextContent, "card_content": card, "sender_name": item.SenderName,
			"platform_sent_at": item.PlatformSentAt, "created_at": item.CreatedAt,
		})
	}
	return result
}

// publicCandidateConfirmationItems 返回可审计的确认项，不暴露隐藏思考过程。
func publicCandidateConfirmationItems(items []CandidateConfirmationItem) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, map[string]any{
			"id": item.ID, "item_type": item.ItemType, "content": item.Content,
			"status": item.Status, "source_type": item.SourceType, "source_ref": item.SourceRef,
			"evidence_text": item.EvidenceText, "summary": item.Summary,
			"created_by_kind": item.CreatedByKind, "created_at": item.CreatedAt, "updated_at": item.UpdatedAt,
		})
	}
	return result
}

// publicCandidateAIRecords 返回候选人 AI 输入、结果、错误和工具调用审计。
func publicCandidateAIRecords(items []AutoReplyAuditRecord) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, map[string]any{
			"id": item.Run.ID, "conversation_id": item.Run.ConversationID,
			"position_id": item.Run.PositionID, "position_name": item.PositionName,
			"platform_id": item.PlatformID, "trace_id": item.Run.TraceID,
			"model": item.Run.Model, "status": item.Run.Status,
			"input_messages": item.Run.InputMessages, "output_message": item.Run.OutputMessage,
			"error_code": item.Run.ErrorCode, "error_message": item.Run.ErrorMessage,
			"token_usage": item.Run.TokenUsage, "started_at": item.Run.StartedAt,
			"completed_at": item.Run.CompletedAt, "tool_calls": publicCandidateToolCalls(item.ToolCalls),
		})
	}
	return result
}

// publicCandidateToolCalls 返回一次 AI 运行中的工具名称、参数、结果和错误。
func publicCandidateToolCalls(items []AutoReplyToolCall) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, map[string]any{
			"id": item.ID, "sequence_no": item.SequenceNo, "tool_name": item.ToolName,
			"arguments_json": item.ArgumentsJSON, "result_json": item.ResultJSON,
			"status": item.Status, "error_code": item.ErrorCode, "error_message": item.ErrorMessage,
			"started_at": item.StartedAt, "completed_at": item.CompletedAt,
		})
	}
	return result
}
