-- 本迁移新增用户教学完成状态，并写入教学相关系统配置。
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS onboarding JSONB NOT NULL DEFAULT jsonb_build_object(
        'completed', false,
        'completed_at', null
    );

COMMENT ON COLUMN users.onboarding IS '用户新手教学状态JSON，包含completed是否完成和completed_at完成时间';

INSERT INTO system_configs (config_key, config_value, description, enabled)
VALUES (
  'system.onboarding_config',
  '{
    "local_agent_download_url": "",
    "trial_days": 3
  }'::jsonb,
  '新手教学配置，包含本地程序下载链接和注册赠送会员天数',
  true
)
ON CONFLICT (config_key) DO UPDATE
SET config_value = EXCLUDED.config_value,
    description = EXCLUDED.description,
    enabled = EXCLUDED.enabled;
