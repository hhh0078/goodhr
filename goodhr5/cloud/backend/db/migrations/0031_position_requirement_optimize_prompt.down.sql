-- 本回滚迁移移除系统其它配置里的岗位要求 AI 优化提示词。
UPDATE system_configs
SET config_value = (config_value::jsonb - 'position_requirement_optimize_prompt')
WHERE config_key = 'system.app_config';
