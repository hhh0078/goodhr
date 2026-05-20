-- 本迁移为平台系统配置补充登录检测 URL，用于前端创建平台账号时复用登录判断逻辑。
UPDATE system_configs
SET config_value = jsonb_set(
    jsonb_set(
        config_value,
        '{auth}',
        '{
          "entry_url": "https://www.zhipin.com/web/chat/recommend",
          "logged_in_url_prefix": "https://www.zhipin.com/web/chat/recommend",
          "login_url_prefixes": ["https://login.zhipin.com", "https://www.zhipin.com/web/user/"]
        }'::jsonb,
        true
    ),
    '{pages}',
    COALESCE(config_value->'pages', '[]'::jsonb),
    true
)
WHERE config_key = 'platform.boss';

UPDATE system_configs
SET config_value = jsonb_set(
    config_value,
    '{auth}',
    '{
      "entry_url": "https://rd6.zhaopin.com/app/recommend",
      "logged_in_url_prefix": "https://rd6.zhaopin.com/app/recommend",
      "login_url_prefixes": ["https://passport.zhaopin.com", "https://login.zhaopin.com"]
    }'::jsonb,
    true
)
WHERE config_key = 'platform.zhaopin';
