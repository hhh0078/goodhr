// 本文件负责提供任务日志摘要的 PostgreSQL 存储实现。
package httpapi

import (
	"context"
	"database/sql"
	"time"
)

// PostgresTaskLogStore 使用 PostgreSQL 持久化任务日志摘要。
type PostgresTaskLogStore struct {
	db *sql.DB
}

// NewPostgresTaskLogStore 创建 PostgreSQL 任务日志存储。
func NewPostgresTaskLogStore(db *sql.DB) *PostgresTaskLogStore {
	return &PostgresTaskLogStore{db: db}
}

// AddTaskLog 新增一条 PostgreSQL 任务日志摘要。
func (s *PostgresTaskLogStore) AddTaskLog(log TaskLog) (TaskLog, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	userID, err := ensureUserID(ctx, s.db, log.UserEmail)
	if err != nil {
		return TaskLog{}, err
	}

	level := log.Level
	if level == "" {
		level = "info"
	}

	var saved TaskLog
	saved.UserEmail = log.UserEmail
	err = s.db.QueryRowContext(
		ctx,
		`
		INSERT INTO task_logs (task_id, user_id, level, message)
		VALUES ($1, $2, $3, $4)
		RETURNING id, task_id, level, message, created_at
		`,
		log.TaskID,
		userID,
		level,
		log.Message,
	).Scan(
		&saved.ID,
		&saved.TaskID,
		&saved.Level,
		&saved.Message,
		&saved.CreatedAt,
	)
	if err != nil {
		return TaskLog{}, err
	}
	return saved, nil
}

// ListTaskLogs 列出 PostgreSQL 中当前用户某个任务的日志摘要。
func (s *PostgresTaskLogStore) ListTaskLogs(tenantID, userEmail, taskID string, isAdmin bool, since *time.Time) ([]TaskLog, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	query := `
		SELECT tl.id, tl.task_id, tl.level, tl.message, tl.created_at
		FROM task_logs tl
		INNER JOIN users u ON u.id = tl.user_id
		WHERE u.email = $1 AND tl.task_id = $2
	`
	args := []any{userEmail, taskID}
	if since != nil {
		query += ` AND tl.created_at >= $3`
		args = append(args, *since)
	}
	query += `
		ORDER BY tl.created_at DESC
	`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]TaskLog, 0)
	for rows.Next() {
		var item TaskLog
		item.UserEmail = userEmail
		if err := rows.Scan(
			&item.ID,
			&item.TaskID,
			&item.Level,
			&item.Message,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// SummarizeTaskCounts 汇总 PostgreSQL 中各任务的扫描/打招呼/跳过/失败数量。
func (s *PostgresTaskLogStore) SummarizeTaskCounts(tenantID, userEmail string, isAdmin bool, since *time.Time) (map[string]TaskCountSummary, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	query := `
		SELECT tl.task_id, tl.message
		FROM task_logs tl
		INNER JOIN task_runs tr ON tr.id = tl.task_id
		INNER JOIN users u ON u.id = tr.user_id
		WHERE u.tenant_id = $1
	`
	args := []any{tenantID}
	if !isAdmin {
		query += ` AND u.email = $2`
		args = append(args, userEmail)
	}
	if since != nil {
		query += ` AND tl.created_at >= $` + intString(len(args)+1)
		args = append(args, *since)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := map[string]TaskCountSummary{}
	for rows.Next() {
		var taskID string
		var message string
		if err := rows.Scan(&taskID, &message); err != nil {
			return nil, err
		}
		scanned, greeted, skipped, failed := classifyTaskLogMessage(message)
		if scanned == 0 && greeted == 0 && skipped == 0 && failed == 0 {
			continue
		}
		item := result[taskID]
		item.ScannedCount += scanned
		item.GreetedCount += greeted
		item.SkippedCount += skipped
		item.FailedCount += failed
		result[taskID] = item
	}
	return result, rows.Err()
}
