-- 文件作用：新增岗位独立开启状态，保留原任务运行状态和全部岗位数据，旧岗位默认开启。
ALTER TABLE positions ADD COLUMN IF NOT EXISTS is_open BOOLEAN NOT NULL DEFAULT true;
COMMENT ON COLUMN positions.is_open IS '岗位是否开启；关闭后不再出现在默认岗位列表或推荐选择中，不删除岗位及推荐历史';
