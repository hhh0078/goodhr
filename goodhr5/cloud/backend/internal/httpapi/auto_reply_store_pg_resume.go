// Package httpapi 本文件负责自动回复简历附件、候选人确认项和状态证据的 PostgreSQL 存储。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// SaveResumeAttachment 幂等保存云端持久化目录中的简历附件元数据。
func (s *PostgresAutoReplyStore) SaveResumeAttachment(ctx context.Context, item StoredResumeAttachment) (StoredResumeAttachment, error) {
	if err := validateResumeAttachment(item); err != nil {
		return StoredResumeAttachment{}, err
	}
	for kind, id := range map[string]string{
		"candidate": item.CandidateID, "conversation": item.ConversationID, "message": item.SourceMessageID,
	} {
		if err := s.ensureAutoReplyReference(ctx, item.TenantID, kind, id); err != nil {
			return StoredResumeAttachment{}, err
		}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO candidate_resume_attachments (
			tenant_id, candidate_id, conversation_id, source_message_id, platform_id,
			original_name, storage_path, sha256, mime_type, size_bytes, extracted_text,
			created_by_user_id
		) VALUES ($1,NULLIF($2,'')::uuid,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,
			$5,$6,$7,$8,$9,$10,$11,NULLIF($12,'')::uuid)
		ON CONFLICT DO NOTHING
	`, item.TenantID, item.CandidateID, item.ConversationID, item.SourceMessageID,
		strings.TrimSpace(item.PlatformID), strings.TrimSpace(item.OriginalName),
		strings.TrimSpace(item.StoragePath), strings.ToLower(strings.TrimSpace(item.SHA256)),
		strings.TrimSpace(item.MIMEType), item.SizeBytes, item.ExtractedText, item.CreatedByUserID)
	if err != nil {
		return StoredResumeAttachment{}, err
	}
	err = s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, COALESCE(candidate_id::text,''), COALESCE(conversation_id::text,''),
			COALESCE(source_message_id::text,''), platform_id, original_name, storage_path,
			sha256, mime_type, size_bytes, extracted_text, COALESCE(created_by_user_id::text,''), created_at
		FROM candidate_resume_attachments
		WHERE tenant_id=$1 AND sha256=$2
	`, item.TenantID, strings.ToLower(strings.TrimSpace(item.SHA256))).Scan(
		&item.ID, &item.TenantID, &item.CandidateID, &item.ConversationID,
		&item.SourceMessageID, &item.PlatformID, &item.OriginalName, &item.StoragePath,
		&item.SHA256, &item.MIMEType, &item.SizeBytes, &item.ExtractedText,
		&item.CreatedByUserID, &item.CreatedAt,
	)
	return item, err
}

// ListResumeAttachments 返回候选人或临时会话关联的简历附件。
func (s *PostgresAutoReplyStore) ListResumeAttachments(ctx context.Context, tenantID, candidateID, conversationID string) ([]StoredResumeAttachment, error) {
	if strings.TrimSpace(candidateID) == "" && strings.TrimSpace(conversationID) == "" {
		return nil, newAutoReplyValidationError("读取简历附件需要候选人或会话标识")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, COALESCE(candidate_id::text,''), COALESCE(conversation_id::text,''),
			COALESCE(source_message_id::text,''), platform_id, original_name, storage_path,
			sha256, mime_type, size_bytes, extracted_text, COALESCE(created_by_user_id::text,''), created_at
		FROM candidate_resume_attachments
		WHERE tenant_id=$1
		  AND (($2<>'' AND candidate_id=NULLIF($2,'')::uuid) OR ($3<>'' AND conversation_id=NULLIF($3,'')::uuid))
		ORDER BY created_at DESC, id
	`, tenantID, candidateID, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]StoredResumeAttachment, 0)
	for rows.Next() {
		var item StoredResumeAttachment
		if err = rows.Scan(&item.ID, &item.TenantID, &item.CandidateID, &item.ConversationID,
			&item.SourceMessageID, &item.PlatformID, &item.OriginalName, &item.StoragePath,
			&item.SHA256, &item.MIMEType, &item.SizeBytes, &item.ExtractedText,
			&item.CreatedByUserID, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetResumeAttachment 返回当前团队指定简历附件元数据。
func (s *PostgresAutoReplyStore) GetResumeAttachment(ctx context.Context, tenantID, attachmentID string) (StoredResumeAttachment, error) {
	var item StoredResumeAttachment
	err := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, COALESCE(candidate_id::text,''), COALESCE(conversation_id::text,''),
			COALESCE(source_message_id::text,''), platform_id, original_name, storage_path,
			sha256, mime_type, size_bytes, extracted_text, COALESCE(created_by_user_id::text,''), created_at
		FROM candidate_resume_attachments
		WHERE tenant_id=$1 AND id=$2
	`, tenantID, attachmentID).Scan(
		&item.ID, &item.TenantID, &item.CandidateID, &item.ConversationID,
		&item.SourceMessageID, &item.PlatformID, &item.OriginalName, &item.StoragePath,
		&item.SHA256, &item.MIMEType, &item.SizeBytes, &item.ExtractedText,
		&item.CreatedByUserID, &item.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return StoredResumeAttachment{}, ErrNotFound
	}
	return item, err
}

// UpsertConfirmationItem 新增或修改候选人确认项，并在状态或证据变化时保存事件。
func (s *PostgresAutoReplyStore) UpsertConfirmationItem(ctx context.Context, item CandidateConfirmationItem) (CandidateConfirmationItem, error) {
	if err := validateConfirmationItem(item); err != nil {
		return CandidateConfirmationItem{}, err
	}
	for kind, id := range map[string]string{
		"conversation": item.ConversationID, "candidate": item.CandidateID, "position": item.PositionID,
	} {
		if err := s.ensureAutoReplyReference(ctx, item.TenantID, kind, id); err != nil {
			return CandidateConfirmationItem{}, err
		}
	}
	item.DedupeKey = normalizeAutoReplyDedupeKey(item.Content)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CandidateConfirmationItem{}, err
	}
	defer tx.Rollback()
	var oldStatus, oldEvidence, oldSourceRef string
	var existingID string
	err = tx.QueryRowContext(ctx, `
		SELECT id, status, evidence_text, source_ref
		FROM candidate_confirmation_items
		WHERE conversation_id=$1 AND dedupe_key=$2
		FOR UPDATE
	`, item.ConversationID, item.DedupeKey).Scan(&existingID, &oldStatus, &oldEvidence, &oldSourceRef)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return CandidateConfirmationItem{}, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `
			INSERT INTO candidate_confirmation_items (
				tenant_id, conversation_id, candidate_id, position_id, item_type, content,
				dedupe_key, status, source_type, source_ref, evidence_text, summary, created_by_kind
			) VALUES ($1,$2,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9,$10,$11,$12,$13)
			RETURNING id
		`, item.TenantID, item.ConversationID, item.CandidateID, item.PositionID,
			item.ItemType, strings.TrimSpace(item.Content), item.DedupeKey, item.Status,
			item.SourceType, strings.TrimSpace(item.SourceRef), strings.TrimSpace(item.EvidenceText),
			strings.TrimSpace(item.Summary), item.CreatedByKind).Scan(&existingID)
		oldStatus = ""
	} else {
		_, err = tx.ExecContext(ctx, `
			UPDATE candidate_confirmation_items
			SET candidate_id=COALESCE(NULLIF($3,'')::uuid,candidate_id),
				position_id=COALESCE(NULLIF($4,'')::uuid,position_id), item_type=$5,
				content=$6, status=$7, source_type=$8, source_ref=$9,
				evidence_text=$10, summary=$11, updated_at=now()
			WHERE tenant_id=$1 AND id=$2
		`, item.TenantID, existingID, item.CandidateID, item.PositionID, item.ItemType,
			strings.TrimSpace(item.Content), item.Status, item.SourceType,
			strings.TrimSpace(item.SourceRef), strings.TrimSpace(item.EvidenceText), strings.TrimSpace(item.Summary))
	}
	if err != nil {
		return CandidateConfirmationItem{}, err
	}
	changed := oldStatus != item.Status || oldEvidence != strings.TrimSpace(item.EvidenceText) || oldSourceRef != strings.TrimSpace(item.SourceRef)
	if changed {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO candidate_confirmation_events (
				tenant_id, confirmation_item_id, old_status, new_status,
				evidence_text, source_ref, changed_by_kind
			) VALUES ($1,$2,$3,$4,$5,$6,$7)
		`, item.TenantID, existingID, oldStatus, item.Status,
			strings.TrimSpace(item.EvidenceText), strings.TrimSpace(item.SourceRef), item.CreatedByKind)
		if err != nil {
			return CandidateConfirmationItem{}, err
		}
	}
	if err = tx.QueryRowContext(ctx, `
		SELECT id, tenant_id, conversation_id, COALESCE(candidate_id::text,''),
			COALESCE(position_id::text,''), item_type, content, dedupe_key, status,
			source_type, source_ref, evidence_text, summary, created_by_kind, created_at, updated_at
		FROM candidate_confirmation_items WHERE id=$1
	`, existingID).Scan(
		&item.ID, &item.TenantID, &item.ConversationID, &item.CandidateID,
		&item.PositionID, &item.ItemType, &item.Content, &item.DedupeKey, &item.Status,
		&item.SourceType, &item.SourceRef, &item.EvidenceText, &item.Summary,
		&item.CreatedByKind, &item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return CandidateConfirmationItem{}, err
	}
	if err = tx.Commit(); err != nil {
		return CandidateConfirmationItem{}, err
	}
	return item, nil
}

// ListConfirmationItems 返回会话全部确认项和当前状态。
func (s *PostgresAutoReplyStore) ListConfirmationItems(ctx context.Context, tenantID, conversationID string) ([]CandidateConfirmationItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, conversation_id, COALESCE(candidate_id::text,''),
			COALESCE(position_id::text,''), item_type, content, dedupe_key, status,
			source_type, source_ref, evidence_text, summary, created_by_kind, created_at, updated_at
		FROM candidate_confirmation_items
		WHERE tenant_id=$1 AND conversation_id=$2
		ORDER BY CASE item_type WHEN 'required' THEN 0 WHEN 'confirm' THEN 1 ELSE 2 END,
			created_at, id
	`, tenantID, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]CandidateConfirmationItem, 0)
	for rows.Next() {
		var item CandidateConfirmationItem
		if err = rows.Scan(&item.ID, &item.TenantID, &item.ConversationID, &item.CandidateID,
			&item.PositionID, &item.ItemType, &item.Content, &item.DedupeKey, &item.Status,
			&item.SourceType, &item.SourceRef, &item.EvidenceText, &item.Summary,
			&item.CreatedByKind, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
