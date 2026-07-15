-- 本迁移新增用户招聘流程快照和事件，用于准确定位首次跑通任务前的停留节点。
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS flow_state JSONB NOT NULL DEFAULT '{}'::jsonb;

-- 旧 onboarding 只有一个人工完成标记，无法准确表示真实业务进度，由新流程快照替代。
ALTER TABLE users DROP COLUMN IF EXISTS onboarding;

COMMENT ON COLUMN users.flow_state IS '用户首次跑通招聘任务的流程快照，仅由后端流程服务维护';

CREATE TABLE IF NOT EXISTS user_flow_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    flow_version INTEGER NOT NULL DEFAULT 2,
    event_key TEXT NOT NULL,
    status TEXT NOT NULL,
    reason_code TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT '',
    task_id UUID REFERENCES task_runs(id) ON DELETE SET NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_user_flow_events_user_created
ON user_flow_events(user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_user_flow_events_key_status
ON user_flow_events(event_key, status, created_at DESC);

COMMENT ON TABLE user_flow_events IS '用户招聘流程节点事件，用于还原尝试、成功和失败原因';
COMMENT ON COLUMN user_flow_events.id IS '流程事件唯一标识';
COMMENT ON COLUMN user_flow_events.user_id IS '事件所属用户标识';
COMMENT ON COLUMN user_flow_events.flow_version IS '流程定义版本';
COMMENT ON COLUMN user_flow_events.event_key IS '流程节点英文键';
COMMENT ON COLUMN user_flow_events.status IS '节点状态：pending、blocked、completed';
COMMENT ON COLUMN user_flow_events.reason_code IS '结构化失败原因，避免依赖中文错误文案判断';
COMMENT ON COLUMN user_flow_events.message IS '供管理端查看的简短失败说明';
COMMENT ON COLUMN user_flow_events.source IS '事件来源，例如前端、云端后端或本地程序';
COMMENT ON COLUMN user_flow_events.task_id IS '关联任务标识，没有关联任务时为空';
COMMENT ON COLUMN user_flow_events.metadata IS '关联岗位、任务、平台和运行环境等补充证据';
COMMENT ON COLUMN user_flow_events.created_at IS '事件发生时间';

-- 用已有业务数据回填能够被证明确实完成的节点。后续节点可以反向证明此前必经节点完成。
WITH facts AS (
    SELECT
        u.id,
        (SELECT MIN(la.created_at) FROM local_agents la WHERE la.user_id = u.id) AS legacy_agent_at,
        (SELECT MIN(p.created_at) FROM positions p WHERE p.user_id = u.id) AS position_at,
        (SELECT MIN(tr.created_at) FROM task_runs tr WHERE tr.user_id = u.id) AS task_at,
        (SELECT MIN(COALESCE(tr.started_at, tr.created_at))
         FROM task_runs tr
         WHERE tr.user_id = u.id
           AND (tr.started_at IS NOT NULL OR tr.status IN ('running', 'completed', 'stopped', 'failed') OR tr.scanned_count > 0 OR tr.greeted_count > 0)) AS started_at,
        (SELECT MIN(COALESCE(tr.started_at, tr.created_at))
         FROM task_runs tr WHERE tr.user_id = u.id AND tr.scanned_count > 0) AS resume_at,
        (SELECT MIN(COALESCE(tr.started_at, tr.created_at))
         FROM task_runs tr WHERE tr.user_id = u.id AND (tr.greeted_count > 0 OR tr.daily_greeted_count > 0)) AS greet_at
    FROM users u
), normalized AS (
    SELECT
        id,
        COALESCE(legacy_agent_at, started_at, resume_at, greet_at) AS agent_at,
        COALESCE(started_at, resume_at, greet_at) AS runtime_at,
        COALESCE(position_at, task_at, started_at, resume_at, greet_at) AS position_at,
        COALESCE(task_at, started_at, resume_at, greet_at) AS task_at,
        COALESCE(started_at, resume_at, greet_at) AS platform_at,
        COALESCE(started_at, resume_at, greet_at) AS started_at,
        COALESCE(resume_at, greet_at) AS resume_at,
        greet_at
    FROM facts
)
UPDATE users u
SET flow_state = jsonb_build_object(
    'version', 2,
    'stage', CASE
        WHEN n.greet_at IS NOT NULL THEN 'completed'
        WHEN n.agent_at IS NULL THEN 'agent_detected'
        WHEN n.runtime_at IS NULL THEN 'runtime_ready'
        WHEN n.position_at IS NULL THEN 'position_created'
        WHEN n.task_at IS NULL THEN 'task_created'
        WHEN n.platform_at IS NULL THEN 'platform_login_verified'
        WHEN n.started_at IS NULL THEN 'task_started'
        WHEN n.resume_at IS NULL THEN 'first_resume_processed'
        ELSE 'first_greet_success'
    END,
    'stage_name', CASE
        WHEN n.greet_at IS NOT NULL THEN '核心流程已跑通'
        WHEN n.agent_at IS NULL THEN '启动本地程序'
        WHEN n.runtime_at IS NULL THEN '安装运行组件'
        WHEN n.position_at IS NULL THEN '创建岗位'
        WHEN n.task_at IS NULL THEN '创建任务'
        WHEN n.platform_at IS NULL THEN '登录招聘平台'
        WHEN n.started_at IS NULL THEN '启动任务'
        WHEN n.resume_at IS NULL THEN '处理首份简历'
        ELSE '首次打招呼成功'
    END,
    'state', CASE WHEN n.greet_at IS NOT NULL THEN 'completed' ELSE 'pending' END,
    'last_activity_at', GREATEST(n.agent_at, n.runtime_at, n.position_at, n.task_at, n.platform_at, n.started_at, n.resume_at, n.greet_at),
    'completed_at', n.greet_at,
    'steps', jsonb_strip_nulls(jsonb_build_object(
        'agent_detected', CASE WHEN n.agent_at IS NOT NULL THEN jsonb_build_object('status', 'completed', 'completed_at', n.agent_at) END,
        'runtime_ready', CASE WHEN n.runtime_at IS NOT NULL THEN jsonb_build_object('status', 'completed', 'completed_at', n.runtime_at) END,
        'position_created', CASE WHEN n.position_at IS NOT NULL THEN jsonb_build_object('status', 'completed', 'completed_at', n.position_at) END,
        'task_created', CASE WHEN n.task_at IS NOT NULL THEN jsonb_build_object('status', 'completed', 'completed_at', n.task_at) END,
        'platform_login_verified', CASE WHEN n.platform_at IS NOT NULL THEN jsonb_build_object('status', 'completed', 'completed_at', n.platform_at) END,
        'task_started', CASE WHEN n.started_at IS NOT NULL THEN jsonb_build_object('status', 'completed', 'completed_at', n.started_at) END,
        'first_resume_processed', CASE WHEN n.resume_at IS NOT NULL THEN jsonb_build_object('status', 'completed', 'completed_at', n.resume_at) END,
        'first_greet_success', CASE WHEN n.greet_at IS NOT NULL THEN jsonb_build_object('status', 'completed', 'completed_at', n.greet_at) END
    ))
)
FROM normalized n
WHERE u.id = n.id
  AND (u.flow_state = '{}'::jsonb OR COALESCE((u.flow_state->>'version')::int, 0) < 2);

-- 自动挽回邮件改为新的真实流程节点，支付和旧 AI/平台账号节点不再属于主流程。
UPDATE system_configs
SET config_value = jsonb_set(
    config_value,
    '{templates}',
    '{
      "agent_detected": {"subject":"GoodHR 本地程序还没启动","html":"<p>我还没检测到本地程序，任务暂时跑不起来。</p><p>启动 GoodHR 本地程序后，再回到后台刷新一下就行。</p>"},
      "runtime_ready": {"subject":"GoodHR 运行组件还差一步","html":"<p>本地程序已经找到了，但浏览器运行组件还没准备好。</p><p>回到后台完成组件安装，我再继续开工。</p>"},
      "position_created": {"subject":"GoodHR 岗位还没创建","html":"<p>岗位还没创建，我暂时不知道该帮你筛谁。</p><p>先建一个岗位，后面的任务就顺了。</p>"},
      "task_created": {"subject":"GoodHR 招聘任务还没创建","html":"<p>岗位已经准备好了，还差一条招聘任务。</p><p>创建任务后就可以检查平台登录并开始运行。</p>"},
      "platform_login_verified": {"subject":"GoodHR 招聘平台还没确认登录","html":"<p>任务已经有了，但招聘平台登录状态还没确认。</p><p>打开任务并完成平台登录，我再继续干活。</p>"},
      "task_started": {"subject":"GoodHR 任务还没成功启动","html":"<p>任务还没有真正跑起来。</p><p>回到任务列表再启动一次，页面会告诉你具体卡在哪里。</p>"},
      "first_resume_processed": {"subject":"GoodHR 还没处理到第一份简历","html":"<p>任务已经启动过，但还没成功处理到第一份简历。</p><p>可以看看任务日志里的最近一条提示。</p>"},
      "first_greet_success": {"subject":"GoodHR 还差第一次成功打招呼","html":"<p>简历已经开始处理，但还没有第一次打招呼成功。</p><p>看看筛选结果和失败日志，通常很快就能定位。</p>"},
      "inactive_3_days": {"subject":"3 天没见你了，我先小声冒个泡","html":"<p>3 天没见，我来看看招聘任务是不是遇到了小卡点。</p>"},
      "inactive_7_days": {"subject":"一周没见，GoodHR 还在原地等你","html":"<p>一周没见，GoodHR 还在等你回来继续。</p>"},
      "inactive_30_days": {"subject":"一个月没见，我来弱弱问候一下","html":"<p>一个月没见，我来问问是不是哪里不太顺手。</p>"}
    }'::jsonb,
    true
)
WHERE config_key = 'system.email_recovery';
