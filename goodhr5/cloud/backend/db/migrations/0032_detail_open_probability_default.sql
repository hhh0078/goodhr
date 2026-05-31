-- 本迁移将关键词模式详情查看概率默认值从 30% 调整为 80%，并同步更新仍使用旧默认值的用户配置。
ALTER TABLE user_preferences
    ALTER COLUMN detail_open_probability SET DEFAULT 80;

UPDATE user_preferences
SET detail_open_probability = 80,
    updated_at = now()
WHERE detail_open_probability = 30;
