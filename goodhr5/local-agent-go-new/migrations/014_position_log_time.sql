-- 文件作用：按岗位、实际发生时间与入库编号检索和裁剪日志，让断线补领的旧记录归回原时间。
CREATE INDEX IF NOT EXISTS idx_task_logs_position_occurred
    ON task_logs(position_id, julianday(created_at) DESC, id DESC);
