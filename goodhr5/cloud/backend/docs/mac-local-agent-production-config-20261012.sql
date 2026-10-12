-- 文件作用说明：生产发布 macOS 双架构本地程序及运行组件，仅定点修改 system.onboarding_config。
-- 执行前须由部署人员在私有目录用 pg_dump 备份 system_configs 并校验备份文件权限为 0600。
-- 本文件供人工审阅和执行，不放入自动迁移目录；失败会回滚整笔事务。

BEGIN;

DO $goodhr_mac_release$
DECLARE
    old_config jsonb;
    new_config jsonb;
    old_releases jsonb;
    mac_release jsonb := jsonb_build_object(
        'version', '6.2.10.4',
        'url_win', '',
        'url_mac', 'https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.2.10.4-mac-universal-d24d8a5e.pkg',
        'sha256', 'd24d8a5ecfd4fdeef5591903886cd8350f1c5f1fe3541c12d4b1f8f48701bdc2',
        'note', 'macOS Apple 芯片与 Intel 芯片通用安装包'
    );
    mac_node jsonb := jsonb_build_object(
        'version', '22.19.0',
        'url', 'https://oss2.58it.cn/goodhr-node-runtime-v22.19.0-darwin-arm64.tar.gz',
        'sha256', '4f2fdfb47352e3ab3908268dd2552081ce1e72f86b4e0ef45b379c18816af552',
        'note', 'GoodHR Node 运行环境 macOS Apple Silicon'
    );
    intel_node jsonb := jsonb_build_object(
        'version', '22.19.0',
        'url', 'https://oss2.58it.cn/goodhr-node-runtime-v22.19.0-darwin-x64.tar.gz',
        'sha256', 'bae90145c832278b2f93dbcf0ed3865755c6ef726efb9c7a5dc77ed519dec3ca',
        'note', 'GoodHR Node 运行环境 macOS Intel'
    );
    mac_camoufox jsonb := jsonb_build_object(
        'version', '152.0.4-beta.30',
        'url', 'https://oss2.58it.cn/camoufox-152.0.4-beta.30-mac.arm64.zip',
        'sha256', '3b43e766574f286a6a63296cf58b660b7a3120952086c869b4df4c9a71604bc3',
        'note', 'Camoufox macOS Apple Silicon'
    );
    intel_camoufox jsonb := jsonb_build_object(
        'version', '152.0.4-beta.30',
        'url', 'https://oss2.58it.cn/camoufox-152.0.4-beta.30-mac.x86_64.zip',
        'sha256', 'f12f90a3650478e670064a2f071c13b998fdd1125f79dc9f6cad5a9b9395b4bc',
        'note', 'Camoufox macOS Intel'
    );
    mac_ocr jsonb := jsonb_build_object(
        'version', 'goodhr-ocr-vision-v1.0.0',
        'url', 'https://oss2.58it.cn/goodhr-ocr-vision-v1.0.0-darwin-universal.zip',
        'sha256', '6a1afd55ce506659b15dc2c88d601e8947a5a20f153959f1c81c8a2e063099e3',
        'note', 'GoodHR OCR macOS 通用安装包'
    );
    changed_rows integer;
BEGIN
    -- 锁定唯一配置行；结构与现有 Windows 发布记录不符合预期时停止发布。
    SELECT config_value INTO STRICT old_config
    FROM system_configs
    WHERE config_key = 'system.onboarding_config'
    FOR UPDATE;

    IF jsonb_typeof(old_config) IS DISTINCT FROM 'object'
        OR jsonb_typeof(old_config -> 'local_agent') IS DISTINCT FROM 'array'
        OR jsonb_typeof(old_config -> 'runtime_components') IS DISTINCT FROM 'object'
        OR jsonb_typeof(old_config #> '{runtime_components,node_runtime}') IS DISTINCT FROM 'object'
        OR jsonb_typeof(old_config #> '{runtime_components,camoufox}') IS DISTINCT FROM 'object'
        OR jsonb_typeof(old_config #> '{runtime_components,ocr}') IS DISTINCT FROM 'object' THEN
        RAISE EXCEPTION 'system.onboarding_config 结构不符合预期，停止 Mac 发布';
    END IF;

    old_releases := old_config -> 'local_agent';
    IF NOT EXISTS (
        SELECT 1 FROM jsonb_array_elements(old_releases) AS releases(entry)
        WHERE NULLIF(BTRIM(entry ->> 'url_win'), '') IS NOT NULL
    ) THEN
        RAISE EXCEPTION '缺少现有 Windows 发布记录，停止 Mac 发布';
    END IF;
    IF EXISTS (
        SELECT 1 FROM jsonb_array_elements(old_releases) AS releases(entry)
        WHERE entry ->> 'version' = '6.2.10.4'
          AND NULLIF(BTRIM(entry ->> 'url_mac'), '') IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'Mac 6.2.10.4 发布记录已存在，停止重复发布';
    END IF;

    -- 只合并三个组件的 Mac 架构键，原 Windows 和其他组件保持原值。
    new_config := jsonb_set(
        jsonb_set(
            old_config,
            '{runtime_components}',
            (old_config -> 'runtime_components') || jsonb_build_object(
                'node_runtime', (old_config #> '{runtime_components,node_runtime}') ||
                    jsonb_build_object('mac', mac_node, 'darwin-x64', intel_node),
                'camoufox', (old_config #> '{runtime_components,camoufox}') ||
                    jsonb_build_object('mac', mac_camoufox, 'darwin-x64', intel_camoufox),
                'ocr', (old_config #> '{runtime_components,ocr}') ||
                    jsonb_build_object('mac', mac_ocr, 'darwin-x64', mac_ocr)
            ),
            true
        ),
        '{local_agent}',
        jsonb_build_array(mac_release) || old_releases,
        true
    );

    -- 更新前断言：旧发布列表、Windows 资源和无关配置字段完整保留。
    IF (new_config -> 'local_agent') - 0 <> old_releases
        OR new_config - 'local_agent' - 'runtime_components' <>
           old_config - 'local_agent' - 'runtime_components'
        OR (new_config -> 'runtime_components') - 'node_runtime' - 'camoufox' - 'ocr' <>
           (old_config -> 'runtime_components') - 'node_runtime' - 'camoufox' - 'ocr'
        OR (new_config #> '{runtime_components,node_runtime}') - 'mac' - 'darwin-x64' <>
           (old_config #> '{runtime_components,node_runtime}') - 'mac' - 'darwin-x64'
        OR (new_config #> '{runtime_components,camoufox}') - 'mac' - 'darwin-x64' <>
           (old_config #> '{runtime_components,camoufox}') - 'mac' - 'darwin-x64'
        OR (new_config #> '{runtime_components,ocr}') - 'mac' - 'darwin-x64' <>
           (old_config #> '{runtime_components,ocr}') - 'mac' - 'darwin-x64' THEN
        RAISE EXCEPTION '非 Mac 配置发生变化，停止发布';
    END IF;

    UPDATE system_configs
    SET config_value = new_config, updated_at = now()
    WHERE config_key = 'system.onboarding_config';
    GET DIAGNOSTICS changed_rows = ROW_COUNT;
    IF changed_rows <> 1 THEN
        RAISE EXCEPTION 'system.onboarding_config 更新行数异常：%', changed_rows;
    END IF;
    SELECT config_value INTO STRICT old_config
    FROM system_configs
    WHERE config_key = 'system.onboarding_config';
    IF old_config IS DISTINCT FROM new_config THEN
        RAISE EXCEPTION 'system.onboarding_config 写入后内容不符，回滚发布';
    END IF;
END
$goodhr_mac_release$;

-- 输出最新 Mac 发布、发布总条数和双架构组件地址，供执行后复核。
SELECT
    config_value #>> '{local_agent,0,version}' AS mac_version,
    config_value #>> '{local_agent,0,url_mac}' AS mac_pkg_url,
    config_value #>> '{local_agent,0,sha256}' AS mac_pkg_sha256,
    jsonb_array_length(config_value -> 'local_agent') AS release_count,
    config_value #>> '{runtime_components,node_runtime,mac,url}' AS arm_node_url,
    config_value #>> '{runtime_components,node_runtime,darwin-x64,url}' AS intel_node_url,
    config_value #>> '{runtime_components,camoufox,darwin-x64,url}' AS intel_camoufox_url,
    config_value #>> '{runtime_components,ocr,darwin-x64,url}' AS intel_ocr_url
FROM system_configs
WHERE config_key = 'system.onboarding_config';

COMMIT;
