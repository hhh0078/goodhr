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
	store := &Store{db: db}
	if err := store.Migrate(context.Background()); err != nil {
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
	return task, nil
}

// TaskExists 判断任务编号是否已经使用。
func (s *Store) TaskExists(ctx context.Context, taskID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_runs WHERE task_id = ?`, taskID).Scan(&count)
	return count > 0, err
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

// SaveCandidate 保存候选人动作摘要并自动去重。
func (s *Store) SaveCandidate(ctx context.Context, record CandidateRecord) error {
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
