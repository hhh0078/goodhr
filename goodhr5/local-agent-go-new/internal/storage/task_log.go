// Package storage 文件作用：保存、读取和清理任务步骤日志，供本地控制台按岗位查看。
package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const maxPositionTaskLogs = 1000
const maxGlobalTaskLogPage = 5000

// TaskLogRetainedAfterAppend 判断再增加一条日志后原文是否仍在千条保留范围内，避免引用即将淘汰的记录。
func (s *Store) TaskLogRetainedAfterAppend(ctx context.Context, logID int64) (bool, error) {
	if logID <= 0 {
		return false, nil
	}
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM task_logs AS original WHERE original.id = ? AND (
			original.position_id = '' OR (
				SELECT COUNT(*) FROM task_logs AS newer WHERE newer.position_id = original.position_id AND newer.id > original.id
			) < ?
		)
	)`, logID, maxPositionTaskLogs-1).Scan(&exists)
	return exists, err
}

// TaskLog 表示一条本地任务日志，AI 失败原文在独立诊断字段中保存。
type TaskLog struct {
	ID           int64           `json:"id"`
	TaskID       string          `json:"task_id"`
	PositionID   string          `json:"position_id"`
	Flow         string          `json:"flow"`
	Step         string          `json:"step"`
	Status       string          `json:"status"`
	Level        string          `json:"level"`
	Message      string          `json:"message"`
	Category     string          `json:"category"`
	IsError      bool            `json:"is_error"`
	ErrorMessage string          `json:"error_message"`
	Input        json.RawMessage `json:"input"`
	Output       json.RawMessage `json:"output"`
	DurationMS   int64           `json:"duration_ms"`
	CreatedAt    string          `json:"created_at"`
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
	item.Category = strings.TrimSpace(item.Category)
	item.ErrorMessage = strings.TrimSpace(item.ErrorMessage)
	if !json.Valid(item.Input) {
		item.Input = json.RawMessage(`{}`)
	}
	if !json.Valid(item.Output) {
		item.Output = json.RawMessage(`{}`)
	}
	if item.Level == "" {
		item.Level = logLevel(item.Status)
	}
	if item.Level == "error" || item.Status == "failed" || item.Status == "panic" {
		item.IsError = true
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
			task_id, position_id, flow, step, status, level, message, duration_ms, created_at,
			category, is_error, error_message, input_json, output_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.TaskID,
		item.PositionID,
		item.Flow,
		item.Step,
		item.Status,
		item.Level,
		item.Message,
		max(item.DurationMS, 0),
		item.CreatedAt,
		item.Category,
		item.IsError,
		item.ErrorMessage,
		string(item.Input),
		string(item.Output),
	)
	if err != nil {
		return TaskLog{}, fmt.Errorf("保存任务日志失败：%w", err)
	}
	item.ID, _ = result.LastInsertId()
	if item.PositionID != "" {
		if err := trimPositionTaskLogs(ctx, transaction, item.PositionID); err != nil {
			return TaskLog{}, err
		}
	}
	if err := transaction.Commit(); err != nil {
		return TaskLog{}, fmt.Errorf("提交任务日志失败：%w", err)
	}
	return item, nil
}

// ListPositionLogs 从最近千条原始日志中隐藏正常鼠标细节，再按时间正序返回最后指定条数，不删除原始记录。
func (s *Store) ListPositionLogs(ctx context.Context, positionID string, limit int) ([]TaskLog, error) {
	positionID = strings.TrimSpace(positionID)
	if positionID == "" {
		return nil, fmt.Errorf("岗位编号不能为空")
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > maxPositionTaskLogs {
		limit = maxPositionTaskLogs
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, task_id, position_id, flow, step, status, level, message, duration_ms, created_at,
		       category, is_error, error_message, input_json, output_json
		FROM (
			SELECT id, task_id, position_id, flow, step, status, level, message, duration_ms, created_at,
			       category, is_error, error_message, input_json, output_json
			FROM task_logs
			WHERE position_id = ?
			ORDER BY id DESC
			LIMIT ?
		)
		ORDER BY id ASC`,
		positionID,
		maxPositionTaskLogs,
	)
	if err != nil {
		return nil, fmt.Errorf("读取岗位日志失败：%w", err)
	}
	defer rows.Close()
	return scanTaskLogs(rows, "岗位日志", limit)
}

// ListTaskLogs 读取最近的全局任务日志，并保持岗位日志原有保存规则。
func (s *Store) ListTaskLogs(ctx context.Context, limit int) ([]TaskLog, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > maxGlobalTaskLogPage {
		limit = maxGlobalTaskLogPage
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, task_id, position_id, flow, step, status, level, message, duration_ms, created_at,
		       category, is_error, error_message, input_json, output_json
		FROM (
			SELECT id, task_id, position_id, flow, step, status, level, message, duration_ms, created_at,
			       category, is_error, error_message, input_json, output_json
			FROM task_logs ORDER BY id DESC LIMIT ?
		) ORDER BY id ASC`, limit)
	if err != nil {
		return nil, fmt.Errorf("读取全局任务日志失败：%w", err)
	}
	defer rows.Close()
	return scanTaskLogs(rows, "全局任务日志", limit)
}

// scanTaskLogs 把岗位或全局日志查询结果转换为统一日志结构。
func scanTaskLogs(rows *sql.Rows, label string, limit int) ([]TaskLog, error) {
	logs := make([]TaskLog, 0)
	for rows.Next() {
		var item TaskLog
		var inputJSON, outputJSON string
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
			&item.Category,
			&item.IsError,
			&item.ErrorMessage,
			&inputJSON,
			&outputJSON,
		); err != nil {
			return nil, fmt.Errorf("解析%s失败：%w", label, err)
		}
		item.Input = json.RawMessage(inputJSON)
		item.Output = json.RawMessage(outputJSON)
		if !IsRoutineMouseDiagnostic(item) {
			logs = append(logs, item)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历%s失败：%w", label, err)
	}
	if len(logs) > limit {
		logs = logs[len(logs)-limit:]
	}
	return logs, nil
}

// IsRoutineMouseDiagnostic 只隐藏已确认正常的鼠标首尾，保留当前异常、恢复和无法解析的输出，不把页面历史快照当成本次故障。
func IsRoutineMouseDiagnostic(item TaskLog) bool {
	if item.Flow != "browser_worker" || item.IsError || strings.TrimSpace(item.ErrorMessage) != "" || strings.Contains(item.Category, "错误") || strings.Contains(item.Category, "异常") {
		return false
	}
	switch strings.TrimSpace(item.Step) {
	case "mouse_move", "mouse_down", "mouse_up", "mouse_wheel":
	default:
		return false
	}
	level := strings.ToLower(strings.TrimSpace(item.Level))
	if level != "" && level != "info" && level != "debug" {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(item.Status))
	if status != "start" && status != "success" {
		return false
	}
	message := strings.ToLower(item.Message)
	if strings.Contains(message, "恢复") || strings.Contains(message, "重试") || strings.Contains(message, "recover") || strings.Contains(message, "retry") {
		return false
	}
	var output map[string]any
	if json.Unmarshal(item.Output, &output) != nil || output == nil {
		return false
	}
	return !hasMouseDiagnosticEvidence(output, status, false)
}

// hasMouseDiagnosticEvidence 检查当前输出的明确异常字段，页面累计事件只保留在原始日志，不据此误判当前鼠标操作。
func hasMouseDiagnosticEvidence(value any, operationStatus string, pageSnapshot bool) bool {
	switch item := value.(type) {
	case map[string]any:
		for key, child := range item {
			key = strings.ToLower(key)
			if pageSnapshot && (key == "recent_page_events" || key == "recent_page_failures") {
				continue
			}
			if key == "is_error" || key == "page_closed" || key == "recovery_required" {
				flag, ok := child.(bool)
				if !ok || flag {
					return true
				}
			}
			if strings.Contains(key, "error") || strings.Contains(key, "failure") || strings.Contains(key, "recover") || key == "page_closed" {
				if mouseDiagnosticValueHasEvidence(child) {
					return true
				}
			}
			if key == "status" || key == "operation_outcome" {
				status, ok := child.(string)
				if !ok {
					return true
				}
				switch strings.ToLower(strings.TrimSpace(status)) {
				case "start", "success", "completed":
				case "pending", "running":
					if operationStatus != "start" {
						return true
					}
				default:
					return true
				}
			}
			if key == "level" {
				level, ok := child.(string)
				level = strings.ToLower(strings.TrimSpace(level))
				if !ok || (level != "" && level != "info" && level != "debug") {
					return true
				}
			}
			if key == "input_wait" {
				wait, ok := child.(map[string]any)
				if !ok || wait["status"] == nil {
					return true
				}
			}
			if key == "native_mouse" {
				if _, ok := child.(map[string]any); !ok {
					return true
				}
			}
			if hasMouseDiagnosticEvidence(child, operationStatus, pageSnapshot || key == "page_state" || key == "page_state_after" || key == "page_state_before") {
				return true
			}
		}
	case []any:
		for _, child := range item {
			if hasMouseDiagnosticEvidence(child, operationStatus, pageSnapshot) {
				return true
			}
		}
	}
	return false
}

// mouseDiagnosticValueHasEvidence 判断异常字段是否含有实际内容，空值和明确的零值不构成故障证据。
func mouseDiagnosticValueHasEvidence(value any) bool {
	switch item := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(item) != ""
	case bool:
		return item
	case float64:
		return item != 0
	case map[string]any:
		return len(item) > 0
	case []any:
		return len(item) > 0
	default:
		return true
	}
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

// ClearTaskLogs 清空全局本地任务日志及历史错误提示。
func (s *Store) ClearTaskLogs(ctx context.Context) error {
	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始清空全局日志失败：%w", err)
	}
	defer transaction.Rollback()
	if _, err = transaction.ExecContext(ctx, `DELETE FROM task_logs`); err != nil {
		return fmt.Errorf("清空全局日志失败：%w", err)
	}
	if _, err = transaction.ExecContext(ctx, `UPDATE task_runs SET error_code = '', error_message = ''`); err != nil {
		return fmt.Errorf("清空历史错误失败：%w", err)
	}
	if err = transaction.Commit(); err != nil {
		return fmt.Errorf("提交全局日志清理失败：%w", err)
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
	if err = trimPositionTaskLogs(ctx, transaction, positionID); err != nil {
		return err
	}
	return transaction.Commit()
}

// trimPositionTaskLogs 删除岗位最早日志，只保留最近 1000 条。
func trimPositionTaskLogs(ctx context.Context, transaction *sql.Tx, positionID string) error {
	_, err := transaction.ExecContext(ctx, `
		DELETE FROM task_logs
		WHERE position_id = ? AND id NOT IN (
			SELECT id FROM task_logs
			WHERE position_id = ?
			ORDER BY id DESC
			LIMIT ?
		)`,
		positionID,
		positionID,
		maxPositionTaskLogs,
	)
	if err != nil {
		return fmt.Errorf("裁剪岗位任务日志失败：%w", err)
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
