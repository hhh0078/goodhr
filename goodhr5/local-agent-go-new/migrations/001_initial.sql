-- 文件作用说明：创建新本地程序的任务、候选人、会话和迁移版本表，不保存 Cookie、Token、截图或完整候选人详情。

CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT PRIMARY KEY, -- 迁移版本号
    applied_at TEXT NOT NULL  -- 迁移执行时间
);

CREATE TABLE IF NOT EXISTS task_runs (
    task_id TEXT PRIMARY KEY,             -- 本地任务编号
    position_id TEXT NOT NULL,            -- 云端岗位编号
    platform_id TEXT NOT NULL,            -- 招聘平台编号
    task_type TEXT NOT NULL,              -- 任务类型
    status TEXT NOT NULL,                 -- 当前运行状态
    current_step TEXT NOT NULL DEFAULT '',-- 当前执行步骤
    summary TEXT NOT NULL DEFAULT '',     -- 不含敏感数据的运行摘要
    error_code TEXT NOT NULL DEFAULT '',  -- 稳定错误码
    error_message TEXT NOT NULL DEFAULT '',-- 用户可见错误
    started_at TEXT NOT NULL,             -- 任务开始时间
    updated_at TEXT NOT NULL,             -- 状态更新时间
    finished_at TEXT NOT NULL DEFAULT ''  -- 任务结束时间
);

CREATE INDEX IF NOT EXISTS idx_task_runs_position ON task_runs(position_id, started_at);

CREATE TABLE IF NOT EXISTS candidate_records (
    id INTEGER PRIMARY KEY AUTOINCREMENT, -- 本地主键
    task_id TEXT NOT NULL,                -- 所属本地任务编号
    fingerprint TEXT NOT NULL,            -- 平台候选人去重指纹
    platform_id TEXT NOT NULL,            -- 招聘平台编号
    display_name TEXT NOT NULL DEFAULT '',-- 候选人页面展示名
    action TEXT NOT NULL,                 -- 本次执行动作
    result TEXT NOT NULL,                 -- 动作结果
    reason TEXT NOT NULL DEFAULT '',       -- 不含详情的判断原因
    created_at TEXT NOT NULL,             -- 记录创建时间
    UNIQUE(task_id, fingerprint, action)
);

CREATE TABLE IF NOT EXISTS conversation_records (
    id INTEGER PRIMARY KEY AUTOINCREMENT, -- 本地主键
    task_id TEXT NOT NULL,                -- 所属本地任务编号
    conversation_key TEXT NOT NULL,       -- 会话去重标识
    platform_id TEXT NOT NULL,            -- 招聘平台编号
    reply_hash TEXT NOT NULL,             -- 回复内容哈希
    result TEXT NOT NULL,                 -- 回复结果
    created_at TEXT NOT NULL,             -- 记录创建时间
    UNIQUE(task_id, conversation_key, reply_hash)
);
