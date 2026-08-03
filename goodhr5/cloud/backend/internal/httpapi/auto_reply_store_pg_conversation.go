// Package httpapi 本文件负责自动回复平台身份、临时会话、聊天差量和手机号身份的 PostgreSQL 存储。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// UpsertCandidatePlatformIdentity 新增或更新一个有稳定平台候选人 ID 的身份。
func (s *PostgresAutoReplyStore) UpsertCandidatePlatformIdentity(ctx context.Context, item CandidatePlatformIdentity) (CandidatePlatformIdentity, error) {
	item.PlatformID = strings.TrimSpace(item.PlatformID)
	item.PlatformCandidateID = strings.TrimSpace(item.PlatformCandidateID)
	item.Gender = strings.TrimSpace(item.Gender)
	item.NormalizedPhone = normalizeCandidatePhone(item.NormalizedPhone)
	if item.TenantID == "" || item.PlatformID == "" || item.PlatformCandidateID == "" {
		return CandidatePlatformIdentity{}, newAutoReplyValidationError("平台候选人身份缺少团队、平台或稳定标识")
	}
	if item.Gender != "" && item.Gender != "男" && item.Gender != "女" {
		return CandidatePlatformIdentity{}, newAutoReplyValidationError("候选人性别只支持男、女或空值")
	}
	if err := s.ensureAutoReplyReference(ctx, item.TenantID, "candidate", item.CandidateID); err != nil {
		return CandidatePlatformIdentity{}, err
	}
	if err := s.ensureAutoReplyReference(ctx, item.TenantID, "account", item.PlatformAccountID); err != nil {
		return CandidatePlatformIdentity{}, err
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO candidate_platform_identities (
			tenant_id, candidate_id, platform_id, platform_account_id, platform_candidate_id,
			candidate_name, gender, normalized_phone, first_seen_at, last_seen_at
		) VALUES ($1,NULLIF($2,'')::uuid,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,
			COALESCE($9,now()),COALESCE($10,now()))
		ON CONFLICT DO NOTHING
	`, item.TenantID, item.CandidateID, item.PlatformID, item.PlatformAccountID,
		item.PlatformCandidateID, strings.TrimSpace(item.CandidateName), item.Gender,
		item.NormalizedPhone, nullableTime(item.FirstSeenAt), nullableTime(item.LastSeenAt))
	if err != nil {
		return CandidatePlatformIdentity{}, err
	}
	return scanCandidatePlatformIdentity(s.db.QueryRowContext(ctx, `
		UPDATE candidate_platform_identities
		SET candidate_id=COALESCE(NULLIF($5,'')::uuid,candidate_id),
			candidate_name=CASE WHEN $6='' THEN candidate_name ELSE $6 END,
			gender=CASE WHEN $7='' THEN gender ELSE $7 END,
			normalized_phone=CASE WHEN $8='' THEN normalized_phone ELSE $8 END,
			last_seen_at=COALESCE($9,now()), updated_at=now()
		WHERE tenant_id=$1 AND platform_id=$2
			AND platform_account_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid
			AND platform_candidate_id=$4
		RETURNING id, tenant_id, COALESCE(candidate_id::text,''), platform_id,
			COALESCE(platform_account_id::text,''), platform_candidate_id, candidate_name,
			gender, normalized_phone, first_seen_at, last_seen_at, created_at, updated_at
	`, item.TenantID, item.PlatformID, item.PlatformAccountID, item.PlatformCandidateID,
		item.CandidateID, strings.TrimSpace(item.CandidateName), item.Gender,
		item.NormalizedPhone, nullableTime(item.LastSeenAt)))
}

// UpsertAutoReplyConversation 新增或更新一个平台会话，并校验所有关联数据属于同一团队。
func (s *PostgresAutoReplyStore) UpsertAutoReplyConversation(ctx context.Context, item AutoReplyConversation) (AutoReplyConversation, error) {
	item.PlatformID = strings.TrimSpace(item.PlatformID)
	item.PlatformThreadID = strings.TrimSpace(item.PlatformThreadID)
	item.Gender = strings.TrimSpace(item.Gender)
	if item.TenantID == "" || item.PlatformID == "" || item.PlatformThreadID == "" {
		return AutoReplyConversation{}, newAutoReplyValidationError("自动回复会话缺少团队、平台或稳定会话标识")
	}
	if item.Status == "" {
		item.Status = "active"
	}
	if !validConversationStatus(item.Status) {
		return AutoReplyConversation{}, newAutoReplyValidationError("自动回复会话状态不支持")
	}
	if item.Gender != "" && item.Gender != "男" && item.Gender != "女" {
		return AutoReplyConversation{}, newAutoReplyValidationError("候选人性别只支持男、女或空值")
	}
	for kind, id := range map[string]string{
		"candidate": item.CandidateID, "identity": item.PlatformIdentityID,
		"engagement": item.EngagementID, "position": item.PositionID,
		"account": item.PlatformAccountID,
	} {
		if err := s.ensureAutoReplyReference(ctx, item.TenantID, kind, id); err != nil {
			return AutoReplyConversation{}, err
		}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO candidate_conversations (
			tenant_id, candidate_id, platform_identity_id, engagement_id, position_id,
			platform_account_id, platform_id, platform_thread_id, candidate_name,
			gender, page_position_text, status, history_complete,
			last_synced_message_key, last_candidate_message_key, unresolved_reason, last_checked_at
		) VALUES ($1,NULLIF($2,'')::uuid,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,
			NULLIF($5,'')::uuid,NULLIF($6,'')::uuid,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		ON CONFLICT DO NOTHING
	`, item.TenantID, item.CandidateID, item.PlatformIdentityID, item.EngagementID,
		item.PositionID, item.PlatformAccountID, item.PlatformID, item.PlatformThreadID,
		strings.TrimSpace(item.CandidateName), item.Gender, strings.TrimSpace(item.PagePositionText),
		item.Status, item.HistoryComplete, item.LastSyncedMessageKey,
		item.LastCandidateMessageKey, item.UnresolvedReason, item.LastCheckedAt)
	if err != nil {
		return AutoReplyConversation{}, err
	}
	return scanAutoReplyConversation(s.db.QueryRowContext(ctx, `
		UPDATE candidate_conversations
		SET candidate_id=COALESCE(NULLIF($5,'')::uuid,candidate_id),
			platform_identity_id=COALESCE(NULLIF($6,'')::uuid,platform_identity_id),
			engagement_id=COALESCE(NULLIF($7,'')::uuid,engagement_id),
			position_id=COALESCE(NULLIF($8,'')::uuid,position_id),
			candidate_name=CASE WHEN $9='' THEN candidate_name ELSE $9 END,
			gender=CASE WHEN $10='' THEN gender ELSE $10 END,
			page_position_text=CASE WHEN $11='' THEN page_position_text ELSE $11 END,
			status=$12,
			history_complete=history_complete OR $13,
			unresolved_reason=$14,
			last_checked_at=COALESCE($15,now()), updated_at=now()
		WHERE tenant_id=$1 AND platform_id=$2
			AND platform_account_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid
			AND platform_thread_id=$4
		RETURNING id, tenant_id, COALESCE(candidate_id::text,''),
			COALESCE(platform_identity_id::text,''), COALESCE(engagement_id::text,''),
			COALESCE(position_id::text,''), COALESCE(platform_account_id::text,''),
			platform_id, platform_thread_id, candidate_name, gender, page_position_text,
			status, history_complete, last_synced_message_key, last_candidate_message_key,
			unresolved_reason, last_checked_at, created_at, updated_at
	`, item.TenantID, item.PlatformID, item.PlatformAccountID, item.PlatformThreadID,
		item.CandidateID, item.PlatformIdentityID, item.EngagementID, item.PositionID,
		strings.TrimSpace(item.CandidateName), item.Gender, strings.TrimSpace(item.PagePositionText),
		item.Status, item.HistoryComplete, strings.TrimSpace(item.UnresolvedReason), item.LastCheckedAt))
}

// FindCandidatePlatformIdentity 按团队、平台账号和平台候选人标识读取身份映射。
func (s *PostgresAutoReplyStore) FindCandidatePlatformIdentity(ctx context.Context, tenantID, platformID, accountID, platformCandidateID string) (CandidatePlatformIdentity, error) {
	item, err := scanCandidatePlatformIdentity(s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, COALESCE(candidate_id::text,''), platform_id,
			COALESCE(platform_account_id::text,''), platform_candidate_id, candidate_name,
			gender, normalized_phone, first_seen_at, last_seen_at, created_at, updated_at
		FROM candidate_platform_identities
		WHERE tenant_id=$1 AND platform_id=$2
			AND platform_account_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid
			AND platform_candidate_id=$4
	`, tenantID, strings.TrimSpace(platformID), strings.TrimSpace(accountID), strings.TrimSpace(platformCandidateID)))
	if errors.Is(err, sql.ErrNoRows) {
		return CandidatePlatformIdentity{}, ErrNotFound
	}
	return item, err
}

// FindAutoReplyConversation 按平台会话标识读取当前团队已同步的会话和差量游标。
func (s *PostgresAutoReplyStore) FindAutoReplyConversation(ctx context.Context, tenantID, platformID, accountID, platformThreadID string) (AutoReplyConversation, error) {
	item, err := scanAutoReplyConversation(s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, COALESCE(candidate_id::text,''),
			COALESCE(platform_identity_id::text,''), COALESCE(engagement_id::text,''),
			COALESCE(position_id::text,''), COALESCE(platform_account_id::text,''),
			platform_id, platform_thread_id, candidate_name, gender, page_position_text,
			status, history_complete, last_synced_message_key, last_candidate_message_key,
			unresolved_reason, last_checked_at, created_at, updated_at
		FROM candidate_conversations
		WHERE tenant_id=$1 AND platform_id=$2
			AND platform_account_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid
			AND platform_thread_id=$4
	`, tenantID, strings.TrimSpace(platformID), strings.TrimSpace(accountID), strings.TrimSpace(platformThreadID)))
	if errors.Is(err, sql.ErrNoRows) {
		return AutoReplyConversation{}, ErrNotFound
	}
	return item, err
}

// SyncAutoReplyMessages 幂等写入一批聊天消息并更新会话差量游标。
func (s *PostgresAutoReplyStore) SyncAutoReplyMessages(ctx context.Context, tenantID, conversationID string, historyComplete bool, messages []AutoReplyMessage) (MessageSyncResult, error) {
	result := MessageSyncResult{}
	if len(messages) > 5000 {
		return result, newAutoReplyValidationError("首次聊天同步最多5000条")
	}
	for _, message := range messages {
		if err := validateAutoReplyMessage(message); err != nil {
			return result, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err = ensureConversationTenant(ctx, tx, tenantID, conversationID); err != nil {
		return result, err
	}
	for _, message := range messages {
		card := message.CardContent
		if len(strings.TrimSpace(string(card))) == 0 {
			card = json.RawMessage(`{}`)
		}
		execResult, execErr := tx.ExecContext(ctx, `
			INSERT INTO candidate_messages (
				tenant_id, conversation_id, platform_message_id, fingerprint,
				direction, message_type, text_content, card_content, sender_name, platform_sent_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10)
			ON CONFLICT DO NOTHING
		`, tenantID, conversationID, strings.TrimSpace(message.PlatformMessageID),
			strings.TrimSpace(message.Fingerprint), message.Direction, strings.TrimSpace(message.MessageType),
			message.TextContent, string(card), strings.TrimSpace(message.SenderName), message.PlatformSentAt)
		if execErr != nil {
			return result, execErr
		}
		affected, execErr := execResult.RowsAffected()
		if execErr != nil {
			return result, execErr
		}
		result.Inserted += int(affected)
		key := messageStableKey(message)
		if key != "" {
			result.LastSyncedMessageKey = key
			if message.Direction == "candidate" {
				result.LastCandidateMessageKey = key
			}
		}
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE candidate_conversations
		SET history_complete=history_complete OR $3,
			last_synced_message_key=CASE WHEN $4='' THEN last_synced_message_key ELSE $4 END,
			last_candidate_message_key=CASE WHEN $5='' THEN last_candidate_message_key ELSE $5 END,
			last_checked_at=now(), updated_at=now()
		WHERE tenant_id=$1 AND id=$2
	`, tenantID, conversationID, historyComplete, result.LastSyncedMessageKey, result.LastCandidateMessageKey)
	if err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

// ListAutoReplyMessages 按时间正序返回会话最近的聊天消息。
func (s *PostgresAutoReplyStore) ListAutoReplyMessages(ctx context.Context, tenantID, conversationID string, limit int) ([]AutoReplyMessage, error) {
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, conversation_id, platform_message_id, fingerprint,
			direction, message_type, text_content, card_content, sender_name,
			platform_sent_at, ingested_at, created_at
		FROM (
			SELECT * FROM candidate_messages
			WHERE tenant_id=$1 AND conversation_id=$2
			ORDER BY COALESCE(platform_sent_at,ingested_at) DESC, id DESC
			LIMIT $3
		) recent
		ORDER BY COALESCE(platform_sent_at,ingested_at), id
	`, tenantID, conversationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AutoReplyMessage, 0)
	for rows.Next() {
		var item AutoReplyMessage
		if err = rows.Scan(&item.ID, &item.TenantID, &item.ConversationID,
			&item.PlatformMessageID, &item.Fingerprint, &item.Direction, &item.MessageType,
			&item.TextContent, &item.CardContent, &item.SenderName, &item.PlatformSentAt,
			&item.IngestedAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ResolveCanonicalCandidateByPhone 返回团队手机号身份对应的正式候选人。
func (s *PostgresAutoReplyStore) ResolveCanonicalCandidateByPhone(ctx context.Context, tenantID, candidateID, phone string) (string, error) {
	normalized := normalizeCandidatePhone(phone)
	if normalized == "" {
		return "", newAutoReplyValidationError("手机号为空，候选人暂时不能进入正式简历库")
	}
	if len(normalized) > 32 {
		return "", newAutoReplyValidationError("手机号格式过长")
	}
	if err := s.ensureAutoReplyReference(ctx, tenantID, "candidate", candidateID); err != nil {
		return "", err
	}
	var canonicalID string
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO candidate_phone_identities (tenant_id, normalized_phone, candidate_id)
		VALUES ($1,$2,$3)
		ON CONFLICT (tenant_id, normalized_phone) DO UPDATE SET updated_at=now()
		RETURNING candidate_id
	`, tenantID, normalized, candidateID).Scan(&canonicalID)
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE candidate_profiles
		SET normalized_phone=$2, updated_at=now()
		WHERE tenant_id=$1 AND id=$3
	`, tenantID, normalized, canonicalID)
	return canonicalID, err
}

// CandidateIDByPhone 返回团队内已经绑定该标准化手机号的正式候选人标识。
func (s *PostgresAutoReplyStore) CandidateIDByPhone(ctx context.Context, tenantID, phone string) (string, error) {
	normalized := normalizeCandidatePhone(phone)
	if normalized == "" {
		return "", newAutoReplyValidationError("手机号为空，候选人暂时不能进入正式简历库")
	}
	var candidateID string
	err := s.db.QueryRowContext(ctx, `
		SELECT candidate_id FROM candidate_phone_identities
		WHERE tenant_id=$1 AND normalized_phone=$2
	`, tenantID, normalized).Scan(&candidateID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return candidateID, err
}

// ensureAutoReplyReference 校验可选关联ID存在且属于当前团队。
func (s *PostgresAutoReplyStore) ensureAutoReplyReference(ctx context.Context, tenantID, kind, id string) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	queries := map[string]string{
		"candidate":  `SELECT EXISTS (SELECT 1 FROM candidate_profiles WHERE tenant_id=$1 AND id=$2)`,
		"identity":   `SELECT EXISTS (SELECT 1 FROM candidate_platform_identities WHERE tenant_id=$1 AND id=$2)`,
		"engagement": `SELECT EXISTS (SELECT 1 FROM candidate_engagements WHERE tenant_id=$1 AND id=$2)`,
		"position": `SELECT EXISTS (
			SELECT 1 FROM positions p JOIN users u ON u.id=p.user_id
			WHERE u.tenant_id=$1 AND p.id=$2 AND u.status='active'
		)`,
		"account": `SELECT EXISTS (
			SELECT 1 FROM platform_accounts pa
			JOIN users u ON u.id=pa.user_id
			WHERE u.tenant_id=$1 AND pa.id=$2 AND u.status='active'
		)`,
		"conversation": `SELECT EXISTS (SELECT 1 FROM candidate_conversations WHERE tenant_id=$1 AND id=$2)`,
		"message":      `SELECT EXISTS (SELECT 1 FROM candidate_messages WHERE tenant_id=$1 AND id=$2)`,
	}
	query, ok := queries[kind]
	if !ok {
		return fmt.Errorf("未知自动回复关联类型：%s", kind)
	}
	var exists bool
	if err := s.db.QueryRowContext(ctx, query, tenantID, id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

// ensureConversationTenant 在事务内确认会话属于当前团队并锁定游标更新。
func ensureConversationTenant(ctx context.Context, tx *sql.Tx, tenantID, conversationID string) error {
	var id string
	err := tx.QueryRowContext(ctx, `
		SELECT id FROM candidate_conversations WHERE tenant_id=$1 AND id=$2 FOR UPDATE
	`, tenantID, conversationID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// messageStableKey 返回消息平台ID或指纹组成的稳定游标键。
func messageStableKey(item AutoReplyMessage) string {
	if value := strings.TrimSpace(item.PlatformMessageID); value != "" {
		return "id:" + value
	}
	if value := strings.TrimSpace(item.Fingerprint); value != "" {
		return "fp:" + value
	}
	return ""
}

// scanCandidatePlatformIdentity 从查询结果读取一条平台候选人身份。
func scanCandidatePlatformIdentity(scanner candidateScanner) (CandidatePlatformIdentity, error) {
	var item CandidatePlatformIdentity
	err := scanner.Scan(
		&item.ID, &item.TenantID, &item.CandidateID, &item.PlatformID,
		&item.PlatformAccountID, &item.PlatformCandidateID, &item.CandidateName,
		&item.Gender, &item.NormalizedPhone, &item.FirstSeenAt, &item.LastSeenAt,
		&item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}

// scanAutoReplyConversation 从查询结果读取一条自动回复会话。
func scanAutoReplyConversation(scanner candidateScanner) (AutoReplyConversation, error) {
	var item AutoReplyConversation
	err := scanner.Scan(
		&item.ID, &item.TenantID, &item.CandidateID, &item.PlatformIdentityID,
		&item.EngagementID, &item.PositionID, &item.PlatformAccountID,
		&item.PlatformID, &item.PlatformThreadID, &item.CandidateName, &item.Gender,
		&item.PagePositionText, &item.Status, &item.HistoryComplete,
		&item.LastSyncedMessageKey, &item.LastCandidateMessageKey,
		&item.UnresolvedReason, &item.LastCheckedAt, &item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}

// validConversationStatus 判断会话状态是否在数据库约束范围内。
func validConversationStatus(value string) bool {
	return value == "active" || value == "waiting_resume" || value == "ready" || value == "unresolved" || value == "ended"
}

// nullableTime 把时间零值转换成数据库NULL。
func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
