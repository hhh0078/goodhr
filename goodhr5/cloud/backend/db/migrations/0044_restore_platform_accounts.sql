-- 本迁移恢复平台账号信息表，用于云端保存账号名称和本地 profile 标识，不保存 cookie 内容。
CREATE TABLE IF NOT EXISTS platform_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    local_agent_id UUID REFERENCES local_agents(id) ON DELETE SET NULL,
    platform_id TEXT NOT NULL,
    display_name TEXT NOT NULL,
    local_profile_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, platform_id, local_profile_id)
);

COMMENT ON TABLE platform_accounts IS '招聘平台账号信息表，只保存账号名称和本地 profile 标识，不保存 cookie 内容';
COMMENT ON COLUMN platform_accounts.user_id IS '账号所属云端用户 ID';
COMMENT ON COLUMN platform_accounts.local_agent_id IS '最近关联的本地程序机器 ID，可为空';
COMMENT ON COLUMN platform_accounts.platform_id IS '招聘平台标识，例如 boss、zhaopin、liepin';
COMMENT ON COLUMN platform_accounts.display_name IS '用户设置的平台账号展示名称';
COMMENT ON COLUMN platform_accounts.local_profile_id IS '本地程序浏览器 profile 目录标识';
COMMENT ON COLUMN platform_accounts.created_at IS '账号信息创建时间';

CREATE INDEX IF NOT EXISTS idx_platform_accounts_user_id ON platform_accounts(user_id);

-- 将旧 cookie_data 中已有的账号信息回填到 platform_accounts。
-- 这样升级后前端平台账号页仍能看到历史账号，旧任务引用的账号 ID 也不会断开。
INSERT INTO platform_accounts (
    id,
    user_id,
    platform_id,
    display_name,
    local_profile_id,
    created_at
)
SELECT
    cd.id,
    cd.user_id,
    cd.platform_id,
    COALESCE(NULLIF(cd.display_name, ''), cd.platform_id || '账号'),
    cd.id::text,
    cd.created_at
FROM cookie_data cd
WHERE cd.user_id IS NOT NULL
  AND COALESCE(cd.platform_id, '') <> ''
ON CONFLICT DO NOTHING;

ALTER TABLE task_runs
    ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS enable_sound BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS enable_thinking BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS daily_greeted_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS daily_greeted_date DATE NOT NULL DEFAULT CURRENT_DATE,
    ADD COLUMN IF NOT EXISTS local_task_id TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN task_runs.name IS '任务名称，默认由岗位模板名称和筛选模式生成，也允许用户手动修改';
COMMENT ON COLUMN task_runs.enable_sound IS '打招呼成功后是否播放提示音';
COMMENT ON COLUMN task_runs.enable_thinking IS 'AI 调用时是否开启思考模式';
COMMENT ON COLUMN task_runs.daily_greeted_count IS '任务当天已打招呼数量，跨天后由后端自动重置';
COMMENT ON COLUMN task_runs.daily_greeted_date IS 'daily_greeted_count 对应的日期';
COMMENT ON COLUMN task_runs.local_task_id IS 'Local Agent 本地任务目录或任务 ID';
COMMENT ON COLUMN task_runs.platform_account_id IS '关联平台账号信息 ID，对应 platform_accounts.id';

ALTER TABLE candidate_engagements
    DROP CONSTRAINT IF EXISTS candidate_engagements_platform_account_id_fkey;

ALTER TABLE candidate_events
    DROP CONSTRAINT IF EXISTS candidate_events_platform_account_id_fkey;

COMMENT ON COLUMN candidate_engagements.platform_account_id IS '关联平台账号信息 ID，对应 platform_accounts.id';
COMMENT ON COLUMN candidate_events.platform_account_id IS '关联平台账号信息 ID，对应 platform_accounts.id';
