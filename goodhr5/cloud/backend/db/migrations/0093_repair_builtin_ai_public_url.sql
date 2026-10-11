-- 本迁移将历史内置 AI 地址改为正式域名，并严格保留用户自定义的第三方接口地址。
UPDATE system_configs
SET config_value = jsonb_set(
        COALESCE(config_value, '{}'::jsonb),
        '{builtin_ai}',
        COALESCE(config_value->'builtin_ai', '{}'::jsonb)
            || jsonb_build_object(
                'public_base_url',
                'https://goodhr.58it.cn/api/ai-compatible/v1/chat/completions'
            ),
        true
    )
WHERE config_key = 'system.app_config'
  AND (
      COALESCE(BTRIM(config_value #>> '{builtin_ai,public_base_url}'), '') = ''
      OR LOWER(BTRIM(config_value #>> '{builtin_ai,public_base_url}'))
          = 'https://goodhr5.58it.cn/api/ai-compatible/v1/chat/completions'
  );

UPDATE user_ai_configs
SET base_url = 'https://goodhr.58it.cn/api/ai-compatible/v1/chat/completions'
WHERE LOWER(BTRIM(base_url))
        = 'https://goodhr5.58it.cn/api/ai-compatible/v1/chat/completions'
  AND LEFT(api_key_encrypted, 5) = 'ghai_';
