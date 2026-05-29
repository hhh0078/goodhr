-- 重构候选人简历库：删除旧任务候选人表，新增候选人主体、触达上下文和事件流水三张表。
-- 说明：候选人主体只保存简历字段；岗位、账号、任务和 AI 分析结果均放入触达/事件表。

DROP TABLE IF EXISTS task_candidates;

CREATE TABLE IF NOT EXISTS candidate_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    source_platform_id TEXT NOT NULL DEFAULT '',
    source_platform_candidate_id TEXT NOT NULL DEFAULT '',
    candidate_name TEXT NOT NULL DEFAULT '',
    birth_ym TEXT NOT NULL DEFAULT '',
    phone TEXT NOT NULL DEFAULT '',
    email TEXT NOT NULL DEFAULT '',
    work_region TEXT NOT NULL DEFAULT '',
    work_years TEXT NOT NULL DEFAULT '',
    expected_salary_min INTEGER,
    expected_salary_max INTEGER,
    personal_description TEXT NOT NULL DEFAULT '',
    work_status TEXT NOT NULL DEFAULT '',
    expected_position TEXT NOT NULL DEFAULT '',
    online_status TEXT NOT NULL DEFAULT '',
    education_level TEXT NOT NULL DEFAULT '',
    basic_info TEXT NOT NULL DEFAULT '',
    raw_text TEXT NOT NULL DEFAULT '',
    filter_text TEXT NOT NULL DEFAULT '',
    work_experiences JSONB NOT NULL DEFAULT '[]'::jsonb,
    educations JSONB NOT NULL DEFAULT '[]'::jsonb,
    certificates JSONB NOT NULL DEFAULT '[]'::jsonb,
    honors JSONB NOT NULL DEFAULT '[]'::jsonb,
    project_experiences JSONB NOT NULL DEFAULT '[]'::jsonb,
    colleague_communications JSONB NOT NULL DEFAULT '[]'::jsonb,
    resume_attachment_url TEXT NOT NULL DEFAULT '',
    resume_attachment_extracted_text TEXT NOT NULL DEFAULT '',
    ext JSONB NOT NULL DEFAULT '{}'::jsonb,
    first_seen_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE candidate_profiles IS '候选人简历主体表，只保存候选人自身信息，不保存岗位账号和AI分析结果';
COMMENT ON COLUMN candidate_profiles.tenant_id IS '所属团队ID';
COMMENT ON COLUMN candidate_profiles.created_by_user_id IS '首次创建该候选人的用户ID';
COMMENT ON COLUMN candidate_profiles.source_platform_id IS '候选人首次来源平台标识';
COMMENT ON COLUMN candidate_profiles.source_platform_candidate_id IS '来源平台侧候选人原始ID';
COMMENT ON COLUMN candidate_profiles.candidate_name IS '候选人姓名';
COMMENT ON COLUMN candidate_profiles.birth_ym IS '出生年月，格式建议YYYY-MM';
COMMENT ON COLUMN candidate_profiles.phone IS '手机号原文';
COMMENT ON COLUMN candidate_profiles.email IS '邮箱';
COMMENT ON COLUMN candidate_profiles.work_region IS '工作地区';
COMMENT ON COLUMN candidate_profiles.work_years IS '工作年限文本';
COMMENT ON COLUMN candidate_profiles.expected_salary_min IS '期望薪资最低值，单位建议元/月';
COMMENT ON COLUMN candidate_profiles.expected_salary_max IS '期望薪资最高值，单位建议元/月';
COMMENT ON COLUMN candidate_profiles.personal_description IS '个人描述';
COMMENT ON COLUMN candidate_profiles.work_status IS '工作状态';
COMMENT ON COLUMN candidate_profiles.expected_position IS '期望职位';
COMMENT ON COLUMN candidate_profiles.online_status IS '在线状态文本';
COMMENT ON COLUMN candidate_profiles.education_level IS '学历主字段';
COMMENT ON COLUMN candidate_profiles.basic_info IS '候选人基础信息摘要';
COMMENT ON COLUMN candidate_profiles.raw_text IS '平台抓取原始拼接文本';
COMMENT ON COLUMN candidate_profiles.filter_text IS '用于筛选流程的文本';
COMMENT ON COLUMN candidate_profiles.work_experiences IS '工作经历数组JSON';
COMMENT ON COLUMN candidate_profiles.educations IS '教育经历数组JSON';
COMMENT ON COLUMN candidate_profiles.certificates IS '资格证书数组JSON';
COMMENT ON COLUMN candidate_profiles.honors IS '所得荣誉数组JSON';
COMMENT ON COLUMN candidate_profiles.project_experiences IS '项目经验数组JSON';
COMMENT ON COLUMN candidate_profiles.colleague_communications IS '同事沟通记录数组JSON';
COMMENT ON COLUMN candidate_profiles.resume_attachment_url IS '简历附件文件URL';
COMMENT ON COLUMN candidate_profiles.resume_attachment_extracted_text IS '简历附件提取文本';
COMMENT ON COLUMN candidate_profiles.ext IS '候选人扩展字段';
COMMENT ON COLUMN candidate_profiles.first_seen_at IS '候选人首次发现时间';
COMMENT ON COLUMN candidate_profiles.created_at IS '记录创建时间';
COMMENT ON COLUMN candidate_profiles.updated_at IS '记录更新时间';

CREATE INDEX IF NOT EXISTS idx_candidate_profiles_tenant_created_at
    ON candidate_profiles(tenant_id, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_candidate_profiles_source
    ON candidate_profiles(tenant_id, source_platform_id, source_platform_candidate_id);
CREATE INDEX IF NOT EXISTS idx_candidate_profiles_name
    ON candidate_profiles(tenant_id, candidate_name);

CREATE TABLE IF NOT EXISTS candidate_engagements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    candidate_id UUID NOT NULL REFERENCES candidate_profiles(id) ON DELETE CASCADE,
    task_id UUID REFERENCES task_runs(id) ON DELETE SET NULL,
    position_id UUID REFERENCES positions(id) ON DELETE SET NULL,
    platform_account_id UUID REFERENCES cookie_data(id) ON DELETE SET NULL,
    platform_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'created',
    first_seen_at TIMESTAMPTZ,
    detail_fetched_at TIMESTAMPTZ,
    greeted_at TIMESTAMPTZ,
    last_event_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, candidate_id, task_id, position_id, platform_account_id)
);

COMMENT ON TABLE candidate_engagements IS '候选人触达上下文表，表示某个任务岗位账号下对候选人的一次沟通过程';
COMMENT ON COLUMN candidate_engagements.tenant_id IS '所属团队ID';
COMMENT ON COLUMN candidate_engagements.candidate_id IS '候选人主体ID';
COMMENT ON COLUMN candidate_engagements.task_id IS '关联任务ID';
COMMENT ON COLUMN candidate_engagements.position_id IS '关联岗位模板ID';
COMMENT ON COLUMN candidate_engagements.platform_account_id IS '关联平台账号Cookie ID';
COMMENT ON COLUMN candidate_engagements.platform_id IS '本次触达使用的平台标识';
COMMENT ON COLUMN candidate_engagements.status IS '触达状态，如created/analyzed/greeted/skipped/failed';
COMMENT ON COLUMN candidate_engagements.first_seen_at IS '本次触达首次发现时间';
COMMENT ON COLUMN candidate_engagements.detail_fetched_at IS '本次触达详情抓取时间';
COMMENT ON COLUMN candidate_engagements.greeted_at IS '本次触达打招呼成功时间';
COMMENT ON COLUMN candidate_engagements.last_event_at IS '最近事件时间';
COMMENT ON COLUMN candidate_engagements.created_at IS '记录创建时间';
COMMENT ON COLUMN candidate_engagements.updated_at IS '记录更新时间';

CREATE INDEX IF NOT EXISTS idx_candidate_engagements_candidate_created_at
    ON candidate_engagements(candidate_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_candidate_engagements_tenant_task
    ON candidate_engagements(tenant_id, task_id);
CREATE INDEX IF NOT EXISTS idx_candidate_engagements_tenant_position
    ON candidate_engagements(tenant_id, position_id);

CREATE TABLE IF NOT EXISTS candidate_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    candidate_id UUID NOT NULL REFERENCES candidate_profiles(id) ON DELETE CASCADE,
    engagement_id UUID REFERENCES candidate_engagements(id) ON DELETE CASCADE,
    task_id UUID REFERENCES task_runs(id) ON DELETE SET NULL,
    position_id UUID REFERENCES positions(id) ON DELETE SET NULL,
    platform_account_id UUID REFERENCES cookie_data(id) ON DELETE SET NULL,
    platform_id TEXT NOT NULL DEFAULT '',
    event_type TEXT NOT NULL,
    score NUMERIC(6, 2),
    reason TEXT NOT NULL DEFAULT '',
    input_text TEXT NOT NULL DEFAULT '',
    output_text TEXT NOT NULL DEFAULT '',
    message_text TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    token_usage INTEGER NOT NULL DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE candidate_events IS '候选人事件流水表，保存AI分析、打招呼、聊天、邀约等历史记录';
COMMENT ON COLUMN candidate_events.tenant_id IS '所属团队ID';
COMMENT ON COLUMN candidate_events.candidate_id IS '候选人主体ID';
COMMENT ON COLUMN candidate_events.engagement_id IS '关联触达上下文ID';
COMMENT ON COLUMN candidate_events.task_id IS '关联任务ID';
COMMENT ON COLUMN candidate_events.position_id IS '关联岗位模板ID';
COMMENT ON COLUMN candidate_events.platform_account_id IS '关联平台账号Cookie ID';
COMMENT ON COLUMN candidate_events.platform_id IS '事件发生的平台标识';
COMMENT ON COLUMN candidate_events.event_type IS '事件类型，如detail_analysis/greet_analysis/review_analysis/greet_success';
COMMENT ON COLUMN candidate_events.score IS 'AI分析分数';
COMMENT ON COLUMN candidate_events.reason IS 'AI分析原因或事件原因';
COMMENT ON COLUMN candidate_events.input_text IS 'AI输入文本或事件输入';
COMMENT ON COLUMN candidate_events.output_text IS 'AI输出原文';
COMMENT ON COLUMN candidate_events.message_text IS '沟通消息文本';
COMMENT ON COLUMN candidate_events.model IS 'AI模型名称';
COMMENT ON COLUMN candidate_events.token_usage IS 'AI调用Token消耗';
COMMENT ON COLUMN candidate_events.metadata IS '事件扩展字段';
COMMENT ON COLUMN candidate_events.created_at IS '事件创建时间';

CREATE INDEX IF NOT EXISTS idx_candidate_events_engagement_created_at
    ON candidate_events(engagement_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_candidate_events_candidate_created_at
    ON candidate_events(candidate_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_candidate_events_type_created_at
    ON candidate_events(event_type, created_at DESC);
