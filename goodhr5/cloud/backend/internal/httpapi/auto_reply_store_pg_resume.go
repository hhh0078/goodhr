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

// UpdateResumeAttachmentExtractedText 保存云端从真实附件文件提取出的正文。
// tenantID 和 attachmentID 用于限制团队范围，text 为附件正文。
func (s *PostgresAutoReplyStore) UpdateResumeAttachmentExtractedText(ctx context.Context, tenantID, attachmentID, text string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE candidate_resume_attachments
		SET extracted_text=$3
		WHERE tenant_id=$1 AND id=$2
	`, tenantID, attachmentID, strings.TrimSpace(text))
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// LinkConversationResumeAttachments 把临时会话附件关联到刚刚建立的正式候选人。
// tenantID、conversationID 和 candidateID 必须属于同一团队。
func (s *PostgresAutoReplyStore) LinkConversationResumeAttachments(ctx context.Context, tenantID, conversationID, candidateID string) error {
	if err := s.ensureAutoReplyReference(ctx, tenantID, "conversation", conversationID); err != nil {
		return err
	}
	if err := s.ensureAutoReplyReference(ctx, tenantID, "candidate", candidateID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE candidate_resume_attachments
		SET candidate_id=$3
		WHERE tenant_id=$1 AND conversation_id=$2
	`, tenantID, conversationID, candidateID)
	return err
}

// UpsertConfirmationItem 新增或修改候选人确认项，并在状态或证据变化时保存事件。
func (s *PostgresAutoReplyStore) UpsertConfirmationItem(ctx context.Context, item CandidateConfirmationItem) (CandidateConfirmationItem, error) {
	item.StatusReason = firstNonEmpty(item.StatusReason, firstNonEmpty(item.Summary, item.EvidenceText))
	if item.StatusReason == "" {
		item.StatusReason = "暂时没有足够信息，继续等待候选人确认"
	}
	item.Summary = firstNonEmpty(item.Summary, item.StatusReason)
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
	if item.CandidateID != "" && item.PositionID != "" {
		review, err := s.EnsureCandidatePositionReview(ctx, item.TenantID, item.CandidateID, item.PositionID)
		if err != nil {
			return CandidateConfirmationItem{}, err
		}
		item.ReviewID = review.ID
	}
	item.DedupeKey = normalizeAutoReplyDedupeKey(item.Content)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CandidateConfirmationItem{}, err
	}
	defer tx.Rollback()
	var oldStatus, oldReason, oldEvidence, oldSourceRef string
	var existingID string
	lookupQuery := `
		SELECT id, status, status_reason, evidence_text, source_ref
		FROM candidate_confirmation_items
		WHERE conversation_id=$1 AND dedupe_key=$2 AND archived_at IS NULL
		FOR UPDATE`
	lookupArgs := []any{item.ConversationID, item.DedupeKey}
	if item.ReviewID != "" {
		lookupQuery = `
			SELECT id, status, status_reason, evidence_text, source_ref
			FROM candidate_confirmation_items
			WHERE review_id=$1 AND dedupe_key=$2 AND archived_at IS NULL
			FOR UPDATE`
		lookupArgs[0] = item.ReviewID
	}
	err = tx.QueryRowContext(ctx, lookupQuery, lookupArgs...).Scan(&existingID, &oldStatus, &oldReason, &oldEvidence, &oldSourceRef)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return CandidateConfirmationItem{}, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `
			INSERT INTO candidate_confirmation_items (
				tenant_id, review_id, conversation_id, candidate_id, position_id, position_condition_id,
				item_type, content, dedupe_key, status, status_reason, source_type, source_ref,
				evidence_text, summary, created_by_kind, ask_count, last_asked_at,
				last_answered_at, last_reviewed_at
			) VALUES ($1,NULLIF($2,'')::uuid,$3,NULLIF($4,'')::uuid,NULLIF($5,'')::uuid,
				NULLIF($6,'')::uuid,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,COALESCE($20,now()))
			RETURNING id
		`, item.TenantID, item.ReviewID, item.ConversationID, item.CandidateID, item.PositionID,
			item.PositionConditionID, item.ItemType, strings.TrimSpace(item.Content), item.DedupeKey,
			item.Status, item.StatusReason, item.SourceType, strings.TrimSpace(item.SourceRef),
			strings.TrimSpace(item.EvidenceText), strings.TrimSpace(item.Summary), item.CreatedByKind,
			item.AskCount, item.LastAskedAt, item.LastAnsweredAt, item.LastReviewedAt).Scan(&existingID)
		oldStatus = ""
	} else {
		_, err = tx.ExecContext(ctx, `
			UPDATE candidate_confirmation_items
			SET review_id=COALESCE(NULLIF($3,'')::uuid,review_id),
				candidate_id=COALESCE(NULLIF($4,'')::uuid,candidate_id),
				position_id=COALESCE(NULLIF($5,'')::uuid,position_id),
				position_condition_id=COALESCE(NULLIF($6,'')::uuid,position_condition_id),
				item_type=$7, content=$8, status=$9, status_reason=$10, source_type=$11,
				source_ref=$12, evidence_text=$13, summary=$14,
				ask_count=GREATEST(ask_count,$15),
				last_asked_at=COALESCE($16,last_asked_at),
				last_answered_at=COALESCE($17,last_answered_at),
				last_reviewed_at=COALESCE($18,now()), updated_at=now()
			WHERE tenant_id=$1 AND id=$2
		`, item.TenantID, existingID, item.ReviewID, item.CandidateID, item.PositionID,
			item.PositionConditionID, item.ItemType, strings.TrimSpace(item.Content), item.Status,
			item.StatusReason, item.SourceType, strings.TrimSpace(item.SourceRef),
			strings.TrimSpace(item.EvidenceText), strings.TrimSpace(item.Summary), item.AskCount,
			item.LastAskedAt, item.LastAnsweredAt, item.LastReviewedAt)
	}
	if err != nil {
		return CandidateConfirmationItem{}, err
	}
	changed := oldStatus != item.Status || oldReason != item.StatusReason || oldEvidence != strings.TrimSpace(item.EvidenceText) || oldSourceRef != strings.TrimSpace(item.SourceRef)
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
		SELECT id, tenant_id, COALESCE(review_id::text,''), conversation_id,
			COALESCE(candidate_id::text,''), COALESCE(position_id::text,''),
			COALESCE(position_condition_id::text,''), item_type, content, dedupe_key, status,
			status_reason, source_type, source_ref, evidence_text, summary, created_by_kind,
			ask_count, last_asked_at, last_answered_at, last_reviewed_at, archived_at,
			created_at, updated_at
		FROM candidate_confirmation_items WHERE id=$1
	`, existingID).Scan(
		&item.ID, &item.TenantID, &item.ReviewID, &item.ConversationID, &item.CandidateID,
		&item.PositionID, &item.PositionConditionID, &item.ItemType, &item.Content,
		&item.DedupeKey, &item.Status, &item.StatusReason, &item.SourceType,
		&item.SourceRef, &item.EvidenceText, &item.Summary, &item.CreatedByKind,
		&item.AskCount, &item.LastAskedAt, &item.LastAnsweredAt, &item.LastReviewedAt,
		&item.ArchivedAt, &item.CreatedAt, &item.UpdatedAt,
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
		WITH target AS (
			SELECT candidate_id, position_id
			FROM candidate_conversations WHERE tenant_id=$1 AND id=$2
		)
		SELECT item.id, item.tenant_id, COALESCE(item.review_id::text,''), item.conversation_id,
			COALESCE(item.candidate_id::text,''), COALESCE(item.position_id::text,''),
			COALESCE(item.position_condition_id::text,''), item.item_type, item.content,
			item.dedupe_key, item.status, item.status_reason, item.source_type, item.source_ref,
			item.evidence_text, item.summary, item.created_by_kind, item.ask_count,
			item.last_asked_at, item.last_answered_at, item.last_reviewed_at, item.archived_at,
			item.created_at, item.updated_at
		FROM candidate_confirmation_items item, target
		WHERE item.tenant_id=$1 AND item.archived_at IS NULL AND (
			item.conversation_id=$2 OR (
				target.candidate_id IS NOT NULL AND target.position_id IS NOT NULL
				AND item.candidate_id=target.candidate_id AND item.position_id=target.position_id
			)
		)
		ORDER BY CASE item_type WHEN 'required' THEN 0 WHEN 'confirm' THEN 1 ELSE 2 END,
			item.created_at, item.id
	`, tenantID, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]CandidateConfirmationItem, 0)
	for rows.Next() {
		var item CandidateConfirmationItem
		if err = rows.Scan(&item.ID, &item.TenantID, &item.ReviewID, &item.ConversationID,
			&item.CandidateID, &item.PositionID, &item.PositionConditionID, &item.ItemType,
			&item.Content, &item.DedupeKey, &item.Status, &item.StatusReason, &item.SourceType,
			&item.SourceRef, &item.EvidenceText, &item.Summary, &item.CreatedByKind,
			&item.AskCount, &item.LastAskedAt, &item.LastAnsweredAt, &item.LastReviewedAt,
			&item.ArchivedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
