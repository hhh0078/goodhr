// Package localdb 负责管理 Go 版本本地 SQLite 数据库。
package localdb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"goodhr5/local-agent-go/internal/config"

	_ "modernc.org/sqlite"
)

// DB 封装本地 SQLite 数据库连接。
type DB struct {
	conn *sql.DB
	path string
}

// Open 打开并初始化本地 SQLite 数据库。
// cfg 为本地程序配置，返回数据库对象。
func Open(cfg *config.Config) (*DB, error) {
	if cfg == nil || cfg.DataDir == "" {
		return nil, fmt.Errorf("本地数据目录为空")
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建本地数据目录失败：%w", err)
	}
	dbPath := filepath.Join(cfg.DataDir, "goodhr_local_go.db")
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("打开本地数据库失败：%w", err)
	}
	db := &DB{conn: conn, path: dbPath}
	if err := db.migrate(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return db, nil
}

// Path 返回本地数据库文件路径。
// 返回值用于健康检查和排查问题。
func (db *DB) Path() string {
	if db == nil {
		return ""
	}
	return db.path
}

// Close 关闭本地数据库连接。
// 返回错误表示关闭失败。
func (db *DB) Close() error {
	if db == nil || db.conn == nil {
		return nil
	}
	return db.conn.Close()
}

// migrate 创建和升级本地数据库表结构。
// 表和字段使用 SQL 注释说明用途，便于后续维护。
func (db *DB) migrate() error {
	script := `
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;

-- local_meta 保存本地数据库版本等元信息。
CREATE TABLE IF NOT EXISTS local_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);

-- local_tasks 保存用户本机创建的任务。
CREATE TABLE IF NOT EXISTS local_tasks (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    platform_id TEXT NOT NULL DEFAULT '',
    platform_account_id TEXT NOT NULL DEFAULT '',
    position_id TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL DEFAULT 'ai',
    match_limit INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending',
    scanned_count INTEGER NOT NULL DEFAULT 0,
    greeted_count INTEGER NOT NULL DEFAULT 0,
    skipped_count INTEGER NOT NULL DEFAULT 0,
    failed_count INTEGER NOT NULL DEFAULT 0,
    enable_sound INTEGER NOT NULL DEFAULT 0,
    position_snapshot TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- local_task_logs 保存本地任务运行日志。
CREATE TABLE IF NOT EXISTS local_task_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT NOT NULL,
    level TEXT NOT NULL DEFAULT 'info',
    message TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    FOREIGN KEY(task_id) REFERENCES local_tasks(id) ON DELETE CASCADE
);

-- local_candidates 保存本地候选人快照。
CREATE TABLE IF NOT EXISTS local_candidates (
    id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    candidate_name TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    payload TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY(task_id, id),
    FOREIGN KEY(task_id) REFERENCES local_tasks(id) ON DELETE CASCADE
);

INSERT OR REPLACE INTO local_meta(key, value) VALUES('schema_version', '1');
`
	if _, err := db.conn.Exec(script); err != nil {
		return fmt.Errorf("初始化本地数据库失败：%w", err)
	}
	return nil
}
