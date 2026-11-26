-- 文件作用：将个人 AI 接口和系统默认地址从已知中转接口恢复为 GoodHR 主站，保留密钥、模型、其他配置和原更新时间。
-- 只匹配中转域名的完整兼容接口；第三方地址及其他路由不受影响，重复执行不会再次改写。
UPDATE system_configs
SET config_value = jsonb_set(
        config_value,
        '{builtin_ai}',
        COALESCE(NULLIF(config_value->'builtin_ai', 'null'::jsonb), '{}'::jsonb)
            || jsonb_build_object(
                'public_base_url',
                'https://goodhr.58it.cn/api/ai-compatible/v1/chat/completions'
            ),
        true
    )
WHERE config_key = 'system.app_config'
  AND jsonb_typeof(config_value) = 'object'
  AND (
      config_value->'builtin_ai' IS NULL
      OR jsonb_typeof(config_value->'builtin_ai') IN ('object', 'null')
  )
  AND (
      COALESCE(BTRIM(config_value #>> '{builtin_ai,public_base_url}'), '') = ''
      OR LOWER(BTRIM(config_value #>> '{builtin_ai,public_base_url}'))
          = 'https://goodhr.transit.ai.58it.cn/api/ai-compatible/v1/chat/completions'
  );

UPDATE user_ai_configs
SET base_url = 'https://goodhr.58it.cn/api/ai-compatible/v1/chat/completions'
WHERE LOWER(BTRIM(base_url))
    = 'https://goodhr.transit.ai.58it.cn/api/ai-compatible/v1/chat/completions';
