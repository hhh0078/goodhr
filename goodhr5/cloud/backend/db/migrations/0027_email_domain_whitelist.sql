-- 本迁移为 system.app_config 其它配置补充邮箱域名白名单字段；仅在字段缺失时写入默认值，避免覆盖管理员后续修改。
INSERT INTO system_configs (config_key, config_value, description, enabled)
VALUES (
  'system.app_config',
  '{
    "local_agent_version": "5.0.0",
    "email_domain_whitelist": ["qq.com", "foxmail.com", "163.com", "126.com", "yeah.net", "sina.com", "sina.cn", "sohu.com", "aliyun.com", "139.com", "189.cn", "wo.cn", "gmail.com", "outlook.com", "hotmail.com", "live.com", "icloud.com", "yahoo.com", "proton.me", "protonmail.com"],
    "announcements_enabled": true,
    "announcements": [
      {
        "id": "2026-05-26-v1",
        "title": "GoodHR 5 更新公告",
        "content": "GoodHR 5 本地执行器版本从 5.0.0 起步，低版本请及时更新。",
        "once": true,
        "enabled": true,
        "created_at": "2026-05-26"
      }
    ]
  }'::jsonb,
  '前端公共系统配置：本地执行器版本要求、邮箱域名白名单和系统公告列表',
  true
)
ON CONFLICT (config_key) DO UPDATE
SET config_value = CASE
    WHEN system_configs.config_value::jsonb ? 'email_domain_whitelist' THEN system_configs.config_value::jsonb
    ELSE system_configs.config_value::jsonb || jsonb_build_object(
      'email_domain_whitelist',
      jsonb_build_array('qq.com', 'foxmail.com', '163.com', '126.com', 'yeah.net', 'sina.com', 'sina.cn', 'sohu.com', 'aliyun.com', '139.com', '189.cn', 'wo.cn', 'gmail.com', 'outlook.com', 'hotmail.com', 'live.com', 'icloud.com', 'yahoo.com', 'proton.me', 'protonmail.com')
    )
  END,
    description = system_configs.description,
    enabled = system_configs.enabled;
