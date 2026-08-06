// Package httpapi 本文件负责把候选人的简历附件、沟通记录、确认项和 AI 记录聚合给简历详情页。
package httpapi

import (
	"context"
	"encoding/json"
)

// loadCandidateAutoReplyDetail 读取候选人全部自动回复关联资料并返回前端安全字段。
func (s *CandidateService) loadCandidateAutoReplyDetail(ctx context.Context, tenantID, candidateID string) (map[string]any, error) {
	attachments, err := s.autoReply.ListResumeAttachments(ctx, tenantID, candidateID, "")
	if err != nil {
		return nil, err
	}
	conversations, err := s.autoReply.ListCandidateAutoReplyConversations(ctx, tenantID, candidateID)
	if err != nil {
		return nil, err
	}
	conversationDetails := make([]map[string]any, 0, len(conversations))
	for _, conversation := range conversations {
		messages, messageErr := s.autoReply.ListAutoReplyMessages(ctx, tenantID, conversation.ID, 5000)
		if messageErr != nil {
			return nil, messageErr
		}
		confirmations, confirmationErr := s.autoReply.ListConfirmationItems(ctx, tenantID, conversation.ID)
		if confirmationErr != nil {
			return nil, confirmationErr
		}
		conversationDetails = append(conversationDetails, publicCandidateConversationDetail(conversation, messages, confirmations))
	}
	audit, err := s.autoReply.ListCandidateAutoReplyAudit(ctx, tenantID, candidateID, 100)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"attachments":   publicCandidateAttachments(attachments),
		"conversations": conversationDetails,
		"ai_records":    publicCandidateAIRecords(audit),
	}, nil
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
