// Package localdb 负责管理 Go 版本本地 SQLite 数据库。
package localdb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

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

-- local_positions 保存用户本机创建的岗位运行。
CREATE TABLE IF NOT EXISTS local_positions (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    platform_id TEXT NOT NULL DEFAULT '',
    platform_account_id TEXT NOT NULL DEFAULT '',
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

-- local_position_logs 保存本地岗位运行运行日志。
CREATE TABLE IF NOT EXISTS local_position_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    position_id TEXT NOT NULL,
    level TEXT NOT NULL DEFAULT 'info',
    message TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    FOREIGN KEY(position_id) REFERENCES local_positions(id) ON DELETE CASCADE
);

-- local_candidates 保存打招呼后的候选人结构化信息，结构与云端 position_candidates 对齐。
CREATE TABLE IF NOT EXISTS local_candidates (
    id TEXT NOT NULL,
    position_id TEXT NOT NULL,
    candidate_name TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    birth_ym TEXT NOT NULL DEFAULT '',
    phone TEXT NOT NULL DEFAULT '',
    email TEXT NOT NULL DEFAULT '',
    work_region TEXT NOT NULL DEFAULT '',
    work_years TEXT NOT NULL DEFAULT '',
    expected_salary_min INTEGER,
    expected_salary_max INTEGER,
    personal_description TEXT NOT NULL DEFAULT '',
    work_status TEXT NOT NULL DEFAULT '',
    expected_position TEXT NOT NULL DEFAULT '',
    online_status TEXT NOT NULL DEFAULT '',
    education_level TEXT NOT NULL DEFAULT '',
    basic_info TEXT NOT NULL DEFAULT '',
    raw_text TEXT NOT NULL DEFAULT '',
    filter_text TEXT NOT NULL DEFAULT '',
    work_experiences TEXT NOT NULL DEFAULT '[]',
    educations TEXT NOT NULL DEFAULT '[]',
    certificates TEXT NOT NULL DEFAULT '[]',
    honors TEXT NOT NULL DEFAULT '[]',
    project_experiences TEXT NOT NULL DEFAULT '[]',
    colleague_communications TEXT NOT NULL DEFAULT '[]',
    resume_attachment_url TEXT NOT NULL DEFAULT '',
    resume_attachment_extracted_text TEXT NOT NULL DEFAULT '',
    ai_detail_reason TEXT NOT NULL DEFAULT '',
    ai_detail_score REAL,
    ai_greet_reason TEXT NOT NULL DEFAULT '',
    ai_greet_score REAL,
    ai_review_reason TEXT NOT NULL DEFAULT '',
    ai_review_score REAL,
    ext TEXT NOT NULL DEFAULT '{}',
    first_seen_at TEXT NOT NULL DEFAULT '',
    detail_fetched_at TEXT NOT NULL DEFAULT '',
    greeted_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY(position_id, id),
    FOREIGN KEY(position_id) REFERENCES local_positions(id) ON DELETE CASCADE
);

-- local_settings 保存本机通用设置。
CREATE TABLE IF NOT EXISTS local_settings (
    -- 设置键名。
    key TEXT PRIMARY KEY,
    -- 设置值 JSON。
    value TEXT NOT NULL DEFAULT '',
    -- 更新时间。
    updated_at TEXT NOT NULL
);

-- local_downloads 保存本机下载记录。
CREATE TABLE IF NOT EXISTS local_downloads (
    -- 下载记录唯一 ID。
    id TEXT PRIMARY KEY,
    -- 关联岗位运行 ID。
    position_id TEXT NOT NULL DEFAULT '',
    -- 原始下载地址。
    url TEXT NOT NULL DEFAULT '',
    -- 本机文件路径。
    file_path TEXT NOT NULL DEFAULT '',
    -- 文件名。
    file_name TEXT NOT NULL DEFAULT '',
    -- 文件 MIME 类型。
    mime_type TEXT NOT NULL DEFAULT '',
    -- 文件大小，单位字节。
    size INTEGER NOT NULL DEFAULT 0,
    -- 下载状态。
    status TEXT NOT NULL DEFAULT '',
    -- 创建时间。
    created_at TEXT NOT NULL,
    -- 更新时间。
    updated_at TEXT NOT NULL
);

INSERT OR REPLACE INTO local_meta(key, value) VALUES('schema_version', '1');
`
	if _, err := db.conn.Exec(script); err != nil {
		return fmt.Errorf("初始化本地数据库失败：%w", err)
	}
	// 新版本不再保存本地截图记录，只保留岗位运行级最新截图文件。
	_, _ = db.conn.Exec(`DROP TABLE IF EXISTS local_screenshots`)
	// 后向兼容迁移：低版本数据库在首次 migrate 后仍缺少 enable_thinking 字段。
	_, _ = db.conn.Exec(`ALTER TABLE local_positions ADD COLUMN enable_thinking INTEGER NOT NULL DEFAULT 0`)
	// 后向兼容迁移：重建 local_candidates 表以支持结构化字段。
	db.migrateLocalCandidates()
	// 后向兼容迁移：把旧版误用“已保存人数”的扫描统计校正为已知处理总数。
	if err := db.migratePositionScannedCounts(); err != nil {
		return err
	}
	return nil
}

// migratePositionScannedCounts 一次性补回旧版累计扫描中漏记的跳过和失败人数。
// 迁移标记写入 local_meta，确保同一个本地数据库不会重复累加。
func (db *DB) migratePositionScannedCounts() error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("开始修复累计扫描统计失败：%w", err)
	}
	defer tx.Rollback()

	var migrated int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM local_meta WHERE key='position_scanned_count_semantics_v2'`).Scan(&migrated); err != nil {
		return fmt.Errorf("读取累计扫描统计迁移标记失败：%w", err)
	}
	if migrated > 0 {
		return nil
	}
	if _, err := tx.Exec(`
UPDATE local_positions
SET scanned_count=scanned_count+skipped_count+failed_count,
    updated_at=?
WHERE skipped_count>0 OR failed_count>0
`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("修复累计扫描统计失败：%w", err)
	}
	if _, err := tx.Exec(`INSERT INTO local_meta(key, value) VALUES('position_scanned_count_semantics_v2', 'completed')`); err != nil {
		return fmt.Errorf("保存累计扫描统计迁移标记失败：%w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交累计扫描统计迁移失败：%w", err)
	}
	return nil
}

// migrateLocalCandidates 将旧版 local_candidates 表升级为结构化字段版本。
// SQLite 不支持直接修改列，采用"重命名旧表 → 创建新表 → 迁移数据"策略。
func (db *DB) migrateLocalCandidates() error {
	// 检测旧表是否有 payload 字段（旧版标志）
	row := db.conn.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('local_candidates') WHERE name='payload'`)
	var hasPayload int
	if err := row.Scan(&hasPayload); err != nil || hasPayload == 0 {
		return nil // 已经是新结构，无需迁移
	}
	// 开始迁移
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("开始迁移事务失败：%w", err)
	}
	defer tx.Rollback()
	// 重命名旧表
	if _, err := tx.Exec(`ALTER TABLE local_candidates RENAME TO local_candidates_old`); err != nil {
		return fmt.Errorf("重命名旧表失败：%w", err)
	}
	// 创建新表
	_, err = tx.Exec(`
CREATE TABLE local_candidates (
    id TEXT NOT NULL,
    position_id TEXT NOT NULL,
    candidate_name TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    birth_ym TEXT NOT NULL DEFAULT '',
    phone TEXT NOT NULL DEFAULT '',
    email TEXT NOT NULL DEFAULT '',
    work_region TEXT NOT NULL DEFAULT '',
    work_years TEXT NOT NULL DEFAULT '',
    expected_salary_min INTEGER,
    expected_salary_max INTEGER,
    personal_description TEXT NOT NULL DEFAULT '',
    work_status TEXT NOT NULL DEFAULT '',
    expected_position TEXT NOT NULL DEFAULT '',
    online_status TEXT NOT NULL DEFAULT '',
    education_level TEXT NOT NULL DEFAULT '',
    basic_info TEXT NOT NULL DEFAULT '',
    raw_text TEXT NOT NULL DEFAULT '',
    filter_text TEXT NOT NULL DEFAULT '',
    work_experiences TEXT NOT NULL DEFAULT '[]',
    educations TEXT NOT NULL DEFAULT '[]',
    certificates TEXT NOT NULL DEFAULT '[]',
    honors TEXT NOT NULL DEFAULT '[]',
    project_experiences TEXT NOT NULL DEFAULT '[]',
    colleague_communications TEXT NOT NULL DEFAULT '[]',
    resume_attachment_url TEXT NOT NULL DEFAULT '',
    resume_attachment_extracted_text TEXT NOT NULL DEFAULT '',
    ai_detail_reason TEXT NOT NULL DEFAULT '',
    ai_detail_score REAL,
    ai_greet_reason TEXT NOT NULL DEFAULT '',
    ai_greet_score REAL,
    ai_review_reason TEXT NOT NULL DEFAULT '',
    ai_review_score REAL,
    ext TEXT NOT NULL DEFAULT '{}',
    first_seen_at TEXT NOT NULL DEFAULT '',
    detail_fetched_at TEXT NOT NULL DEFAULT '',
    greeted_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY(position_id, id),
    FOREIGN KEY(position_id) REFERENCES local_positions(id) ON DELETE CASCADE
)`)
	if err != nil {
		return fmt.Errorf("创建新表失败：%w", err)
	}
	// 从旧表迁移数据（payload 中的字段映射到新表）
	if _, err := tx.Exec(`
INSERT INTO local_candidates(id, position_id, candidate_name, status, created_at, updated_at)
SELECT id, position_id, candidate_name, status, created_at, updated_at FROM local_candidates_old
`); err != nil {
		return fmt.Errorf("迁移旧数据失败：%w", err)
	}
	// 删除旧表
	if _, err := tx.Exec(`DROP TABLE local_candidates_old`); err != nil {
		return fmt.Errorf("删除旧表失败：%w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交迁移事务失败：%w", err)
	}
	return nil
}
