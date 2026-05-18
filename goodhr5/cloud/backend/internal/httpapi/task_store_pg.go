// 本文件负责提供任务运行记录的 PostgreSQL 存储实现。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// PostgresTaskStore 使用 PostgreSQL 持久化任务运行记录。
type PostgresTaskStore struct {
	db *sql.DB
}

// NewPostgresTaskStore 创建 PostgreSQL 任务存储。
func NewPostgresTaskStore(db *sql.DB) *PostgresTaskStore {
	return &PostgresTaskStore{db: db}
}

// CreateTask 创建 PostgreSQL 任务运行记录。
func (s *PostgresTaskStore) CreateTask(task TaskRun) (TaskRun, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	userID, err := ensureUserID(ctx, s.db, task.UserEmail)
	if err != nil {
		return TaskRun{}, err
	}

	platformAccountID, err := s.nullPlatformAccountID(ctx, userID, task.PlatformAccountID)
	if err != nil {
		return TaskRun{}, err
	}

	var saved TaskRun
	saved.UserEmail = task.UserEmail
	err = s.db.QueryRowContext(
		ctx,
		`
		INSERT INTO task_runs (
			user_id,
			platform_account_id,
			platform_id,
			mode,
			match_limit,
			status,
			scanned_count,
			greeted_count,
			skipped_count,
			failed_count,
			local_task_id
		)
		VALUES ($1, $2, $3, $4, $5, 'created', 0, 0, 0, 0, '')
		RETURNING
			id,
			platform_id,
			COALESCE(platform_account_id::text, ''),
			mode,
			match_limit,
			status,
			scanned_count,
			greeted_count,
			skipped_count,
			failed_count,
			local_task_id,
			created_at,
			started_at,
			finished_at
		`,
		userID,
		platformAccountID,
		task.PlatformID,
		task.Mode,
		task.MatchLimit,
	).Scan(
		&saved.ID,
		&saved.PlatformID,
		&saved.PlatformAccountID,
		&saved.Mode,
		&saved.MatchLimit,
		&saved.Status,
		&saved.ScannedCount,
		&saved.GreetedCount,
		&saved.SkippedCount,
		&saved.FailedCount,
		&saved.LocalTaskID,
		&saved.CreatedAt,
		&saved.StartedAt,
		&saved.FinishedAt,
	)
	if errors.Is(err, ErrNotFound) {
		return TaskRun{}, err
	}
	if err != nil {
		return TaskRun{}, err
	}
	return saved, nil
}

// ListTasks 列出 PostgreSQL 中当前用户的任务运行记录。
func (s *PostgresTaskStore) ListTasks(userEmail string) ([]TaskRun, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := s.db.QueryContext(
		ctx,
		`
		SELECT
			tr.id,
			tr.platform_id,
			COALESCE(tr.platform_account_id::text, ''),
			tr.mode,
			tr.match_limit,
			tr.status,
			tr.scanned_count,
			tr.greeted_count,
			tr.skipped_count,
			tr.failed_count,
			tr.local_task_id,
			tr.created_at,
			tr.started_at,
			tr.finished_at
		FROM task_runs tr
		INNER JOIN users u ON u.id = tr.user_id
		WHERE u.email = $1
		ORDER BY tr.created_at DESC
		`,
		userEmail,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]TaskRun, 0)
	for rows.Next() {
		var item TaskRun
		item.UserEmail = userEmail
		if err := rows.Scan(
			&item.ID,
			&item.PlatformID,
			&item.PlatformAccountID,
			&item.Mode,
			&item.MatchLimit,
			&item.Status,
			&item.ScannedCount,
			&item.GreetedCount,
			&item.SkippedCount,
			&item.FailedCount,
			&item.LocalTaskID,
			&item.CreatedAt,
			&item.StartedAt,
			&item.FinishedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// TaskByID 读取 PostgreSQL 中当前用户的单个任务运行记录。
func (s *PostgresTaskStore) TaskByID(userEmail string, taskID string) (TaskRun, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var item TaskRun
	item.UserEmail = userEmail
	err := s.db.QueryRowContext(
		ctx,
		`
		SELECT
			tr.id,
			tr.platform_id,
			COALESCE(tr.platform_account_id::text, ''),
			tr.mode,
			tr.match_limit,
			tr.status,
			tr.scanned_count,
			tr.greeted_count,
			tr.skipped_count,
			tr.failed_count,
			tr.local_task_id,
			tr.created_at,
			tr.started_at,
			tr.finished_at
		FROM task_runs tr
		INNER JOIN users u ON u.id = tr.user_id
		WHERE u.email = $1 AND tr.id = $2
		`,
		userEmail,
		taskID,
	).Scan(
		&item.ID,
		&item.PlatformID,
		&item.PlatformAccountID,
		&item.Mode,
		&item.MatchLimit,
		&item.Status,
		&item.ScannedCount,
		&item.GreetedCount,
		&item.SkippedCount,
		&item.FailedCount,
		&item.LocalTaskID,
		&item.CreatedAt,
		&item.StartedAt,
		&item.FinishedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskRun{}, ErrNotFound
	}
	if err != nil {
		return TaskRun{}, err
	}
	return item, nil
}

// nullPlatformAccountID 校验平台账号是否属于当前用户，并返回可写入数据库的值。
func (s *PostgresTaskStore) nullPlatformAccountID(ctx context.Context, userID string, platformAccountID string) (sql.NullString, error) {
	if platformAccountID == "" {
		return sql.NullString{}, nil
	}

	var accountID string
	err := s.db.QueryRowContext(
		ctx,
		`
		SELECT id
		FROM platform_accounts
		WHERE user_id = $1 AND id = $2
		`,
		userID,
		platformAccountID,
	).Scan(&accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.NullString{}, ErrNotFound
	}
	if err != nil {
		return sql.NullString{}, err
	}
	return sql.NullString{String: accountID, Valid: true}, nil
}
