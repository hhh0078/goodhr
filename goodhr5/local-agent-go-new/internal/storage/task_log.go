// Package storage 文件作用：保存、读取和清理任务步骤日志，供本地控制台按岗位查看。
package storage

import (
	"context"
	"fmt"
	"strings"
	"time"
)

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
	result, err := s.db.ExecContext(ctx, `
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
	return item, nil
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
	if limit > 5000 {
		limit = 5000
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
	logs := make([]TaskLog, 0)
	for rows.Next() {
		var item TaskLog
		if err = rows.Scan(
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
			return nil, fmt.Errorf("解析岗位日志失败：%w", err)
		}
		logs = append(logs, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历岗位日志失败：%w", err)
	}
	return logs, nil
}

// ClearPositionLogs 清空一个岗位的全部本地任务日志。
func (s *Store) ClearPositionLogs(ctx context.Context, positionID string) error {
	positionID = strings.TrimSpace(positionID)
	if positionID == "" {
		return fmt.Errorf("岗位编号不能为空")
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM task_logs WHERE position_id = ?`, positionID); err != nil {
		return fmt.Errorf("清空岗位日志失败：%w", err)
	}
	return nil
}

// attachTaskLogs 把任务启动前产生的日志补充到对应岗位。
func (s *Store) attachTaskLogs(ctx context.Context, taskID string, positionID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE task_logs SET position_id = ?
		WHERE task_id = ? AND position_id = ''`,
		positionID,
		taskID,
	)
	return err
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
