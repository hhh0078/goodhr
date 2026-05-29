// 本文件负责候选人 PostgreSQL 存储实现。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PostgresCandidateStore 使用 PostgreSQL 持久化任务候选人。
type PostgresCandidateStore struct {
	db *sql.DB
}

// NewPostgresCandidateStore 创建 PostgreSQL 候选人存储。
func NewPostgresCandidateStore(db *sql.DB) *PostgresCandidateStore {
	return &PostgresCandidateStore{db: db}
}

// SaveTaskCandidate 新增任务候选人记录。
func (s *PostgresCandidateStore) SaveTaskCandidate(item TaskCandidate) (TaskCandidate, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	userID, err := ensureUserID(ctx, s.db, item.UserEmail)
	if err != nil {
		return TaskCandidate{}, err
	}

	var saved TaskCandidate
	saved.UserEmail = item.UserEmail
	err = s.db.QueryRowContext(
		ctx,
		`
		INSERT INTO task_candidates (
			task_id,
			user_id,
			platform_id,
			platform_candidate_id,
			candidate_name,
			birth_ym,
			phone,
			email,
			work_region,
			work_years,
			expected_salary_min,
			expected_salary_max,
			basic_info,
			education_level,
			expected_position,
			online_status,
			personal_description,
			raw_text,
			filter_text,
			work_experiences,
			educations,
			certificates,
			honors,
			project_experiences,
			colleague_communications,
			resume_attachment_url,
			resume_attachment_extracted_text,
			ai_detail_reason,
			ai_detail_score,
			ai_greet_reason,
			ai_greet_score,
			ai_review_reason,
			ai_review_score,
			ext,
			first_seen_at,
			detail_fetched_at,
			greeted_at
		)
		VALUES (
			$1,$2,$3,$4,$5,
			$6,$7,$8,$9,$10,$11,$12,
			$13,$14,$15,$16,$17,$18,$19,
			$20::jsonb,$21::jsonb,$22::jsonb,$23::jsonb,$24::jsonb,$25::jsonb,
			$26,$27,$28,$29,$30,$31,$32,$33,$34::jsonb,$35,$36,$37
		)
		RETURNING
			id, task_id, platform_id, platform_candidate_id, candidate_name,
			birth_ym, phone, email, work_region, work_years, expected_salary_min, expected_salary_max,
			basic_info, education_level, expected_position, online_status, personal_description, raw_text, filter_text,
			resume_attachment_url, resume_attachment_extracted_text,
			ai_detail_reason, ai_detail_score, ai_greet_reason, ai_greet_score, ai_review_reason, ai_review_score,
			first_seen_at, detail_fetched_at, greeted_at,
			created_at, updated_at
		`,
		item.TaskID,
		userID,
		item.PlatformID,
		item.PlatformCandidateID,
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
		item.AIDetailReason,
		item.AIDetailScore,
		item.AIGreetReason,
		item.AIGreetScore,
		item.AIReviewReason,
		item.AIReviewScore,
		string(toJSONB(item.Ext)),
		item.FirstSeenAt,
		item.DetailFetchedAt,
		item.GreetedAt,
	).Scan(
		&saved.ID,
		&saved.TaskID,
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
		&saved.RawText,
		&saved.FilterText,
		&saved.ResumeURL,
		&saved.ResumeText,
		&saved.AIDetailReason,
		&saved.AIDetailScore,
		&saved.AIGreetReason,
		&saved.AIGreetScore,
		&saved.AIReviewReason,
		&saved.AIReviewScore,
		&saved.FirstSeenAt,
		&saved.DetailFetchedAt,
		&saved.GreetedAt,
		&saved.CreatedAt,
		&saved.UpdatedAt,
	)
	if err != nil {
		return TaskCandidate{}, err
	}
	return saved, nil
}

// ListTaskCandidates 按团队和筛选条件分页读取候选人记录。
// tenantID 为当前用户团队 ID，query 可传搜索词、任务 ID、岗位 ID 和分页条件。
func (s *PostgresCandidateStore) ListTaskCandidates(tenantID string, query TaskCandidateQuery) (TaskCandidateListResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	page, pageSize := normalizeCandidatePage(query.Page, query.PageSize)
	where, args := buildCandidateWhere(tenantID, query)
	countSQL := "SELECT COUNT(*) FROM task_candidates tc INNER JOIN users u ON u.id = tc.user_id INNER JOIN task_runs tr ON tr.id = tc.task_id LEFT JOIN positions p ON p.id = tr.position_id WHERE " + where
	var total int
	if err := s.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return TaskCandidateListResult{}, err
	}

	offset := (page - 1) * pageSize
	listArgs := append(args, pageSize, offset)
	rows, err := s.db.QueryContext(
		ctx,
		`
		SELECT
			tc.id,
			tc.task_id,
			COALESCE(tr.position_id::text, ''),
			COALESCE(p.name, ''),
			u.email,
			tc.platform_id,
			tc.platform_candidate_id,
			tc.candidate_name,
			tc.birth_ym,
			tc.phone,
			tc.email,
			tc.work_region,
			tc.work_years,
			tc.expected_salary_min,
			tc.expected_salary_max,
			tc.basic_info,
			tc.education_level,
			tc.expected_position,
			tc.online_status,
			tc.personal_description,
			tc.raw_text,
			tc.filter_text,
			tc.work_experiences,
			tc.educations,
			tc.certificates,
			tc.honors,
			tc.project_experiences,
			tc.colleague_communications,
			tc.resume_attachment_url,
			tc.resume_attachment_extracted_text,
			tc.ai_detail_reason,
			tc.ai_detail_score,
			tc.ai_greet_reason,
			tc.ai_greet_score,
			tc.ai_review_reason,
			tc.ai_review_score,
			tc.ext,
			tc.first_seen_at,
			tc.detail_fetched_at,
			tc.greeted_at,
			tc.created_at,
			tc.updated_at
		FROM task_candidates tc
		INNER JOIN users u ON u.id = tc.user_id
		INNER JOIN task_runs tr ON tr.id = tc.task_id
		LEFT JOIN positions p ON p.id = tr.position_id
		WHERE `+where+`
		ORDER BY tc.created_at DESC
		LIMIT $`+fmt.Sprint(len(args)+1)+`
		OFFSET $`+fmt.Sprint(len(args)+2)+`
		`,
		listArgs...,
	)
	if err != nil {
		return TaskCandidateListResult{}, err
	}
	defer rows.Close()

	items := make([]TaskCandidate, 0)
	for rows.Next() {
		item, err := scanTaskCandidate(rows)
		if err != nil {
			return TaskCandidateListResult{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return TaskCandidateListResult{}, err
	}
	return TaskCandidateListResult{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

// GetTaskCandidate 按 ID 读取当前团队内的候选人详情。
// tenantID 为当前用户团队 ID，candidateID 为候选人 ID。
func (s *PostgresCandidateStore) GetTaskCandidate(tenantID string, candidateID string) (TaskCandidate, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	item, err := scanTaskCandidate(s.db.QueryRowContext(
		ctx,
		`
		SELECT
			tc.id,
			tc.task_id,
			COALESCE(tr.position_id::text, ''),
			COALESCE(p.name, ''),
			u.email,
			tc.platform_id,
			tc.platform_candidate_id,
			tc.candidate_name,
			tc.birth_ym,
			tc.phone,
			tc.email,
			tc.work_region,
			tc.work_years,
			tc.expected_salary_min,
			tc.expected_salary_max,
			tc.basic_info,
			tc.education_level,
			tc.expected_position,
			tc.online_status,
			tc.personal_description,
			tc.raw_text,
			tc.filter_text,
			tc.work_experiences,
			tc.educations,
			tc.certificates,
			tc.honors,
			tc.project_experiences,
			tc.colleague_communications,
			tc.resume_attachment_url,
			tc.resume_attachment_extracted_text,
			tc.ai_detail_reason,
			tc.ai_detail_score,
			tc.ai_greet_reason,
			tc.ai_greet_score,
			tc.ai_review_reason,
			tc.ai_review_score,
			tc.ext,
			tc.first_seen_at,
			tc.detail_fetched_at,
			tc.greeted_at,
			tc.created_at,
			tc.updated_at
		FROM task_candidates tc
		INNER JOIN users u ON u.id = tc.user_id
		INNER JOIN task_runs tr ON tr.id = tc.task_id
		LEFT JOIN positions p ON p.id = tr.position_id
		WHERE u.tenant_id = $1 AND tc.id::text = $2
		`,
		tenantID,
		candidateID,
	))
	if err == sql.ErrNoRows {
		return TaskCandidate{}, ErrNotFound
	}
	return item, err
}

// buildCandidateWhere 组装候选人查询条件和参数。
// tenantID 为当前团队 ID，query 为前端传入筛选条件。
func buildCandidateWhere(tenantID string, query TaskCandidateQuery) (string, []any) {
	clauses := []string{"u.tenant_id = $1"}
	args := []any{tenantID}
	if query.TaskID != "" {
		args = append(args, query.TaskID)
		clauses = append(clauses, fmt.Sprintf("tc.task_id::text = $%d", len(args)))
	}
	if query.PositionID != "" {
		args = append(args, query.PositionID)
		clauses = append(clauses, fmt.Sprintf("tr.position_id::text = $%d", len(args)))
	}
	if query.Keyword != "" {
		args = append(args, "%"+query.Keyword+"%")
		placeholder := fmt.Sprintf("$%d", len(args))
		clauses = append(clauses, `(tc.candidate_name ILIKE `+placeholder+`
			OR tc.phone ILIKE `+placeholder+`
			OR tc.email ILIKE `+placeholder+`
			OR tc.work_region ILIKE `+placeholder+`
			OR tc.work_years ILIKE `+placeholder+`
			OR tc.education_level ILIKE `+placeholder+`
			OR tc.expected_position ILIKE `+placeholder+`
			OR tc.basic_info ILIKE `+placeholder+`
			OR tc.personal_description ILIKE `+placeholder+`
			OR tc.raw_text ILIKE `+placeholder+`
			OR tc.filter_text ILIKE `+placeholder+`
			OR tc.resume_attachment_extracted_text ILIKE `+placeholder+`)`)
	}
	return strings.Join(clauses, " AND "), args
}

// candidateScanner 抽象 QueryRow 和 Rows 的 Scan 能力。
type candidateScanner interface {
	Scan(dest ...any) error
}

// scanTaskCandidate 从数据库行解析候选人结构。
// scanner 为数据库扫描器，返回可直接给前端转换的候选人记录。
func scanTaskCandidate(scanner candidateScanner) (TaskCandidate, error) {
	var item TaskCandidate
	var workExperiences []byte
	var educations []byte
	var certificates []byte
	var honors []byte
	var projectExperiences []byte
	var communications []byte
	var ext []byte
	err := scanner.Scan(
		&item.ID,
		&item.TaskID,
		&item.PositionID,
		&item.PositionName,
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
		&item.RawText,
		&item.FilterText,
		&workExperiences,
		&educations,
		&certificates,
		&honors,
		&projectExperiences,
		&communications,
		&item.ResumeURL,
		&item.ResumeText,
		&item.AIDetailReason,
		&item.AIDetailScore,
		&item.AIGreetReason,
		&item.AIGreetScore,
		&item.AIReviewReason,
		&item.AIReviewScore,
		&ext,
		&item.FirstSeenAt,
		&item.DetailFetchedAt,
		&item.GreetedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return TaskCandidate{}, err
	}
	decodeCandidateJSON(workExperiences, &item.WorkExperiences)
	decodeCandidateJSON(educations, &item.Educations)
	decodeCandidateJSON(certificates, &item.Certificates)
	decodeCandidateJSON(honors, &item.Honors)
	decodeCandidateJSON(projectExperiences, &item.ProjectExperiences)
	decodeCandidateJSON(communications, &item.Communications)
	decodeCandidateJSON(ext, &item.Ext)
	return item, nil
}

// decodeCandidateJSON 安全解析候选人 JSON 字段。
// raw 为数据库 JSONB 原文，target 为目标结构指针。
func decodeCandidateJSON(raw []byte, target any) {
	if len(raw) == 0 {
		return
	}
	_ = json.Unmarshal(raw, target)
}
