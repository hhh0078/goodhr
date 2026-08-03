// 本文件负责候选人三表模型的 PostgreSQL 存储实现。
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

// PostgresCandidateStore 使用 PostgreSQL 持久化候选人主体、触达和事件。
type PostgresCandidateStore struct {
	db *sql.DB
}

// NewPostgresCandidateStore 创建 PostgreSQL 候选人存储。
func NewPostgresCandidateStore(db *sql.DB) *PostgresCandidateStore {
	return &PostgresCandidateStore{db: db}
}

// SaveCandidateProfile 新增或更新候选人主体。
// item 为候选人简历字段，返回保存后的候选人主体。
func (s *PostgresCandidateStore) SaveCandidateProfile(item CandidateProfileInput) (PositionCandidate, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	userID, err := ensureUserID(ctx, s.db, item.UserEmail)
	if err != nil {
		return PositionCandidate{}, err
	}
	tenantID, err := userTenantID(ctx, s.db, userID)
	if err != nil {
		return PositionCandidate{}, err
	}
	key := candidateIdentityKey(item)
	item.Gender = strings.TrimSpace(item.Gender)
	if item.Gender != "" && item.Gender != "男" && item.Gender != "女" {
		return PositionCandidate{}, fmt.Errorf("候选人性别只支持男、女或空值")
	}
	item.BirthYMPrecision = strings.TrimSpace(item.BirthYMPrecision)
	if item.BirthYMPrecision != "" && item.BirthYMPrecision != "month" && item.BirthYMPrecision != "year_estimated" {
		return PositionCandidate{}, fmt.Errorf("候选人出生年月精度不支持")
	}
	item.NormalizedPhone = normalizeCandidatePhone(firstNonEmpty(item.NormalizedPhone, item.Phone))
	if strings.TrimSpace(item.CandidateID) != "" {
		return updateCandidateProfileByID(ctx, s.db, tenantID, item)
	}
	var saved PositionCandidate
	err = s.db.QueryRowContext(
		ctx,
		`
		INSERT INTO candidate_profiles (
			tenant_id, created_by_user_id, source_platform_id, source_platform_candidate_id,
			candidate_name, birth_ym, phone, email, work_region, work_years,
			expected_salary_min, expected_salary_max, basic_info, education_level,
			expected_position, online_status, personal_description, work_status,
			raw_text, work_experiences, educations, certificates, honors,
			project_experiences, colleague_communications,
			ai_detail_reason, ai_detail_score, ai_greet_reason, ai_greet_score, first_seen_at,
			gender, birth_ym_precision, normalized_phone
		)
		VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,
			$16,$17,$18,$19,$20::jsonb,$21::jsonb,$22::jsonb,$23::jsonb,
			$24::jsonb,$25::jsonb,$26,$27,$28,$29,$30,$31,$32,$33
		)
		ON CONFLICT (tenant_id, source_platform_id, source_platform_candidate_id)
		DO UPDATE SET
			candidate_name = EXCLUDED.candidate_name,
			birth_ym = EXCLUDED.birth_ym,
			phone = EXCLUDED.phone,
			email = EXCLUDED.email,
			work_region = EXCLUDED.work_region,
			work_years = EXCLUDED.work_years,
			expected_salary_min = EXCLUDED.expected_salary_min,
			expected_salary_max = EXCLUDED.expected_salary_max,
			basic_info = EXCLUDED.basic_info,
			education_level = EXCLUDED.education_level,
			expected_position = EXCLUDED.expected_position,
			online_status = EXCLUDED.online_status,
			personal_description = EXCLUDED.personal_description,
			work_status = EXCLUDED.work_status,
			raw_text = EXCLUDED.raw_text,
			work_experiences = EXCLUDED.work_experiences,
			educations = EXCLUDED.educations,
			certificates = EXCLUDED.certificates,
			honors = EXCLUDED.honors,
			project_experiences = EXCLUDED.project_experiences,
			colleague_communications = EXCLUDED.colleague_communications,
			ai_detail_reason = EXCLUDED.ai_detail_reason,
			ai_detail_score = EXCLUDED.ai_detail_score,
			ai_greet_reason = EXCLUDED.ai_greet_reason,
			ai_greet_score = EXCLUDED.ai_greet_score,
			gender = CASE WHEN EXCLUDED.gender='' THEN candidate_profiles.gender ELSE EXCLUDED.gender END,
			birth_ym_precision = CASE WHEN EXCLUDED.birth_ym_precision='' THEN candidate_profiles.birth_ym_precision ELSE EXCLUDED.birth_ym_precision END,
			normalized_phone = CASE WHEN EXCLUDED.normalized_phone='' THEN candidate_profiles.normalized_phone ELSE EXCLUDED.normalized_phone END,
			first_seen_at = COALESCE(candidate_profiles.first_seen_at, EXCLUDED.first_seen_at),
			updated_at = now()
		RETURNING
			id, source_platform_id, source_platform_candidate_id, candidate_name, birth_ym,
			phone, email, work_region, work_years, expected_salary_min, expected_salary_max,
			basic_info, education_level, expected_position, online_status, personal_description,
			work_status, raw_text, work_experiences, educations, certificates, honors,
			project_experiences, colleague_communications, ai_detail_reason, ai_detail_score,
			ai_greet_reason, ai_greet_score, first_seen_at, created_at, updated_at,
			gender, birth_ym_precision, normalized_phone
		`,
		tenantID,
		userID,
		item.PlatformID,
		key,
		item.CandidateName,
		item.BirthYM,
		item.Phone,
		item.Email,
		item.WorkRegion,
		item.WorkYears,
		item.ExpectedSalaryMin,
		item.ExpectedSalaryMax,
		item.BasicInfo,
		item.EducationLevel,
		item.ExpectedPosition,
		item.OnlineStatus,
		item.PersonalDescription,
		item.WorkStatus,
		item.RawText,
		string(toJSONB(item.WorkExperiences)),
		string(toJSONB(item.Educations)),
		string(toJSONB(item.Certificates)),
		string(toJSONB(item.Honors)),
		string(toJSONB(item.ProjectExperiences)),
		string(toJSONB(item.Communications)),
		item.AIDetailReason,
		item.AIDetailScore,
		item.AIGreetReason,
		item.AIGreetScore,
		item.FirstSeenAt,
		item.Gender,
		item.BirthYMPrecision,
		item.NormalizedPhone,
	).Scan(
		&saved.ID,
		&saved.PlatformID,
		&saved.PlatformCandidateID,
		&saved.CandidateName,
		&saved.BirthYM,
		&saved.Phone,
		&saved.Email,
		&saved.WorkRegion,
		&saved.WorkYears,
		&saved.ExpectedSalaryMin,
		&saved.ExpectedSalaryMax,
		&saved.BasicInfo,
		&saved.EducationLevel,
		&saved.ExpectedPosition,
		&saved.OnlineStatus,
		&saved.PersonalDescription,
		&saved.WorkStatus,
		&saved.RawText,
		jsonScanner(&saved.WorkExperiences),
		jsonScanner(&saved.Educations),
		jsonScanner(&saved.Certificates),
		jsonScanner(&saved.Honors),
		jsonScanner(&saved.ProjectExperiences),
		jsonScanner(&saved.Communications),
		&saved.AIDetailReason,
		&saved.AIDetailScore,
		&saved.AIGreetReason,
		&saved.AIGreetScore,
		&saved.FirstSeenAt,
		&saved.CreatedAt,
		&saved.UpdatedAt,
		&saved.Gender,
		&saved.BirthYMPrecision,
		&saved.NormalizedPhone,
	)
	if err != nil {
		return PositionCandidate{}, err
	}
	return saved, nil
}

// updateCandidateProfileByID 使用团队内手机号已解析出的正式候选人标识更新完整简历。
func updateCandidateProfileByID(ctx context.Context, db *sql.DB, tenantID string, item CandidateProfileInput) (PositionCandidate, error) {
	result, err := db.ExecContext(ctx, `
		UPDATE candidate_profiles
		SET source_platform_id=COALESCE(NULLIF($3,''),source_platform_id),
			source_platform_candidate_id=COALESCE(NULLIF($4,''),source_platform_candidate_id),
			candidate_name=COALESCE(NULLIF($5,''),candidate_name), birth_ym=COALESCE(NULLIF($6,''),birth_ym),
			phone=COALESCE(NULLIF($7,''),phone), email=COALESCE(NULLIF($8,''),email),
			work_region=COALESCE(NULLIF($9,''),work_region), work_years=COALESCE(NULLIF($10,''),work_years),
			expected_salary_min=COALESCE($11,expected_salary_min), expected_salary_max=COALESCE($12,expected_salary_max),
			basic_info=COALESCE(NULLIF($13,''),basic_info), education_level=COALESCE(NULLIF($14,''),education_level),
			expected_position=COALESCE(NULLIF($15,''),expected_position), online_status=COALESCE(NULLIF($16,''),online_status),
			personal_description=COALESCE(NULLIF($17,''),personal_description), work_status=COALESCE(NULLIF($18,''),work_status),
			raw_text=COALESCE(NULLIF($19,''),raw_text), work_experiences=$20::jsonb,
			educations=$21::jsonb, certificates=$22::jsonb, honors=$23::jsonb,
			project_experiences=$24::jsonb, colleague_communications=$25::jsonb,
			gender=COALESCE(NULLIF($26,''),gender), birth_ym_precision=COALESCE(NULLIF($27,''),birth_ym_precision),
			normalized_phone=COALESCE(NULLIF($28,''),normalized_phone), updated_at=now()
		WHERE tenant_id=$1 AND id=$2
	`, tenantID, item.CandidateID, item.PlatformID, item.PlatformCandidateID,
		strings.TrimSpace(item.CandidateName), strings.TrimSpace(item.BirthYM), strings.TrimSpace(item.Phone),
		strings.TrimSpace(item.Email), strings.TrimSpace(item.WorkRegion), strings.TrimSpace(item.WorkYears),
		item.ExpectedSalaryMin, item.ExpectedSalaryMax, strings.TrimSpace(item.BasicInfo),
		strings.TrimSpace(item.EducationLevel), strings.TrimSpace(item.ExpectedPosition), strings.TrimSpace(item.OnlineStatus),
		strings.TrimSpace(item.PersonalDescription), strings.TrimSpace(item.WorkStatus), strings.TrimSpace(item.RawText),
		string(toJSONB(item.WorkExperiences)), string(toJSONB(item.Educations)), string(toJSONB(item.Certificates)),
		string(toJSONB(item.Honors)), string(toJSONB(item.ProjectExperiences)), string(toJSONB(item.Communications)),
		item.Gender, item.BirthYMPrecision, item.NormalizedPhone)
	if err != nil {
		return PositionCandidate{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return PositionCandidate{}, err
	}
	if affected == 0 {
		return PositionCandidate{}, ErrNotFound
	}
	var createdAt, updatedAt time.Time
	if err = db.QueryRowContext(ctx, `SELECT created_at, updated_at FROM candidate_profiles WHERE tenant_id=$1 AND id=$2`, tenantID, item.CandidateID).Scan(&createdAt, &updatedAt); err != nil {
		return PositionCandidate{}, err
	}
	return PositionCandidate{
		ID: item.CandidateID, UserEmail: item.UserEmail, PlatformID: item.PlatformID,
		PlatformCandidateID: item.PlatformCandidateID, CandidateName: item.CandidateName,
		Gender: item.Gender, BirthYM: item.BirthYM, BirthYMPrecision: item.BirthYMPrecision,
		NormalizedPhone: item.NormalizedPhone, Phone: item.Phone, Email: item.Email,
		WorkRegion: item.WorkRegion, WorkYears: item.WorkYears, ExpectedSalaryMin: item.ExpectedSalaryMin,
		ExpectedSalaryMax: item.ExpectedSalaryMax, BasicInfo: item.BasicInfo,
		EducationLevel: item.EducationLevel, ExpectedPosition: item.ExpectedPosition,
		OnlineStatus: item.OnlineStatus, PersonalDescription: item.PersonalDescription,
		WorkStatus: item.WorkStatus, RawText: item.RawText, WorkExperiences: item.WorkExperiences,
		Educations: item.Educations, Certificates: item.Certificates, Honors: item.Honors,
		ProjectExperiences: item.ProjectExperiences, Communications: item.Communications,
		CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, nil
}

// UpsertCandidateEngagement 新增或更新候选人触达上下文。
// item 为候选人、岗位和账号关系，返回保存后的触达记录。
func (s *PostgresCandidateStore) UpsertCandidateEngagement(item CandidateEngagement) (CandidateEngagement, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	userID, err := ensureUserID(ctx, s.db, item.UserEmail)
	if err != nil {
		return CandidateEngagement{}, err
	}
	tenantID, err := userTenantID(ctx, s.db, userID)
	if err != nil {
		return CandidateEngagement{}, err
	}
	conflictTarget := "(tenant_id, candidate_id, position_id, platform_account_id) WHERE platform_account_id IS NOT NULL"
	if strings.TrimSpace(item.PlatformAccountID) == "" {
		conflictTarget = "(tenant_id, candidate_id, position_id) WHERE platform_account_id IS NULL"
	}
	query := `
		INSERT INTO candidate_engagements (
			tenant_id, candidate_id, position_id, platform_account_id,
			platform_id, status, first_seen_at, detail_fetched_at, greeted_at
		)
		VALUES ($1,$2,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9)
		ON CONFLICT ` + conflictTarget + `
		DO UPDATE SET
			platform_id = EXCLUDED.platform_id,
			status = EXCLUDED.status,
			first_seen_at = COALESCE(candidate_engagements.first_seen_at, EXCLUDED.first_seen_at),
			detail_fetched_at = COALESCE(EXCLUDED.detail_fetched_at, candidate_engagements.detail_fetched_at),
			greeted_at = COALESCE(EXCLUDED.greeted_at, candidate_engagements.greeted_at),
			updated_at = now()
		RETURNING id, candidate_id, COALESCE(position_id::text,''), COALESCE(platform_account_id::text,''),
			platform_id, status, first_seen_at, detail_fetched_at, greeted_at, last_event_at, created_at, updated_at
	`
	var saved CandidateEngagement
	err = s.db.QueryRowContext(
		ctx,
		query,
		tenantID,
		item.CandidateID,
		item.PositionID,
		item.PlatformAccountID,
		item.PlatformID,
		firstNonEmpty(item.Status, "created"),
		item.FirstSeenAt,
		item.DetailFetchedAt,
		item.GreetedAt,
	).Scan(
		&saved.ID,
		&saved.CandidateID,
		&saved.PositionID,
		&saved.PlatformAccountID,
		&saved.PlatformID,
		&saved.Status,
		&saved.FirstSeenAt,
		&saved.DetailFetchedAt,
		&saved.GreetedAt,
		&saved.LastEventAt,
		&saved.CreatedAt,
		&saved.UpdatedAt,
	)
	if err != nil {
		return CandidateEngagement{}, err
	}
	saved.UserEmail = item.UserEmail
	return saved, nil
}

// SaveCandidateEvent 保存候选人事件流水。
// item 为事件内容，返回保存后的事件。
func (s *PostgresCandidateStore) SaveCandidateEvent(item CandidateEvent) (CandidateEvent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tenantID, err := candidateTenantID(ctx, s.db, item.CandidateID)
	if err != nil {
		return CandidateEvent{}, err
	}
	var saved CandidateEvent
	err = s.db.QueryRowContext(
		ctx,
		`
		INSERT INTO candidate_events (
			tenant_id, candidate_id, engagement_id, position_id, platform_account_id,
			platform_id, event_type, score, reason, input_text, output_text,
			message_text, model, token_usage, metadata
		)
		VALUES ($1,$2,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,NULLIF($5,'')::uuid,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::jsonb)
		RETURNING id, candidate_id, COALESCE(engagement_id::text,''), COALESCE(position_id::text,''), COALESCE(platform_account_id::text,''),
			platform_id, event_type, score, reason, input_text, output_text, message_text, model, token_usage, metadata, created_at
		`,
		tenantID,
		item.CandidateID,
		item.EngagementID,
		item.PositionID,
		item.PlatformAccountID,
		item.PlatformID,
		item.EventType,
		item.Score,
		item.Reason,
		item.InputText,
		item.OutputText,
		item.MessageText,
		item.Model,
		item.TokenUsage,
		string(toJSONB(item.Metadata)),
	).Scan(
		&saved.ID,
		&saved.CandidateID,
		&saved.EngagementID,
		&saved.PositionID,
		&saved.PlatformAccountID,
		&saved.PlatformID,
		&saved.EventType,
		&saved.Score,
		&saved.Reason,
		&saved.InputText,
		&saved.OutputText,
		&saved.MessageText,
		&saved.Model,
		&saved.TokenUsage,
		jsonScanner(&saved.Metadata),
		&saved.CreatedAt,
	)
	if err != nil {
		return CandidateEvent{}, err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE candidate_engagements SET last_event_at=$1, updated_at=now() WHERE id=$2`, saved.CreatedAt, saved.EngagementID)
	return saved, nil
}

// UpdateCandidateEngagementStatus 更新触达上下文状态和关键时间。
// engagementID 为触达 ID，status 为目标状态，时间字段为空时不覆盖。
func (s *PostgresCandidateStore) UpdateCandidateEngagementStatus(engagementID string, status string, detailFetchedAt *time.Time, greetedAt *time.Time) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := s.db.ExecContext(
		ctx,
		`
		UPDATE candidate_engagements
		SET status = COALESCE(NULLIF($2,''), status),
			detail_fetched_at = COALESCE($3, detail_fetched_at),
			greeted_at = COALESCE($4, greeted_at),
			last_event_at = now(),
			updated_at = now()
		WHERE id = $1
		`,
		engagementID,
		status,
		detailFetchedAt,
		greetedAt,
	)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListPositionCandidates 按团队和筛选条件分页读取候选人记录。
// tenantID 为当前用户团队 ID，query 可传搜索词、岗位 ID、岗位 ID 和分页条件。
func (s *PostgresCandidateStore) ListPositionCandidates(tenantID string, query PositionCandidateQuery) (PositionCandidateListResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	page, pageSize := normalizeCandidatePage(query.Page, query.PageSize)
	where, args := buildCandidateWhere(tenantID, query)
	countSQL := "SELECT COUNT(*) FROM candidate_profiles cp WHERE " + where
	var total int
	if err := s.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return PositionCandidateListResult{}, err
	}
	offset := (page - 1) * pageSize
	listArgs := append(args, pageSize, offset)
	rows, err := s.db.QueryContext(
		ctx,
		candidateSelectSQL("WHERE "+where, candidateEngagementScope(query))+`
		ORDER BY COALESCE(latest_engagement.created_at, cp.created_at) DESC
		LIMIT $`+fmt.Sprint(len(args)+1)+`
		OFFSET $`+fmt.Sprint(len(args)+2),
		listArgs...,
	)
	if err != nil {
		return PositionCandidateListResult{}, err
	}
	defer rows.Close()
	items, err := scanCandidateRows(rows)
	if err != nil {
		return PositionCandidateListResult{}, err
	}
	return PositionCandidateListResult{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

// GetPositionCandidate 按 ID 读取当前团队内的候选人详情。
// tenantID 为当前用户团队 ID，candidateID 为候选人主体 ID，engagementID 为空时使用最近一次触达。
func (s *PostgresCandidateStore) GetPositionCandidate(tenantID string, candidateID string, engagementID string, userEmail string, isAdmin bool) (PositionCandidate, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	args := []any{tenantID, candidateID}
	whereClause := "WHERE cp.tenant_id = $1 AND cp.id::text = $2"
	engagementScope := ""
	if !isAdmin {
		args = append(args, userEmail)
		whereClause += fmt.Sprintf(" AND u.email = $%d", len(args))
	}
	if strings.TrimSpace(engagementID) != "" {
		args = append(args, strings.TrimSpace(engagementID))
		whereClause += fmt.Sprintf(" AND EXISTS (SELECT 1 FROM candidate_engagements ce_match WHERE ce_match.candidate_id = cp.id AND ce_match.id::text = $%d)", len(args))
		engagementScope = fmt.Sprintf("AND ce2.id::text = $%d", len(args))
	}
	rows, err := s.db.QueryContext(ctx, candidateSelectSQL(whereClause, engagementScope), args...)
	if err != nil {
		return PositionCandidate{}, err
	}
	defer rows.Close()
	items, err := scanCandidateRows(rows)
	if err != nil {
		return PositionCandidate{}, err
	}
	if len(items) == 0 {
		return PositionCandidate{}, ErrNotFound
	}
	events, err := s.listCandidateEvents(ctx, tenantID, candidateID, items[0].EngagementID)
	if err != nil {
		return PositionCandidate{}, err
	}
	items[0].Events = events
	return items[0], nil
}

// ListCandidateNotes 读取候选人的人工备注记录。
// tenantID 为团队 ID，candidateID 为候选人 ID，返回最新备注在前的列表。
func (s *PostgresCandidateStore) ListCandidateNotes(tenantID string, candidateID string) ([]CandidateNote, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `
SELECT id::text, candidate_id::text, message_text, COALESCE(metadata->>'author_email', ''), created_at
FROM candidate_events
WHERE tenant_id = $1 AND candidate_id::text = $2 AND event_type = 'manual_note'
ORDER BY created_at DESC
`, tenantID, candidateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	notes := make([]CandidateNote, 0)
	for rows.Next() {
		var note CandidateNote
		if err := rows.Scan(&note.ID, &note.CandidateID, &note.Content, &note.AuthorEmail, &note.CreatedAt); err != nil {
			return nil, err
		}
		notes = append(notes, note)
	}
	return notes, rows.Err()
}

// DeleteTeamCandidates 清空团队候选人数据。
// tenantID 为当前团队 ID，返回删除的候选人主体数量；事件和触达记录由外键级联删除。
func (s *PostgresCandidateStore) DeleteTeamCandidates(tenantID string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `DELETE FROM candidate_profiles WHERE tenant_id = $1`, tenantID)
	if err != nil {
		return 0, err
	}
	rows, _ := result.RowsAffected()
	return int(rows), nil
}

// listCandidateEvents 读取候选人事件流水。
// tenantID 为团队 ID，candidateID 为候选人主体 ID，engagementID 为空时读取该候选人全部事件。
func (s *PostgresCandidateStore) listCandidateEvents(ctx context.Context, tenantID string, candidateID string, engagementID string) ([]CandidateEvent, error) {
	args := []any{tenantID, candidateID}
	whereClause := "tenant_id = $1 AND candidate_id::text = $2"
	if strings.TrimSpace(engagementID) != "" {
		args = append(args, strings.TrimSpace(engagementID))
		whereClause += fmt.Sprintf(" AND engagement_id::text = $%d", len(args))
	}
	rows, err := s.db.QueryContext(
		ctx,
		`
		SELECT id, candidate_id, COALESCE(engagement_id::text,''), COALESCE(position_id::text,''),
			COALESCE(platform_account_id::text,''), platform_id, event_type, score, reason, input_text, output_text,
			message_text, model, token_usage, metadata, created_at
		FROM candidate_events
		WHERE `+whereClause+`
		ORDER BY created_at DESC
		LIMIT 200
		`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]CandidateEvent, 0)
	for rows.Next() {
		var event CandidateEvent
		if err := rows.Scan(
			&event.ID,
			&event.CandidateID,
			&event.EngagementID,
			&event.PositionID,
			&event.PlatformAccountID,
			&event.PlatformID,
			&event.EventType,
			&event.Score,
			&event.Reason,
			&event.InputText,
			&event.OutputText,
			&event.MessageText,
			&event.Model,
			&event.TokenUsage,
			jsonScanner(&event.Metadata),
			&event.CreatedAt,
		); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

// candidateSelectSQL 返回简历库候选人列表查询 SQL。
// whereClause 为调用方传入的 WHERE 条件。
func candidateSelectSQL(whereClause string, engagementScope string) string {
	return `
	SELECT
		cp.id,
		COALESCE(latest_engagement.id::text, ''),
		COALESCE(latest_engagement.status, ''),
		COALESCE(latest_engagement.position_id::text, ''),
		COALESCE(p.name, ''),
		COALESCE(latest_engagement.platform_account_id::text, ''),
		COALESCE(u.email, ''),
		COALESCE(NULLIF(latest_engagement.platform_id, ''), cp.source_platform_id),
		cp.source_platform_candidate_id,
		cp.candidate_name,
		cp.birth_ym,
		cp.gender,
		cp.birth_ym_precision,
		cp.normalized_phone,
		cp.phone,
		cp.email,
		cp.work_region,
		cp.work_years,
		cp.expected_salary_min,
		cp.expected_salary_max,
		cp.basic_info,
		cp.education_level,
		cp.expected_position,
		cp.online_status,
		cp.personal_description,
		cp.work_status,
		cp.raw_text, cp.work_experiences, cp.educations, cp.certificates, cp.honors, cp.project_experiences, cp.colleague_communications, cp.ai_detail_reason, cp.ai_detail_score, cp.ai_greet_reason, cp.ai_greet_score, cp.first_seen_at,
		latest_engagement.detail_fetched_at,
		latest_engagement.greeted_at,
		cp.created_at,
		cp.updated_at,
		COALESCE(latest_notes.notes, '[]'::jsonb)
	FROM candidate_profiles cp
	LEFT JOIN LATERAL (
		SELECT * FROM candidate_engagements ce2
		WHERE ce2.candidate_id = cp.id
		` + engagementScope + `
		ORDER BY ce2.created_at DESC
		LIMIT 1
	) latest_engagement ON true
	LEFT JOIN LATERAL (
		SELECT jsonb_agg(jsonb_build_object(
			'id', note.id::text,
			'candidate_id', note.candidate_id::text,
			'content', note.message_text,
			'author_email', COALESCE(note.metadata->>'author_email', ''),
			'created_at', note.created_at
		) ORDER BY note.created_at DESC) AS notes
		FROM (
			SELECT id, candidate_id, message_text, metadata, created_at
			FROM candidate_events
			WHERE candidate_id = cp.id AND event_type = 'manual_note'
			ORDER BY created_at DESC
			LIMIT 2
		) note
	) latest_notes ON true
	LEFT JOIN users u ON u.id = cp.created_by_user_id
	LEFT JOIN positions p ON p.id = latest_engagement.position_id
	` + whereClause + `
	`
}

// candidateEngagementScope 生成候选人触达上下文筛选条件。
// query 为简历库筛选条件，返回用于 latest_engagement 的 SQL 片段。
func candidateEngagementScope(query PositionCandidateQuery) string {
	parts := make([]string, 0, 1)
	nextArg := 2
	if query.UserEmail != "" {
		nextArg++
	}
	if strings.TrimSpace(query.PositionID) != "" {
		parts = append(parts, fmt.Sprintf("AND ce2.position_id::text = $%d", nextArg))
	}
	return strings.Join(parts, "\n\t\t")
}

// scanCandidateRows 解析候选人查询结果集。
// rows 为数据库查询结果，返回简历库记录数组。
func scanCandidateRows(rows *sql.Rows) ([]PositionCandidate, error) {
	items := make([]PositionCandidate, 0)
	for rows.Next() {
		item, err := scanCandidateRow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// candidateScanner 抽象 QueryRow 和 Rows 的 Scan 能力。
type candidateScanner interface {
	Scan(dest ...any) error
}

// scanCandidateRow 从数据库行解析候选人结构。
// scanner 为数据库扫描器，返回可直接给前端转换的候选人记录。
func scanCandidateRow(scanner candidateScanner) (PositionCandidate, error) {
	var item PositionCandidate
	err := scanner.Scan(
		&item.ID,
		&item.EngagementID,
		&item.EngagementStatus,
		&item.PositionID,
		&item.PositionName,
		&item.PlatformAccountID,
		&item.UserEmail,
		&item.PlatformID,
		&item.PlatformCandidateID,
		&item.CandidateName,
		&item.BirthYM,
		&item.Gender,
		&item.BirthYMPrecision,
		&item.NormalizedPhone,
		&item.Phone,
		&item.Email,
		&item.WorkRegion,
		&item.WorkYears,
		&item.ExpectedSalaryMin,
		&item.ExpectedSalaryMax,
		&item.BasicInfo,
		&item.EducationLevel,
		&item.ExpectedPosition,
		&item.OnlineStatus,
		&item.PersonalDescription,
		&item.WorkStatus,
		&item.RawText, jsonScanner(&item.WorkExperiences), jsonScanner(&item.Educations), jsonScanner(&item.Certificates), jsonScanner(&item.Honors), jsonScanner(&item.ProjectExperiences), jsonScanner(&item.Communications), &item.AIDetailReason, &item.AIDetailScore, &item.AIGreetReason, &item.AIGreetScore, &item.FirstSeenAt,
		&item.DetailFetchedAt,
		&item.GreetedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
		jsonScanner(&item.Notes),
	)
	return item, err
}

// buildCandidateWhere 组装候选人查询条件和参数。
// tenantID 为当前团队 ID，query 为前端传入筛选条件。
func buildCandidateWhere(tenantID string, query PositionCandidateQuery) (string, []any) {
	clauses := []string{"cp.tenant_id = $1"}
	args := []any{tenantID}
	if query.UserEmail != "" {
		args = append(args, query.UserEmail)
		clauses = append(clauses, fmt.Sprintf("u.email = $%d", len(args)))
	}
	if query.PositionID != "" {
		args = append(args, query.PositionID)
		clauses = append(clauses, fmt.Sprintf("EXISTS (SELECT 1 FROM candidate_engagements ce_filter WHERE ce_filter.candidate_id = cp.id AND ce_filter.position_id::text = $%d)", len(args)))
	}
	if query.Keyword != "" {
		args = append(args, "%"+query.Keyword+"%")
		placeholder := fmt.Sprintf("$%d", len(args))
		clauses = append(clauses, `(cp.candidate_name ILIKE `+placeholder+`
			OR cp.phone ILIKE `+placeholder+`
			OR cp.email ILIKE `+placeholder+`
			OR cp.work_region ILIKE `+placeholder+`
			OR cp.work_years ILIKE `+placeholder+`
			OR cp.education_level ILIKE `+placeholder+`
			OR cp.expected_position ILIKE `+placeholder+`
			OR cp.basic_info ILIKE `+placeholder+`
			OR cp.personal_description ILIKE `+placeholder+` OR cp.raw_text ILIKE `+placeholder+`)`)
	}
	return strings.Join(clauses, " AND "), args
}

// userTenantID 读取用户所属团队 ID。
// userID 为用户主键，返回 tenant_id。
func userTenantID(ctx context.Context, db *sql.DB, userID string) (string, error) {
	var tenantID sql.NullString
	err := db.QueryRowContext(ctx, `SELECT COALESCE(tenant_id::text,'') FROM users WHERE id=$1`, userID).Scan(&tenantID)
	if err != nil {
		return "", err
	}
	if !tenantID.Valid || strings.TrimSpace(tenantID.String) == "" {
		return "", errors.New("用户未绑定团队")
	}
	return tenantID.String, nil
}

// candidateTenantID 读取候选人所属团队 ID。
// candidateID 为候选人主体 ID，返回 tenant_id。
func candidateTenantID(ctx context.Context, db *sql.DB, candidateID string) (string, error) {
	var tenantID string
	err := db.QueryRowContext(ctx, `SELECT tenant_id::text FROM candidate_profiles WHERE id::text=$1`, candidateID).Scan(&tenantID)
	return tenantID, err
}

// candidateIdentityKey 生成候选人来源唯一键。
// item 为候选人主体保存参数，优先使用平台候选人ID，否则用稳定文本兜底。
func candidateIdentityKey(item CandidateProfileInput) string {
	if strings.TrimSpace(item.PlatformCandidateID) != "" {
		return strings.TrimSpace(item.PlatformCandidateID)
	}
	parts := []string{item.CandidateName, item.Phone, item.Email, item.WorkRegion, item.WorkYears, item.BasicInfo}
	return strings.TrimSpace(strings.Join(parts, "|"))
}

// jsonScanner 返回可用于扫描 JSONB 字段的目标。
// target 为需要反序列化的目标指针。
func jsonScanner(target any) sql.Scanner {
	return jsonScanFunc(func(value any) error {
		if value == nil {
			return nil
		}
		raw, ok := value.([]byte)
		if !ok {
			if text, ok := value.(string); ok {
				raw = []byte(text)
			}
		}
		if len(raw) == 0 {
			return nil
		}
		return json.Unmarshal(raw, target)
	})
}

type jsonScanFunc func(value any) error

func (f jsonScanFunc) Scan(value any) error { return f(value) }
