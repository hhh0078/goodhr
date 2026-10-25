-- 本迁移独立保存岗位候选人每次筛选的两轮真实判定、版本摘要及查询索引，不依赖简历入库。
CREATE TABLE IF NOT EXISTS position_screening_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    position_id UUID NOT NULL REFERENCES positions(id) ON DELETE CASCADE,
    owner_user_id UUID NOT NULL REFERENCES users(id),
    attempt_id UUID NOT NULL,
    revision BIGINT NOT NULL CHECK (revision > 0),
    occurred_at TIMESTAMPTZ NOT NULL,
    snapshot JSONB NOT NULL,
    revision_hashes JSONB NOT NULL DEFAULT '{}'::jsonb,
    first_score DOUBLE PRECISION NULL CHECK (first_score >= 0 AND first_score <= 100),
    second_score DOUBLE PRECISION NULL CHECK (second_score >= 0 AND second_score <= 100),
    first_started_at TIMESTAMPTZ NULL,
    second_started_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, position_id, attempt_id)
);
COMMENT ON TABLE position_screening_records IS '本人岗位每次候选人筛选的两轮完整快照，独立于简历入库与可清理日志';
COMMENT ON COLUMN position_screening_records.id IS '云端筛选记录主键';
COMMENT ON COLUMN position_screening_records.tenant_id IS '可信会话所属租户，所有查询必须显式限定';
COMMENT ON COLUMN position_screening_records.position_id IS '本次筛选所属平台岗位，删除岗位时清理其记录';
COMMENT ON COLUMN position_screening_records.owner_user_id IS '首次上报时的岗位所有者，读取时与当前岗位和租户重新核对';
COMMENT ON COLUMN position_screening_records.attempt_id IS '本地每次筛选生成的UUID，后续轮次与断网重传保持不变';
COMMENT ON COLUMN position_screening_records.revision IS '当前已保存的完整快照递增版本，旧版本不得覆盖';
COMMENT ON COLUMN position_screening_records.occurred_at IS '本次筛选实际发生时间，UTC存储且禁止跨版本换绑';
COMMENT ON COLUMN position_screening_records.snapshot IS '当前完整筛选快照，两轮空值与真实零分及零耗时分别保留';
COMMENT ON COLUMN position_screening_records.revision_hashes IS '已接收各版本规范JSON摘要，用于识别同版本不同内容冲突';
COMMENT ON COLUMN position_screening_records.first_score IS '第一轮真实AI分数，关键词或尚未获得分数时为空，零分有效';
COMMENT ON COLUMN position_screening_records.second_score IS '第二轮真实AI分数，关键词或尚未获得分数时为空，零分有效';
COMMENT ON COLUMN position_screening_records.first_started_at IS '第一轮实际判定开始时间，未执行为空';
COMMENT ON COLUMN position_screening_records.second_started_at IS '第二轮实际判定开始时间，未执行为空';
COMMENT ON COLUMN position_screening_records.created_at IS '云端首次收到本次筛选快照的时间';
COMMENT ON COLUMN position_screening_records.updated_at IS '云端收到更新版本快照的时间，旧版本重传不会改变';
CREATE INDEX IF NOT EXISTS idx_position_screening_records_latest ON position_screening_records (tenant_id, position_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_position_screening_records_first_score ON position_screening_records (tenant_id, position_id, first_score) WHERE first_score IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_position_screening_records_second_score ON position_screening_records (tenant_id, position_id, second_score) WHERE second_score IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_position_screening_records_first_started ON position_screening_records (tenant_id, position_id, first_started_at) WHERE first_started_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_position_screening_records_second_started ON position_screening_records (tenant_id, position_id, second_started_at) WHERE second_started_at IS NOT NULL;
