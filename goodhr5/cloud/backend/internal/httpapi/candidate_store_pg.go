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
			gender, birth_ym_precision, normalized_phone, wechat, avatar_url
		)
		VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,
			$16,$17,$18,$19,$20::jsonb,$21::jsonb,$22::jsonb,$23::jsonb,
			$24::jsonb,$25::jsonb,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35
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
			wechat = CASE WHEN EXCLUDED.wechat='' THEN candidate_profiles.wechat ELSE EXCLUDED.wechat END,
			avatar_url = CASE WHEN EXCLUDED.avatar_url='' THEN candidate_profiles.avatar_url ELSE EXCLUDED.avatar_url END,
			first_seen_at = COALESCE(candidate_profiles.first_seen_at, EXCLUDED.first_seen_at),
			updated_at = now()
		RETURNING
			id, source_platform_id, source_platform_candidate_id, candidate_name, birth_ym,
			phone, email, work_region, work_years, expected_salary_min, expected_salary_max,
			basic_info, education_level, expected_position, online_status, personal_description,
			work_status, raw_text, work_experiences, educations, certificates, honors,
			project_experiences, colleague_communications, ai_detail_reason, ai_detail_score,
			ai_greet_reason, ai_greet_score, first_seen_at, created_at, updated_at,
			gender, birth_ym_precision, normalized_phone, wechat, avatar_url
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
		item.Wechat,
		strings.TrimSpace(item.AvatarURL),
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
		&saved.Wechat,
		&saved.AvatarURL,
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
			normalized_phone=COALESCE(NULLIF($28,''),normalized_phone),
			wechat=COALESCE(NULLIF($29,''),wechat),
			avatar_url=COALESCE(NULLIF($30,''),avatar_url),
			updated_at=now()
		WHERE tenant_id=$1 AND id=$2
	`, tenantID, item.CandidateID, item.PlatformID, item.PlatformCandidateID,
		strings.TrimSpace(item.CandidateName), strings.TrimSpace(item.BirthYM), strings.TrimSpace(item.Phone),
		strings.TrimSpace(item.Email), strings.TrimSpace(item.WorkRegion), strings.TrimSpace(item.WorkYears),
		item.ExpectedSalaryMin, item.ExpectedSalaryMax, strings.TrimSpace(item.BasicInfo),
		strings.TrimSpace(item.EducationLevel), strings.TrimSpace(item.ExpectedPosition), strings.TrimSpace(item.OnlineStatus),
		strings.TrimSpace(item.PersonalDescription), strings.TrimSpace(item.WorkStatus), strings.TrimSpace(item.RawText),
		string(toJSONB(item.WorkExperiences)), string(toJSONB(item.Educations)), string(toJSONB(item.Certificates)),
		string(toJSONB(item.Honors)), string(toJSONB(item.ProjectExperiences)), string(toJSONB(item.Communications)),
		item.Gender, item.BirthYMPrecision, item.NormalizedPhone, strings.TrimSpace(item.Wechat), strings.TrimSpace(item.AvatarURL))
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
		AvatarURL: item.AvatarURL,
		Gender:    item.Gender, BirthYM: item.BirthYM, BirthYMPrecision: item.BirthYMPrecision,
		NormalizedPhone: item.NormalizedPhone, Phone: item.Phone, Email: item.Email, Wechat: item.Wechat,
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
// tenantID 为当前用户团队 ID，query 可传搜索词、岗位、平台、手机号、条件状态、排序和分页条件。
func (s *PostgresCandidateStore) ListPositionCandidates(tenantID string, query PositionCandidateQuery) (PositionCandidateListResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	page, pageSize := normalizeCandidatePage(query.Page, query.PageSize)
	where, args := buildCandidateWhere(tenantID, query)
	countSQL := candidateCountSQL(where)
	var total int
	if err := s.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return PositionCandidateListResult{}, err
	}
	offset := (page - 1) * pageSize
	listArgs := append(args, pageSize, offset)
	rows, err := s.db.QueryContext(
		ctx,
		candidateSelectSQL("WHERE "+where, candidateEngagementScope(query), candidatePlatformIdentityScope(query))+`
		`+candidateOrderSQL(query.Sort)+`
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

// candidateCountSQL 返回与候选人列表相同用户范围的总数查询。
// whereClause 为列表共用的筛选条件，用户表关联确保非管理员邮箱条件可用。
func candidateCountSQL(whereClause string) string {
	return `SELECT COUNT(*)
		FROM candidate_profiles cp
		LEFT JOIN users u ON u.id = cp.created_by_user_id
		WHERE ` + whereClause
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
ORDER BY created_at DESC, id DESC
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

// DeleteCandidate 删除单个候选人及其自动回复会话关联数据。
// tenantID 和 candidateID 共同限制删除范围，返回待清理的附件相对路径。
func (s *PostgresCandidateStore) DeleteCandidate(tenantID string, candidateID string) (CandidateDeleteResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CandidateDeleteResult{}, err
	}
	defer tx.Rollback()
	result, err := deleteCandidateInTx(ctx, tx, tenantID, candidateID)
	if err != nil {
		return CandidateDeleteResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return CandidateDeleteResult{}, err
	}
	return result, nil
}

// DeleteTeamCandidates 清空团队候选人及其自动回复会话关联数据。
// tenantID 为当前团队 ID，返回删除数量和待清理的附件相对路径。
func (s *PostgresCandidateStore) DeleteTeamCandidates(tenantID string) (CandidateDeleteResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CandidateDeleteResult{}, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id::text FROM candidate_profiles WHERE tenant_id=$1 ORDER BY id FOR UPDATE`, tenantID)
	if err != nil {
		return CandidateDeleteResult{}, err
	}
	candidateIDs := make([]string, 0)
	for rows.Next() {
		var candidateID string
		if err = rows.Scan(&candidateID); err != nil {
			rows.Close()
			return CandidateDeleteResult{}, err
		}
		candidateIDs = append(candidateIDs, candidateID)
	}
	if err = rows.Close(); err != nil {
		return CandidateDeleteResult{}, err
	}
	result := CandidateDeleteResult{AttachmentPaths: []string{}, AvatarURLs: []string{}}
	for _, candidateID := range candidateIDs {
		deleted, deleteErr := deleteCandidateInTx(ctx, tx, tenantID, candidateID)
		if deleteErr != nil {
			return CandidateDeleteResult{}, deleteErr
		}
		result.Deleted += deleted.Deleted
		result.AttachmentPaths = append(result.AttachmentPaths, deleted.AttachmentPaths...)
		result.AvatarURLs = append(result.AvatarURLs, deleted.AvatarURLs...)
	}
	if err = tx.Commit(); err != nil {
		return CandidateDeleteResult{}, err
	}
	return result, nil
}

const relatedCandidateConversationsSQL = `
	SELECT conversation.id
	FROM candidate_conversations conversation
	WHERE conversation.tenant_id=$1 AND (
		conversation.candidate_id::text=$2
		OR EXISTS (
			SELECT 1 FROM candidate_platform_identities identity
			WHERE identity.id=conversation.platform_identity_id
				AND identity.tenant_id=$1 AND identity.candidate_id::text=$2
		)
		OR EXISTS (
			SELECT 1 FROM candidate_resume_attachments attachment
			WHERE attachment.tenant_id=$1 AND attachment.conversation_id=conversation.id
				AND attachment.candidate_id::text=$2
		)
		OR EXISTS (
			SELECT 1 FROM candidate_confirmation_items confirmation
			WHERE confirmation.tenant_id=$1 AND confirmation.conversation_id=conversation.id
				AND confirmation.candidate_id::text=$2
		)
		OR EXISTS (
			SELECT 1 FROM auto_reply_ai_runs ai_run
			WHERE ai_run.tenant_id=$1 AND ai_run.conversation_id=conversation.id
				AND ai_run.candidate_id::text=$2
		)
	)
`

// deleteCandidateInTx 在事务中删除一份候选人和其全部会话关联数据。
// ctx 和 tx 控制同一事务，tenantID 与 candidateID 限定目标。
func deleteCandidateInTx(ctx context.Context, tx *sql.Tx, tenantID string, candidateID string) (CandidateDeleteResult, error) {
	var avatarURL string
	if err := tx.QueryRowContext(ctx, `
		SELECT avatar_url FROM candidate_profiles
		WHERE tenant_id=$1 AND id::text=$2
		FOR UPDATE
	`, tenantID, candidateID).Scan(&avatarURL); errors.Is(err, sql.ErrNoRows) {
		return CandidateDeleteResult{}, ErrNotFound
	} else if err != nil {
		return CandidateDeleteResult{}, err
	}
	avatarURLs := []string{avatarURL}
	avatarRows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT report_data #>> '{candidate,avatar_url}'
		FROM candidate_recommendations
		WHERE tenant_id=$1 AND candidate_id::text=$2
			AND COALESCE(report_data #>> '{candidate,avatar_url}', '') <> ''
	`, tenantID, candidateID)
	if err != nil {
		return CandidateDeleteResult{}, err
	}
	for avatarRows.Next() {
		var snapshotAvatarURL string
		if err = avatarRows.Scan(&snapshotAvatarURL); err != nil {
			avatarRows.Close()
			return CandidateDeleteResult{}, err
		}
		avatarURLs = append(avatarURLs, snapshotAvatarURL)
	}
	if err = avatarRows.Close(); err != nil {
		return CandidateDeleteResult{}, err
	}
	rows, err := tx.QueryContext(ctx, `
		WITH related_conversations AS (`+relatedCandidateConversationsSQL+`)
		SELECT attachment.storage_path
		FROM candidate_resume_attachments attachment
		WHERE attachment.tenant_id=$1 AND (
			attachment.candidate_id::text=$2
			OR attachment.conversation_id IN (SELECT id FROM related_conversations)
		)
	`, tenantID, candidateID)
	if err != nil {
		return CandidateDeleteResult{}, err
	}
	paths := make([]string, 0)
	for rows.Next() {
		var path string
		if err = rows.Scan(&path); err != nil {
			rows.Close()
			return CandidateDeleteResult{}, err
		}
		paths = append(paths, path)
	}
	if err = rows.Close(); err != nil {
		return CandidateDeleteResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `
		WITH related_conversations AS (`+relatedCandidateConversationsSQL+`)
		DELETE FROM auto_reply_config_suggestions
		WHERE tenant_id=$1 AND conversation_id IN (SELECT id FROM related_conversations)
	`, tenantID, candidateID); err != nil {
		return CandidateDeleteResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `
		WITH related_conversations AS (`+relatedCandidateConversationsSQL+`)
		DELETE FROM candidate_conversations
		WHERE tenant_id=$1 AND id IN (SELECT id FROM related_conversations)
	`, tenantID, candidateID); err != nil {
		return CandidateDeleteResult{}, err
	}
	deleteResult, err := tx.ExecContext(ctx, `DELETE FROM candidate_profiles WHERE tenant_id=$1 AND id::text=$2`, tenantID, candidateID)
	if err != nil {
		return CandidateDeleteResult{}, err
	}
	deleted, err := deleteResult.RowsAffected()
	if err != nil {
		return CandidateDeleteResult{}, err
	}
	if deleted == 0 {
		return CandidateDeleteResult{}, ErrNotFound
	}
	return CandidateDeleteResult{Deleted: int(deleted), AttachmentPaths: paths, AvatarURLs: avatarURLs}, nil
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
		ORDER BY created_at DESC, id DESC
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
// whereClause 为调用方传入的 WHERE 条件，engagementScope 和 identityScopes 分别限定触达与平台身份。
func candidateSelectSQL(whereClause string, engagementScope string, identityScopes ...string) string {
	identityScope := ""
	if len(identityScopes) > 0 {
		identityScope = identityScopes[0]
	}
	return `
	SELECT
		cp.id,
		COALESCE(latest_engagement.id::text, ''),
		COALESCE(latest_engagement.status, ''),
		COALESCE(latest_engagement.position_id::text, ''),
		COALESCE(p.name, ''),
		COALESCE(latest_engagement.platform_account_id::text, ''),
		COALESCE(u.email, ''),
		COALESCE(NULLIF(latest_engagement.platform_id, ''), NULLIF(latest_platform_identity.platform_id, ''), cp.source_platform_id),
		cp.source_platform_candidate_id,
		cp.candidate_name,
		cp.avatar_url,
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
		cp.raw_text, cp.work_experiences, cp.educations, cp.certificates, cp.honors, cp.project_experiences, cp.colleague_communications, cp.ai_detail_reason, cp.ai_detail_score,
		COALESCE(latest_greet_analysis.reason, ''),
		latest_greet_analysis.score, cp.first_seen_at,
		latest_engagement.detail_fetched_at,
		latest_engagement.greeted_at,
		cp.created_at,
		cp.updated_at,
		cp.wechat,
		COALESCE(latest_notes.notes, '[]'::jsonb)
	FROM candidate_profiles cp
	LEFT JOIN LATERAL (
		SELECT * FROM candidate_engagements ce2
		WHERE ce2.candidate_id = cp.id
		` + engagementScope + `
		ORDER BY ce2.created_at DESC, ce2.id DESC
		LIMIT 1
	) latest_engagement ON true
	LEFT JOIN LATERAL (
		SELECT identity.platform_id
		FROM candidate_platform_identities identity
		WHERE identity.candidate_id = cp.id
		` + identityScope + `
		ORDER BY identity.last_seen_at DESC, identity.id DESC
		LIMIT 1
	) latest_platform_identity ON true
	LEFT JOIN LATERAL (
		SELECT event.score, event.reason
		FROM candidate_events event
		WHERE event.candidate_id = cp.id
			AND event.engagement_id = latest_engagement.id
			AND event.event_type = 'greet_analysis'
		ORDER BY event.created_at DESC, event.id DESC
		LIMIT 1
	) latest_greet_analysis ON true
	LEFT JOIN LATERAL (
		SELECT jsonb_agg(jsonb_build_object(
			'id', note.id::text,
			'candidate_id', note.candidate_id::text,
			'content', note.message_text,
			'author_email', COALESCE(note.metadata->>'author_email', ''),
			'created_at', note.created_at
		) ORDER BY note.created_at DESC, note.id DESC) AS notes
		FROM (
			SELECT id, candidate_id, message_text, metadata, created_at
			FROM candidate_events
			WHERE candidate_id = cp.id AND event_type = 'manual_note'
			ORDER BY created_at DESC, id DESC
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
	parts := make([]string, 0, 2)
	positionPlaceholder, platformPlaceholder := candidateFilterPlaceholders(query)
	if positionPlaceholder != "" {
		parts = append(parts, "AND ce2.position_id::text = "+positionPlaceholder)
	}
	if platformPlaceholder != "" {
		parts = append(parts, "AND LOWER(ce2.platform_id) = "+platformPlaceholder)
	}
	if predicate := candidateReviewStatusPredicate(query.ConditionStatus, "review_scope"); predicate != "" {
		parts = append(parts, `AND EXISTS (
			SELECT 1 FROM candidate_position_reviews review_scope
			WHERE review_scope.tenant_id = ce2.tenant_id
				AND review_scope.candidate_id = ce2.candidate_id
				AND review_scope.position_id = ce2.position_id
				AND `+predicate+`
		)`)
	}
	return strings.Join(parts, "\n\t\t")
}

// candidatePlatformIdentityScope 生成候选人平台身份的筛选片段。
// query 未指定平台时返回空值，指定平台时与列表平台参数使用同一占位符。
func candidatePlatformIdentityScope(query PositionCandidateQuery) string {
	_, platformPlaceholder := candidateFilterPlaceholders(query)
	if platformPlaceholder == "" {
		return ""
	}
	return "AND LOWER(identity.platform_id) = " + platformPlaceholder
}

// candidateFilterPlaceholders 返回岗位和平台在列表参数中的稳定占位符。
// query 参数顺序固定为团队、用户、岗位、平台和关键词。
func candidateFilterPlaceholders(query PositionCandidateQuery) (string, string) {
	nextArg := 2
	if query.UserEmail != "" {
		nextArg++
	}
	positionPlaceholder := ""
	if strings.TrimSpace(query.PositionID) != "" {
		positionPlaceholder = fmt.Sprintf("$%d", nextArg)
		nextArg++
	}
	platformPlaceholder := ""
	if strings.TrimSpace(query.PlatformID) != "" {
		platformPlaceholder = fmt.Sprintf("$%d", nextArg)
	}
	return positionPlaceholder, platformPlaceholder
}

// candidateOrderSQL 返回简历库允许使用的排序语句。
// sortMode 为规范后的排序值，空值继续使用旧版最近入库排序。
func candidateOrderSQL(sortMode string) string {
	if sortMode == "second_score_desc" {
		return `ORDER BY latest_greet_analysis.score DESC NULLS LAST,
			COALESCE(latest_engagement.created_at, cp.created_at) DESC,
			cp.id ASC`
	}
	return "ORDER BY COALESCE(latest_engagement.created_at, cp.created_at) DESC, cp.id ASC"
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
		&item.AvatarURL,
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
		&item.Wechat,
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
	}
	if query.PlatformID != "" {
		args = append(args, strings.ToLower(query.PlatformID))
	}
	positionPlaceholder, platformPlaceholder := candidateFilterPlaceholders(query)
	if positionPlaceholder != "" {
		engagementScope := "ce_filter.tenant_id = cp.tenant_id AND ce_filter.candidate_id = cp.id AND ce_filter.position_id::text = " + positionPlaceholder
		if platformPlaceholder != "" {
			engagementScope += " AND LOWER(ce_filter.platform_id) = " + platformPlaceholder
		}
		clauses = append(clauses, "EXISTS (SELECT 1 FROM candidate_engagements ce_filter WHERE "+engagementScope+")")
	} else if platformPlaceholder != "" {
		clauses = append(clauses, `(EXISTS (
			SELECT 1 FROM candidate_engagements ce_filter
			WHERE ce_filter.tenant_id = cp.tenant_id AND ce_filter.candidate_id = cp.id
				AND LOWER(ce_filter.platform_id) = `+platformPlaceholder+`
		) OR EXISTS (
			SELECT 1 FROM candidate_platform_identities identity_filter
			WHERE identity_filter.tenant_id = cp.tenant_id AND identity_filter.candidate_id = cp.id
				AND LOWER(identity_filter.platform_id) = `+platformPlaceholder+`
		))`)
	}
	switch query.PhoneStatus {
	case "has":
		clauses = append(clauses, "COALESCE(NULLIF(BTRIM(cp.normalized_phone),''), NULLIF(BTRIM(cp.phone),'')) IS NOT NULL")
	case "none":
		clauses = append(clauses, "COALESCE(NULLIF(BTRIM(cp.normalized_phone),''), NULLIF(BTRIM(cp.phone),'')) IS NULL")
	}
	if conditionClause := candidateConditionStatusClause(query.ConditionStatus, positionPlaceholder, platformPlaceholder); conditionClause != "" {
		clauses = append(clauses, conditionClause)
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

// candidateConditionStatusClause 返回候选人岗位条件状态筛选语句。
// status 为规范后的条件状态；岗位和平台占位符非空时，评估必须来自同一岗位和平台触达。
func candidateConditionStatusClause(status string, positionPlaceholder string, platformPlaceholder string) string {
	reviewScope := "review.tenant_id = cp.tenant_id AND review.candidate_id = cp.id"
	if positionPlaceholder != "" {
		reviewScope += " AND review.position_id::text = " + positionPlaceholder
	}
	if platformPlaceholder != "" {
		reviewScope += ` AND EXISTS (
			SELECT 1 FROM candidate_engagements review_engagement
			WHERE review_engagement.tenant_id = review.tenant_id
				AND review_engagement.candidate_id = review.candidate_id
				AND review_engagement.position_id = review.position_id
				AND LOWER(review_engagement.platform_id) = ` + platformPlaceholder + `
		)`
	}
	if status == "untracked" {
		return "NOT EXISTS (SELECT 1 FROM candidate_position_reviews review WHERE " + reviewScope + ")"
	}
	predicate := candidateReviewStatusPredicate(status, "review")
	if predicate == "" {
		return ""
	}
	return "EXISTS (SELECT 1 FROM candidate_position_reviews review WHERE " + reviewScope + " AND " + predicate + ")"
}

// candidateReviewStatusPredicate 返回指定评估表别名对应的安全状态条件。
// status 为前端条件状态，alias 只能由程序传入固定 SQL 别名。
func candidateReviewStatusPredicate(status string, alias string) string {
	switch status {
	case "all_matched":
		return alias + ".status IN ('qualified','generating','recommended','failed')"
	case "pending":
		return alias + ".status IN ('collecting','pending')"
	case "unmatched":
		return alias + ".status = 'unmatched'"
	default:
		return ""
	}
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
	parts := []string{item.CandidateName, item.Phone, item.Email, item.Wechat, item.WorkRegion, item.WorkYears, item.BasicInfo}
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
