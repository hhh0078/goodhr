-- 文件作用说明：仅撤回 Mac 本地程序 6.2.10.7 的生产下载记录，保留其他发布和系统配置。
-- 本文件仅供人工审阅后手动执行；目标记录不存在时幂等，同版本 Mac 内容不同或重复时停止回退。
-- 通过行锁与执行时的 JSON 比较，确保 Windows 记录及 local_agent 以外的配置保持原样。

BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';

DO $goodhr_mac_62107_rollback$
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
    existing_mac jsonb;
    existing_mac_index integer;
    existing_mac_rows bigint;
    remaining_releases jsonb;
    new_config jsonb;
    stored_config jsonb;
    changed_rows integer;
BEGIN
    -- 锁定当前配置，仅删除与本次发布内容完全一致的 Mac 记录。
    SELECT config_value INTO STRICT old_config
    FROM system_configs
    WHERE config_key = 'system.onboarding_config'
    FOR UPDATE;

    IF jsonb_typeof(old_config) IS DISTINCT FROM 'object'
        OR jsonb_typeof(old_config -> 'local_agent') IS DISTINCT FROM 'array' THEN
        RAISE EXCEPTION 'system.onboarding_config 结构不符合预期，停止 Mac 回退';
    END IF;

    old_releases := old_config -> 'local_agent';
    IF EXISTS (
        SELECT 1 FROM jsonb_array_elements(old_releases) AS releases(entry)
        WHERE jsonb_typeof(entry) IS DISTINCT FROM 'object'
    ) THEN
        RAISE EXCEPTION '本地程序列表包含无效记录，停止 Mac 回退';
    END IF;

    SELECT count(*) INTO existing_mac_rows
    FROM jsonb_array_elements(old_releases) AS releases(entry)
    WHERE entry ->> 'version' = mac_release ->> 'version'
        AND NULLIF(BTRIM(entry ->> 'url_mac'), '') IS NOT NULL;

    IF existing_mac_rows = 0 THEN
        RAISE NOTICE 'Mac 6.2.10.7 记录不存在，本次不修改配置';
        RETURN;
    END IF;
    IF existing_mac_rows <> 1 THEN
        RAISE EXCEPTION 'Mac 6.2.10.7 记录重复（% 条），停止回退', existing_mac_rows;
    END IF;

    SELECT entry, (ordinal - 1)::integer INTO STRICT existing_mac, existing_mac_index
    FROM jsonb_array_elements(old_releases) WITH ORDINALITY AS releases(entry, ordinal)
    WHERE entry ->> 'version' = mac_release ->> 'version'
        AND NULLIF(BTRIM(entry ->> 'url_mac'), '') IS NOT NULL;
    IF existing_mac IS DISTINCT FROM mac_release THEN
        RAISE EXCEPTION 'Mac 6.2.10.7 内容已变化，停止回退';
    END IF;

    SELECT COALESCE(jsonb_agg(entry ORDER BY ordinal), '[]'::jsonb) INTO old_windows
    FROM jsonb_array_elements(old_releases) WITH ORDINALITY AS releases(entry, ordinal)
    WHERE NULLIF(BTRIM(entry ->> 'url_win'), '') IS NOT NULL;

    -- 只删除已校验的数组位置，其他记录及其顺序不变。
    remaining_releases := old_releases - existing_mac_index;
    new_config := jsonb_set(old_config, '{local_agent}', remaining_releases, false);
    SELECT COALESCE(jsonb_agg(entry ORDER BY ordinal), '[]'::jsonb) INTO new_windows
    FROM jsonb_array_elements(remaining_releases) WITH ORDINALITY AS releases(entry, ordinal)
    WHERE NULLIF(BTRIM(entry ->> 'url_win'), '') IS NOT NULL;

    IF jsonb_array_length(remaining_releases) <> jsonb_array_length(old_releases) - 1
        OR new_windows IS DISTINCT FROM old_windows
        OR new_config - 'local_agent' IS DISTINCT FROM old_config - 'local_agent' THEN
        RAISE EXCEPTION '回退范围超出 Mac 6.2.10.7 记录，停止回退';
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
        RAISE EXCEPTION 'system.onboarding_config 回退后校验失败，回滚事务';
    END IF;
END
$goodhr_mac_62107_rollback$;

COMMIT;
