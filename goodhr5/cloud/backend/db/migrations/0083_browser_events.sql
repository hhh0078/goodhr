-- 文件作用说明：保存账号维度的浏览器异常退出日志，供管理员排查无岗位任务时的闪退。
CREATE TABLE IF NOT EXISTS browser_events (
    event_id TEXT PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    position_id UUID REFERENCES positions(id) ON DELETE SET NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);
COMMENT ON TABLE browser_events IS '本地浏览器与 Worker 异常退出日志';
COMMENT ON COLUMN browser_events.event_id IS '本地生成的事件唯一编号，用于上传重试去重';
COMMENT ON COLUMN browser_events.user_id IS '发生异常时绑定的账号编号';
COMMENT ON COLUMN browser_events.position_id IS '发生异常时正在运行的岗位编号，无任务时为空';
COMMENT ON COLUMN browser_events.payload IS '已脱敏的完整 JSON 行日志';
COMMENT ON COLUMN browser_events.created_at IS '异常发生时间';
CREATE INDEX IF NOT EXISTS idx_browser_events_user_time ON browser_events(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_browser_events_time ON browser_events(created_at DESC);
