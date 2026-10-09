-- 文件作用：增量升级简历库，保留历史空手机号和重复简历，补人工维护、来源、幂等和推荐历史。
ALTER TABLE candidate_profiles
  ALTER COLUMN expected_salary_min TYPE NUMERIC(12,3),
  ALTER COLUMN expected_salary_max TYPE NUMERIC(12,3),
  ADD COLUMN IF NOT EXISTS library_fields JSONB NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS manual_fields JSONB NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN IF NOT EXISTS normalized_phone TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS row_version BIGINT NOT NULL DEFAULT 1;
COMMENT ON COLUMN candidate_profiles.expected_salary_min IS '期望最低月薪，单位K，可为小数';
COMMENT ON COLUMN candidate_profiles.expected_salary_max IS '期望最高月薪，单位K，可为小数';
COMMENT ON COLUMN candidate_profiles.library_fields IS '简历库补充字段：性别、当前及期望地区、标签、来源、状态与AI总结';
COMMENT ON COLUMN candidate_profiles.manual_fields IS '人工维护字段名数组，自动采集不得覆盖';
COMMENT ON COLUMN candidate_profiles.normalized_phone IS '用于团队内查重的标准手机号，历史允许空值及重复';
COMMENT ON COLUMN candidate_profiles.row_version IS '简历并发修改版本号';
UPDATE candidate_profiles SET normalized_phone = regexp_replace(phone, '[[:space:]()-]', '', 'g');
UPDATE candidate_profiles SET normalized_phone = substring(normalized_phone from 3)
 WHERE normalized_phone ~ '^861[3-9][0-9]{9}$';
UPDATE candidate_profiles SET normalized_phone = substring(normalized_phone from 4) WHERE normalized_phone ~ '^\+861[3-9][0-9]{9}$';
UPDATE candidate_profiles SET normalized_phone = substring(normalized_phone from 5) WHERE normalized_phone ~ '^00861[3-9][0-9]{9}$';
CREATE INDEX IF NOT EXISTS idx_candidate_profiles_phone ON candidate_profiles(tenant_id, normalized_phone) WHERE normalized_phone <> '';

CREATE TABLE IF NOT EXISTS candidate_profile_sources (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 candidate_id UUID NOT NULL REFERENCES candidate_profiles(id) ON DELETE CASCADE,
 platform_id TEXT NOT NULL DEFAULT '',
 platform_candidate_id TEXT NOT NULL DEFAULT '',
 source_type TEXT NOT NULL DEFAULT '',
 source_url TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE candidate_profile_sources IS '同一候选人的多个平台身份和来源映射';
COMMENT ON COLUMN candidate_profile_sources.id IS '来源记录ID';
COMMENT ON COLUMN candidate_profile_sources.tenant_id IS '所属团队ID';
COMMENT ON COLUMN candidate_profile_sources.candidate_id IS '候选人主体ID';
COMMENT ON COLUMN candidate_profile_sources.platform_id IS '来源招聘平台标识';
COMMENT ON COLUMN candidate_profile_sources.platform_candidate_id IS '平台候选人原始身份标识';
COMMENT ON COLUMN candidate_profile_sources.source_type IS '自动采集、手动、AI导入或浏览器插件来源';
COMMENT ON COLUMN candidate_profile_sources.source_url IS '用户明确保存的候选人来源页面地址';
COMMENT ON COLUMN candidate_profile_sources.created_at IS '首次记录时间';
COMMENT ON COLUMN candidate_profile_sources.updated_at IS '最近确认时间';
CREATE UNIQUE INDEX IF NOT EXISTS idx_candidate_sources_identity ON candidate_profile_sources(tenant_id,platform_id,platform_candidate_id) WHERE platform_id <> '' AND platform_candidate_id <> '';
CREATE INDEX IF NOT EXISTS idx_candidate_sources_candidate ON candidate_profile_sources(candidate_id);
INSERT INTO candidate_profile_sources(tenant_id,candidate_id,platform_id,platform_candidate_id,source_type)
 SELECT tenant_id,id,source_platform_id,source_platform_candidate_id,'automatic' FROM candidate_profiles
 WHERE source_platform_id <> '' AND source_platform_candidate_id <> '' ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS candidate_phone_identities (
 tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 normalized_phone TEXT NOT NULL,
 candidate_id UUID NOT NULL REFERENCES candidate_profiles(id) ON DELETE CASCADE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,normalized_phone)
);
COMMENT ON TABLE candidate_phone_identities IS '新入口经确认的团队手机号身份登记，不强制合并历史重复';
COMMENT ON COLUMN candidate_phone_identities.tenant_id IS '所属团队ID';
COMMENT ON COLUMN candidate_phone_identities.normalized_phone IS '用户确认的有效标准手机号';
COMMENT ON COLUMN candidate_phone_identities.candidate_id IS '明确绑定的候选人ID';
COMMENT ON COLUMN candidate_phone_identities.created_at IS '首次确认时间';

CREATE TABLE IF NOT EXISTS candidate_library_requests (
 tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 actor_email TEXT NOT NULL,
 operation TEXT NOT NULL,
 request_key TEXT NOT NULL,
 fingerprint TEXT NOT NULL,
 result_id TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,actor_email,operation,request_key)
);
COMMENT ON TABLE candidate_library_requests IS '简历人工操作的幂等请求凭据';
COMMENT ON COLUMN candidate_library_requests.tenant_id IS '所属团队ID';
COMMENT ON COLUMN candidate_library_requests.actor_email IS '操作人邮箱';
COMMENT ON COLUMN candidate_library_requests.operation IS '操作类型';
COMMENT ON COLUMN candidate_library_requests.request_key IS '客户端请求幂等键';
COMMENT ON COLUMN candidate_library_requests.fingerprint IS '请求内容摘要，防止键被用于不同请求';
COMMENT ON COLUMN candidate_library_requests.result_id IS '已提交结果记录ID';
COMMENT ON COLUMN candidate_library_requests.created_at IS '请求完成时间';

CREATE TABLE IF NOT EXISTS candidate_recommendations (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 candidate_id UUID NOT NULL REFERENCES candidate_profiles(id) ON DELETE CASCADE,
 position_id UUID REFERENCES positions(id) ON DELETE SET NULL,
 position_key TEXT NOT NULL,
 position_name TEXT NOT NULL DEFAULT '',
 stage TEXT NOT NULL DEFAULT 'draft',
 author_email TEXT NOT NULL DEFAULT '',
 author_name TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,candidate_id,position_key)
);
COMMENT ON TABLE candidate_recommendations IS '候选人与岗位的人工推荐关系，独立于自动任务触达和运行统计';
COMMENT ON COLUMN candidate_recommendations.id IS '推荐关系ID';
COMMENT ON COLUMN candidate_recommendations.tenant_id IS '所属团队ID';
COMMENT ON COLUMN candidate_recommendations.candidate_id IS '候选人ID';
COMMENT ON COLUMN candidate_recommendations.position_id IS '关联岗位ID，删除岗位后为空';
COMMENT ON COLUMN candidate_recommendations.position_key IS '原岗位ID快照，用于关系去重及保留删除历史';
COMMENT ON COLUMN candidate_recommendations.position_name IS '岗位名称快照';
COMMENT ON COLUMN candidate_recommendations.stage IS '当前人工推进阶段';
COMMENT ON COLUMN candidate_recommendations.author_email IS '首次关联人邮箱';
COMMENT ON COLUMN candidate_recommendations.author_name IS '首次关联人姓名快照';
COMMENT ON COLUMN candidate_recommendations.created_at IS '首次关联时间';
COMMENT ON COLUMN candidate_recommendations.updated_at IS '最近推进时间';
CREATE TABLE IF NOT EXISTS candidate_recommendation_events (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 recommendation_id UUID NOT NULL REFERENCES candidate_recommendations(id) ON DELETE CASCADE,
 from_stage TEXT NOT NULL DEFAULT '',
 to_stage TEXT NOT NULL,
 content TEXT NOT NULL DEFAULT '',
 author_email TEXT NOT NULL,
 author_name TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE candidate_recommendation_events IS '人工推荐和跟进的只追加阶段历史';
COMMENT ON COLUMN candidate_recommendation_events.id IS '历史事件ID';
COMMENT ON COLUMN candidate_recommendation_events.recommendation_id IS '所属推荐关系ID';
COMMENT ON COLUMN candidate_recommendation_events.from_stage IS '操作前阶段';
COMMENT ON COLUMN candidate_recommendation_events.to_stage IS '操作后阶段';
COMMENT ON COLUMN candidate_recommendation_events.content IS '本次推荐或推进备注';
COMMENT ON COLUMN candidate_recommendation_events.author_email IS '操作人邮箱';
COMMENT ON COLUMN candidate_recommendation_events.author_name IS '操作人姓名快照';
COMMENT ON COLUMN candidate_recommendation_events.created_at IS '发生时间';
CREATE INDEX IF NOT EXISTS idx_recommendation_events_time ON candidate_recommendation_events(recommendation_id,created_at DESC);
ALTER TABLE candidate_events ADD COLUMN IF NOT EXISTS idempotency_key TEXT NOT NULL DEFAULT '';
COMMENT ON COLUMN candidate_events.idempotency_key IS '人工备注请求幂等键，自动事件可为空';
CREATE UNIQUE INDEX IF NOT EXISTS idx_candidate_events_request ON candidate_events(tenant_id,candidate_id,event_type,idempotency_key) WHERE idempotency_key <> '';
