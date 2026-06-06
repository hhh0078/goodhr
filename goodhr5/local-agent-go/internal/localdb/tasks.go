// Package localdb 负责管理本地任务、日志和候选人数据。
package localdb

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Task 表示本地任务记录。
type Task struct {
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	PlatformID        string         `json:"platform_id"`
	PlatformAccountID string         `json:"platform_account_id"`
	PositionID        string         `json:"position_id"`
	Mode              string         `json:"mode"`
	MatchLimit        int            `json:"match_limit"`
	Status            string         `json:"status"`
	ScannedCount      int            `json:"scanned_count"`
	GreetedCount      int            `json:"greeted_count"`
	SkippedCount      int            `json:"skipped_count"`
	FailedCount       int            `json:"failed_count"`
	EnableSound       bool           `json:"enable_sound"`
	PositionSnapshot  map[string]any `json:"position_snapshot"`
	CreatedAt         string         `json:"created_at"`
	UpdatedAt         string         `json:"updated_at"`
}

// Log 表示本地任务日志。
type Log struct {
	ID        int64  `json:"id"`
	TaskID    string `json:"task_id"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
}

// CreateTask 创建本地任务。
// payload 为任务参数，返回新建任务。
func (db *DB) CreateTask(payload map[string]any) (Task, error) {
	now := nowISO()
	task := Task{
		ID:                stringOr(payload["id"], uuid.NewString()),
		Name:              stringOr(payload["name"], ""),
		PlatformID:        stringOr(payload["platform_id"], "boss"),
		PlatformAccountID: stringOr(payload["platform_account_id"], ""),
		PositionID:        stringOr(payload["position_id"], ""),
		Mode:              stringOr(payload["mode"], "ai"),
		MatchLimit:        maxInt(0, intValue(payload["match_limit"])),
		Status:            "pending",
		EnableSound:       boolValue(payload["enable_sound"]),
		PositionSnapshot:  mapValue(payload["position_snapshot"]),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	positionJSON, err := json.Marshal(task.PositionSnapshot)
	if err != nil {
		return Task{}, fmt.Errorf("岗位快照格式不正确：%w", err)
	}
	_, err = db.conn.Exec(`
INSERT INTO local_tasks (
    id, name, platform_id, platform_account_id, position_id, mode, match_limit,
    status, enable_sound, position_snapshot, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.Name, task.PlatformID, task.PlatformAccountID, task.PositionID, task.Mode,
		task.MatchLimit, task.Status, boolInt(task.EnableSound), string(positionJSON), task.CreatedAt, task.UpdatedAt,
	)
	if err != nil {
		return Task{}, fmt.Errorf("创建本地任务失败：%w", err)
	}
	return task, nil
}

// ListTasks 读取本地任务列表。
// 返回值按创建时间倒序排列。
func (db *DB) ListTasks() ([]Task, error) {
	rows, err := db.conn.Query(`SELECT * FROM local_tasks ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("读取本地任务失败：%w", err)
	}
	defer rows.Close()
	tasks := []Task{}
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

// GetTask 读取单个本地任务。
// taskID 为任务 ID。
func (db *DB) GetTask(taskID string) (Task, error) {
	row := db.conn.QueryRow(`SELECT * FROM local_tasks WHERE id=?`, taskID)
	task, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, fmt.Errorf("本地任务不存在")
	}
	return task, err
}

// UpdateTask 更新本地任务基础信息。
// taskID 为任务 ID，payload 为更新参数。
func (db *DB) UpdateTask(taskID string, payload map[string]any) (Task, error) {
	existing, err := db.GetTask(taskID)
	if err != nil {
		return Task{}, err
	}
	updated := existing
	updated.Name = stringOr(payload["name"], existing.Name)
	updated.PlatformID = stringOr(payload["platform_id"], existing.PlatformID)
	updated.PlatformAccountID = stringOr(payload["platform_account_id"], existing.PlatformAccountID)
	updated.PositionID = stringOr(payload["position_id"], existing.PositionID)
	updated.Mode = stringOr(payload["mode"], existing.Mode)
	if _, ok := payload["match_limit"]; ok {
		updated.MatchLimit = maxInt(0, intValue(payload["match_limit"]))
	}
	if _, ok := payload["enable_sound"]; ok {
		updated.EnableSound = boolValue(payload["enable_sound"])
	}
	if _, ok := payload["position_snapshot"]; ok {
		updated.PositionSnapshot = mapValue(payload["position_snapshot"])
	}
	updated.UpdatedAt = nowISO()
	positionJSON, err := json.Marshal(updated.PositionSnapshot)
	if err != nil {
		return Task{}, fmt.Errorf("岗位快照格式不正确：%w", err)
	}
	_, err = db.conn.Exec(`
UPDATE local_tasks
SET name=?, platform_id=?, platform_account_id=?, position_id=?, mode=?,
    match_limit=?, enable_sound=?, position_snapshot=?, updated_at=?
WHERE id=?`,
		updated.Name, updated.PlatformID, updated.PlatformAccountID, updated.PositionID,
		updated.Mode, updated.MatchLimit, boolInt(updated.EnableSound), string(positionJSON),
		updated.UpdatedAt, taskID,
	)
	if err != nil {
		return Task{}, fmt.Errorf("更新本地任务失败：%w", err)
	}
	return db.GetTask(taskID)
}

// UpdateTaskStatus 更新任务状态。
// taskID 为任务 ID，status 为新状态。
func (db *DB) UpdateTaskStatus(taskID string, status string) (Task, error) {
	if status == "" {
		return Task{}, fmt.Errorf("任务状态不能为空")
	}
	result, err := db.conn.Exec(`UPDATE local_tasks SET status=?, updated_at=? WHERE id=?`, status, nowISO(), taskID)
	if err != nil {
		return Task{}, fmt.Errorf("更新任务状态失败：%w", err)
	}
	if count, _ := result.RowsAffected(); count <= 0 {
		return Task{}, fmt.Errorf("本地任务不存在")
	}
	return db.GetTask(taskID)
}

// IncrementTaskCounts 累加任务统计数量。
// taskID 为任务 ID，scanned/greeted/skipped/failed 为增量。
func (db *DB) IncrementTaskCounts(taskID string, scanned int, greeted int, skipped int, failed int) (Task, error) {
	result, err := db.conn.Exec(`
UPDATE local_tasks
SET scanned_count=scanned_count+?,
    greeted_count=greeted_count+?,
    skipped_count=skipped_count+?,
    failed_count=failed_count+?,
    updated_at=?
WHERE id=?`,
		maxInt(0, scanned), maxInt(0, greeted), maxInt(0, skipped), maxInt(0, failed), nowISO(), taskID,
	)
	if err != nil {
		return Task{}, fmt.Errorf("更新任务统计失败：%w", err)
	}
	if count, _ := result.RowsAffected(); count <= 0 {
		return Task{}, fmt.Errorf("本地任务不存在")
	}
	return db.GetTask(taskID)
}

// DeleteTask 删除本地任务及关联数据。
// taskID 为任务 ID。
func (db *DB) DeleteTask(taskID string) error {
	result, err := db.conn.Exec(`DELETE FROM local_tasks WHERE id=?`, taskID)
	if err != nil {
		return fmt.Errorf("删除本地任务失败：%w", err)
	}
	if count, _ := result.RowsAffected(); count <= 0 {
		return fmt.Errorf("本地任务不存在")
	}
	return nil
}

// AddTaskLog 写入本地任务日志。
// taskID 为任务 ID，level 为日志级别，message 为日志内容。
func (db *DB) AddTaskLog(taskID string, level string, message string) (Log, error) {
	if _, err := db.GetTask(taskID); err != nil {
		return Log{}, err
	}
	if level == "" {
		level = "info"
	}
	now := nowISO()
	result, err := db.conn.Exec(
		`INSERT INTO local_task_logs(task_id, level, message, created_at) VALUES(?, ?, ?, ?)`,
		taskID, level, message, now,
	)
	if err != nil {
		return Log{}, fmt.Errorf("写入任务日志失败：%w", err)
	}
	id, _ := result.LastInsertId()
	return Log{ID: id, TaskID: taskID, Level: level, Message: message, CreatedAt: now}, nil
}

// ListTaskLogs 读取本地任务日志。
// taskID 为任务 ID，limit 为最大返回数量。
func (db *DB) ListTaskLogs(taskID string, limit int) ([]Log, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := db.conn.Query(
		`SELECT id, task_id, level, message, created_at FROM local_task_logs WHERE task_id=? ORDER BY id DESC LIMIT ?`,
		taskID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("读取任务日志失败：%w", err)
	}
	defer rows.Close()
	logs := []Log{}
	for rows.Next() {
		var item Log
		if err := rows.Scan(&item.ID, &item.TaskID, &item.Level, &item.Message, &item.CreatedAt); err != nil {
			return nil, err
		}
		logs = append([]Log{item}, logs...)
	}
	return logs, rows.Err()
}

// SaveCandidate 保存本地候选人快照。
// taskID 为任务 ID，candidate 为候选人数据。
func (db *DB) SaveCandidate(taskID string, candidate map[string]any) (map[string]any, error) {
	if _, err := db.GetTask(taskID); err != nil {
		return nil, err
	}
	now := nowISO()
	candidateID := stringOr(candidate["id"], uuid.NewString())
	candidate["id"] = candidateID
	candidateName := stringOr(candidate["candidate_name"], stringOr(candidate["name"], ""))
	status := stringOr(candidate["status"], "")
	payload, err := json.Marshal(candidate)
	if err != nil {
		return nil, fmt.Errorf("候选人数据格式不正确：%w", err)
	}
	_, err = db.conn.Exec(`
INSERT INTO local_candidates(id, task_id, candidate_name, status, payload, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(task_id, id) DO UPDATE SET
    candidate_name=excluded.candidate_name,
    status=excluded.status,
    payload=excluded.payload,
    updated_at=excluded.updated_at`,
		candidateID, taskID, candidateName, status, string(payload), now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("保存候选人失败：%w", err)
	}
	return candidate, nil
}

// ListCandidates 读取本地候选人列表。
// taskID 为任务 ID。
func (db *DB) ListCandidates(taskID string) ([]map[string]any, error) {
	rows, err := db.conn.Query(`SELECT payload FROM local_candidates WHERE task_id=? ORDER BY updated_at DESC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("读取候选人失败：%w", err)
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		item := map[string]any{}
		if err := json.Unmarshal([]byte(raw), &item); err == nil {
			result = append(result, item)
		}
	}
	return result, rows.Err()
}

// scanTask 从数据库行扫描任务。
// scanner 为 QueryRow 或 Rows。
func scanTask(scanner interface{ Scan(dest ...any) error }) (Task, error) {
	var task Task
	var enableSound int
	var positionJSON string
	err := scanner.Scan(
		&task.ID, &task.Name, &task.PlatformID, &task.PlatformAccountID, &task.PositionID, &task.Mode,
		&task.MatchLimit, &task.Status, &task.ScannedCount, &task.GreetedCount, &task.SkippedCount,
		&task.FailedCount, &enableSound, &positionJSON, &task.CreatedAt, &task.UpdatedAt,
	)
	if err != nil {
		return Task{}, err
	}
	task.EnableSound = enableSound == 1
	task.PositionSnapshot = map[string]any{}
	_ = json.Unmarshal([]byte(positionJSON), &task.PositionSnapshot)
	return task, nil
}

// nowISO 返回当前 UTC 时间字符串。
// 返回值用于数据库 created_at 和 updated_at。
func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// stringOr 将值转换为字符串，空值使用默认值。
// value 为原始值，fallback 为默认值。
func stringOr(value any, fallback string) string {
	if text, ok := value.(string); ok && text != "" {
		return text
	}
	return fallback
}

// intValue 将值转换为整数。
// value 为原始值。
func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	case json.Number:
		v, _ := typed.Int64()
		return int(v)
	default:
		return 0
	}
}

// boolValue 将值转换为布尔值。
// value 为原始值。
func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case float64:
		return typed != 0
	default:
		return false
	}
}

// mapValue 将值转换为 map。
// value 为原始值。
func mapValue(value any) map[string]any {
	if item, ok := value.(map[string]any); ok && item != nil {
		return item
	}
	return map[string]any{}
}

// boolInt 将布尔值转换为 SQLite 整数。
// value 为布尔值。
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// maxInt 返回两个整数中的较大值。
// a 和 b 为参与比较的整数。
func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
