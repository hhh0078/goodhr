-- 本迁移新增 GoodHR 5 前端公共系统配置，用于本地执行器版本校验和系统公告展示。
INSERT INTO system_configs (config_key, config_value, description, enabled)
VALUES (
  'system.app_config',
  '{
    "local_agent_version": "5.0.0",
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
  '前端公共系统配置：本地执行器版本要求和系统公告列表',
  true
)
ON CONFLICT (config_key) DO UPDATE
SET config_value = EXCLUDED.config_value,
    description = EXCLUDED.description,
    enabled = EXCLUDED.enabled;
