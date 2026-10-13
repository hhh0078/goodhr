-- 文件作用说明：发布 Mac 通用本地程序 6.2.10.7，仅在生产本地程序列表首位新增该 Mac 记录。
-- 本文件供人工审阅后单独执行；执行前须在服务器私有目录备份 system_configs。
-- 使用行锁保留执行时的全部旧记录；同版本 Mac 记录完全相同时幂等，存在不同内容或重复记录时停止发布。

BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';

DO $goodhr_mac_62107$
DECLARE
    mac_release CONSTANT jsonb := jsonb_build_object(
        'version', '6.2.10.7',
        'url_win', '',
        'url_mac', 'https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.2.10.7-mac-universal-4f933881.pkg',
        'sha256', '4f9338811b85b6da2fc447bb160f845fb8b6878573fd61a2774c4dadf12c2f31',
        'note', '修复 Mac Firefox 中文乱码；更新后请退出旧 Firefox，再重新打开。'
    );
    old_config jsonb;
    old_releases jsonb;
    old_windows jsonb;
    new_windows jsonb;
    new_config jsonb;
    stored_config jsonb;
    existing_mac_rows bigint;
    changed_rows integer;
BEGIN
    -- 锁定唯一配置行，禁止并发发布覆盖当前本地程序列表。
    SELECT config_value INTO STRICT old_config
    FROM system_configs
    WHERE config_key = 'system.onboarding_config'
    FOR UPDATE;

    IF jsonb_typeof(old_config) IS DISTINCT FROM 'object'
        OR jsonb_typeof(old_config -> 'local_agent') IS DISTINCT FROM 'array' THEN
        RAISE EXCEPTION 'system.onboarding_config 结构不符合预期，停止 Mac 发布';
    END IF;

    old_releases := old_config -> 'local_agent';
    IF EXISTS (
        SELECT 1 FROM jsonb_array_elements(old_releases) AS releases(entry)
        WHERE jsonb_typeof(entry) IS DISTINCT FROM 'object'
    ) THEN
        RAISE EXCEPTION '本地程序列表包含无效记录，停止 Mac 发布';
    END IF;

    -- Windows 可以独立使用相同版本号；这里只识别包含 Mac 下载地址的同版本记录。
    SELECT count(*) INTO existing_mac_rows
    FROM jsonb_array_elements(old_releases) AS releases(entry)
    WHERE entry ->> 'version' = mac_release ->> 'version'
        AND NULLIF(BTRIM(entry ->> 'url_mac'), '') IS NOT NULL;

    IF existing_mac_rows > 1 THEN
        RAISE EXCEPTION 'Mac 6.2.10.7 记录重复（% 条），停止发布', existing_mac_rows;
    END IF;
    IF existing_mac_rows = 1 THEN
        IF EXISTS (
            SELECT 1 FROM jsonb_array_elements(old_releases) AS releases(entry)
            WHERE entry ->> 'version' = mac_release ->> 'version'
                AND NULLIF(BTRIM(entry ->> 'url_mac'), '') IS NOT NULL
                AND entry IS DISTINCT FROM mac_release
        ) THEN
            RAISE EXCEPTION 'Mac 6.2.10.7 已存在但内容不同，停止发布';
        END IF;
        RAISE NOTICE 'Mac 6.2.10.7 已发布且内容相同，本次不修改配置';
        RETURN;
    END IF;

    -- 保存 Windows 记录的完整内容和顺序，更新后逐项比较。
    SELECT COALESCE(jsonb_agg(entry ORDER BY ordinal), '[]'::jsonb) INTO old_windows
    FROM jsonb_array_elements(old_releases) WITH ORDINALITY AS releases(entry, ordinal)
    WHERE NULLIF(BTRIM(entry ->> 'url_win'), '') IS NOT NULL;

    new_config := jsonb_set(old_config, '{local_agent}', jsonb_build_array(mac_release) || old_releases, false);
    SELECT COALESCE(jsonb_agg(entry ORDER BY ordinal), '[]'::jsonb) INTO new_windows
    FROM jsonb_array_elements(new_config -> 'local_agent') WITH ORDINALITY AS releases(entry, ordinal)
    WHERE NULLIF(BTRIM(entry ->> 'url_win'), '') IS NOT NULL;

    IF jsonb_array_length(new_config -> 'local_agent') <> jsonb_array_length(old_releases) + 1
        OR (new_config -> 'local_agent') - 0 IS DISTINCT FROM old_releases
        OR new_windows IS DISTINCT FROM old_windows
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
$goodhr_mac_62107$;

-- 执行后复核 Mac 记录；不依赖旧列表长度或 Windows 当前版本。
SELECT
    jsonb_array_length(config_value -> 'local_agent') AS release_count,
    entry ->> 'version' AS mac_version,
    entry ->> 'url_mac' AS mac_pkg_url,
    entry ->> 'sha256' AS mac_pkg_sha256,
    entry ->> 'note' AS mac_note
FROM system_configs,
    LATERAL jsonb_array_elements(config_value -> 'local_agent') AS releases(entry)
WHERE config_key = 'system.onboarding_config'
    AND entry ->> 'version' = '6.2.10.7'
    AND NULLIF(BTRIM(entry ->> 'url_mac'), '') IS NOT NULL;

COMMIT;
