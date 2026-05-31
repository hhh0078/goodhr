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
func (s *PostgresCandidateStore) SaveCandidateProfile(item CandidateProfileInput) (TaskCandidate, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	userID, err := ensureUserID(ctx, s.db, item.UserEmail)
	if err != nil {
		return TaskCandidate{}, err
	}
	tenantID, err := userTenantID(ctx, s.db, userID)
	if err != nil {
		return TaskCandidate{}, err
	}
	key := candidateIdentityKey(item)
	var saved TaskCandidate
	err = s.db.QueryRowContext(
		ctx,
		`
		INSERT INTO candidate_profiles (
			tenant_id, created_by_user_id, source_platform_id, source_platform_candidate_id,
			candidate_name, birth_ym, phone, email, work_region, work_years,
			expected_salary_min, expected_salary_max, basic_info, education_level,
			expected_position, online_status, personal_description, work_status,
			raw_text, filter_text, work_experiences, educations, certificates,
			honors, project_experiences, colleague_communications,
			resume_attachment_url, resume_attachment_extracted_text, ext, first_seen_at
		)
		VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,
			$16,$17,$18,$19,$20,$21::jsonb,$22::jsonb,$23::jsonb,$24::jsonb,
			$25::jsonb,$26::jsonb,$27,$28,$29::jsonb,$30
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
			filter_text = EXCLUDED.filter_text,
			work_experiences = EXCLUDED.work_experiences,
			educations = EXCLUDED.educations,
			certificates = EXCLUDED.certificates,
			honors = EXCLUDED.honors,
			project_experiences = EXCLUDED.project_experiences,
			colleague_communications = EXCLUDED.colleague_communications,
			resume_attachment_url = EXCLUDED.resume_attachment_url,
			resume_attachment_extracted_text = EXCLUDED.resume_attachment_extracted_text,
			ext = EXCLUDED.ext,
			first_seen_at = COALESCE(candidate_profiles.first_seen_at, EXCLUDED.first_seen_at),
			updated_at = now()
		RETURNING
			id, source_platform_id, source_platform_candidate_id, candidate_name, birth_ym,
			phone, email, work_region, work_years, expected_salary_min, expected_salary_max,
			basic_info, education_level, expected_position, online_status, personal_description,
			work_status, raw_text, filter_text, work_experiences, educations, certificates,
			honors, project_experiences, colleague_communications, resume_attachment_url,
			resume_attachment_extracted_text, ext, first_seen_at, created_at, updated_at
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
		item.FilterText,
		string(toJSONB(item.WorkExperiences)),
		string(toJSONB(item.Educations)),
		string(toJSONB(item.Certificates)),
		string(toJSONB(item.Honors)),
		string(toJSONB(item.ProjectExperiences)),
		string(toJSONB(item.Communications)),
		item.ResumeURL,
		item.ResumeText,
		string(toJSONB(item.Ext)),
		item.FirstSeenAt,
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
		&saved.FilterText,
		jsonScanner(&saved.WorkExperiences),
		jsonScanner(&saved.Educations),
		jsonScanner(&saved.Certificates),
		jsonScanner(&saved.Honors),
		jsonScanner(&saved.ProjectExperiences),
		jsonScanner(&saved.Communications),
		&saved.ResumeURL,
		&saved.ResumeText,
		jsonScanner(&saved.Ext),
		&saved.FirstSeenAt,
		&saved.CreatedAt,
		&saved.UpdatedAt,
	)
	if err != nil {
		return TaskCandidate{}, err
	}
	return saved, nil
}

// UpsertCandidateEngagement 新增或更新候选人触达上下文。
// item 为候选人、任务、岗位和账号关系，返回保存后的触达记录。
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
	var saved CandidateEngagement
	err = s.db.QueryRowContext(
		ctx,
		`
		INSERT INTO candidate_engagements (
			tenant_id, candidate_id, task_id, position_id, platform_account_id,
			platform_id, status, first_seen_at, detail_fetched_at, greeted_at
		)
		VALUES ($1,$2,$3::uuid,NULLIF($4,'')::uuid,NULLIF($5,'')::uuid,$6,$7,$8,$9,$10)
		ON CONFLICT (tenant_id, candidate_id, task_id, position_id, platform_account_id)
		DO UPDATE SET
			platform_id = EXCLUDED.platform_id,
			status = EXCLUDED.status,
			first_seen_at = COALESCE(candidate_engagements.first_seen_at, EXCLUDED.first_seen_at),
			detail_fetched_at = COALESCE(EXCLUDED.detail_fetched_at, candidate_engagements.detail_fetched_at),
			greeted_at = COALESCE(EXCLUDED.greeted_at, candidate_engagements.greeted_at),
			updated_at = now()
		RETURNING id, candidate_id, COALESCE(task_id::text,''), COALESCE(position_id::text,''), COALESCE(platform_account_id::text,''),
			platform_id, status, first_seen_at, detail_fetched_at, greeted_at, last_event_at, created_at, updated_at
		`,
		tenantID,
		item.CandidateID,
		item.TaskID,
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
		&saved.TaskID,
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
			tenant_id, candidate_id, engagement_id, task_id, position_id, platform_account_id,
			platform_id, event_type, score, reason, input_text, output_text,
			message_text, model, token_usage, metadata
		)
		VALUES ($1,$2,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,NULLIF($5,'')::uuid,NULLIF($6,'')::uuid,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16::jsonb)
		RETURNING id, candidate_id, COALESCE(engagement_id::text,''), COALESCE(task_id::text,''), COALESCE(position_id::text,''), COALESCE(platform_account_id::text,''),
			platform_id, event_type, score, reason, input_text, output_text, message_text, model, token_usage, metadata, created_at
		`,
		tenantID,
		item.CandidateID,
		item.EngagementID,
		item.TaskID,
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
		&saved.TaskID,
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

// ListTaskCandidates 按团队和筛选条件分页读取候选人记录。
// tenantID 为当前用户团队 ID，query 可传搜索词、任务 ID、岗位 ID 和分页条件。
func (s *PostgresCandidateStore) ListTaskCandidates(tenantID string, query TaskCandidateQuery) (TaskCandidateListResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	page, pageSize := normalizeCandidatePage(query.Page, query.PageSize)
	where, args := buildCandidateWhere(tenantID, query)
	countSQL := "SELECT COUNT(*) FROM candidate_profiles cp WHERE " + where
	var total int
	if err := s.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return TaskCandidateListResult{}, err
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
		return TaskCandidateListResult{}, err
	}
	defer rows.Close()
	items, err := scanCandidateRows(rows)
	if err != nil {
		return TaskCandidateListResult{}, err
	}
	return TaskCandidateListResult{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

// GetTaskCandidate 按 ID 读取当前团队内的候选人详情。
// tenantID 为当前用户团队 ID，candidateID 为候选人主体 ID，engagementID 为空时使用最近一次触达。
func (s *PostgresCandidateStore) GetTaskCandidate(tenantID string, candidateID string, engagementID string) (TaskCandidate, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	args := []any{tenantID, candidateID}
	whereClause := "WHERE cp.tenant_id = $1 AND cp.id::text = $2"
	engagementScope := ""
	if strings.TrimSpace(engagementID) != "" {
		args = append(args, strings.TrimSpace(engagementID))
		whereClause += fmt.Sprintf(" AND EXISTS (SELECT 1 FROM candidate_engagements ce_match WHERE ce_match.candidate_id = cp.id AND ce_match.id::text = $%d)", len(args))
		engagementScope = fmt.Sprintf("AND ce2.id::text = $%d", len(args))
	}
	rows, err := s.db.QueryContext(ctx, candidateSelectSQL(whereClause, engagementScope), args...)
	if err != nil {
		return TaskCandidate{}, err
	}
	defer rows.Close()
	items, err := scanCandidateRows(rows)
	if err != nil {
		return TaskCandidate{}, err
	}
	if len(items) == 0 {
		return TaskCandidate{}, ErrNotFound
	}
	events, err := s.listCandidateEvents(ctx, tenantID, candidateID, items[0].EngagementID)
	if err != nil {
		return TaskCandidate{}, err
	}
	items[0].Events = events
	return items[0], nil
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
		SELECT id, candidate_id, COALESCE(engagement_id::text,''), COALESCE(task_id::text,''), COALESCE(position_id::text,''),
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
			&event.TaskID,
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
		COALESCE(latest_engagement.task_id::text, ''),
		COALESCE(latest_engagement.position_id::text, ''),
		COALESCE(p.name, ''),
		COALESCE(latest_engagement.platform_account_id::text, ''),
		COALESCE(u.email, ''),
		COALESCE(NULLIF(latest_engagement.platform_id, ''), cp.source_platform_id),
		cp.source_platform_candidate_id,
		cp.candidate_name,
		cp.birth_ym,
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
		cp.raw_text,
		cp.filter_text,
		cp.work_experiences,
		cp.educations,
		cp.certificates,
		cp.honors,
		cp.project_experiences,
		cp.colleague_communications,
		cp.resume_attachment_url,
		cp.resume_attachment_extracted_text,
		COALESCE(detail_event.reason, ''),
		detail_event.score,
		COALESCE(greet_event.reason, ''),
		greet_event.score,
		COALESCE(review_event.reason, ''),
		review_event.score,
		cp.ext,
		cp.first_seen_at,
		latest_engagement.detail_fetched_at,
		latest_engagement.greeted_at,
		cp.created_at,
		cp.updated_at
	FROM candidate_profiles cp
	LEFT JOIN LATERAL (
		SELECT * FROM candidate_engagements ce2
		WHERE ce2.candidate_id = cp.id
		` + engagementScope + `
		ORDER BY ce2.created_at DESC
		LIMIT 1
	) latest_engagement ON true
	LEFT JOIN users u ON u.id = cp.created_by_user_id
	LEFT JOIN positions p ON p.id = latest_engagement.position_id
	LEFT JOIN LATERAL (
		SELECT score, reason FROM candidate_events ev
		WHERE ev.engagement_id = latest_engagement.id AND ev.event_type = 'detail_analysis'
		ORDER BY ev.created_at DESC
		LIMIT 1
	) detail_event ON true
	LEFT JOIN LATERAL (
		SELECT score, reason FROM candidate_events ev
		WHERE ev.engagement_id = latest_engagement.id AND ev.event_type = 'greet_analysis'
		ORDER BY ev.created_at DESC
		LIMIT 1
	) greet_event ON true
	LEFT JOIN LATERAL (
		SELECT score, reason FROM candidate_events ev
		WHERE ev.engagement_id = latest_engagement.id AND ev.event_type = 'review_analysis'
		ORDER BY ev.created_at DESC
		LIMIT 1
	) review_event ON true
	` + whereClause + `
	`
}

// candidateEngagementScope 生成候选人触达上下文筛选条件。
// query 为简历库筛选条件，返回用于 latest_engagement 的 SQL 片段。
func candidateEngagementScope(query TaskCandidateQuery) string {
	parts := make([]string, 0, 2)
	nextArg := 2
	if strings.TrimSpace(query.TaskID) != "" {
		parts = append(parts, fmt.Sprintf("AND ce2.task_id::text = $%d", nextArg))
		nextArg++
	}
	if strings.TrimSpace(query.PositionID) != "" {
		parts = append(parts, fmt.Sprintf("AND ce2.position_id::text = $%d", nextArg))
	}
	return strings.Join(parts, "\n\t\t")
}

// scanCandidateRows 解析候选人查询结果集。
// rows 为数据库查询结果，返回简历库记录数组。
func scanCandidateRows(rows *sql.Rows) ([]TaskCandidate, error) {
	items := make([]TaskCandidate, 0)
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
func scanCandidateRow(scanner candidateScanner) (TaskCandidate, error) {
	var item TaskCandidate
	err := scanner.Scan(
		&item.ID,
		&item.EngagementID,
		&item.TaskID,
		&item.PositionID,
		&item.PositionName,
		&item.PlatformAccountID,
		&item.UserEmail,
		&item.PlatformID,
		&item.PlatformCandidateID,
		&item.CandidateName,
		&item.BirthYM,
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
		&item.RawText,
		&item.FilterText,
		jsonScanner(&item.WorkExperiences),
		jsonScanner(&item.Educations),
		jsonScanner(&item.Certificates),
		jsonScanner(&item.Honors),
		jsonScanner(&item.ProjectExperiences),
		jsonScanner(&item.Communications),
		&item.ResumeURL,
		&item.ResumeText,
		&item.AIDetailReason,
		&item.AIDetailScore,
		&item.AIGreetReason,
		&item.AIGreetScore,
		&item.AIReviewReason,
		&item.AIReviewScore,
		jsonScanner(&item.Ext),
		&item.FirstSeenAt,
		&item.DetailFetchedAt,
		&item.GreetedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

// buildCandidateWhere 组装候选人查询条件和参数。
// tenantID 为当前团队 ID，query 为前端传入筛选条件。
func buildCandidateWhere(tenantID string, query TaskCandidateQuery) (string, []any) {
	clauses := []string{"cp.tenant_id = $1"}
	args := []any{tenantID}
	if query.TaskID != "" {
		args = append(args, query.TaskID)
		clauses = append(clauses, fmt.Sprintf("EXISTS (SELECT 1 FROM candidate_engagements ce_filter WHERE ce_filter.candidate_id = cp.id AND ce_filter.task_id::text = $%d)", len(args)))
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
			OR cp.personal_description ILIKE `+placeholder+`
			OR cp.raw_text ILIKE `+placeholder+`
			OR cp.filter_text ILIKE `+placeholder+`
			OR cp.resume_attachment_extracted_text ILIKE `+placeholder+`)`)
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
