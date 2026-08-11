-- 本迁移把自动回复确认项收口为候选人与岗位的长期评估，并新增可靠的推荐报告任务和公开分享数据。

CREATE TABLE IF NOT EXISTS candidate_position_reviews (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    candidate_id UUID NOT NULL REFERENCES candidate_profiles(id) ON DELETE CASCADE,
    position_id UUID NOT NULL REFERENCES positions(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'collecting',
    conditions_initialized_at TIMESTAMPTZ,
    qualified_at TIMESTAMPTZ,
    current_recommendation_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, candidate_id, position_id),
    CHECK (status IN ('collecting', 'pending', 'qualified', 'unmatched', 'generating', 'recommended', 'failed'))
);

COMMENT ON TABLE candidate_position_reviews IS '候选人与岗位之间唯一的长期评估主体，汇总多个平台会话的条件判断和推荐状态';
COMMENT ON COLUMN candidate_position_reviews.id IS '候选人岗位评估唯一标识';
COMMENT ON COLUMN candidate_position_reviews.tenant_id IS '评估所属团队标识';
COMMENT ON COLUMN candidate_position_reviews.candidate_id IS '评估关联的正式候选人标识';
COMMENT ON COLUMN candidate_position_reviews.position_id IS '评估关联的岗位标识';
COMMENT ON COLUMN candidate_position_reviews.status IS '评估状态：收集中、待确认、符合、未满足、生成中、已推荐或失败';
COMMENT ON COLUMN candidate_position_reviews.conditions_initialized_at IS '岗位条件首次同步到候选人评估的时间';
COMMENT ON COLUMN candidate_position_reviews.qualified_at IS '关键条件最近一次全部满足的时间';
COMMENT ON COLUMN candidate_position_reviews.current_recommendation_id IS '当前最新推荐记录标识，外键在推荐表创建后补充';
COMMENT ON COLUMN candidate_position_reviews.created_at IS '评估创建时间';
COMMENT ON COLUMN candidate_position_reviews.updated_at IS '评估最近更新时间';

ALTER TABLE candidate_confirmation_items
    ADD COLUMN IF NOT EXISTS review_id UUID REFERENCES candidate_position_reviews(id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS position_condition_id UUID REFERENCES position_reply_conditions(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS status_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS ask_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_asked_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_answered_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_reviewed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ;

COMMENT ON COLUMN candidate_confirmation_items.review_id IS '确认项所属候选人岗位长期评估标识';
COMMENT ON COLUMN candidate_confirmation_items.position_condition_id IS '确认项对应的岗位原始条件标识，AI补充条件时为空';
COMMENT ON COLUMN candidate_confirmation_items.status_reason IS '当前未确认、已满足或未满足状态的简短理由';
COMMENT ON COLUMN candidate_confirmation_items.ask_count IS '该条件已经主动询问候选人的次数';
COMMENT ON COLUMN candidate_confirmation_items.last_asked_at IS '最近一次向候选人询问该条件的时间';
COMMENT ON COLUMN candidate_confirmation_items.last_answered_at IS '最近一次从候选人回答更新该条件的时间';
COMMENT ON COLUMN candidate_confirmation_items.last_reviewed_at IS 'AI最近一次检查该条件的时间';
COMMENT ON COLUMN candidate_confirmation_items.archived_at IS '条件不再适用或被岗位配置替换时的归档时间';

INSERT INTO candidate_position_reviews (tenant_id, candidate_id, position_id, status, conditions_initialized_at)
SELECT DISTINCT tenant_id, candidate_id, position_id, 'collecting', now()
FROM candidate_confirmation_items
WHERE candidate_id IS NOT NULL AND position_id IS NOT NULL
ON CONFLICT (tenant_id, candidate_id, position_id) DO NOTHING;

UPDATE candidate_confirmation_items item
SET review_id = review.id
FROM candidate_position_reviews review
WHERE item.review_id IS NULL
  AND item.tenant_id = review.tenant_id
  AND item.candidate_id = review.candidate_id
  AND item.position_id = review.position_id;

ALTER TABLE candidate_confirmation_items DROP CONSTRAINT IF EXISTS candidate_confirmation_items_status_check;
ALTER TABLE candidate_confirmation_events DROP CONSTRAINT IF EXISTS candidate_confirmation_events_old_status_check;
ALTER TABLE candidate_confirmation_events DROP CONSTRAINT IF EXISTS candidate_confirmation_events_new_status_check;

UPDATE candidate_confirmation_items
SET status_reason = COALESCE(NULLIF(status_reason, ''), NULLIF(summary, ''), evidence_text),
    archived_at = CASE WHEN status = 'not_applicable' THEN COALESCE(archived_at, now()) ELSE archived_at END,
    status = CASE WHEN status IN ('not_applicable', 'conflicted') THEN 'pending' ELSE status END;

UPDATE candidate_confirmation_events
SET old_status = CASE WHEN old_status IN ('not_applicable', 'conflicted') THEN 'pending' ELSE old_status END,
    new_status = CASE WHEN new_status IN ('not_applicable', 'conflicted') THEN 'pending' ELSE new_status END;

WITH duplicates AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY review_id, dedupe_key
               ORDER BY updated_at DESC, created_at DESC, id DESC
           ) AS row_number
    FROM candidate_confirmation_items
    WHERE review_id IS NOT NULL AND archived_at IS NULL
)
UPDATE candidate_confirmation_items item
SET archived_at = now()
FROM duplicates duplicate
WHERE item.id = duplicate.id AND duplicate.row_number > 1;

ALTER TABLE candidate_confirmation_items
    ADD CONSTRAINT candidate_confirmation_items_status_check
    CHECK (status IN ('pending', 'matched', 'unmatched'));
ALTER TABLE candidate_confirmation_events
    ADD CONSTRAINT candidate_confirmation_events_old_status_check
    CHECK (old_status IN ('', 'pending', 'matched', 'unmatched'));
ALTER TABLE candidate_confirmation_events
    ADD CONSTRAINT candidate_confirmation_events_new_status_check
    CHECK (new_status IN ('pending', 'matched', 'unmatched'));

DROP INDEX IF EXISTS idx_candidate_confirmation_items_dedupe;
CREATE UNIQUE INDEX IF NOT EXISTS idx_candidate_confirmation_items_review_dedupe
    ON candidate_confirmation_items (review_id, dedupe_key)
    WHERE review_id IS NOT NULL AND archived_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_candidate_confirmation_items_conversation_dedupe
    ON candidate_confirmation_items (conversation_id, dedupe_key)
    WHERE review_id IS NULL AND archived_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_candidate_confirmation_items_review_status
    ON candidate_confirmation_items (review_id, status, updated_at DESC)
    WHERE archived_at IS NULL;

CREATE TABLE IF NOT EXISTS candidate_recommendations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    public_id TEXT NOT NULL UNIQUE,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    review_id UUID NOT NULL REFERENCES candidate_position_reviews(id) ON DELETE CASCADE,
    candidate_id UUID NOT NULL REFERENCES candidate_profiles(id) ON DELETE CASCADE,
    position_id UUID NOT NULL REFERENCES positions(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    input_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'completed',
    match_score NUMERIC(6, 2),
    recommendation_level TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    report_data JSONB NOT NULL DEFAULT '{}'::jsonb,
    model TEXT NOT NULL DEFAULT '',
    token_usage INTEGER NOT NULL DEFAULT 0,
    share_enabled BOOLEAN NOT NULL DEFAULT true,
    notification_status TEXT NOT NULL DEFAULT 'pending',
    notification_error TEXT NOT NULL DEFAULT '',
    notified_at TIMESTAMPTZ,
    source_cutoff_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    generated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (review_id, version),
    UNIQUE (review_id, input_hash),
    CHECK (status IN ('completed', 'revoked', 'superseded')),
    CHECK (notification_status IN ('pending', 'sending', 'sent', 'failed'))
);

COMMENT ON TABLE candidate_recommendations IS '候选人通过岗位关键条件后生成的不可变推荐报告快照';
COMMENT ON COLUMN candidate_recommendations.id IS '推荐记录内部唯一标识';
COMMENT ON COLUMN candidate_recommendations.public_id IS '公开页面使用的不可猜测分享编号';
COMMENT ON COLUMN candidate_recommendations.tenant_id IS '推荐记录所属团队标识';
COMMENT ON COLUMN candidate_recommendations.review_id IS '推荐记录所属候选人岗位评估标识';
COMMENT ON COLUMN candidate_recommendations.candidate_id IS '推荐记录中的候选人标识';
COMMENT ON COLUMN candidate_recommendations.position_id IS '推荐记录中的岗位标识';
COMMENT ON COLUMN candidate_recommendations.version IS '同一候选人岗位评估下的推荐版本号';
COMMENT ON COLUMN candidate_recommendations.input_hash IS '生成输入快照哈希，用于避免重复报告';
COMMENT ON COLUMN candidate_recommendations.status IS '推荐状态：已完成、已撤销或已被新版替代';
COMMENT ON COLUMN candidate_recommendations.match_score IS 'AI生成的岗位匹配分数';
COMMENT ON COLUMN candidate_recommendations.recommendation_level IS 'AI生成的推荐等级';
COMMENT ON COLUMN candidate_recommendations.summary IS '面试官可快速阅读的推荐摘要';
COMMENT ON COLUMN candidate_recommendations.report_data IS '完整候选人、岗位、条件、优势、风险和面试建议快照';
COMMENT ON COLUMN candidate_recommendations.model IS '生成推荐记录使用的AI模型';
COMMENT ON COLUMN candidate_recommendations.token_usage IS '生成推荐记录消耗的Token数量';
COMMENT ON COLUMN candidate_recommendations.share_enabled IS '公开链接是否仍然有效';
COMMENT ON COLUMN candidate_recommendations.notification_status IS '岗位创建人推荐邮件发送状态：待发送、发送中、已发送或失败';
COMMENT ON COLUMN candidate_recommendations.notification_error IS '推荐邮件最近一次失败原因';
COMMENT ON COLUMN candidate_recommendations.notified_at IS '推荐邮件成功发送时间';
COMMENT ON COLUMN candidate_recommendations.source_cutoff_at IS '报告采用的业务数据截止时间';
COMMENT ON COLUMN candidate_recommendations.generated_at IS '推荐报告生成完成时间';
COMMENT ON COLUMN candidate_recommendations.created_at IS '推荐记录创建时间';
COMMENT ON COLUMN candidate_recommendations.updated_at IS '推荐记录最近更新时间';

ALTER TABLE candidate_position_reviews
    DROP CONSTRAINT IF EXISTS candidate_position_reviews_current_recommendation_fk;

ALTER TABLE candidate_position_reviews
    ADD CONSTRAINT candidate_position_reviews_current_recommendation_fk
    FOREIGN KEY (current_recommendation_id) REFERENCES candidate_recommendations(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_candidate_recommendations_candidate
    ON candidate_recommendations (tenant_id, candidate_id, generated_at DESC);
CREATE INDEX IF NOT EXISTS idx_candidate_recommendations_public
    ON candidate_recommendations (public_id)
    WHERE share_enabled = true;

CREATE TABLE IF NOT EXISTS candidate_recommendation_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    review_id UUID NOT NULL REFERENCES candidate_position_reviews(id) ON DELETE CASCADE,
    candidate_id UUID NOT NULL REFERENCES candidate_profiles(id) ON DELETE CASCADE,
    position_id UUID NOT NULL REFERENCES positions(id) ON DELETE CASCADE,
    input_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (review_id, input_hash),
    CHECK (status IN ('pending', 'running', 'completed', 'failed'))
);

COMMENT ON TABLE candidate_recommendation_jobs IS '候选人推荐报告的可重试数据库任务，避免后端重启时丢失生成工作';
COMMENT ON COLUMN candidate_recommendation_jobs.id IS '推荐生成任务唯一标识';
COMMENT ON COLUMN candidate_recommendation_jobs.tenant_id IS '任务所属团队标识';
COMMENT ON COLUMN candidate_recommendation_jobs.review_id IS '任务所属候选人岗位评估标识';
COMMENT ON COLUMN candidate_recommendation_jobs.candidate_id IS '任务关联候选人标识';
COMMENT ON COLUMN candidate_recommendation_jobs.position_id IS '任务关联岗位标识';
COMMENT ON COLUMN candidate_recommendation_jobs.input_hash IS '任务输入快照哈希';
COMMENT ON COLUMN candidate_recommendation_jobs.status IS '任务状态：等待、执行中、已完成或失败';
COMMENT ON COLUMN candidate_recommendation_jobs.attempt_count IS '任务已经执行的次数';
COMMENT ON COLUMN candidate_recommendation_jobs.next_attempt_at IS '失败后允许再次执行的时间';
COMMENT ON COLUMN candidate_recommendation_jobs.locked_at IS '任务最近一次被后端领取的时间';
COMMENT ON COLUMN candidate_recommendation_jobs.last_error IS '任务最近一次失败原因';
COMMENT ON COLUMN candidate_recommendation_jobs.created_at IS '任务创建时间';
COMMENT ON COLUMN candidate_recommendation_jobs.updated_at IS '任务最近更新时间';

CREATE INDEX IF NOT EXISTS idx_candidate_recommendation_jobs_pending
    ON candidate_recommendation_jobs (status, next_attempt_at, created_at);
