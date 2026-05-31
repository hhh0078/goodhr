-- 本迁移为新手教学配置增加 Mac 和 Windows 本地程序下载链接，前端可按用户系统自动展示。
UPDATE system_configs
SET
    config_value = jsonb_set(
        jsonb_set(
            config_value,
            '{local_agent_download_url_mac}',
            to_jsonb(COALESCE(config_value ->> 'local_agent_download_url_mac', config_value ->> 'local_agent_download_url', '')),
            true
        ),
        '{local_agent_download_url_windows}',
        to_jsonb(COALESCE(config_value ->> 'local_agent_download_url_windows', '')),
        true
    ),
    description = '新手教学配置，包含本地程序下载链接、Mac/Windows 下载链接和注册赠送会员天数'
WHERE config_key = 'system.onboarding_config';
