// Package httpapi 本文件负责读取自动回复 AI 总审计、工具记录和待审核配置建议。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// GetAutoReplyCandidateProfile 返回团队内自动回复需要的完整正式简历，不按创建成员隔离。
func (s *PostgresAutoReplyStore) GetAutoReplyCandidateProfile(ctx context.Context, tenantID, candidateID string) (PositionCandidate, error) {
	item, err := scanCandidateRow(s.db.QueryRowContext(ctx, candidateSelectSQL(
		"WHERE cp.tenant_id=$1 AND cp.id::text=$2", "",
	), tenantID, strings.TrimSpace(candidateID)))
	if errors.Is(err, sql.ErrNoRows) {
		return PositionCandidate{}, ErrNotFound
	}
	return item, err
}

// ListAutoReplyAudit 返回当前团队最近的 AI 总记录及其工具调用。
func (s *PostgresAutoReplyStore) ListAutoReplyAudit(ctx context.Context, tenantID, positionID string, limit int) ([]AutoReplyAuditRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT ar.id, ar.tenant_id, ar.conversation_id, COALESCE(ar.candidate_id::text,''),
			COALESCE(ar.position_id::text,''), ar.trace_id, ar.model, ar.status,
			ar.based_on_message_key, ar.input_messages, ar.output_message, ar.error_code,
			ar.error_message, ar.token_usage, ar.started_at, ar.completed_at, ar.expires_at,
			ar.created_at, cc.candidate_name, cc.gender, cc.platform_id, COALESCE(p.name,'')
		FROM auto_reply_ai_runs ar
		JOIN candidate_conversations cc ON cc.id=ar.conversation_id
		LEFT JOIN positions p ON p.id=ar.position_id
		WHERE ar.tenant_id=$1 AND ($2='' OR ar.position_id=NULLIF($2,'')::uuid)
		ORDER BY ar.created_at DESC, ar.id DESC
		LIMIT $3
	`, tenantID, strings.TrimSpace(positionID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AutoReplyAuditRecord, 0)
	for rows.Next() {
		var item AutoReplyAuditRecord
		if err = rows.Scan(
			&item.Run.ID, &item.Run.TenantID, &item.Run.ConversationID, &item.Run.CandidateID,
			&item.Run.PositionID, &item.Run.TraceID, &item.Run.Model, &item.Run.Status,
			&item.Run.BasedOnMessageKey, &item.Run.InputMessages, &item.Run.OutputMessage,
			&item.Run.ErrorCode, &item.Run.ErrorMessage, &item.Run.TokenUsage,
			&item.Run.StartedAt, &item.Run.CompletedAt, &item.Run.ExpiresAt, &item.Run.CreatedAt,
			&item.CandidateName, &item.Gender, &item.PlatformID, &item.PositionName,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for index := range items {
		items[index].ToolCalls, err = s.listAutoReplyToolCalls(ctx, tenantID, items[index].Run.ID)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

// listAutoReplyToolCalls 返回一次 AI 运行按执行顺序排列的工具记录。
func (s *PostgresAutoReplyStore) listAutoReplyToolCalls(ctx context.Context, tenantID, aiRunID string) ([]AutoReplyToolCall, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, ai_run_id, tool_call_id, sequence_no, tool_name,
			arguments_json, result_json, status, error_code, error_message,
			started_at, completed_at, created_at
		FROM auto_reply_tool_calls
		WHERE tenant_id=$1 AND ai_run_id=$2
		ORDER BY sequence_no, id
	`, tenantID, aiRunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AutoReplyToolCall, 0)
	for rows.Next() {
		var item AutoReplyToolCall
		if err = rows.Scan(
			&item.ID, &item.TenantID, &item.AIRunID, &item.ToolCallID, &item.SequenceNo,
			&item.ToolName, &item.ArgumentsJSON, &item.ResultJSON, &item.Status,
			&item.ErrorCode, &item.ErrorMessage, &item.StartedAt, &item.CompletedAt, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ListAutoReplyConfigSuggestions 返回当前团队待审核或最近处理的配置建议。
func (s *PostgresAutoReplyStore) ListAutoReplyConfigSuggestions(ctx context.Context, tenantID, status string, limit int) ([]AutoReplyConfigSuggestion, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	status = strings.TrimSpace(status)
	if status != "" && status != "pending" && status != "approved" && status != "rejected" {
		return nil, newAutoReplyValidationError("配置建议状态不支持")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, COALESCE(conversation_id::text,''), COALESCE(position_id::text,''),
			COALESCE(company_profile_id::text,''), suggestion_type, operation, target_id,
			proposed_value, reason, status, COALESCE(reviewed_by_user_id::text,''),
			reviewed_at, created_at, updated_at
		FROM auto_reply_config_suggestions
		WHERE tenant_id=$1 AND ($2='' OR status=$2)
		ORDER BY created_at DESC, id DESC
		LIMIT $3
	`, tenantID, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AutoReplyConfigSuggestion, 0)
	for rows.Next() {
		var item AutoReplyConfigSuggestion
		if err = rows.Scan(
			&item.ID, &item.TenantID, &item.ConversationID, &item.PositionID,
			&item.CompanyProfileID, &item.SuggestionType, &item.Operation, &item.TargetID,
			&item.ProposedValue, &item.Reason, &item.Status, &item.ReviewedByUserID,
			&item.ReviewedAt, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ReviewAutoReplyConfigSuggestion 保存 HR 对配置建议的审核结果，不直接修改岗位或公司资料。
func (s *PostgresAutoReplyStore) ReviewAutoReplyConfigSuggestion(ctx context.Context, tenantID, userEmail, suggestionID, status string) (AutoReplyConfigSuggestion, error) {
	if status != "approved" && status != "rejected" {
		return AutoReplyConfigSuggestion{}, newAutoReplyValidationError("配置建议只能通过或拒绝")
	}
	userID, err := s.activeTenantUserID(ctx, tenantID, userEmail)
	if err != nil {
		return AutoReplyConfigSuggestion{}, err
	}
	var item AutoReplyConfigSuggestion
	err = s.db.QueryRowContext(ctx, `
		UPDATE auto_reply_config_suggestions
		SET status=$4, reviewed_by_user_id=$3, reviewed_at=now(), updated_at=now()
		WHERE tenant_id=$1 AND id=$2 AND status='pending'
		RETURNING id, tenant_id, COALESCE(conversation_id::text,''), COALESCE(position_id::text,''),
			COALESCE(company_profile_id::text,''), suggestion_type, operation, target_id,
			proposed_value, reason, status, COALESCE(reviewed_by_user_id::text,''),
			reviewed_at, created_at, updated_at
	`, tenantID, suggestionID, userID, status).Scan(
		&item.ID, &item.TenantID, &item.ConversationID, &item.PositionID,
		&item.CompanyProfileID, &item.SuggestionType, &item.Operation, &item.TargetID,
		&item.ProposedValue, &item.Reason, &item.Status, &item.ReviewedByUserID,
		&item.ReviewedAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AutoReplyConfigSuggestion{}, ErrNotFound
	}
	return item, err
}
