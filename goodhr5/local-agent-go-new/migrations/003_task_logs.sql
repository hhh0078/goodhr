-- 文件作用说明：新增任务步骤日志表，供现有控制台按岗位查看、追加和清空本地运行日志。

CREATE TABLE IF NOT EXISTS task_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT, -- 本地主键
    task_id TEXT NOT NULL DEFAULT '',     -- 所属本地任务编号
    position_id TEXT NOT NULL DEFAULT '', -- 所属云端岗位编号
    flow TEXT NOT NULL DEFAULT '',        -- 主流程名称
    step TEXT NOT NULL DEFAULT '',        -- 当前步骤名称
    status TEXT NOT NULL DEFAULT '',      -- 步骤状态
    level TEXT NOT NULL DEFAULT 'info',   -- 日志级别
    message TEXT NOT NULL DEFAULT '',     -- 简短日志内容
    duration_ms INTEGER NOT NULL DEFAULT 0,-- 步骤耗时毫秒数
    created_at TEXT NOT NULL              -- 创建时间
);

CREATE INDEX IF NOT EXISTS idx_task_logs_position ON task_logs(position_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_task_logs_task ON task_logs(task_id, id DESC);
