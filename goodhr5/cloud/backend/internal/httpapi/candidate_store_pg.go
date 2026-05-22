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
			basic_info,
			education_level,
			personal_description,
			raw_text,
			filter_text
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id, task_id, platform_id, platform_candidate_id, candidate_name, basic_info, education_level, personal_description, raw_text, filter_text, created_at, updated_at
		`,
		item.TaskID,
		userID,
		item.PlatformID,
		item.PlatformCandidateID,
		item.CandidateName,
		item.BasicInfo,
		item.EducationLevel,
		item.PersonalDescription,
		item.RawText,
		item.FilterText,
	).Scan(
		&saved.ID,
		&saved.TaskID,
		&saved.PlatformID,
		&saved.PlatformCandidateID,
		&saved.CandidateName,
		&saved.BasicInfo,
		&saved.EducationLevel,
		&saved.PersonalDescription,
		&saved.RawText,
		&saved.FilterText,
		&saved.CreatedAt,
		&saved.UpdatedAt,
	)
	if err != nil {
		return TaskCandidate{}, err
	}
	return saved, nil
}
