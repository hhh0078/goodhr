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
	positionID, err := s.nullPositionID(ctx, userID, task.PositionID)
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
			name,
			platform_account_id,
			position_id,
			platform_id,
			mode,
			match_limit,
			enable_sound,
			status,
			scanned_count,
			greeted_count,
			skipped_count,
			failed_count,
			local_task_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'created', 0, 0, 0, 0, $9)
		RETURNING
			id,
			name,
			platform_id,
			COALESCE(platform_account_id::text, ''),
			COALESCE(position_id::text, ''),
			mode,
			match_limit,
			enable_sound,
			status,
			scanned_count,
			greeted_count,
			skipped_count,
			failed_count,
			COALESCE(local_task_id, ''),
			created_at,
			started_at,
			finished_at
		`,
		userID,
		task.Name,
		platformAccountID,
		positionID,
		task.PlatformID,
		task.Mode,
		task.MatchLimit,
		task.EnableSound,
		localTaskID(task),
	).Scan(
		&saved.ID,
		&saved.Name,
		&saved.PlatformID,
		&saved.PlatformAccountID,
		&saved.PositionID,
		&saved.Mode,
		&saved.MatchLimit,
		&saved.EnableSound,
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
	if saved.LocalTaskID == "" {
		saved.LocalTaskID = saved.ID
	}
	return saved, nil
}

// ListTasks 列出 PostgreSQL 中当前用户的任务运行记录。
func (s *PostgresTaskStore) ListTasks(tenantID, userEmail string, isAdmin bool) ([]TaskRun, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := s.db.QueryContext(
		ctx,
		`
		SELECT
			tr.id,
			COALESCE(tr.name, ''),
			tr.platform_id,
			COALESCE(tr.platform_account_id::text, ''),
			COALESCE(tr.position_id::text, ''),
			tr.mode,
			tr.match_limit,
			tr.enable_sound,
			tr.status,
			tr.scanned_count,
			tr.greeted_count,
			tr.skipped_count,
			tr.failed_count,
			COALESCE(tr.local_task_id, ''),
			tr.created_at,
			tr.started_at,
			tr.finished_at
		FROM task_runs tr
		INNER JOIN users u ON u.id = tr.user_id
		WHERE u.tenant_id = $1
		  AND (u.email = $2 OR $3::boolean)
		ORDER BY tr.created_at DESC
		`,
		tenantID, userEmail, isAdmin,
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
			&item.Name,
			&item.PlatformID,
			&item.PlatformAccountID,
			&item.PositionID,
			&item.Mode,
			&item.MatchLimit,
			&item.EnableSound,
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
		normalizeTaskRunDefaults(&item)
		items = append(items, item)
	}
	return items, rows.Err()
}

// TaskByID 读取 PostgreSQL 中当前用户的单个任务运行记录。
func (s *PostgresTaskStore) TaskByID(tenantID, userEmail, taskID string, isAdmin bool) (TaskRun, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var item TaskRun
	item.UserEmail = userEmail
	err := s.db.QueryRowContext(
		ctx,
		`
		SELECT
			tr.id,
			COALESCE(tr.name, ''),
			tr.platform_id,
			COALESCE(tr.platform_account_id::text, ''),
			COALESCE(tr.position_id::text, ''),
			tr.mode,
			tr.match_limit,
			tr.enable_sound,
			tr.status,
			tr.scanned_count,
			tr.greeted_count,
			tr.skipped_count,
			tr.failed_count,
			COALESCE(tr.local_task_id, ''),
			tr.created_at,
			tr.started_at,
			tr.finished_at
		FROM task_runs tr
		INNER JOIN users u ON u.id = tr.user_id
		WHERE (($3::boolean AND $1 = '') OR u.tenant_id = $1) AND (u.email = $2 OR $3::boolean) AND tr.id = $4
		`,
		tenantID, userEmail, isAdmin, taskID,
	).Scan(
		&item.ID,
		&item.Name,
		&item.PlatformID,
		&item.PlatformAccountID,
		&item.PositionID,
		&item.Mode,
		&item.MatchLimit,
		&item.EnableSound,
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
	normalizeTaskRunDefaults(&item)
	return item, nil
}

func (s *PostgresTaskStore) DeleteTask(tenantID, userEmail, taskID string, isAdmin bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := s.db.ExecContext(
		ctx,
		`
		DELETE FROM task_runs tr
		USING users u
		WHERE tr.user_id = u.id
		  AND u.tenant_id = $1
		  AND (u.email = $2 OR $3::boolean)
		  AND tr.id = $4
		`,
		tenantID,
		userEmail,
		isAdmin,
		taskID,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateTask 更新 PostgreSQL 任务的可编辑参数。
func (s *PostgresTaskStore) UpdateTask(taskID string, task TaskRun) (TaskRun, error) {
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
	positionID, err := s.nullPositionID(ctx, userID, task.PositionID)
	if err != nil {
		return TaskRun{}, err
	}

	var saved TaskRun
	saved.UserEmail = task.UserEmail
	err = s.db.QueryRowContext(
		ctx,
		`
		UPDATE task_runs
		SET name=$1, platform_account_id=$2, position_id=$3, platform_id=$4, mode=$5, match_limit=$6, enable_sound=$7
		WHERE id=$8
		RETURNING
			id,
			name,
			platform_id,
			COALESCE(platform_account_id::text, ''),
			COALESCE(position_id::text, ''),
			mode,
			match_limit,
			enable_sound,
			status,
			scanned_count,
			greeted_count,
			skipped_count,
			failed_count,
			COALESCE(local_task_id, ''),
			created_at,
			started_at,
			finished_at
		`,
		task.Name,
		platformAccountID,
		positionID,
		task.PlatformID,
		task.Mode,
		task.MatchLimit,
		task.EnableSound,
		taskID,
	).Scan(
		&saved.ID,
		&saved.Name,
		&saved.PlatformID,
		&saved.PlatformAccountID,
		&saved.PositionID,
		&saved.Mode,
		&saved.MatchLimit,
		&saved.EnableSound,
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
	if errors.Is(err, sql.ErrNoRows) {
		return TaskRun{}, ErrNotFound
	}
	if err != nil {
		return TaskRun{}, err
	}
	normalizeTaskRunDefaults(&saved)
	return saved, nil
}

// normalizeTaskRunDefaults 兜底修正历史任务中的空字段。
// task 为读取到的任务记录，函数会补齐本地任务 ID 和任务名称。
func normalizeTaskRunDefaults(task *TaskRun) {
	if task == nil {
		return
	}
	if task.LocalTaskID == "" {
		task.LocalTaskID = task.ID
	}
	if task.Name == "" {
		task.Name = "未命名任务"
	}
}

// nullPositionID 校验岗位模板是否属于当前用户，并返回可写入数据库的值。
func (s *PostgresTaskStore) nullPositionID(ctx context.Context, userID string, positionID string) (sql.NullString, error) {
	if positionID == "" {
		return sql.NullString{}, nil
	}

	var savedID string
	err := s.db.QueryRowContext(
		ctx,
		`
		SELECT id
		FROM positions
		WHERE user_id = $1 AND id = $2
		`,
		userID,
		positionID,
	).Scan(&savedID)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.NullString{}, ErrNotFound
	}
	if err != nil {
		return sql.NullString{}, err
	}
	return sql.NullString{String: savedID, Valid: true}, nil
}

// nullPlatformAccountID 校验选择的 cookie 是否属于当前用户租户，并返回可写入数据库的值。
func (s *PostgresTaskStore) nullPlatformAccountID(ctx context.Context, userID string, platformAccountID string) (sql.NullString, error) {
	if platformAccountID == "" {
		return sql.NullString{}, nil
	}

	var cookieID string
	err := s.db.QueryRowContext(
		ctx,
		`
		SELECT cd.id
		FROM cookie_data cd
		INNER JOIN users u ON u.tenant_id = cd.tenant_id
		WHERE u.id = $1 AND cd.id = $2
		`,
		userID,
		platformAccountID,
	).Scan(&cookieID)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.NullString{}, ErrNotFound
	}
	if err != nil {
		return sql.NullString{}, err
	}
	return sql.NullString{String: cookieID, Valid: true}, nil
}

// UpdateTaskStatus 更新 PostgreSQL 任务状态。
func (s *PostgresTaskStore) UpdateTaskStatus(taskID string, status string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `
		UPDATE task_runs
		SET
			status=$1,
			started_at=CASE WHEN $1='running' THEN NOW() ELSE started_at END,
			finished_at=CASE WHEN $1 IN ('failed','stopped') THEN NOW() WHEN $1='running' THEN NULL ELSE finished_at END
		WHERE id=$2
	`, status, taskID)
	return err
}

func localTaskID(task TaskRun) string {
	if task.LocalTaskID != "" {
		return task.LocalTaskID
	}
	return task.ID
}

// IncrementTaskCounts 累加 PostgreSQL 任务统计计数。
func (s *PostgresTaskStore) IncrementTaskCounts(taskID string, scanned, greeted, skipped, failed int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx,
		`UPDATE task_runs SET scanned_count=scanned_count+$1, greeted_count=greeted_count+$2, skipped_count=skipped_count+$3, failed_count=failed_count+$4 WHERE id=$5`,
		scanned, greeted, skipped, failed, taskID)
	return err
}
