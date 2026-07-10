-- 本文件定义 GoodHR 5 云端数据库初始表结构。
-- 云端只保存账号、配置、本地程序连接记录、任务元信息和统计摘要。
-- 候选人详情、截图、OCR 原文、招聘平台 cookie/profile 必须留在本地 Agent。

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- users 保存云端登录用户。
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at TIMESTAMPTZ
);

COMMENT ON TABLE users IS '云端登录用户表';
COMMENT ON COLUMN users.email IS '用户邮箱，验证码登录的唯一账号';
COMMENT ON COLUMN users.last_login_at IS '最近一次验证码登录成功时间';

-- local_agents 保存本地 Agent 的连接记录。
CREATE TABLE IF NOT EXISTS local_agents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    machine_id TEXT NOT NULL,
    agent_version TEXT NOT NULL DEFAULT '',
    bind_status TEXT NOT NULL DEFAULT 'active',
    last_seen_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, machine_id)
);

COMMENT ON TABLE local_agents IS '用户本地 Agent 连接记录';
COMMENT ON COLUMN local_agents.machine_id IS 'Local Agent 上报的哈希机器码';
COMMENT ON COLUMN local_agents.bind_status IS '连接记录状态，初期使用 active/disabled';

-- platform_accounts 保存云端可见的平台账号映射，不保存真实 cookie。
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

COMMENT ON TABLE platform_accounts IS '招聘平台账号映射表，不保存 cookie/profile 原文';
COMMENT ON COLUMN platform_accounts.platform_id IS '招聘平台标识，例如 boss、zhaopin';
COMMENT ON COLUMN platform_accounts.local_profile_id IS 'Local Agent 内部 profile ID';

-- positions 保存用户配置的招聘岗位和筛选关键词。
CREATE TABLE IF NOT EXISTS positions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    keywords JSONB NOT NULL DEFAULT '[]'::jsonb,
    exclude_keywords JSONB NOT NULL DEFAULT '[]'::jsonb,
    description TEXT NOT NULL DEFAULT '',
    greet_message TEXT NOT NULL DEFAULT '',
    is_and_mode BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE positions IS '招聘岗位和关键词筛选配置';
COMMENT ON COLUMN positions.keywords IS '正向关键词数组';
COMMENT ON COLUMN positions.exclude_keywords IS '排除关键词数组';
COMMENT ON COLUMN positions.is_and_mode IS '关键词是否使用 AND 匹配';

-- user_ai_configs 保存用户自定义 AI 配置。
CREATE TABLE IF NOT EXISTS user_ai_configs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    base_url TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    api_key_encrypted TEXT NOT NULL DEFAULT '',
    temperature NUMERIC(3, 2),
    prompt_template TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE user_ai_configs IS '用户自定义 AI 配置';
COMMENT ON COLUMN user_ai_configs.api_key_encrypted IS '加密后的用户 API Key';

-- task_runs 保存云端任务元信息和统计摘要。
CREATE TABLE IF NOT EXISTS task_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    local_agent_id UUID REFERENCES local_agents(id) ON DELETE SET NULL,
    platform_account_id UUID REFERENCES platform_accounts(id) ON DELETE SET NULL,
    position_id UUID REFERENCES positions(id) ON DELETE SET NULL,
    platform_id TEXT NOT NULL,
    mode TEXT NOT NULL DEFAULT 'keyword',
    match_limit INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'created',
    scanned_count INTEGER NOT NULL DEFAULT 0,
    greeted_count INTEGER NOT NULL DEFAULT 0,
    daily_greeted_count INTEGER NOT NULL DEFAULT 0,
    daily_greeted_date DATE NOT NULL DEFAULT CURRENT_DATE,
    skipped_count INTEGER NOT NULL DEFAULT 0,
    failed_count INTEGER NOT NULL DEFAULT 0,
    local_task_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ
);

COMMENT ON TABLE task_runs IS '云端任务运行记录，仅保存摘要不保存候选人详情';
COMMENT ON COLUMN task_runs.mode IS '筛选模式，例如 keyword 或 ai';
COMMENT ON COLUMN task_runs.local_task_id IS 'Local Agent 本地任务目录或任务 ID';
COMMENT ON COLUMN task_runs.daily_greeted_count IS '任务当天已打招呼数量，跨天后由后端自动重置';
COMMENT ON COLUMN task_runs.daily_greeted_date IS 'daily_greeted_count 对应的日期';

-- task_logs 保存任务运行日志摘要，详细候选人数据仍由本地 Agent 保存。
CREATE TABLE IF NOT EXISTS task_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL REFERENCES task_runs(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    level TEXT NOT NULL DEFAULT 'info',
    message TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE task_logs IS '云端任务日志摘要';
COMMENT ON COLUMN task_logs.message IS '任务运行日志摘要，不写入候选人完整详情';

CREATE INDEX IF NOT EXISTS idx_local_agents_user_id ON local_agents(user_id);
CREATE INDEX IF NOT EXISTS idx_platform_accounts_user_id ON platform_accounts(user_id);
CREATE INDEX IF NOT EXISTS idx_positions_user_id ON positions(user_id);
CREATE INDEX IF NOT EXISTS idx_task_runs_user_id_created_at ON task_runs(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_task_runs_status ON task_runs(status);
CREATE INDEX IF NOT EXISTS idx_task_logs_task_id_created_at ON task_logs(task_id, created_at DESC);
