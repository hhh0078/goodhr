-- 本迁移为系统应用配置增加免费版每日打招呼上限，缺省为 100，不覆盖已有设置。
UPDATE system_configs
SET config_value = config_value::jsonb || jsonb_build_object('free_daily_greet_limit', 100)
WHERE config_key = 'system.app_config'
  AND NOT (config_value::jsonb ? 'free_daily_greet_limit');
