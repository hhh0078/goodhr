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

-- local_positions 保存用户本机创建的岗位模板。
CREATE TABLE IF NOT EXISTS local_positions (
    -- 岗位模板唯一 ID。
    id TEXT PRIMARY KEY,
    -- 招聘平台 ID，例如 boss。
    platform_id TEXT NOT NULL DEFAULT '',
    -- 岗位模板名称。
    name TEXT NOT NULL DEFAULT '',
    -- 包含关键词 JSON 列表。
    keywords_json TEXT NOT NULL DEFAULT '[]',
    -- 排除关键词 JSON 列表。
    exclude_keywords_json TEXT NOT NULL DEFAULT '[]',
    -- 岗位说明。
    description TEXT NOT NULL DEFAULT '',
    -- 招呼语模板。
    greet_message TEXT NOT NULL DEFAULT '',
    -- 是否使用关键词 AND 匹配。
    is_and_mode INTEGER NOT NULL DEFAULT 0,
    -- 通用筛选配置 JSON。
    common_config_json TEXT NOT NULL DEFAULT '{}',
    -- 岗位级 AI 配置 JSON。
    ai_config_json TEXT NOT NULL DEFAULT '{}',
    -- 关键词配置 JSON。
    keyword_config_json TEXT NOT NULL DEFAULT '{}',
    -- 创建时间。
    created_at TEXT NOT NULL,
    -- 更新时间。
    updated_at TEXT NOT NULL
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

-- local_profiles 保存招聘平台账号对应的本机浏览器目录。
CREATE TABLE IF NOT EXISTS local_profiles (
    -- Profile 唯一 ID。
    id TEXT PRIMARY KEY,
    -- 招聘平台 ID，例如 boss。
    platform_id TEXT NOT NULL DEFAULT '',
    -- 页面展示名称。
    display_name TEXT NOT NULL DEFAULT '',
    -- 本机浏览器目录名称。
    local_profile_id TEXT NOT NULL DEFAULT '',
    -- Profile 状态。
    status TEXT NOT NULL DEFAULT 'active',
    -- 创建时间。
    created_at TEXT NOT NULL,
    -- 更新时间。
    updated_at TEXT NOT NULL
);

-- local_downloads 保存本机下载记录。
CREATE TABLE IF NOT EXISTS local_downloads (
    -- 下载记录唯一 ID。
    id TEXT PRIMARY KEY,
    -- 关联任务 ID。
    task_id TEXT NOT NULL DEFAULT '',
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

-- local_screenshots 保存本机截图记录。
CREATE TABLE IF NOT EXISTS local_screenshots (
    -- 截图记录唯一 ID。
    id TEXT PRIMARY KEY,
    -- 关联任务 ID。
    task_id TEXT NOT NULL DEFAULT '',
    -- 本机文件路径。
    file_path TEXT NOT NULL DEFAULT '',
    -- 截图标签。
    label TEXT NOT NULL DEFAULT '',
    -- 图片宽度。
    width INTEGER NOT NULL DEFAULT 0,
    -- 图片高度。
    height INTEGER NOT NULL DEFAULT 0,
    -- 创建时间。
    created_at TEXT NOT NULL
);

-- local_ai_config 明文保存用户本机 AI 接口配置。
CREATE TABLE IF NOT EXISTS local_ai_config (
    -- 配置唯一 ID，目前固定为 default。
    id TEXT PRIMARY KEY,
    -- AI 服务提供商。
    provider TEXT NOT NULL DEFAULT '',
    -- AI 接口地址。
    base_url TEXT NOT NULL DEFAULT '',
    -- AI 接口密钥。
    api_key TEXT NOT NULL DEFAULT '',
    -- AI 模型名称。
    model TEXT NOT NULL DEFAULT '',
    -- 生成温度。
    temperature REAL NOT NULL DEFAULT 0.2,
    -- 请求超时时间，单位秒。
    timeout INTEGER NOT NULL DEFAULT 120,
    -- 额外请求参数 JSON。
    extra_json TEXT NOT NULL DEFAULT '{}',
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
	// 后向兼容迁移：低版本数据库在首次 migrate 后仍缺少 enable_thinking 字段。
	_, _ = db.conn.Exec(`ALTER TABLE local_tasks ADD COLUMN enable_thinking INTEGER NOT NULL DEFAULT 0`)
	return nil
}
