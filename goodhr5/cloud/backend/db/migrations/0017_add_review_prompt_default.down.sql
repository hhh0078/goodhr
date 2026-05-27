-- 回滚复核提示词默认值补齐迁移：删除 review_prompt 字段。
UPDATE system_configs
SET config_value = config_value - 'review_prompt'
WHERE config_key = 'ai.default_prompts';
