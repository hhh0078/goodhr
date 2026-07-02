// 本文件负责超管邮件发送记录的 PostgreSQL 存储实现。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

type PostgresEmailCampaignStore struct {
	db *sql.DB
}

// NewPostgresEmailCampaignStore 创建 PostgreSQL 邮件记录存储。
func NewPostgresEmailCampaignStore(db *sql.DB) *PostgresEmailCampaignStore {
	return &PostgresEmailCampaignStore{db: db}
}

// CreateBatch 创建邮件批次和收件人记录。
func (s *PostgresEmailCampaignStore) CreateBatch(subject string, targetSummary string, sourceKey string, createdBy string, emails []string) (EmailBatch, []EmailRecipient, error) {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return EmailBatch{}, nil, err
	}
	defer tx.Rollback()

	var batch EmailBatch
	err = tx.QueryRowContext(ctx, `
		INSERT INTO email_batches (subject, target_summary, source_key, created_by_email, total_count)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id::text, subject, target_summary, source_key, created_by_email, total_count, sent_count, failed_count, opened_count, created_at, finished_at
	`, subject, targetSummary, sourceKey, createdBy, len(emails)).Scan(&batch.ID, &batch.Subject, &batch.TargetSummary, &batch.SourceKey, &batch.CreatedByEmail, &batch.TotalCount, &batch.SentCount, &batch.FailedCount, &batch.OpenedCount, &batch.CreatedAt, &batch.FinishedAt)
	if err != nil {
		return EmailBatch{}, nil, err
	}

	recipients := make([]EmailRecipient, 0, len(emails))
	for _, email := range emails {
		var item EmailRecipient
		err = tx.QueryRowContext(ctx, `
			INSERT INTO email_recipients (batch_id, email)
			VALUES ($1::uuid, $2)
			ON CONFLICT (batch_id, email) DO UPDATE SET email = EXCLUDED.email
			RETURNING id::text, batch_id::text, email, status, error_message, opened, opened_at, created_at, sent_at
		`, batch.ID, email).Scan(&item.ID, &item.BatchID, &item.Email, &item.Status, &item.ErrorMessage, &item.Opened, &item.OpenedAt, &item.CreatedAt, &item.SentAt)
		if err != nil {
			return EmailBatch{}, nil, err
		}
		recipients = append(recipients, item)
	}
	if err := tx.Commit(); err != nil {
		return EmailBatch{}, nil, err
	}
	return batch, recipients, nil
}

// GetBatch 读取邮件批次和收件人记录。
func (s *PostgresEmailCampaignStore) GetBatch(id string) (EmailBatch, []EmailRecipient, error) {
	var batch EmailBatch
	err := s.db.QueryRow(`
		SELECT id::text, subject, target_summary, source_key, created_by_email, total_count, sent_count, failed_count, opened_count, created_at, finished_at
		FROM email_batches WHERE id=$1::uuid
	`, id).Scan(&batch.ID, &batch.Subject, &batch.TargetSummary, &batch.SourceKey, &batch.CreatedByEmail, &batch.TotalCount, &batch.SentCount, &batch.FailedCount, &batch.OpenedCount, &batch.CreatedAt, &batch.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return EmailBatch{}, nil, ErrNotFound
	}
	if err != nil {
		return EmailBatch{}, nil, err
	}
	rows, err := s.db.Query(`
		SELECT id::text, batch_id::text, email, status, error_message, opened, opened_at, created_at, sent_at
		FROM email_recipients WHERE batch_id=$1::uuid ORDER BY created_at ASC
	`, id)
	if err != nil {
		return EmailBatch{}, nil, err
	}
	defer rows.Close()
	recipients := []EmailRecipient{}
	for rows.Next() {
		var item EmailRecipient
		if err := rows.Scan(&item.ID, &item.BatchID, &item.Email, &item.Status, &item.ErrorMessage, &item.Opened, &item.OpenedAt, &item.CreatedAt, &item.SentAt); err != nil {
			return EmailBatch{}, nil, err
		}
		recipients = append(recipients, item)
	}
	return batch, recipients, rows.Err()
}

// ListBatches 返回最近邮件批次。
func (s *PostgresEmailCampaignStore) ListBatches(limit int) ([]EmailBatch, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.Query(`
		SELECT id::text, subject, target_summary, source_key, created_by_email, total_count, sent_count, failed_count, opened_count, created_at, finished_at
		FROM email_batches ORDER BY created_at DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []EmailBatch{}
	for rows.Next() {
		var item EmailBatch
		if err := rows.Scan(&item.ID, &item.Subject, &item.TargetSummary, &item.SourceKey, &item.CreatedByEmail, &item.TotalCount, &item.SentCount, &item.FailedCount, &item.OpenedCount, &item.CreatedAt, &item.FinishedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// MarkRecipientSent 标记收件人发送成功。
func (s *PostgresEmailCampaignStore) MarkRecipientSent(id string) error {
	_, err := s.db.Exec(`UPDATE email_recipients SET status='sent', sent_at=now(), error_message='' WHERE id=$1::uuid`, id)
	if err != nil {
		return err
	}
	return s.recountRecipientBatch(id)
}

// MarkRecipientFailed 标记收件人发送失败。
func (s *PostgresEmailCampaignStore) MarkRecipientFailed(id string, message string) error {
	_, err := s.db.Exec(`UPDATE email_recipients SET status='failed', error_message=$2 WHERE id=$1::uuid`, id, strings.TrimSpace(message))
	if err != nil {
		return err
	}
	return s.recountRecipientBatch(id)
}

// MarkRecipientOpened 标记收件人已打开追踪图片。
func (s *PostgresEmailCampaignStore) MarkRecipientOpened(id string) error {
	_, err := s.db.Exec(`UPDATE email_recipients SET opened=true, opened_at=COALESCE(opened_at, now()) WHERE id=$1::uuid`, id)
	if err != nil {
		return err
	}
	return s.recountRecipientBatch(id)
}

// SourceKeyExists 判断自动任务幂等键是否已存在。
func (s *PostgresEmailCampaignStore) SourceKeyExists(sourceKey string) (bool, error) {
	if strings.TrimSpace(sourceKey) == "" {
		return false, nil
	}
	var exists bool
	err := s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM email_batches WHERE source_key=$1)`, sourceKey).Scan(&exists)
	return exists, err
}

// FindTargetUsers 按标签、流程卡点和注册日期筛选用户。
func (s *PostgresEmailCampaignStore) FindTargetUsers(filter EmailTargetFilter) ([]EmailTargetUser, error) {
	args := []any{}
	where := []string{"true"}
	if filter.CreatedDay != "" {
		args = append(args, filter.CreatedDay)
		where = append(where, "u.created_at::date = $"+intString(len(args))+"::date")
	}
	rows, err := s.db.Query(`
		SELECT
			u.email,
			COALESCE(u.notification_profile, '{}'::jsonb),
			EXISTS (SELECT 1 FROM local_agents la WHERE la.user_id = u.id AND la.bind_status = 'active'),
			EXISTS (SELECT 1 FROM user_ai_configs ai WHERE ai.user_id = u.id AND ai.enabled = true AND COALESCE(ai.base_url, '') <> '' AND COALESCE(ai.model, '') <> '' AND COALESCE(ai.api_key_encrypted, '') <> ''),
			EXISTS (SELECT 1 FROM platform_accounts pa WHERE pa.user_id = u.id),
			EXISTS (SELECT 1 FROM positions p WHERE p.user_id = u.id),
			EXISTS (SELECT 1 FROM task_runs tr WHERE tr.user_id = u.id AND (tr.greeted_count > 0 OR tr.daily_greeted_count > 0)),
			EXISTS (SELECT 1 FROM payment_orders po WHERE po.user_id = u.id AND po.status = 'paid')
		FROM users u
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY u.created_at DESC
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []EmailTargetUser{}
	tagSet, flowSet := stringSet(filter.Tags), stringSet(filter.FlowSteps)
	for rows.Next() {
		var email string
		var rawProfile []byte
		var hasAgent, hasAI, hasPlatformAccount, hasPosition, hasGreeted, hasPaid bool
		if err := rows.Scan(&email, &rawProfile, &hasAgent, &hasAI, &hasPlatformAccount, &hasPosition, &hasGreeted, &hasPaid); err != nil {
			return nil, err
		}
		flow := buildAdminUserFlow(hasAgent, hasAI, hasPlatformAccount, hasPosition, hasGreeted, hasPaid)
		tags := profileTagsFromRaw(rawProfile)
		if len(tagSet) == 0 && len(flowSet) == 0 || tagSetMatch(tagSet, tags) || flowSet[flowKey(flow)] {
			result = append(result, EmailTargetUser{Email: email, Flow: flow, Tags: tags})
		}
	}
	return result, rows.Err()
}

// recountRecipientBatch 根据收件人 ID 重算所属批次统计。
func (s *PostgresEmailCampaignStore) recountRecipientBatch(recipientID string) error {
	_, err := s.db.Exec(`
		UPDATE email_batches b SET
			sent_count = c.sent_count,
			failed_count = c.failed_count,
			opened_count = c.opened_count,
			finished_at = CASE WHEN c.done_count >= b.total_count THEN COALESCE(b.finished_at, now()) ELSE b.finished_at END
		FROM (
			SELECT batch_id,
				COUNT(*) FILTER (WHERE status='sent') AS sent_count,
				COUNT(*) FILTER (WHERE status='failed') AS failed_count,
				COUNT(*) FILTER (WHERE opened) AS opened_count,
				COUNT(*) FILTER (WHERE status IN ('sent', 'failed')) AS done_count
			FROM email_recipients
			WHERE batch_id = (SELECT batch_id FROM email_recipients WHERE id=$1::uuid)
			GROUP BY batch_id
		) c
		WHERE b.id = c.batch_id
	`, recipientID)
	return err
}

// profileTagsFromRaw 从通知画像 JSON 里生成可筛选标签。
func profileTagsFromRaw(raw []byte) []string {
	var profile NotificationProfile
	_ = json.Unmarshal(raw, &profile)
	tags := []string{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			tags = append(tags, value)
		}
	}
	add(profile.UserType)
	add(profile.Gender)
	add(profile.OS)
	add(profile.Browser)
	for _, item := range profile.Platforms {
		add(item)
	}
	return tags
}

func stringSet(items []string) map[string]bool {
	set := map[string]bool{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			set[item] = true
		}
	}
	return set
}

func tagSetMatch(set map[string]bool, tags []string) bool {
	for _, tag := range tags {
		if set[tag] {
			return true
		}
	}
	return false
}

func flowKey(flow AdminUserFlow) string {
	if flow.Completed {
		return "completed"
	}
	for _, step := range flow.Steps {
		if !step.Done {
			return step.Key
		}
	}
	return "completed"
}
