// 本文件负责提供岗位日志摘要的 PostgreSQL 存储实现。
package httpapi

import (
	"context"
	"database/sql"
	"time"
)

// PostgresPositionLogStore 使用 PostgreSQL 持久化岗位日志摘要。
type PostgresPositionLogStore struct {
	db *sql.DB
}

// NewPostgresPositionLogStore 创建 PostgreSQL 岗位日志存储。
func NewPostgresPositionLogStore(db *sql.DB) *PostgresPositionLogStore {
	return &PostgresPositionLogStore{db: db}
}

// ReplacePositionLogs 在一个事务中覆盖该用户岗位的旧日志，最多保存最新 1000 条。
func (s *PostgresPositionLogStore) ReplacePositionLogs(positionID, userEmail string, logs []PositionLog) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	userID, err := ensureUserID(ctx, s.db, userEmail)
	if err != nil {
		return err
	}
	if len(logs) > maxPositionLogsPerPosition {
		logs = logs[len(logs)-maxPositionLogsPerPosition:]
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, positionID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM position_logs WHERE position_id=$1 AND user_id=$2`, positionID, userID); err != nil {
		return err
	}
	statement, err := tx.PrepareContext(ctx, `INSERT INTO position_logs(position_id,user_id,level,message,created_at) VALUES($1,$2,$3,$4,$5)`)
	if err != nil {
		return err
	}
	defer statement.Close()
	for _, item := range logs {
		createdAt := item.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		if _, err = statement.ExecContext(ctx, positionID, userID, item.Level, item.Message, createdAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AddPositionLog 新增一条 PostgreSQL 岗位日志摘要。
func (s *PostgresPositionLogStore) AddPositionLog(log PositionLog) (PositionLog, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	userID, err := ensureUserID(ctx, s.db, log.UserEmail)
	if err != nil {
		return PositionLog{}, err
	}

	level := log.Level
	if level == "" {
		level = "info"
	}
	createdAt := log.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	var saved PositionLog
	saved.UserEmail = log.UserEmail
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PositionLog{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, log.PositionID); err != nil {
		return PositionLog{}, err
	}
	err = tx.QueryRowContext(
		ctx,
		`
		INSERT INTO position_logs (position_id, user_id, level, message, created_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, position_id, level, message, created_at
		`,
		log.PositionID,
		userID,
		level,
		log.Message,
		createdAt,
	).Scan(
		&saved.ID,
		&saved.PositionID,
		&saved.Level,
		&saved.Message,
		&saved.CreatedAt,
	)
	if err != nil {
		return PositionLog{}, err
	}
	if _, err = tx.ExecContext(ctx, `
		DELETE FROM position_logs WHERE position_id=$1 AND user_id=$2 AND id NOT IN (
			SELECT id FROM position_logs WHERE position_id=$1 AND user_id=$2
			ORDER BY created_at DESC, id DESC LIMIT $3
		)`, log.PositionID, userID, maxPositionLogsPerPosition); err != nil {
		return PositionLog{}, err
	}
	if err = tx.Commit(); err != nil {
		return PositionLog{}, err
	}
	return saved, nil
}

// ListPositionLogs 列出 PostgreSQL 中当前用户某个岗位的日志摘要。
func (s *PostgresPositionLogStore) ListPositionLogs(tenantID, userEmail, positionID string, isAdmin bool, logQuery PositionLogQuery) ([]PositionLog, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	query := `
		SELECT tl.id, tl.position_id, tl.level, tl.message, tl.created_at
		FROM position_logs tl
		INNER JOIN users u ON u.id = tl.user_id
		WHERE u.email = $1 AND tl.position_id = $2
	`
	args := []any{userEmail, positionID}
	if logQuery.Since != nil {
		query += ` AND tl.created_at >= $` + intString(len(args)+1)
		args = append(args, *logQuery.Since)
	}
	if logQuery.Before != nil {
		query += ` AND tl.created_at < $` + intString(len(args)+1)
		args = append(args, *logQuery.Before)
	}
	query += `
		ORDER BY tl.created_at DESC
		LIMIT $` + intString(len(args)+1) + `
	`
	limit := normalizePositionLogLimit(logQuery.Limit)
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	items := make([]PositionLog, 0)
	for rows.Next() {
		var item PositionLog
		item.UserEmail = userEmail
		if err := rows.Scan(
			&item.ID,
			&item.PositionID,
			&item.Level,
			&item.Message,
			&item.CreatedAt,
		); err != nil {
			return nil, false, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore, nil
}

// ClearPositionLogs 清空 PostgreSQL 中当前用户某个岗位的日志摘要。
func (s *PostgresPositionLogStore) ClearPositionLogs(tenantID, userEmail, positionID string, isAdmin bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	query := `
		DELETE FROM position_logs tl
		USING users u
		WHERE tl.user_id = u.id
		  AND tl.position_id = $1
		  AND u.email = $2
	`
	args := []any{positionID, userEmail}
	if isAdmin {
		query = `
			DELETE FROM position_logs
			WHERE position_id = $1
		`
		args = []any{positionID}
	}
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

// SummarizePositionCounts 汇总 PostgreSQL 中各岗位的扫描/打招呼/跳过/失败数量。
func (s *PostgresPositionLogStore) SummarizePositionCounts(tenantID, userEmail string, isAdmin bool, since *time.Time) (map[string]PositionCountSummary, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	query := `
		SELECT tl.position_id, tl.message
		FROM position_logs tl
		INNER JOIN positions tr ON tr.id = tl.position_id
		INNER JOIN users u ON u.id = tr.user_id
		WHERE u.tenant_id = NULLIF($1, '')::uuid
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

	result := map[string]PositionCountSummary{}
	for rows.Next() {
		var positionID string
		var message string
		if err := rows.Scan(&positionID, &message); err != nil {
			return nil, err
		}
		scanned, greeted, skipped, failed := classifyPositionLogMessage(message)
		if scanned == 0 && greeted == 0 && skipped == 0 && failed == 0 {
			continue
		}
		item := result[positionID]
		item.ScannedCount += scanned
		item.GreetedCount += greeted
		item.SkippedCount += skipped
		item.FailedCount += failed
		result[positionID] = item
	}
	return result, rows.Err()
}

// trimPositionLogs 写入前检查当前岗位日志数量，超过上限时删除最早日志。
func (s *PostgresPositionLogStore) trimPositionLogs(ctx context.Context, positionID, userEmail string, incoming int) error {
	userID, err := ensureUserID(ctx, s.db, userEmail)
	if err != nil {
		return err
	}
	var count int
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM position_logs WHERE position_id=$1 AND user_id=$2`,
		positionID,
		userID,
	).Scan(&count); err != nil {
		return err
	}
	removeCount := count + incoming - maxPositionLogsPerPosition
	if removeCount <= 0 {
		return nil
	}
	_, err = s.db.ExecContext(
		ctx,
		`
		DELETE FROM position_logs
		WHERE id IN (
			SELECT id
			FROM position_logs
			WHERE position_id=$1 AND user_id=$2
			ORDER BY created_at ASC
			LIMIT $3
		)
		`,
		positionID,
		userID,
		removeCount,
	)
	return err
}
