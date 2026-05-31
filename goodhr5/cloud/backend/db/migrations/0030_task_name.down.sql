-- 本回滚迁移移除任务名称字段，仅用于本地回滚数据库结构。
ALTER TABLE task_runs
  DROP COLUMN IF EXISTS name;
