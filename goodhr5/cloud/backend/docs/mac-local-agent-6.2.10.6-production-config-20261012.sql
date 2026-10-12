-- 文件作用说明：发布 Mac 通用版本地程序 6.2.10.6，只在生产 system.onboarding_config 的本地程序列表首位加入一条 Mac 记录。
-- 本文件供人工审阅后单独执行，不放入自动迁移目录；执行前须在私有目录备份 system_configs，并确认备份文件权限为 0600。
-- 仅接受当前已核验的 41 条发布记录和顶部 Windows 6.2.10.6；任何配置漂移都会使事务回滚。

BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';

DO $goodhr_mac_62106$
DECLARE
    expected_win_url CONSTANT text := 'https://oss2.58it.cn/GoodHR-Local-Agent-v6.2.10.6-windows-x64-57710b0c.zip';
    expected_win_sha256 CONSTANT text := '57710b0c163fe2822e6ae4a66111c88af8c5b7d507c483d26ebf1c34561b21a0';
    mac_release CONSTANT jsonb := jsonb_build_object(
        'version', '6.2.10.6',
        'url_win', '',
        'url_mac', 'https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.2.10.6-mac-universal-b7886055.pkg',
        'sha256', 'b7886055e93995fc626f0f67961e73976eb4288fdb700b6d28ff54a6c6239412',
        'note', 'macOS Apple 芯片与 Intel 芯片通用安装包'
    );
    old_config jsonb;
    old_releases jsonb;
    new_config jsonb;
    stored_config jsonb;
    changed_rows integer;
BEGIN
    -- 锁定配置行，避免审阅与写入之间有并发发布覆盖列表。
    SELECT config_value INTO STRICT old_config
    FROM system_configs
    WHERE config_key = 'system.onboarding_config'
    FOR UPDATE;

    IF jsonb_typeof(old_config) IS DISTINCT FROM 'object'
        OR jsonb_typeof(old_config -> 'local_agent') IS DISTINCT FROM 'array'
        OR jsonb_typeof(old_config -> 'runtime_components') IS DISTINCT FROM 'object' THEN
        RAISE EXCEPTION 'system.onboarding_config 结构不符合预期，停止 Mac 发布';
    END IF;

    old_releases := old_config -> 'local_agent';
    IF jsonb_array_length(old_releases) <> 41
        OR jsonb_typeof(old_releases -> 0) IS DISTINCT FROM 'object'
        OR old_releases -> 0 ->> 'version' IS DISTINCT FROM '6.2.10.6'
        OR old_releases -> 0 ->> 'url_win' IS DISTINCT FROM expected_win_url
        OR old_releases -> 0 ->> 'sha256' IS DISTINCT FROM expected_win_sha256 THEN
        RAISE EXCEPTION '当前发布列表或 Windows 6.2.10.6 记录已变化，停止 Mac 发布';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM jsonb_array_elements(old_releases) AS releases(entry)
        WHERE jsonb_typeof(entry) IS DISTINCT FROM 'object'
            OR NULLIF(BTRIM(entry ->> 'url_mac'), '') IS NOT NULL
    ) THEN
        RAISE EXCEPTION '发布列表已包含 Mac 地址或无效记录，停止重复发布';
    END IF;

    -- 仅更新 local_agent；包括 Windows 记录和 runtime_components 在内的其余 JSON 内容保持原样。
    new_config := jsonb_set(
        old_config,
        '{local_agent}',
        jsonb_build_array(mac_release) || old_releases,
        false
    );
    IF jsonb_array_length(new_config -> 'local_agent') <> 42
        OR (new_config -> 'local_agent') - 0 IS DISTINCT FROM old_releases
        OR new_config - 'local_agent' IS DISTINCT FROM old_config - 'local_agent' THEN
        RAISE EXCEPTION 'Mac 发布记录以外的配置发生变化，停止发布';
    END IF;

    UPDATE system_configs
    SET config_value = new_config, updated_at = now()
    WHERE config_key = 'system.onboarding_config';
    GET DIAGNOSTICS changed_rows = ROW_COUNT;
    IF changed_rows <> 1 THEN
        RAISE EXCEPTION 'system.onboarding_config 更新行数异常：%', changed_rows;
    END IF;

    SELECT config_value INTO STRICT stored_config
    FROM system_configs
    WHERE config_key = 'system.onboarding_config';
    IF stored_config IS DISTINCT FROM new_config THEN
        RAISE EXCEPTION 'system.onboarding_config 写入后校验失败，回滚发布';
    END IF;
END
$goodhr_mac_62106$;

-- 执行后复核：首条为 Mac 安装包，第二条仍为原 Windows 安装包，总数为 42。
SELECT
    jsonb_array_length(config_value -> 'local_agent') AS release_count,
    config_value #>> '{local_agent,0,version}' AS mac_version,
    config_value #>> '{local_agent,0,url_mac}' AS mac_pkg_url,
    config_value #>> '{local_agent,0,sha256}' AS mac_pkg_sha256,
    config_value #>> '{local_agent,1,version}' AS win_version,
    config_value #>> '{local_agent,1,url_win}' AS win_pkg_url,
    config_value #>> '{local_agent,1,sha256}' AS win_pkg_sha256
FROM system_configs
WHERE config_key = 'system.onboarding_config';

COMMIT;
