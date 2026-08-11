// Package httpapi 本文件负责候选人岗位评估、推荐报告任务、推荐快照和公开沟通记录的 PostgreSQL 存储。
package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// recommendationGenerationSource 表示推荐 AI 使用的完整、可哈希业务快照。
type recommendationGenerationSource struct {
	Review        CandidatePositionReview              `json:"review"`
	Candidate     RecommendationCandidateSnapshot      `json:"candidate"`
	Position      RecommendationPositionSnapshot       `json:"position"`
	Conditions    []RecommendationConditionSnapshot    `json:"conditions"`
	Conversations []RecommendationConversationSnapshot `json:"conversations"`
	CreatorEmail  string                               `json:"-"`
	InputHash     string                               `json:"-"`
	SourceCutoff  time.Time                            `json:"-"`
}

// EnsureCandidatePositionReview 幂等创建或读取候选人与岗位的长期评估主体。
func (s *PostgresAutoReplyStore) EnsureCandidatePositionReview(ctx context.Context, tenantID, candidateID, positionID string) (CandidatePositionReview, error) {
	var item CandidatePositionReview
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO candidate_position_reviews (tenant_id, candidate_id, position_id)
		VALUES ($1,$2,$3)
		ON CONFLICT (tenant_id, candidate_id, position_id)
		DO UPDATE SET updated_at=candidate_position_reviews.updated_at
		RETURNING id, tenant_id, candidate_id, position_id, status,
			conditions_initialized_at, qualified_at,
			COALESCE(current_recommendation_id::text,''), created_at, updated_at
	`, tenantID, candidateID, positionID).Scan(
		&item.ID, &item.TenantID, &item.CandidateID, &item.PositionID, &item.Status,
		&item.ConditionsInitializedAt, &item.QualifiedAt, &item.CurrentRecommendationID,
		&item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}

// EnqueueRecommendationIfQualified 检查全部关键条件，并为首次或新版输入创建唯一推荐任务。
func (s *PostgresAutoReplyStore) EnqueueRecommendationIfQualified(ctx context.Context, tenantID, candidateID, positionID string) (bool, error) {
	if strings.TrimSpace(candidateID) == "" || strings.TrimSpace(positionID) == "" {
		return false, nil
	}
	review, err := s.EnsureCandidatePositionReview(ctx, tenantID, candidateID, positionID)
	if err != nil {
		return false, err
	}
	var configuredCount, missingConfigured, pendingRequired, unmatchedRequired int
	err = s.db.QueryRowContext(ctx, `
		WITH configured AS (
			SELECT id, condition_type
			FROM position_reply_conditions
			WHERE tenant_id=$1 AND position_id=$2 AND enabled=true
		), active_items AS (
			SELECT * FROM candidate_confirmation_items
			WHERE review_id=$3 AND archived_at IS NULL
		)
		SELECT
			(SELECT COUNT(*) FROM configured),
			(SELECT COUNT(*) FROM configured configured_item
			 WHERE NOT EXISTS (
				SELECT 1 FROM active_items item
				WHERE item.position_condition_id=configured_item.id
			 )),
			(SELECT COUNT(*) FROM active_items
			 WHERE item_type IN ('required','confirm') AND status='pending'),
			(SELECT COUNT(*) FROM active_items
			 WHERE item_type IN ('required','confirm') AND status='unmatched')
	`, tenantID, positionID, review.ID).Scan(&configuredCount, &missingConfigured, &pendingRequired, &unmatchedRequired)
	if err != nil {
		return false, err
	}
	status := "pending"
	if unmatchedRequired > 0 {
		status = "unmatched"
	}
	if configuredCount > 0 && missingConfigured == 0 && pendingRequired == 0 && unmatchedRequired == 0 {
		status = "qualified"
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE candidate_position_reviews
		SET status=$2,
			conditions_initialized_at=CASE WHEN $3>0 AND $4=0 THEN COALESCE(conditions_initialized_at,now()) ELSE conditions_initialized_at END,
			qualified_at=CASE WHEN $2='qualified' THEN COALESCE(qualified_at,now()) ELSE qualified_at END,
			updated_at=now()
		WHERE id=$1
	`, review.ID, status, configuredCount, missingConfigured)
	if err != nil || status != "qualified" {
		return false, err
	}
	source, err := s.LoadRecommendationGenerationSource(ctx, review.ID)
	if err != nil {
		return false, err
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO candidate_recommendation_jobs (
			tenant_id, review_id, candidate_id, position_id, input_hash
		) VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (review_id, input_hash) DO NOTHING
	`, tenantID, review.ID, candidateID, positionID, source.InputHash)
	if err != nil {
		return false, err
	}
	created, err := result.RowsAffected()
	return created > 0, err
}

// LoadRecommendationGenerationSource 读取生成报告所需的简历、岗位、公司、条件和截止时间内聊天快照。
func (s *PostgresAutoReplyStore) LoadRecommendationGenerationSource(ctx context.Context, reviewID string) (recommendationGenerationSource, error) {
	var source recommendationGenerationSource
	var companyID, positionDescription, autoReplyDescription string
	err := s.db.QueryRowContext(ctx, `
		SELECT review.id, review.tenant_id, review.candidate_id, review.position_id,
			review.status, review.conditions_initialized_at, review.qualified_at,
			COALESCE(review.current_recommendation_id::text,''), review.created_at, review.updated_at,
			p.name, p.platform_id, p.description, COALESCE(NULLIF(u.email,''),tenant.owner_email),
			COALESCE(config.company_profile_id::text,''), COALESCE(config.position_description,'')
		FROM candidate_position_reviews review
		JOIN positions p ON p.id=review.position_id
		JOIN users u ON u.id=p.user_id
		JOIN tenants tenant ON tenant.id=review.tenant_id
		LEFT JOIN position_auto_reply_configs config ON config.position_id=p.id
		WHERE review.id=$1
	`, reviewID).Scan(
		&source.Review.ID, &source.Review.TenantID, &source.Review.CandidateID,
		&source.Review.PositionID, &source.Review.Status, &source.Review.ConditionsInitializedAt,
		&source.Review.QualifiedAt, &source.Review.CurrentRecommendationID,
		&source.Review.CreatedAt, &source.Review.UpdatedAt,
		&source.Position.Name, &source.Position.PlatformID, &positionDescription,
		&source.CreatorEmail, &companyID, &autoReplyDescription,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return source, ErrNotFound
	}
	if err != nil {
		return source, err
	}
	source.Position.ID = source.Review.PositionID
	source.Position.Description = firstNonEmpty(autoReplyDescription, positionDescription)
	if companyID != "" {
		err = s.db.QueryRowContext(ctx, `
			SELECT name, address, contact, overview, extra_info
			FROM tenant_company_profiles WHERE tenant_id=$1 AND id=$2
		`, source.Review.TenantID, companyID).Scan(
			&source.Position.CompanyName, &source.Position.Address, &source.Position.Contact,
			&source.Position.Overview, &source.Position.ExtraInfo,
		)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return source, err
		}
	}
	candidate, err := s.GetAutoReplyCandidateProfile(ctx, source.Review.TenantID, source.Review.CandidateID)
	if err != nil {
		return source, err
	}
	source.Candidate = recommendationCandidateSnapshot(candidate)
	source.Conditions, err = s.listRecommendationConditions(ctx, source.Review.ID)
	if err != nil {
		return source, err
	}
	source.SourceCutoff = time.Now().UTC()
	source.Conversations, err = s.listRecommendationConversations(ctx, source.Review.TenantID, source.Review.CandidateID, source.Review.PositionID, source.SourceCutoff)
	if err != nil {
		return source, err
	}
	hashInput := struct {
		Candidate     RecommendationCandidateSnapshot      `json:"candidate"`
		Position      RecommendationPositionSnapshot       `json:"position"`
		Conditions    []RecommendationConditionSnapshot    `json:"conditions"`
		Conversations []RecommendationConversationSnapshot `json:"conversations"`
	}{source.Candidate, source.Position, source.Conditions, source.Conversations}
	encoded, err := json.Marshal(hashInput)
	if err != nil {
		return source, fmt.Errorf("编码推荐输入失败：%w", err)
	}
	sum := sha256.Sum256(encoded)
	source.InputHash = hex.EncodeToString(sum[:])
	return source, nil
}

// recommendationCandidateSnapshot 把正式简历转换为公开报告使用的稳定字段快照。
func recommendationCandidateSnapshot(item PositionCandidate) RecommendationCandidateSnapshot {
	return RecommendationCandidateSnapshot{
		ID: item.ID, Name: item.CandidateName, AvatarURL: item.AvatarURL, Gender: item.Gender,
		BirthYM: item.BirthYM, Phone: item.Phone, Email: item.Email, Wechat: item.Wechat,
		WorkRegion: item.WorkRegion, WorkYears: item.WorkYears, EducationLevel: item.EducationLevel,
		ExpectedPosition: item.ExpectedPosition, ExpectedSalaryMin: item.ExpectedSalaryMin,
		ExpectedSalaryMax: item.ExpectedSalaryMax, WorkStatus: item.WorkStatus,
		OnlineStatus: item.OnlineStatus, PersonalDescription: item.PersonalDescription,
		BasicInfo: item.BasicInfo, WorkExperiences: safeSlice(item.WorkExperiences),
		Educations: safeSlice(item.Educations), Certificates: safeSlice(item.Certificates),
		Honors: safeSlice(item.Honors), ProjectExperiences: safeSlice(item.ProjectExperiences),
		CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

// listRecommendationConditions 返回当前评估全部未归档条件及其可审计依据。
func (s *PostgresAutoReplyStore) listRecommendationConditions(ctx context.Context, reviewID string) ([]RecommendationConditionSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, item_type, content, status, status_reason, evidence_text, source_ref
		FROM candidate_confirmation_items
		WHERE review_id=$1 AND archived_at IS NULL
		ORDER BY CASE item_type WHEN 'required' THEN 0 WHEN 'confirm' THEN 1 ELSE 2 END,
			created_at, id
	`, reviewID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]RecommendationConditionSnapshot, 0)
	for rows.Next() {
		var item RecommendationConditionSnapshot
		var evidence, sourceRef string
		if err = rows.Scan(&item.ID, &item.ItemType, &item.Content, &item.Status,
			&item.StatusReason, &evidence, &sourceRef); err != nil {
			return nil, err
		}
		item.Evidence = cleanRecommendationEvidence(evidence, sourceRef)
		items = append(items, item)
	}
	return items, rows.Err()
}

// cleanRecommendationEvidence 清理条件依据并去除空白重复值。
func cleanRecommendationEvidence(values ...string) []string {
	seen := make(map[string]struct{}, len(values))
	items := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		items = append(items, value)
	}
	return items
}

// listRecommendationConversations 返回指定候选人岗位在数据截止时间前的真实双方沟通。
func (s *PostgresAutoReplyStore) listRecommendationConversations(ctx context.Context, tenantID, candidateID, positionID string, cutoff time.Time) ([]RecommendationConversationSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, platform_id, page_position_text, candidate_name, created_at, updated_at
		FROM candidate_conversations
		WHERE tenant_id=$1 AND candidate_id=$2 AND position_id=$3 AND created_at<=$4
		ORDER BY created_at, id
	`, tenantID, candidateID, positionID, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]RecommendationConversationSnapshot, 0)
	for rows.Next() {
		var item RecommendationConversationSnapshot
		if err = rows.Scan(&item.ID, &item.PlatformID, &item.PositionText,
			&item.CandidateName, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Messages, err = s.listRecommendationMessages(ctx, tenantID, item.ID, cutoff)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// listRecommendationMessages 返回一段会话中的候选人和HR真实消息，不公开系统消息。
func (s *PostgresAutoReplyStore) listRecommendationMessages(ctx context.Context, tenantID, conversationID string, cutoff time.Time) ([]RecommendationMessageSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, direction, message_type, text_content, sender_name,
			platform_sent_at, created_at
		FROM candidate_messages
		WHERE tenant_id=$1 AND conversation_id=$2
			AND direction IN ('candidate','self') AND created_at<=$3
		ORDER BY COALESCE(platform_sent_at,created_at), created_at, id
	`, tenantID, conversationID, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]RecommendationMessageSnapshot, 0)
	for rows.Next() {
		var item RecommendationMessageSnapshot
		if err = rows.Scan(&item.ID, &item.Direction, &item.MessageType, &item.TextContent,
			&item.SenderName, &item.PlatformSentAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ClaimRecommendationJob 原子领取一条到期任务，并回收超过十分钟未完成的旧任务。
func (s *PostgresAutoReplyStore) ClaimRecommendationJob(ctx context.Context) (CandidateRecommendationJob, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CandidateRecommendationJob{}, false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `
		UPDATE candidate_recommendation_jobs
		SET status='pending', next_attempt_at=now(), locked_at=NULL, updated_at=now(),
			last_error=CASE WHEN last_error='' THEN '上一次执行中断，已经自动重新排队' ELSE last_error END
		WHERE status='running' AND locked_at<now()-INTERVAL '10 minutes'
	`); err != nil {
		return CandidateRecommendationJob{}, false, err
	}
	var item CandidateRecommendationJob
	err = tx.QueryRowContext(ctx, `
		SELECT id, tenant_id, review_id, candidate_id, position_id, input_hash,
			status, attempt_count, next_attempt_at, last_error
		FROM candidate_recommendation_jobs
		WHERE status='pending' AND next_attempt_at<=now()
		ORDER BY created_at, id
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`).Scan(&item.ID, &item.TenantID, &item.ReviewID, &item.CandidateID,
		&item.PositionID, &item.InputHash, &item.Status, &item.AttemptCount,
		&item.NextAttemptAt, &item.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return CandidateRecommendationJob{}, false, nil
	}
	if err != nil {
		return CandidateRecommendationJob{}, false, err
	}
	err = tx.QueryRowContext(ctx, `
		UPDATE candidate_recommendation_jobs
		SET status='running', attempt_count=attempt_count+1, locked_at=now(), updated_at=now()
		WHERE id=$1
		RETURNING status, attempt_count
	`, item.ID).Scan(&item.Status, &item.AttemptCount)
	if err != nil {
		return CandidateRecommendationJob{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE candidate_position_reviews SET status='generating', updated_at=now() WHERE id=$1`, item.ReviewID); err != nil {
		return CandidateRecommendationJob{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return CandidateRecommendationJob{}, false, err
	}
	return item, true, nil
}

// CompleteRecommendationJob 保存不可变推荐快照并把当前评估指向新版本。
func (s *PostgresAutoReplyStore) CompleteRecommendationJob(ctx context.Context, job CandidateRecommendationJob, publicID, model string, tokenUsage int, report CandidateRecommendationReport) (CandidateRecommendation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CandidateRecommendation{}, err
	}
	defer tx.Rollback()
	reportJSON, err := json.Marshal(report)
	if err != nil {
		return CandidateRecommendation{}, err
	}
	var version int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM candidate_recommendations WHERE review_id=$1`, job.ReviewID).Scan(&version); err != nil {
		return CandidateRecommendation{}, err
	}
	var item CandidateRecommendation
	var storedReport []byte
	err = tx.QueryRowContext(ctx, `
		INSERT INTO candidate_recommendations (
			public_id, tenant_id, review_id, candidate_id, position_id, version,
			input_hash, match_score, recommendation_level, summary, report_data,
			model, token_usage, source_cutoff_at, generated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12,$13,$14,$15)
		ON CONFLICT (review_id, input_hash) DO UPDATE SET updated_at=candidate_recommendations.updated_at
		RETURNING id, public_id, tenant_id, review_id, candidate_id, position_id, version,
			input_hash, status, COALESCE(match_score,0), recommendation_level, summary,
			report_data, model, token_usage, share_enabled, notification_status,
			notification_error, notified_at, source_cutoff_at, generated_at, created_at, updated_at
	`, publicID, job.TenantID, job.ReviewID, job.CandidateID, job.PositionID, version,
		job.InputHash, report.MatchScore, report.RecommendationLevel, report.ExecutiveSummary,
		string(reportJSON), model, tokenUsage, report.SourceCutoffAt, report.GeneratedAt).Scan(
		&item.ID, &item.PublicID, &item.TenantID, &item.ReviewID, &item.CandidateID,
		&item.PositionID, &item.Version, &item.InputHash, &item.Status, &item.MatchScore,
		&item.RecommendationLevel, &item.Summary, &storedReport, &item.Model,
		&item.TokenUsage, &item.ShareEnabled, &item.NotificationStatus,
		&item.NotificationError, &item.NotifiedAt, &item.SourceCutoffAt,
		&item.GeneratedAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return CandidateRecommendation{}, err
	}
	if err = json.Unmarshal(storedReport, &item.Report); err != nil {
		return CandidateRecommendation{}, err
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE candidate_recommendations
		SET status='superseded', updated_at=now()
		WHERE review_id=$1 AND id<>$2 AND status='completed'
	`, job.ReviewID, item.ID); err != nil {
		return CandidateRecommendation{}, err
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE candidate_position_reviews
		SET status='recommended', current_recommendation_id=$2, updated_at=now()
		WHERE id=$1
	`, job.ReviewID, item.ID); err != nil {
		return CandidateRecommendation{}, err
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE candidate_recommendation_jobs
		SET status='completed', locked_at=NULL, last_error='', updated_at=now()
		WHERE id=$1
	`, job.ID); err != nil {
		return CandidateRecommendation{}, err
	}
	if err = tx.Commit(); err != nil {
		return CandidateRecommendation{}, err
	}
	return item, nil
}

// FailRecommendationJob 保存失败并按最多三次规则重新排队。
func (s *PostgresAutoReplyStore) FailRecommendationJob(ctx context.Context, job CandidateRecommendationJob, cause error) error {
	message := "推荐报告生成失败"
	if cause != nil {
		message = truncateAutoReplyText(cause.Error(), 1000)
	}
	if job.AttemptCount < 3 {
		delay := time.Duration(job.AttemptCount*job.AttemptCount) * time.Minute
		_, err := s.db.ExecContext(ctx, `
			UPDATE candidate_recommendation_jobs
			SET status='pending', next_attempt_at=$2, locked_at=NULL, last_error=$3, updated_at=now()
			WHERE id=$1
		`, job.ID, time.Now().UTC().Add(delay), message)
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `
		UPDATE candidate_recommendation_jobs
		SET status='failed', locked_at=NULL, last_error=$2, updated_at=now()
		WHERE id=$1
	`, job.ID, message); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE candidate_position_reviews SET status='failed', updated_at=now() WHERE id=$1`, job.ReviewID); err != nil {
		return err
	}
	return tx.Commit()
}

// RefreshRecommendationJob 把已过时任务标记完成，并按最新业务快照重新排队。
func (s *PostgresAutoReplyStore) RefreshRecommendationJob(ctx context.Context, job CandidateRecommendationJob, source recommendationGenerationSource) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `
		UPDATE candidate_recommendation_jobs
		SET status='completed', locked_at=NULL, last_error='生成前发现候选人信息已有更新，已改用最新内容', updated_at=now()
		WHERE id=$1
	`, job.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO candidate_recommendation_jobs (
			tenant_id, review_id, candidate_id, position_id, input_hash
		) VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (review_id, input_hash) DO NOTHING
	`, job.TenantID, job.ReviewID, job.CandidateID, job.PositionID, source.InputHash); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE candidate_position_reviews SET status='qualified', updated_at=now() WHERE id=$1
	`, job.ReviewID); err != nil {
		return err
	}
	return tx.Commit()
}

// ListCandidateRecommendations 返回候选人在当前团队中的全部推荐版本。
func (s *PostgresAutoReplyStore) ListCandidateRecommendations(ctx context.Context, tenantID, candidateID string) ([]CandidateRecommendation, error) {
	rows, err := s.db.QueryContext(ctx, recommendationSelectSQL+`
		WHERE recommendation.tenant_id=$1 AND recommendation.candidate_id=$2
		ORDER BY recommendation.generated_at DESC, recommendation.id DESC
	`, tenantID, candidateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]CandidateRecommendation, 0)
	for rows.Next() {
		item, scanErr := scanCandidateRecommendation(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetPublicRecommendation 按不可猜测公开编号返回仍允许分享的报告。
func (s *PostgresAutoReplyStore) GetPublicRecommendation(ctx context.Context, publicID string) (CandidateRecommendation, error) {
	item, err := scanCandidateRecommendation(s.db.QueryRowContext(ctx, recommendationSelectSQL+`
		WHERE recommendation.public_id=$1 AND recommendation.share_enabled=true
	`, strings.TrimSpace(publicID)))
	if errors.Is(err, sql.ErrNoRows) {
		return CandidateRecommendation{}, ErrNotFound
	}
	return item, err
}

// RevokeRecommendationShare 永久关闭一份推荐记录的公开链接。
func (s *PostgresAutoReplyStore) RevokeRecommendationShare(ctx context.Context, tenantID, publicID string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE candidate_recommendations
		SET share_enabled=false, status='revoked', updated_at=now()
		WHERE tenant_id=$1 AND public_id=$2 AND share_enabled=true
	`, tenantID, strings.TrimSpace(publicID))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

// PublicRecommendationConversations 返回公开推荐报告数据截止时间内的真实沟通记录。
func (s *PostgresAutoReplyStore) PublicRecommendationConversations(ctx context.Context, publicID string) ([]RecommendationConversationSnapshot, error) {
	item, err := s.GetPublicRecommendation(ctx, publicID)
	if err != nil {
		return nil, err
	}
	return s.listRecommendationConversations(ctx, item.TenantID, item.CandidateID, item.PositionID, item.SourceCutoffAt)
}

// FinishRecommendationNotification 保存岗位创建人推荐邮件的最终发送结果。
func (s *PostgresAutoReplyStore) FinishRecommendationNotification(ctx context.Context, recommendationID string, sendErr error) error {
	status := "sent"
	message := ""
	if sendErr != nil {
		status = "failed"
		message = truncateAutoReplyText(sendErr.Error(), 1000)
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE candidate_recommendations
		SET notification_status=$2, notification_error=$3,
			notified_at=CASE WHEN $2='sent' THEN now() ELSE notified_at END,
			updated_at=now()
		WHERE id=$1 AND notification_status<>'sent'
	`, recommendationID, status, message)
	return err
}

// ClaimRecommendationNotification 原子领取一份待发送邮件，并回收中断超过十分钟的发送任务。
func (s *PostgresAutoReplyStore) ClaimRecommendationNotification(ctx context.Context) (CandidateRecommendation, string, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CandidateRecommendation{}, "", false, err
	}
	defer tx.Rollback()
	var recommendationID, recipient string
	err = tx.QueryRowContext(ctx, `
		SELECT recommendation.id,
			COALESCE(NULLIF(creator.email,''),tenant.owner_email)
		FROM candidate_recommendations recommendation
		JOIN positions position ON position.id=recommendation.position_id
		JOIN users creator ON creator.id=position.user_id
		JOIN tenants tenant ON tenant.id=recommendation.tenant_id
		WHERE recommendation.notification_status='pending'
			OR (recommendation.notification_status='sending' AND recommendation.updated_at<now()-INTERVAL '10 minutes')
		ORDER BY recommendation.generated_at, recommendation.id
		FOR UPDATE OF recommendation SKIP LOCKED
		LIMIT 1
	`).Scan(&recommendationID, &recipient)
	if errors.Is(err, sql.ErrNoRows) {
		return CandidateRecommendation{}, "", false, nil
	}
	if err != nil {
		return CandidateRecommendation{}, "", false, err
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE candidate_recommendations
		SET notification_status='sending', notification_error='', updated_at=now()
		WHERE id=$1
	`, recommendationID); err != nil {
		return CandidateRecommendation{}, "", false, err
	}
	item, err := scanCandidateRecommendation(tx.QueryRowContext(ctx, recommendationSelectSQL+`
		WHERE recommendation.id=$1
	`, recommendationID))
	if err != nil {
		return CandidateRecommendation{}, "", false, err
	}
	if err = tx.Commit(); err != nil {
		return CandidateRecommendation{}, "", false, err
	}
	return item, strings.TrimSpace(recipient), true, nil
}

const recommendationSelectSQL = `
	SELECT recommendation.id, recommendation.public_id, recommendation.tenant_id,
		recommendation.review_id, recommendation.candidate_id, recommendation.position_id,
		recommendation.version, recommendation.input_hash, recommendation.status,
		COALESCE(recommendation.match_score,0), recommendation.recommendation_level,
		recommendation.summary, recommendation.report_data, recommendation.model,
		recommendation.token_usage, recommendation.share_enabled,
		recommendation.notification_status, recommendation.notification_error,
		recommendation.notified_at, recommendation.source_cutoff_at,
		recommendation.generated_at, recommendation.created_at, recommendation.updated_at
	FROM candidate_recommendations recommendation
`

// scanCandidateRecommendation 从数据库结果解析强类型推荐记录。
func scanCandidateRecommendation(scanner candidateScanner) (CandidateRecommendation, error) {
	var item CandidateRecommendation
	var report []byte
	err := scanner.Scan(
		&item.ID, &item.PublicID, &item.TenantID, &item.ReviewID, &item.CandidateID,
		&item.PositionID, &item.Version, &item.InputHash, &item.Status, &item.MatchScore,
		&item.RecommendationLevel, &item.Summary, &report, &item.Model, &item.TokenUsage,
		&item.ShareEnabled, &item.NotificationStatus, &item.NotificationError,
		&item.NotifiedAt, &item.SourceCutoffAt, &item.GeneratedAt, &item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return item, err
	}
	if err = json.Unmarshal(report, &item.Report); err != nil {
		return item, fmt.Errorf("解析推荐报告失败：%w", err)
	}
	return item, nil
}
