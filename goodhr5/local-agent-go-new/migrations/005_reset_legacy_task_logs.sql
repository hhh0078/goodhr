-- 文件作用说明：首次升级到候选人可读日志版本时，清除旧岗位日志及旧补传队列，避免技术流水再次挤满岗位记录。
DELETE FROM task_logs;
DELETE FROM pending_log_uploads;
