-- 本回滚迁移只移除新增的官网配置，流程模板继续使用当前默认值。
UPDATE system_configs
SET config_value = config_value - 'website',
    updated_at = now()
WHERE config_key = 'system.email_recovery';
