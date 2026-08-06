// Package storage 文件作用：保存、读取和清理本地全局任务日志，供控制台统一查看。
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const maxTaskLogs = 5000

// TaskLog 表示一条不含敏感页面正文的本地任务日志。
type TaskLog struct {
	ID         int64  `json:"id"`
	TaskID     string `json:"task_id"`
	PositionID string `json:"position_id"`
	Flow       string `json:"flow"`
	Step       string `json:"step"`
	Status     string `json:"status"`
	Level      string `json:"level"`
	Message    string `json:"message"`
	DurationMS int64  `json:"duration_ms"`
	CreatedAt  string `json:"created_at"`
}

// SaveTaskLog 保存一条流程日志，并在任务已落库时自动补充岗位编号。
func (s *Store) SaveTaskLog(ctx context.Context, item TaskLog) (TaskLog, error) {
	item.TaskID = strings.TrimSpace(item.TaskID)
	item.PositionID = strings.TrimSpace(item.PositionID)
	item.Flow = strings.TrimSpace(item.Flow)
	item.Step = strings.TrimSpace(item.Step)
	item.Status = strings.TrimSpace(item.Status)
	item.Level = strings.TrimSpace(item.Level)
	item.Message = strings.TrimSpace(item.Message)
	if item.Level == "" {
		item.Level = logLevel(item.Status)
	}
	if item.CreatedAt == "" {
		item.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if item.PositionID == "" && item.TaskID != "" {
		_ = s.db.QueryRowContext(
			ctx,
			`SELECT position_id FROM task_runs WHERE task_id = ?`,
			item.TaskID,
		).Scan(&item.PositionID)
	}
	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TaskLog{}, fmt.Errorf("开始保存任务日志失败：%w", err)
	}
	defer transaction.Rollback()
	result, err := transaction.ExecContext(ctx, `
		INSERT INTO task_logs (
			task_id, position_id, flow, step, status, level, message, duration_ms, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.TaskID,
		item.PositionID,
		item.Flow,
		item.Step,
		item.Status,
		item.Level,
		item.Message,
		max(item.DurationMS, 0),
		item.CreatedAt,
	)
	if err != nil {
		return TaskLog{}, fmt.Errorf("保存任务日志失败：%w", err)
	}
	item.ID, _ = result.LastInsertId()
	if err := trimTaskLogs(ctx, transaction); err != nil {
		return TaskLog{}, err
	}
	if err := transaction.Commit(); err != nil {
		return TaskLog{}, fmt.Errorf("提交任务日志失败：%w", err)
	}
	return item, nil
}

// ListTaskLogs 读取最近的全局本地任务日志，并按时间正序返回。
func (s *Store) ListTaskLogs(ctx context.Context, limit int) ([]TaskLog, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > maxTaskLogs {
		limit = maxTaskLogs
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, task_id, position_id, flow, step, status, level, message, duration_ms, created_at
		FROM (
			SELECT id, task_id, position_id, flow, step, status, level, message, duration_ms, created_at
			FROM task_logs
			ORDER BY id DESC
			LIMIT ?
		)
		ORDER BY id ASC`, limit)
	if err != nil {
		return nil, fmt.Errorf("读取全局日志失败：%w", err)
	}
	defer rows.Close()
	return scanTaskLogs(rows, "全局日志")
}

// ListPositionLogs 按岗位读取最近的本地任务日志，并按时间正序返回。
func (s *Store) ListPositionLogs(ctx context.Context, positionID string, limit int) ([]TaskLog, error) {
	positionID = strings.TrimSpace(positionID)
	if positionID == "" {
		return nil, fmt.Errorf("岗位编号不能为空")
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > maxTaskLogs {
		limit = maxTaskLogs
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, task_id, position_id, flow, step, status, level, message, duration_ms, created_at
		FROM (
			SELECT id, task_id, position_id, flow, step, status, level, message, duration_ms, created_at
			FROM task_logs
			WHERE position_id = ?
			ORDER BY id DESC
			LIMIT ?
		)
		ORDER BY id ASC`,
		positionID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("读取岗位日志失败：%w", err)
	}
	defer rows.Close()
	return scanTaskLogs(rows, "岗位日志")
}

// scanTaskLogs 把 SQLite 查询结果转换成任务日志数组。
func scanTaskLogs(rows *sql.Rows, label string) ([]TaskLog, error) {
	logs := make([]TaskLog, 0)
	for rows.Next() {
		var item TaskLog
		if err := rows.Scan(
			&item.ID,
			&item.TaskID,
			&item.PositionID,
			&item.Flow,
			&item.Step,
			&item.Status,
			&item.Level,
			&item.Message,
			&item.DurationMS,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("解析%s失败：%w", label, err)
		}
		logs = append(logs, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历%s失败：%w", label, err)
	}
	return logs, nil
}

// ClearTaskLogs 清空全部本地任务日志，并清理历史错误提示，避免旧错误反复弹窗。
func (s *Store) ClearTaskLogs(ctx context.Context) error {
	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始清空全局日志失败：%w", err)
	}
	defer transaction.Rollback()
	if _, err = transaction.ExecContext(ctx, `DELETE FROM task_logs`); err != nil {
		return fmt.Errorf("清空全局日志失败：%w", err)
	}
	if _, err = transaction.ExecContext(ctx, `
		UPDATE task_runs
		SET error_code = '', error_message = ''
	`); err != nil {
		return fmt.Errorf("清空历史错误失败：%w", err)
	}
	if err = transaction.Commit(); err != nil {
		return fmt.Errorf("提交全局日志清理失败：%w", err)
	}
	return nil
}

// ClearPositionLogs 清空一个岗位的全部本地任务日志。
func (s *Store) ClearPositionLogs(ctx context.Context, positionID string) error {
	positionID = strings.TrimSpace(positionID)
	if positionID == "" {
		return fmt.Errorf("岗位编号不能为空")
	}
	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始清空岗位日志失败：%w", err)
	}
	defer transaction.Rollback()
	if _, err = transaction.ExecContext(ctx, `DELETE FROM task_logs WHERE position_id = ?`, positionID); err != nil {
		return fmt.Errorf("清空岗位日志失败：%w", err)
	}
	if _, err = transaction.ExecContext(ctx, `
		UPDATE task_runs
		SET error_code = '', error_message = ''
		WHERE position_id = ?`,
		positionID,
	); err != nil {
		return fmt.Errorf("清空岗位历史错误失败：%w", err)
	}
	if err = transaction.Commit(); err != nil {
		return fmt.Errorf("提交岗位日志清理失败：%w", err)
	}
	return nil
}

// attachTaskLogs 把任务启动前产生的日志补充到对应岗位。
func (s *Store) attachTaskLogs(ctx context.Context, taskID string, positionID string) error {
	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if _, err = transaction.ExecContext(ctx, `
		UPDATE task_logs SET position_id = ?
		WHERE task_id = ? AND position_id = ''`,
		positionID,
		taskID,
	); err != nil {
		return err
	}
	if err = trimTaskLogs(ctx, transaction); err != nil {
		return err
	}
	return transaction.Commit()
}

// trimTaskLogs 删除最早日志，全局只保留最近 5000 条。
func trimTaskLogs(ctx context.Context, transaction *sql.Tx) error {
	_, err := transaction.ExecContext(ctx, `
		DELETE FROM task_logs
		WHERE id NOT IN (
			SELECT id FROM task_logs
			ORDER BY id DESC
			LIMIT ?
		)`, maxTaskLogs)
	if err != nil {
		return fmt.Errorf("裁剪全局任务日志失败：%w", err)
	}
	return nil
}

// logLevel 把步骤状态转换为控制台日志级别。
func logLevel(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed":
		return "error"
	case "warning":
		return "warning"
	default:
		return "info"
	}
}
