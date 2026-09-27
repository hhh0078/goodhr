-- 文件作用说明：为本地岗位日志补充结构化诊断字段和上传状态，支持统一 JSON 日志与异常退出后补传。

ALTER TABLE task_logs ADD COLUMN category TEXT NOT NULL DEFAULT ''; -- 日志类别或方法所属模块
ALTER TABLE task_logs ADD COLUMN is_error INTEGER NOT NULL DEFAULT 0; -- 是否为错误日志，0 否 1 是
ALTER TABLE task_logs ADD COLUMN error_message TEXT NOT NULL DEFAULT ''; -- 已脱敏的错误信息
ALTER TABLE task_logs ADD COLUMN input_json TEXT NOT NULL DEFAULT '{}'; -- 已脱敏的入参摘要 JSON
ALTER TABLE task_logs ADD COLUMN output_json TEXT NOT NULL DEFAULT '{}'; -- 已脱敏的出参摘要 JSON

CREATE TABLE IF NOT EXISTS pending_log_uploads (
    position_id TEXT PRIMARY KEY, -- 待补传的云端岗位编号
    updated_at TEXT NOT NULL -- 最近一次标记时间
);
