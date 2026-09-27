-- 文件作用说明：持久保存浏览器异常退出事件，网络中断时等待按账号补传。
CREATE TABLE IF NOT EXISTS browser_events (
    event_id TEXT PRIMARY KEY, -- 事件唯一编号，重试上传时用于云端去重
    owner_id TEXT NOT NULL DEFAULT '', -- 绑定账号的云端用户编号，空值等待首次绑定
    position_id TEXT NOT NULL DEFAULT '', -- 异常发生时正在运行的岗位编号
    payload TEXT NOT NULL, -- 已脱敏的一行 JSON 日志
    created_at TEXT NOT NULL -- 浏览器事件发生时间
);
CREATE INDEX IF NOT EXISTS idx_browser_events_owner_time ON browser_events(owner_id, created_at);
