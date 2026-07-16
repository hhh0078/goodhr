-- 本迁移彻底删除任务层，并把原任务的配置、状态、日志和业务关联迁移到岗位。

ALTER TABLE positions
    ADD COLUMN IF NOT EXISTS match_limit INTEGER NOT NULL DEFAULT 50,
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'created',
    ADD COLUMN IF NOT EXISTS scanned_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS greeted_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS daily_greeted_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS daily_greeted_date DATE NOT NULL DEFAULT CURRENT_DATE,
    ADD COLUMN IF NOT EXISTS skipped_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS failed_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS enable_sound BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS enable_thinking BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS finished_at TIMESTAMPTZ;

COMMENT ON COLUMN positions.match_limit IS '岗位每次启动最多打招呼人数';
COMMENT ON COLUMN positions.status IS '岗位当前运行状态';
COMMENT ON COLUMN positions.scanned_count IS '岗位累计扫描候选人数';
COMMENT ON COLUMN positions.greeted_count IS '岗位累计打招呼人数';
COMMENT ON COLUMN positions.daily_greeted_count IS '岗位当天累计打招呼人数';
COMMENT ON COLUMN positions.daily_greeted_date IS '岗位当天打招呼数量对应日期';
COMMENT ON COLUMN positions.skipped_count IS '岗位累计跳过候选人数';
COMMENT ON COLUMN positions.failed_count IS '岗位累计处理失败人数';
COMMENT ON COLUMN positions.enable_sound IS '岗位打招呼成功后是否播放提示音';
COMMENT ON COLUMN positions.enable_thinking IS '岗位 AI 调用是否开启思考模式';
COMMENT ON COLUMN positions.started_at IS '岗位最近一次开始时间';
COMMENT ON COLUMN positions.finished_at IS '岗位最近一次结束时间';

-- 同一岗位曾存在多条任务时，沿用最近一次任务的运行参数和状态，累计统计汇总全部历史任务。
WITH latest_task AS (
    SELECT DISTINCT ON (position_id)
        position_id, match_limit, status, enable_sound, enable_thinking, started_at, finished_at
    FROM task_runs
    WHERE position_id IS NOT NULL
    ORDER BY position_id, created_at DESC
), task_totals AS (
    SELECT
        position_id,
        SUM(scanned_count) AS scanned_count,
        SUM(greeted_count) AS greeted_count,
        SUM(skipped_count) AS skipped_count,
        SUM(failed_count) AS failed_count,
        SUM(CASE WHEN daily_greeted_date = CURRENT_DATE THEN daily_greeted_count ELSE 0 END) AS daily_greeted_count
    FROM task_runs
    WHERE position_id IS NOT NULL
    GROUP BY position_id
)
UPDATE positions p
SET match_limit = GREATEST(1, COALESCE(lt.match_limit, 50)),
    status = COALESCE(NULLIF(lt.status, ''), 'created'),
    enable_sound = COALESCE(lt.enable_sound, false),
    enable_thinking = COALESCE(lt.enable_thinking, false),
    scanned_count = COALESCE(tt.scanned_count, 0),
    greeted_count = COALESCE(tt.greeted_count, 0),
    skipped_count = COALESCE(tt.skipped_count, 0),
    failed_count = COALESCE(tt.failed_count, 0),
    daily_greeted_count = COALESCE(tt.daily_greeted_count, 0),
    daily_greeted_date = CURRENT_DATE,
    started_at = lt.started_at,
    finished_at = lt.finished_at
FROM latest_task lt
LEFT JOIN task_totals tt ON tt.position_id = lt.position_id
WHERE p.id = lt.position_id;

CREATE TABLE IF NOT EXISTS position_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    position_id UUID NOT NULL REFERENCES positions(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    level TEXT NOT NULL DEFAULT 'info',
    message TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE position_logs IS '岗位运行日志摘要';
COMMENT ON COLUMN position_logs.id IS '岗位日志唯一标识';
COMMENT ON COLUMN position_logs.position_id IS '日志所属岗位标识';
COMMENT ON COLUMN position_logs.user_id IS '日志所属用户标识';
COMMENT ON COLUMN position_logs.level IS '日志级别';
COMMENT ON COLUMN position_logs.message IS '岗位运行日志摘要';
COMMENT ON COLUMN position_logs.created_at IS '日志创建时间';

INSERT INTO position_logs (id, position_id, user_id, level, message, created_at)
SELECT tl.id, tr.position_id, tl.user_id, tl.level, tl.message, tl.created_at
FROM task_logs tl
JOIN task_runs tr ON tr.id = tl.task_id
WHERE tr.position_id IS NOT NULL
ON CONFLICT (id) DO NOTHING;

CREATE INDEX IF NOT EXISTS idx_position_logs_position_created
    ON position_logs(position_id, created_at DESC);

ALTER TABLE user_flow_events
    ADD COLUMN IF NOT EXISTS position_id UUID REFERENCES positions(id) ON DELETE SET NULL;

ALTER TABLE cookie_data
    ADD COLUMN IF NOT EXISTS used_by_position_id UUID REFERENCES positions(id) ON DELETE SET NULL;

UPDATE cookie_data cookie
SET used_by_position_id = task.position_id
FROM task_runs task
WHERE cookie.used_by_task_id = task.id
  AND cookie.used_by_position_id IS NULL;

UPDATE user_flow_events ufe
SET position_id = tr.position_id
FROM task_runs tr
WHERE ufe.task_id = tr.id
  AND ufe.position_id IS NULL;

UPDATE candidate_engagements engagement
SET position_id = tr.position_id
FROM task_runs tr
WHERE engagement.task_id = tr.id
  AND engagement.position_id IS NULL;

UPDATE candidate_events event
SET position_id = tr.position_id
FROM task_runs tr
WHERE event.task_id = tr.id
  AND event.position_id IS NULL;

COMMENT ON COLUMN user_flow_events.position_id IS '关联岗位标识，没有关联岗位时为空';

-- 候选人触达和事件统一使用 position_id；同一岗位的历史重复触达先安全合并，再建立岗位唯一关系。
DO $$
DECLARE constraint_name TEXT;
BEGIN
    FOR constraint_name IN
        SELECT conname FROM pg_constraint
        WHERE conrelid = 'candidate_engagements'::regclass AND contype = 'u'
    LOOP
        EXECUTE format('ALTER TABLE candidate_engagements DROP CONSTRAINT %I', constraint_name);
    END LOOP;
END $$;

DROP INDEX IF EXISTS idx_candidate_engagements_tenant_task;

DROP TABLE IF EXISTS pg_temp.candidate_engagement_merge_map;
CREATE TEMP TABLE candidate_engagement_merge_map AS
SELECT id AS duplicate_id, keep_id
FROM (
    SELECT
        id,
        FIRST_VALUE(id) OVER (
            PARTITION BY tenant_id, candidate_id, position_id, platform_account_id
            ORDER BY updated_at DESC, created_at DESC, id
        ) AS keep_id
    FROM candidate_engagements
) ranked
WHERE id <> keep_id;

UPDATE candidate_events event
SET engagement_id = merge_map.keep_id
FROM candidate_engagement_merge_map merge_map
WHERE event.engagement_id = merge_map.duplicate_id;

DELETE FROM candidate_engagements engagement
USING candidate_engagement_merge_map merge_map
WHERE engagement.id = merge_map.duplicate_id;

DROP TABLE candidate_engagement_merge_map;

ALTER TABLE candidate_engagements DROP COLUMN IF EXISTS task_id;
ALTER TABLE candidate_events DROP COLUMN IF EXISTS task_id;
ALTER TABLE user_flow_events DROP COLUMN IF EXISTS task_id;
ALTER TABLE cookie_data DROP COLUMN IF EXISTS used_by_task_id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_candidate_engagements_position_account
    ON candidate_engagements(tenant_id, candidate_id, position_id, platform_account_id)
    WHERE platform_account_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_candidate_engagements_position_without_account
    ON candidate_engagements(tenant_id, candidate_id, position_id)
    WHERE platform_account_id IS NULL;

DROP TABLE IF EXISTS task_candidates;
DROP TABLE IF EXISTS task_logs;
DROP TABLE IF EXISTS task_runs;

-- 新手流程删除创建任务节点，并把启动任务节点迁移为启动岗位。
UPDATE users
SET flow_state = (flow_state - 'steps') || jsonb_build_object(
    'version', 3,
    'stage', CASE
        WHEN flow_state->>'stage' = 'task_created' THEN 'platform_login_verified'
        WHEN flow_state->>'stage' = 'task_started' THEN 'position_started'
        ELSE flow_state->>'stage'
    END,
    'stage_name', CASE
        WHEN flow_state->>'stage' = 'task_created' THEN '登录招聘平台'
        WHEN flow_state->>'stage' = 'task_started' THEN '启动岗位'
        ELSE flow_state->>'stage_name'
    END,
    'steps', (COALESCE(flow_state->'steps', '{}'::jsonb) - 'task_created' - 'task_started') ||
        CASE WHEN COALESCE(flow_state->'steps'->'task_started', '{}'::jsonb) <> '{}'::jsonb
            THEN jsonb_build_object('position_started', flow_state->'steps'->'task_started')
            ELSE '{}'::jsonb END
)
WHERE COALESCE((flow_state->>'version')::int, 0) < 3;

UPDATE user_flow_events SET event_key = 'position_started' WHERE event_key = 'task_started';
DELETE FROM user_flow_events WHERE event_key = 'task_created';
