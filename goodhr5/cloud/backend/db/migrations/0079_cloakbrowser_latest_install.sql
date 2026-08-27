-- 本迁移保存用户自己的 CloakBrowser Key，并移除旧版 Chromium OSS 下载配置。
ALTER TABLE user_preferences
    ADD COLUMN IF NOT EXISTS cloakbrowser_license_key TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN user_preferences.cloakbrowser_license_key IS '用户自己的 CloakBrowser 许可证 Key，按个人配置明文保存';

UPDATE system_configs
SET
    config_value = jsonb_set(
        config_value,
        '{runtime_components}',
        COALESCE(config_value -> 'runtime_components', '{}'::jsonb) - 'cloakbrowser',
        true
    ),
    description = '新手教学配置，包含本地程序、Node 和 OCR 下载链接、版本说明及注册赠送会员天数'
WHERE config_key = 'system.onboarding_config';
