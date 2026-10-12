-- 文件作用说明：仅回退 Mac 本地程序 6.2.10.6 的生产下载记录，不覆盖后续发布或其他系统配置。
-- 仅在需要撤销该 Mac 安装包入口时手动执行；若匹配记录不存在或不唯一，事务会失败并回滚。
-- Windows 下载记录、runtime_components 以及其他配置字段始终按执行时的最新值保留。

BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';

DO $goodhr_mac_62106_rollback$
DECLARE
    expected_mac_url CONSTANT text := 'https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.2.10.6-mac-universal-b7886055.pkg';
    expected_mac_sha256 CONSTANT text := 'b7886055e93995fc626f0f67961e73976eb4288fdb700b6d28ff54a6c6239412';
    old_config jsonb;
    old_releases jsonb;
    remaining_releases jsonb;
    new_config jsonb;
    matching_rows bigint;
    changed_rows integer;
BEGIN
    -- 锁定当前配置，仅检查并移除要回退的这条 Mac 发布记录。
    SELECT config_value INTO STRICT old_config
    FROM system_configs
    WHERE config_key = 'system.onboarding_config'
    FOR UPDATE;

    IF jsonb_typeof(old_config) IS DISTINCT FROM 'object'
        OR jsonb_typeof(old_config -> 'local_agent') IS DISTINCT FROM 'array' THEN
        RAISE EXCEPTION 'system.onboarding_config 结构不符合预期，停止 Mac 回退';
    END IF;

    old_releases := old_config -> 'local_agent';
    SELECT count(*) INTO matching_rows
    FROM jsonb_array_elements(old_releases) AS releases(entry)
    WHERE entry ->> 'version' = '6.2.10.6'
        AND entry ->> 'url_mac' = expected_mac_url
        AND entry ->> 'sha256' = expected_mac_sha256
        AND entry ->> 'url_win' = '';

    IF matching_rows <> 1 THEN
        RAISE EXCEPTION 'Mac 6.2.10.6 记录不存在或不唯一（匹配 % 条），停止回退', matching_rows;
    END IF;

    SELECT COALESCE(jsonb_agg(entry ORDER BY ordinal), '[]'::jsonb)
    INTO remaining_releases
    FROM jsonb_array_elements(old_releases) WITH ORDINALITY AS releases(entry, ordinal)
    WHERE (
        entry ->> 'version' = '6.2.10.6'
        AND entry ->> 'url_mac' = expected_mac_url
        AND entry ->> 'sha256' = expected_mac_sha256
        AND entry ->> 'url_win' = ''
    ) IS NOT TRUE;

    new_config := jsonb_set(old_config, '{local_agent}', remaining_releases, false);
    IF jsonb_array_length(remaining_releases) <> jsonb_array_length(old_releases) - 1
        OR new_config - 'local_agent' IS DISTINCT FROM old_config - 'local_agent' THEN
        RAISE EXCEPTION '回退范围超出 Mac 6.2.10.6 记录，停止回退';
    END IF;

    UPDATE system_configs
    SET config_value = new_config, updated_at = now()
    WHERE config_key = 'system.onboarding_config';
    GET DIAGNOSTICS changed_rows = ROW_COUNT;
    IF changed_rows <> 1 THEN
        RAISE EXCEPTION 'system.onboarding_config 更新行数异常：%', changed_rows;
    END IF;
END
$goodhr_mac_62106_rollback$;

COMMIT;
