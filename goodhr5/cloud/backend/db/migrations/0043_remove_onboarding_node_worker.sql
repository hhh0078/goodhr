-- 本迁移从新手教学配置中移除 node_worker 下载配置；Worker 已随本地程序安装包内置，不再作为运行组件下载。
UPDATE system_configs
SET
    config_value = jsonb_set(
        config_value,
        '{runtime_components}',
        COALESCE(config_value -> 'runtime_components', '{}'::jsonb) - 'node_worker',
        true
    ),
    description = '新手教学配置，包含本地程序下载链接、运行组件下载链接、版本号、版本说明和注册赠送会员天数'
WHERE config_key = 'system.onboarding_config';
