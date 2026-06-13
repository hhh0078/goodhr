-- 本迁移为任务运行记录增加每日打招呼计数，避免前端继续从日志里推算今日数据。
ALTER TABLE task_runs
    ADD COLUMN IF NOT EXISTS daily_greeted_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS daily_greeted_date DATE NOT NULL DEFAULT CURRENT_DATE;

COMMENT ON COLUMN task_runs.daily_greeted_count IS '任务当天已打招呼数量，跨天后由后端自动重置';
COMMENT ON COLUMN task_runs.daily_greeted_date IS 'daily_greeted_count 对应的日期';
