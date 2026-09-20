-- 文件作用说明：为个人配置增加候选人卡片并发读取数量，并保留原有默认行为。
ALTER TABLE user_preferences
    ADD COLUMN IF NOT EXISTS candidate_read_concurrency INTEGER NOT NULL DEFAULT 100;

COMMENT ON COLUMN user_preferences.candidate_read_concurrency IS '同时读取候选人卡片数量，范围5到100，默认100';
