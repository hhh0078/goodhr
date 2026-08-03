// Package httpapi 本文件负责自动回复 AI 总记录、工具调用、配置建议和180天清理的 PostgreSQL 存储。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// StartAutoReplyAIRun 创建一条正在运行的 AI 总审计记录。
func (s *PostgresAutoReplyStore) StartAutoReplyAIRun(ctx context.Context, item AutoReplyAIRun) (AutoReplyAIRun, error) {
	if strings.TrimSpace(item.TenantID) == "" || strings.TrimSpace(item.ConversationID) == "" || strings.TrimSpace(item.TraceID) == "" {
		return AutoReplyAIRun{}, newAutoReplyValidationError("AI总记录缺少团队、会话或追踪编号")
	}
	if strings.TrimSpace(item.BasedOnMessageKey) == "" {
		return AutoReplyAIRun{}, newAutoReplyValidationError("AI总记录缺少依据消息")
	}
	if err := validateJSONDocument(item.InputMessages, false); err != nil {
		return AutoReplyAIRun{}, err
	}
	for kind, id := range map[string]string{
		"conversation": item.ConversationID, "candidate": item.CandidateID, "position": item.PositionID,
	} {
		if err := s.ensureAutoReplyReference(ctx, item.TenantID, kind, id); err != nil {
			return AutoReplyAIRun{}, err
		}
	}
	item.Status = "running"
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO auto_reply_ai_runs (
			tenant_id, conversation_id, candidate_id, position_id, trace_id,
			model, status, based_on_message_key, input_messages
		) VALUES ($1,$2,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,$5,$6,'running',$7,$8::jsonb)
		ON CONFLICT (tenant_id, trace_id) DO UPDATE SET
			model=EXCLUDED.model,
			based_on_message_key=EXCLUDED.based_on_message_key,
			input_messages=EXCLUDED.input_messages
		RETURNING id, tenant_id, conversation_id, COALESCE(candidate_id::text,''),
			COALESCE(position_id::text,''), trace_id, model, status, based_on_message_key,
			input_messages, output_message, error_code, error_message, token_usage,
			started_at, completed_at, expires_at, created_at
	`, item.TenantID, item.ConversationID, item.CandidateID, item.PositionID,
		strings.TrimSpace(item.TraceID), strings.TrimSpace(item.Model),
		strings.TrimSpace(item.BasedOnMessageKey), string(item.InputMessages)).Scan(
		&item.ID, &item.TenantID, &item.ConversationID, &item.CandidateID,
		&item.PositionID, &item.TraceID, &item.Model, &item.Status,
		&item.BasedOnMessageKey, &item.InputMessages, &item.OutputMessage,
		&item.ErrorCode, &item.ErrorMessage, &item.TokenUsage,
		&item.StartedAt, &item.CompletedAt, &item.ExpiresAt, &item.CreatedAt,
	)
	return item, err
}

// FinishAutoReplyAIRun 完成 AI 总审计记录并保存完整返回或安全错误。
func (s *PostgresAutoReplyStore) FinishAutoReplyAIRun(ctx context.Context, item AutoReplyAIRun) (AutoReplyAIRun, error) {
	if item.Status != "completed" && item.Status != "failed" && item.Status != "notified" {
		return AutoReplyAIRun{}, newAutoReplyValidationError("AI总记录结束状态不支持")
	}
	if item.TokenUsage < 0 {
		return AutoReplyAIRun{}, newAutoReplyValidationError("AI Token使用量不能小于0")
	}
	output := item.OutputMessage
	if len(strings.TrimSpace(string(output))) == 0 {
		output = json.RawMessage(`{}`)
	}
	if err := validateJSONDocument(output, false); err != nil {
		return AutoReplyAIRun{}, err
	}
	err := s.db.QueryRowContext(ctx, `
		UPDATE auto_reply_ai_runs
		SET status=$3, output_message=$4::jsonb, error_code=$5,
			error_message=$6, token_usage=$7, completed_at=now()
		WHERE tenant_id=$1 AND id=$2
		RETURNING id, tenant_id, conversation_id, COALESCE(candidate_id::text,''),
			COALESCE(position_id::text,''), trace_id, model, status, based_on_message_key,
			input_messages, output_message, error_code, error_message, token_usage,
			started_at, completed_at, expires_at, created_at
	`, item.TenantID, item.ID, item.Status, string(output), strings.TrimSpace(item.ErrorCode),
		strings.TrimSpace(item.ErrorMessage), item.TokenUsage).Scan(
		&item.ID, &item.TenantID, &item.ConversationID, &item.CandidateID,
		&item.PositionID, &item.TraceID, &item.Model, &item.Status,
		&item.BasedOnMessageKey, &item.InputMessages, &item.OutputMessage,
		&item.ErrorCode, &item.ErrorMessage, &item.TokenUsage,
		&item.StartedAt, &item.CompletedAt, &item.ExpiresAt, &item.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AutoReplyAIRun{}, ErrNotFound
	}
	return item, err
}

// SaveAutoReplyToolCall 新增或更新一次 AI 工具调用审计。
func (s *PostgresAutoReplyStore) SaveAutoReplyToolCall(ctx context.Context, item AutoReplyToolCall) (AutoReplyToolCall, error) {
	if strings.TrimSpace(item.TenantID) == "" || strings.TrimSpace(item.AIRunID) == "" || strings.TrimSpace(item.ToolCallID) == "" {
		return AutoReplyToolCall{}, newAutoReplyValidationError("AI工具记录缺少团队、AI运行或调用编号")
	}
	if item.SequenceNo < 1 || item.SequenceNo > 8 {
		return AutoReplyToolCall{}, newAutoReplyValidationError("单条候选人消息最多调用8次工具")
	}
	if strings.TrimSpace(item.ToolName) == "" {
		return AutoReplyToolCall{}, newAutoReplyValidationError("AI工具名称不能为空")
	}
	if item.Status != "running" && item.Status != "completed" && item.Status != "failed" {
		return AutoReplyToolCall{}, newAutoReplyValidationError("AI工具状态不支持")
	}
	arguments := nonEmptyJSON(item.ArgumentsJSON)
	result := nonEmptyJSON(item.ResultJSON)
	if err := validateJSONDocument(arguments, false); err != nil {
		return AutoReplyToolCall{}, err
	}
	if err := validateJSONDocument(result, false); err != nil {
		return AutoReplyToolCall{}, err
	}
	var runTenantID string
	if err := s.db.QueryRowContext(ctx, `SELECT tenant_id FROM auto_reply_ai_runs WHERE id=$1`, item.AIRunID).Scan(&runTenantID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AutoReplyToolCall{}, ErrNotFound
		}
		return AutoReplyToolCall{}, err
	}
	if runTenantID != item.TenantID {
		return AutoReplyToolCall{}, ErrAutoReplyForbidden
	}
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO auto_reply_tool_calls (
			tenant_id, ai_run_id, tool_call_id, sequence_no, tool_name,
			arguments_json, result_json, status, error_code, error_message, completed_at
		) VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8,$9,$10,
			CASE WHEN $8='running' THEN NULL ELSE now() END)
		ON CONFLICT (ai_run_id, tool_call_id) DO UPDATE SET
			result_json=EXCLUDED.result_json,
			status=EXCLUDED.status,
			error_code=EXCLUDED.error_code,
			error_message=EXCLUDED.error_message,
			completed_at=EXCLUDED.completed_at
		RETURNING id, tenant_id, ai_run_id, tool_call_id, sequence_no, tool_name,
			arguments_json, result_json, status, error_code, error_message,
			started_at, completed_at, created_at
	`, item.TenantID, item.AIRunID, strings.TrimSpace(item.ToolCallID), item.SequenceNo,
		strings.TrimSpace(item.ToolName), string(arguments), string(result), item.Status,
		strings.TrimSpace(item.ErrorCode), strings.TrimSpace(item.ErrorMessage)).Scan(
		&item.ID, &item.TenantID, &item.AIRunID, &item.ToolCallID, &item.SequenceNo,
		&item.ToolName, &item.ArgumentsJSON, &item.ResultJSON, &item.Status,
		&item.ErrorCode, &item.ErrorMessage, &item.StartedAt, &item.CompletedAt, &item.CreatedAt,
	)
	return item, err
}

// SaveAutoReplyConfigSuggestion 保存 AI 提交、等待 HR 审核的配置修改建议。
func (s *PostgresAutoReplyStore) SaveAutoReplyConfigSuggestion(ctx context.Context, item AutoReplyConfigSuggestion) (AutoReplyConfigSuggestion, error) {
	if item.SuggestionType != "position" && item.SuggestionType != "company" {
		return AutoReplyConfigSuggestion{}, newAutoReplyValidationError("配置建议类型不支持")
	}
	if item.Operation != "create" && item.Operation != "update" && item.Operation != "delete" {
		return AutoReplyConfigSuggestion{}, newAutoReplyValidationError("配置建议操作不支持")
	}
	if strings.TrimSpace(item.Reason) == "" {
		return AutoReplyConfigSuggestion{}, newAutoReplyValidationError("配置建议需要说明原因")
	}
	if strings.TrimSpace(item.PositionID) == "" && strings.TrimSpace(item.CompanyProfileID) == "" {
		return AutoReplyConfigSuggestion{}, newAutoReplyValidationError("配置建议需要关联岗位或公司档案")
	}
	proposed := nonEmptyJSON(item.ProposedValue)
	if err := validateJSONDocument(proposed, false); err != nil {
		return AutoReplyConfigSuggestion{}, err
	}
	for kind, id := range map[string]string{
		"conversation": item.ConversationID, "position": item.PositionID,
	} {
		if err := s.ensureAutoReplyReference(ctx, item.TenantID, kind, id); err != nil {
			return AutoReplyConfigSuggestion{}, err
		}
	}
	if item.CompanyProfileID != "" {
		var exists bool
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM tenant_company_profiles WHERE tenant_id=$1 AND id=$2)`, item.TenantID, item.CompanyProfileID).Scan(&exists); err != nil {
			return AutoReplyConfigSuggestion{}, err
		}
		if !exists {
			return AutoReplyConfigSuggestion{}, ErrNotFound
		}
	}
	item.Status = "pending"
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO auto_reply_config_suggestions (
			tenant_id, conversation_id, position_id, company_profile_id,
			suggestion_type, operation, target_id, proposed_value, reason, status
		) VALUES ($1,NULLIF($2,'')::uuid,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,
			$5,$6,$7,$8::jsonb,$9,'pending')
		RETURNING id, tenant_id, COALESCE(conversation_id::text,''), COALESCE(position_id::text,''),
			COALESCE(company_profile_id::text,''), suggestion_type, operation, target_id,
			proposed_value, reason, status, COALESCE(reviewed_by_user_id::text,''),
			reviewed_at, created_at, updated_at
	`, item.TenantID, item.ConversationID, item.PositionID, item.CompanyProfileID,
		item.SuggestionType, item.Operation, strings.TrimSpace(item.TargetID),
		string(proposed), strings.TrimSpace(item.Reason)).Scan(
		&item.ID, &item.TenantID, &item.ConversationID, &item.PositionID,
		&item.CompanyProfileID, &item.SuggestionType, &item.Operation, &item.TargetID,
		&item.ProposedValue, &item.Reason, &item.Status, &item.ReviewedByUserID,
		&item.ReviewedAt, &item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}

// ClaimAutoReplyNotification 原子占用一条人工接管邮件，重复消息和原因只返回已有记录。
func (s *PostgresAutoReplyStore) ClaimAutoReplyNotification(ctx context.Context, item AutoReplyNotification) (AutoReplyNotification, bool, error) {
	item.TenantID = strings.TrimSpace(item.TenantID)
	item.ConversationID = strings.TrimSpace(item.ConversationID)
	item.PositionID = strings.TrimSpace(item.PositionID)
	item.BasedOnMessageKey = strings.TrimSpace(item.BasedOnMessageKey)
	item.Reason = strings.TrimSpace(item.Reason)
	item.RecipientEmail = strings.ToLower(strings.TrimSpace(item.RecipientEmail))
	item.Gender = strings.TrimSpace(item.Gender)
	if item.TenantID == "" || item.ConversationID == "" || item.BasedOnMessageKey == "" || item.Reason == "" || item.RecipientEmail == "" {
		return AutoReplyNotification{}, false, newAutoReplyValidationError("人工接管通知缺少团队、会话、消息、原因或收件人")
	}
	if item.Gender != "" && item.Gender != "男" && item.Gender != "女" {
		return AutoReplyNotification{}, false, newAutoReplyValidationError("候选人性别只支持男、女或空值")
	}
	for kind, id := range map[string]string{"conversation": item.ConversationID, "position": item.PositionID} {
		if err := s.ensureAutoReplyReference(ctx, item.TenantID, kind, id); err != nil {
			return AutoReplyNotification{}, false, err
		}
	}
	item.ReasonKey = normalizeAutoReplyDedupeKey(item.Reason)
	item.Status = "pending"
	created := true
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO auto_reply_notifications (
			tenant_id, conversation_id, position_id, based_on_message_key, reason_key,
			candidate_name, gender, platform_id, reason, recipient_email, status
		) VALUES ($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,$7,$8,$9,$10,'pending')
		ON CONFLICT (tenant_id, conversation_id, based_on_message_key, reason_key) DO NOTHING
		RETURNING id, tenant_id, conversation_id, COALESCE(position_id::text,''),
			based_on_message_key, reason_key, candidate_name, gender, platform_id,
			reason, recipient_email, status, error_message, sent_at, expires_at, created_at, updated_at
	`, item.TenantID, item.ConversationID, item.PositionID, item.BasedOnMessageKey,
		item.ReasonKey, strings.TrimSpace(item.CandidateName), item.Gender,
		strings.TrimSpace(item.PlatformID), item.Reason, item.RecipientEmail).Scan(
		&item.ID, &item.TenantID, &item.ConversationID, &item.PositionID,
		&item.BasedOnMessageKey, &item.ReasonKey, &item.CandidateName, &item.Gender,
		&item.PlatformID, &item.Reason, &item.RecipientEmail, &item.Status,
		&item.ErrorMessage, &item.SentAt, &item.ExpiresAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		created = false
		err = s.db.QueryRowContext(ctx, `
			SELECT id, tenant_id, conversation_id, COALESCE(position_id::text,''),
				based_on_message_key, reason_key, candidate_name, gender, platform_id,
				reason, recipient_email, status, error_message, sent_at, expires_at, created_at, updated_at
			FROM auto_reply_notifications
			WHERE tenant_id=$1 AND conversation_id=$2 AND based_on_message_key=$3 AND reason_key=$4
		`, item.TenantID, item.ConversationID, item.BasedOnMessageKey, item.ReasonKey).Scan(
			&item.ID, &item.TenantID, &item.ConversationID, &item.PositionID,
			&item.BasedOnMessageKey, &item.ReasonKey, &item.CandidateName, &item.Gender,
			&item.PlatformID, &item.Reason, &item.RecipientEmail, &item.Status,
			&item.ErrorMessage, &item.SentAt, &item.ExpiresAt, &item.CreatedAt, &item.UpdatedAt,
		)
	}
	return item, created, err
}

// FinishAutoReplyNotification 保存人工接管邮件的最终发送结果。
func (s *PostgresAutoReplyStore) FinishAutoReplyNotification(ctx context.Context, item AutoReplyNotification) (AutoReplyNotification, error) {
	if item.Status != "sent" && item.Status != "failed" {
		return AutoReplyNotification{}, newAutoReplyValidationError("人工接管通知结束状态不支持")
	}
	err := s.db.QueryRowContext(ctx, `
		UPDATE auto_reply_notifications
		SET status=$3, error_message=$4,
			sent_at=CASE WHEN $3='sent' THEN now() ELSE NULL END, updated_at=now()
		WHERE tenant_id=$1 AND id=$2
		RETURNING id, tenant_id, conversation_id, COALESCE(position_id::text,''),
			based_on_message_key, reason_key, candidate_name, gender, platform_id,
			reason, recipient_email, status, error_message, sent_at, expires_at, created_at, updated_at
	`, item.TenantID, item.ID, item.Status, strings.TrimSpace(item.ErrorMessage)).Scan(
		&item.ID, &item.TenantID, &item.ConversationID, &item.PositionID,
		&item.BasedOnMessageKey, &item.ReasonKey, &item.CandidateName, &item.Gender,
		&item.PlatformID, &item.Reason, &item.RecipientEmail, &item.Status,
		&item.ErrorMessage, &item.SentAt, &item.ExpiresAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AutoReplyNotification{}, ErrNotFound
	}
	return item, err
}

// DeleteExpiredAutoReplyAudit 删除已超过180天保留期的 AI 总记录和级联工具记录。
func (s *PostgresAutoReplyStore) DeleteExpiredAutoReplyAudit(ctx context.Context, now time.Time) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	aiResult, err := tx.ExecContext(ctx, `DELETE FROM auto_reply_ai_runs WHERE expires_at <= $1`, now)
	if err != nil {
		return 0, err
	}
	notificationResult, err := tx.ExecContext(ctx, `DELETE FROM auto_reply_notifications WHERE expires_at <= $1`, now)
	if err != nil {
		return 0, err
	}
	aiRows, err := aiResult.RowsAffected()
	if err != nil {
		return 0, err
	}
	notificationRows, err := notificationResult.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return aiRows + notificationRows, nil
}

// nonEmptyJSON 把空审计字段转换成空对象，保证数据库始终收到合法JSON。
func nonEmptyJSON(value json.RawMessage) json.RawMessage {
	if strings.TrimSpace(string(value)) == "" {
		return json.RawMessage(`{}`)
	}
	return value
}
