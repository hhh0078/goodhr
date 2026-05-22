-- 本回滚移除统一系统配置中的 AI 默认提示词。
DELETE FROM system_configs WHERE config_key = 'ai.default_prompts';
