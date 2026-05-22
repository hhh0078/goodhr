// 本文件负责候选人 PostgreSQL 存储实现。
package httpapi

import (
	"context"
	"database/sql"
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
