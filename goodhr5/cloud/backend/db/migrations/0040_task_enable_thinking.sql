-- 本迁移为云端任务增加 AI 思考模式开关，供本地任务运行器读取后控制流式思考输出。
ALTER TABLE task_runs
  ADD COLUMN IF NOT EXISTS enable_thinking BOOLEAN NOT NULL DEFAULT false;

COMMENT ON COLUMN task_runs.enable_thinking IS 'AI 调用时是否开启思考模式';
