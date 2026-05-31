-- 本迁移为任务运行记录增加用户可编辑的任务名称，便于在任务列表中区分不同任务。
ALTER TABLE task_runs
  ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN task_runs.name IS '任务名称，默认由岗位模板名称和筛选模式生成，也允许用户手动修改';

-- 为历史任务补齐默认名称，避免升级后列表出现空名称。
UPDATE task_runs tr
SET name = COALESCE(NULLIF(p.name, ''), '未命名岗位') || ' ' ||
  CASE WHEN tr.mode = 'keyword' THEN '关键词筛选' ELSE 'AI筛选' END
FROM positions p
WHERE tr.position_id = p.id
  AND tr.name = '';

UPDATE task_runs
SET name = '未命名任务'
WHERE name = '';
