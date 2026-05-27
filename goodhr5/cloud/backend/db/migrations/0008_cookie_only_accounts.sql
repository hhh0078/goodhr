-- 本迁移将平台账号数据收敛到 cookie_data，删除独立 platform_accounts 表。
-- 如果旧账号表有名称而 cookie 记录缺名称，则先把账号名称回填到同用户同平台的 cookie。
DO $$
BEGIN
    IF to_regclass('public.platform_accounts') IS NOT NULL THEN
        UPDATE cookie_data cd
        SET display_name = pa.display_name,
            updated_at = NOW()
        FROM platform_accounts pa
        WHERE cd.user_id = pa.user_id
          AND cd.platform_id = pa.platform_id
          AND COALESCE(cd.display_name, '') = ''
          AND COALESCE(pa.display_name, '') <> '';
    END IF;
END $$;

ALTER TABLE task_runs
    DROP CONSTRAINT IF EXISTS task_runs_platform_account_id_fkey;

COMMENT ON COLUMN task_runs.platform_account_id IS '兼容字段：当前保存 cookie_data.id，用于选择平台登录 cookie';

DROP INDEX IF EXISTS idx_platform_accounts_user_id;
DROP TABLE IF EXISTS platform_accounts;
