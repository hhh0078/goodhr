// Package storage 负责 SQLite 初始化、迁移和本地任务摘要持久化。
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"goodhr5/local-agent-go-new/migrations"
)

const localDataRetention = 90 * 24 * time.Hour

// Store 管理新本地程序 SQLite 连接。
type Store struct {
	db *sql.DB
}

// TaskRun 表示一条不含敏感详情的任务状态记录。
type TaskRun struct {
	TaskID       string    `json:"task_id"`
	PositionID   string    `json:"position_id"`
	PlatformID   string    `json:"platform_id"`
	TaskType     string    `json:"task_type"`
	Status       string    `json:"status"`
	CurrentStep  string    `json:"current_step"`
	Summary      string    `json:"summary"`
	ScannedCount int       `json:"scanned_count"`
	GreetedCount int       `json:"greeted_count"`
	SkippedCount int       `json:"skipped_count"`
	ErrorCode    string    `json:"error_code"`
	ErrorMessage string    `json:"error_message"`
	StartedAt    time.Time `json:"started_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	FinishedAt   time.Time `json:"finished_at,omitempty"`
}

// CandidateRecord 表示候选人动作摘要，不保存详情正文。
type CandidateRecord struct {
	TaskID      string
	Fingerprint string
	PlatformID  string
	DisplayName string
	Action      string
	Result      string
	Reason      string
}

// ConversationRecord 表示自动回复去重摘要。
type ConversationRecord struct {
	TaskID          string
	ConversationKey string
	PlatformID      string
	ReplyHash       string
	Result          string
}

// Open 打开 SQLite 并执行内嵌迁移。
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite 失败：%w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("设置 SQLite 写入等待时间失败：%w", err)
	}
	store := &Store{db: db}
	if err := store.Migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := store.RecoverInterruptedTasks(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := store.CleanupExpired(context.Background(), time.Now().UTC().Add(-localDataRetention)); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

// Migrate 按文件名顺序执行尚未应用的迁移。
func (s *Store) Migrate(ctx context.Context) error {
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		return fmt.Errorf("读取 SQLite 迁移失败：%w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if err := s.applyMigration(ctx, name); err != nil {
			return err
		}
	}
	return nil
}

// Ready 检查 SQLite 是否可以正常读写连接。
func (s *Store) Ready(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// SaveTask 新增或更新任务状态。
func (s *Store) SaveTask(ctx context.Context, task TaskRun) error {
	now := time.Now().UTC()
	if task.StartedAt.IsZero() {
		task.StartedAt = now
	}
	task.UpdatedAt = now
	finishedAt := ""
	if !task.FinishedAt.IsZero() {
		finishedAt = task.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO task_runs (
			task_id, position_id, platform_id, task_type, status, current_step,
			summary, error_code, error_message, started_at, updated_at, finished_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(task_id) DO UPDATE SET
			status=excluded.status,
			current_step=excluded.current_step,
			summary=excluded.summary,
			error_code=excluded.error_code,
			error_message=excluded.error_message,
			updated_at=excluded.updated_at,
			finished_at=excluded.finished_at`,
		task.TaskID,
		task.PositionID,
		task.PlatformID,
		task.TaskType,
		task.Status,
		task.CurrentStep,
		task.Summary,
		task.ErrorCode,
		task.ErrorMessage,
		task.StartedAt.UTC().Format(time.RFC3339Nano),
		task.UpdatedAt.Format(time.RFC3339Nano),
		finishedAt,
	)
	if err != nil {
		return fmt.Errorf("保存任务状态失败：%w", err)
	}
	if err := s.attachTaskLogs(ctx, task.TaskID, task.PositionID); err != nil {
		return fmt.Errorf("补充任务日志岗位编号失败：%w", err)
	}
	return nil
}

// Task 返回指定任务状态。
func (s *Store) Task(ctx context.Context, taskID string) (TaskRun, error) {
	var task TaskRun
	var startedAt, updatedAt, finishedAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT task_id, position_id, platform_id, task_type, status, current_step,
		       summary, error_code, error_message, started_at, updated_at, finished_at
		FROM task_runs WHERE task_id = ?`, taskID).Scan(
		&task.TaskID,
		&task.PositionID,
		&task.PlatformID,
		&task.TaskType,
		&task.Status,
		&task.CurrentStep,
		&task.Summary,
		&task.ErrorCode,
		&task.ErrorMessage,
		&startedAt,
		&updatedAt,
		&finishedAt,
	)
	if err != nil {
		return TaskRun{}, err
	}
	task.StartedAt, _ = time.Parse(time.RFC3339Nano, startedAt)
	task.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	task.FinishedAt, _ = time.Parse(time.RFC3339Nano, finishedAt)
	if err = s.fillTaskCandidateCounts(ctx, &task); err != nil {
		return TaskRun{}, err
	}
	return task, nil
}

// TaskExists 判断任务编号是否已经使用。
func (s *Store) TaskExists(ctx context.Context, taskID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_runs WHERE task_id = ?`, taskID).Scan(&count)
	return count > 0, err
}

// LatestTaskForPosition 返回指定岗位最近一次本地任务状态。
func (s *Store) LatestTaskForPosition(ctx context.Context, positionID string) (TaskRun, error) {
	var task TaskRun
	var startedAt, updatedAt, finishedAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT task_id, position_id, platform_id, task_type, status, current_step,
		       summary, error_code, error_message, started_at, updated_at, finished_at
		FROM task_runs
		WHERE position_id = ?
		ORDER BY started_at DESC
		LIMIT 1`, strings.TrimSpace(positionID)).Scan(
		&task.TaskID,
		&task.PositionID,
		&task.PlatformID,
		&task.TaskType,
		&task.Status,
		&task.CurrentStep,
		&task.Summary,
		&task.ErrorCode,
		&task.ErrorMessage,
		&startedAt,
		&updatedAt,
		&finishedAt,
	)
	if err != nil {
		return TaskRun{}, err
	}
	task.StartedAt, _ = time.Parse(time.RFC3339Nano, startedAt)
	task.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	task.FinishedAt, _ = time.Parse(time.RFC3339Nano, finishedAt)
	if err = s.fillTaskCandidateCounts(ctx, &task); err != nil {
		return TaskRun{}, err
	}
	return task, nil
}

// LatestTask 返回本地最近一次任务状态。
func (s *Store) LatestTask(ctx context.Context) (TaskRun, error) {
	return s.latestTaskByWhere(ctx, "", nil)
}

// LatestTaskForType 返回指定任务类型最近一次本地任务状态。
func (s *Store) LatestTaskForType(ctx context.Context, taskType string) (TaskRun, error) {
	taskType = strings.TrimSpace(taskType)
	if taskType == "" {
		return TaskRun{}, fmt.Errorf("任务类型不能为空")
	}
	return s.latestTaskByWhere(ctx, "WHERE task_type = ?", []any{taskType})
}

// latestTaskByWhere 根据可选过滤条件读取最近一次任务状态。
func (s *Store) latestTaskByWhere(ctx context.Context, whereClause string, args []any) (TaskRun, error) {
	var task TaskRun
	var startedAt, updatedAt, finishedAt string
	query := `
		SELECT task_id, position_id, platform_id, task_type, status, current_step,
			summary, error_code, error_message, started_at, updated_at, finished_at
		FROM task_runs
		` + whereClause + `
		ORDER BY started_at DESC
		LIMIT 1`
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&task.TaskID,
		&task.PositionID,
		&task.PlatformID,
		&task.TaskType,
		&task.Status,
		&task.CurrentStep,
		&task.Summary,
		&task.ErrorCode,
		&task.ErrorMessage,
		&startedAt,
		&updatedAt,
		&finishedAt,
	)
	if err != nil {
		return TaskRun{}, err
	}
	task.StartedAt, _ = time.Parse(time.RFC3339Nano, startedAt)
	task.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	task.FinishedAt, _ = time.Parse(time.RFC3339Nano, finishedAt)
	if err = s.fillTaskCandidateCounts(ctx, &task); err != nil {
		return TaskRun{}, err
	}
	return task, nil
}

// fillTaskCandidateCounts 从已有候选人摘要计算本次任务的扫描、打招呼和跳过数量。
func (s *Store) fillTaskCandidateCounts(ctx context.Context, task *TaskRun) error {
	if task == nil || strings.TrimSpace(task.TaskID) == "" {
		return nil
	}
	if task.TaskType == "auto_reply" {
		return s.fillTaskConversationCounts(ctx, task)
	}
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(DISTINCT fingerprint),
			COUNT(DISTINCT CASE WHEN action = 'greet' AND result = 'success' THEN fingerprint END),
			COUNT(DISTINCT CASE WHEN result = 'skipped' THEN fingerprint END)
		FROM candidate_records
		WHERE task_id = ?`,
		task.TaskID,
	).Scan(&task.ScannedCount, &task.GreetedCount, &task.SkippedCount)
	if err != nil {
		return fmt.Errorf("统计本次任务候选人数量失败：%w", err)
	}
	return nil
}

// fillTaskConversationCounts 从自动回复摘要计算本次处理会话、成功回复和转人工或跳过数量。
func (s *Store) fillTaskConversationCounts(ctx context.Context, task *TaskRun) error {
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN result = 'success' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN result != 'success' THEN 1 ELSE 0 END), 0)
		FROM conversation_records
		WHERE task_id = ?`,
		task.TaskID,
	).Scan(&task.ScannedCount, &task.GreetedCount, &task.SkippedCount)
	if err != nil {
		return fmt.Errorf("统计本次自动回复会话数量失败：%w", err)
	}
	return nil
}

// UpdateTaskStep 更新运行中任务的当前步骤。
func (s *Store) UpdateTaskStep(ctx context.Context, taskID string, step string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE task_runs SET current_step = ?, updated_at = ?
		WHERE task_id = ? AND status = 'running'`,
		step,
		time.Now().UTC().Format(time.RFC3339Nano),
		taskID,
	)
	return err
}

// RecoverInterruptedTasks 把上次异常退出遗留的 running 任务统一收尾为失败。
func (s *Store) RecoverInterruptedTasks(ctx context.Context) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `
		UPDATE task_runs SET
			status = 'failed',
			current_step = 'finished',
			summary = '上次运行被本地程序退出打断，已经自动收尾',
			error_code = 'AGENT_RESTARTED',
			error_message = '本地程序上次没有正常退出，请重新开始任务',
			updated_at = ?,
			finished_at = ?
		WHERE status = 'running'`,
		now,
		now,
	)
	if err != nil {
		return 0, fmt.Errorf("恢复异常中断任务失败：%w", err)
	}
	return result.RowsAffected()
}

// CleanupExpired 删除保留期限以前的任务、候选人、会话、下载和步骤日志摘要。
func (s *Store) CleanupExpired(ctx context.Context, cutoff time.Time) (int64, error) {
	cutoffText := cutoff.UTC().Format(time.RFC3339Nano)
	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("开始清理过期本地数据失败：%w", err)
	}
	defer transaction.Rollback()
	queries := []string{
		`DELETE FROM candidate_records WHERE created_at < ?`,
		`DELETE FROM conversation_records WHERE created_at < ?`,
		`DELETE FROM download_records WHERE created_at < ?`,
		`DELETE FROM task_logs WHERE created_at < ?`,
		`DELETE FROM task_runs WHERE status != 'running' AND updated_at < ?`,
	}
	var deleted int64
	for _, query := range queries {
		result, executeErr := transaction.ExecContext(ctx, query, cutoffText)
		if executeErr != nil {
			return 0, fmt.Errorf("清理过期本地数据失败：%w", executeErr)
		}
		count, countErr := result.RowsAffected()
		if countErr != nil {
			return 0, fmt.Errorf("统计过期本地数据失败：%w", countErr)
		}
		deleted += count
	}
	if err = transaction.Commit(); err != nil {
		return 0, fmt.Errorf("提交过期本地数据清理失败：%w", err)
	}
	return deleted, nil
}

// SaveCandidate 保存候选人动作摘要并自动去重。
func (s *Store) SaveCandidate(ctx context.Context, record CandidateRecord) error {
	record.Fingerprint = strings.TrimSpace(record.Fingerprint)
	if record.Fingerprint == "" {
		return fmt.Errorf("候选人稳定编号不能为空")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO candidate_records (
			task_id, fingerprint, platform_id, display_name, action, result, reason, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		record.TaskID,
		record.Fingerprint,
		record.PlatformID,
		record.DisplayName,
		record.Action,
		record.Result,
		record.Reason,
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

// SaveConversation 保存自动回复摘要并按回复哈希去重。
func (s *Store) SaveConversation(ctx context.Context, record ConversationRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO conversation_records (
			task_id, conversation_key, platform_id, reply_hash, result, created_at
		) VALUES (?, ?, ?, ?, ?, ?)`,
		record.TaskID,
		record.ConversationKey,
		record.PlatformID,
		record.ReplyHash,
		record.Result,
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

// ConversationExists 判断同一会话是否已经发送过相同回复。
func (s *Store) ConversationExists(ctx context.Context, taskID string, conversationKey string, replyHash string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM conversation_records
		WHERE task_id = ? AND conversation_key = ? AND reply_hash = ?`,
		taskID,
		conversationKey,
		replyHash,
	).Scan(&count)
	return count > 0, err
}

// Close 关闭 SQLite 连接。
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// applyMigration 在事务内执行一个尚未应用的迁移。
func (s *Store) applyMigration(ctx context.Context, name string) error {
	if name != "001_initial.sql" {
		var count int
		err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, name).Scan(&count)
		if err == nil && count > 0 {
			return nil
		}
	}
	content, err := migrations.Files.ReadFile(name)
	if err != nil {
		return fmt.Errorf("读取迁移 %s 失败：%w", name, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始迁移事务失败：%w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, string(content)); err != nil {
		return fmt.Errorf("执行迁移 %s 失败：%w", name, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES (?, ?)`, name, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("记录迁移 %s 失败：%w", name, err)
	}
	return tx.Commit()
}
