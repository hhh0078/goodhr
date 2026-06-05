"""本文件负责 Local Agent SQLite 数据库初始化和访问。"""

from __future__ import annotations

import sqlite3
from pathlib import Path

from app.paths import data_dir


SCHEMA_VERSION = 3


def database_path() -> Path:
    """
    返回本地 SQLite 数据库路径。

    Returns:
        Path: goodhr_local.db 路径。
    """
    path = data_dir() / "goodhr_local.db"
    path.parent.mkdir(parents=True, exist_ok=True)
    return path


def connect() -> sqlite3.Connection:
    """
    创建 SQLite 连接并确保数据库已初始化。

    Returns:
        sqlite3.Connection: 数据库连接。
    """
    conn = sqlite3.connect(database_path())
    conn.row_factory = sqlite3.Row
    conn.execute("PRAGMA journal_mode=WAL")
    conn.execute("PRAGMA foreign_keys=ON")
    migrate(conn)
    return conn


def migrate(conn: sqlite3.Connection) -> None:
    """
    执行本地数据库迁移。

    Args:
        conn: SQLite 连接。
    """
    conn.executescript(
        """
        CREATE TABLE IF NOT EXISTS local_meta (
            key TEXT PRIMARY KEY,
            value TEXT NOT NULL DEFAULT ''
        );

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

        CREATE TABLE IF NOT EXISTS local_task_logs (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            task_id TEXT NOT NULL,
            level TEXT NOT NULL DEFAULT 'info',
            message TEXT NOT NULL DEFAULT '',
            created_at TEXT NOT NULL,
            FOREIGN KEY(task_id) REFERENCES local_tasks(id) ON DELETE CASCADE
        );

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

        CREATE TABLE IF NOT EXISTS local_rule_versions (
            platform_id TEXT PRIMARY KEY,
            version TEXT NOT NULL DEFAULT '',
            status TEXT NOT NULL DEFAULT 'active',
            updated_at TEXT NOT NULL
        );

        CREATE TABLE IF NOT EXISTS local_settings (
            key TEXT PRIMARY KEY,
            value TEXT NOT NULL DEFAULT '',
            updated_at TEXT NOT NULL
        );

        CREATE TABLE IF NOT EXISTS local_downloads (
            id TEXT PRIMARY KEY,
            task_id TEXT NOT NULL DEFAULT '',
            url TEXT NOT NULL DEFAULT '',
            file_path TEXT NOT NULL DEFAULT '',
            file_name TEXT NOT NULL DEFAULT '',
            mime_type TEXT NOT NULL DEFAULT '',
            size INTEGER NOT NULL DEFAULT 0,
            status TEXT NOT NULL DEFAULT '',
            created_at TEXT NOT NULL,
            updated_at TEXT NOT NULL
        );

        CREATE TABLE IF NOT EXISTS local_screenshots (
            id TEXT PRIMARY KEY,
            task_id TEXT NOT NULL DEFAULT '',
            file_path TEXT NOT NULL DEFAULT '',
            label TEXT NOT NULL DEFAULT '',
            width INTEGER NOT NULL DEFAULT 0,
            height INTEGER NOT NULL DEFAULT 0,
            created_at TEXT NOT NULL
        );

        -- 本地 AI 配置表，明文保存用户在本机填写的模型和密钥。
        CREATE TABLE IF NOT EXISTS local_ai_config (
            id TEXT PRIMARY KEY,
            provider TEXT NOT NULL DEFAULT '',
            base_url TEXT NOT NULL DEFAULT '',
            api_key TEXT NOT NULL DEFAULT '',
            model TEXT NOT NULL DEFAULT '',
            temperature REAL NOT NULL DEFAULT 0.2,
            timeout INTEGER NOT NULL DEFAULT 120,
            extra_json TEXT NOT NULL DEFAULT '{}',
            created_at TEXT NOT NULL,
            updated_at TEXT NOT NULL
        );
        """
    )
    _ensure_column(conn, "local_tasks", "enable_sound", "INTEGER NOT NULL DEFAULT 0")
    conn.execute(
        "INSERT OR REPLACE INTO local_meta(key, value) VALUES('schema_version', ?)",
        (str(SCHEMA_VERSION),),
    )
    conn.commit()


def _ensure_column(conn: sqlite3.Connection, table: str, column: str, definition: str) -> None:
    """
    确保 SQLite 表存在指定字段。

    Args:
        conn: SQLite 连接。
        table: 表名。
        column: 字段名。
        definition: ALTER TABLE 使用的字段定义。
    """
    rows = conn.execute(f"PRAGMA table_info({table})").fetchall()
    exists = any(str(row["name"]) == column for row in rows)
    if not exists:
        conn.execute(f"ALTER TABLE {table} ADD COLUMN {column} {definition}")
