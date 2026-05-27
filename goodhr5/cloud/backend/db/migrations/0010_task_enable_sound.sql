-- 任务提示音开关（默认关闭）
ALTER TABLE task_runs
  ADD COLUMN IF NOT EXISTS enable_sound BOOLEAN NOT NULL DEFAULT false;

COMMENT ON COLUMN task_runs.enable_sound IS '打招呼成功后是否播放提示音';
