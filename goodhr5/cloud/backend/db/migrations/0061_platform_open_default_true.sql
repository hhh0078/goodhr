-- 为历史招聘平台配置补充开放开关，后续前端只认明确的 open: true。
UPDATE system_configs
SET config_value = jsonb_set(config_value, '{open}', 'true'::jsonb, true)
WHERE config_key LIKE 'platform.%'
  AND NOT (config_value ? 'open');
