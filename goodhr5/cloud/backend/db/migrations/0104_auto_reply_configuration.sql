-- 本迁移仅新增自动回复公司和岗位配置，聊天与原始附件不进入云端。

CREATE TABLE IF NOT EXISTS tenant_company_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    address TEXT NOT NULL DEFAULT '',
    contact TEXT NOT NULL DEFAULT '',
    overview TEXT NOT NULL DEFAULT '',
    extra_info TEXT NOT NULL DEFAULT '',
    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    updated_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE tenant_company_profiles IS '团队共享的公司档案，岗位自动回复时选择其中一份';
COMMENT ON COLUMN tenant_company_profiles.id IS '公司档案唯一标识';
COMMENT ON COLUMN tenant_company_profiles.tenant_id IS '公司档案所属团队标识';
COMMENT ON COLUMN tenant_company_profiles.name IS '公司档案名称';
COMMENT ON COLUMN tenant_company_profiles.address IS '公司地址等位置说明';
COMMENT ON COLUMN tenant_company_profiles.contact IS '可向候选人公开的联系方式';
COMMENT ON COLUMN tenant_company_profiles.overview IS '公司概况、业务和团队介绍';
COMMENT ON COLUMN tenant_company_profiles.extra_info IS '其他可用于自动回复的公司信息';
COMMENT ON COLUMN tenant_company_profiles.created_by_user_id IS '创建公司档案的用户标识';
COMMENT ON COLUMN tenant_company_profiles.updated_by_user_id IS '最近编辑公司档案的用户标识';
COMMENT ON COLUMN tenant_company_profiles.created_at IS '公司档案创建时间';
COMMENT ON COLUMN tenant_company_profiles.updated_at IS '公司档案最近更新时间';

CREATE UNIQUE INDEX IF NOT EXISTS idx_tenant_company_profiles_unique_name
    ON tenant_company_profiles (tenant_id, LOWER(BTRIM(name)));
CREATE INDEX IF NOT EXISTS idx_tenant_company_profiles_updated
    ON tenant_company_profiles (tenant_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS position_auto_reply_configs (
    position_id UUID PRIMARY KEY REFERENCES positions(id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    company_profile_id UUID REFERENCES tenant_company_profiles(id) ON DELETE RESTRICT,
    enabled BOOLEAN NOT NULL DEFAULT false,
    position_description TEXT NOT NULL DEFAULT '',
    resume_request_message TEXT NOT NULL DEFAULT '你好，能发一份简历吗？',
    poll_interval_seconds INTEGER NOT NULL DEFAULT 5,
    max_threads_per_checkpoint INTEGER NOT NULL DEFAULT 3,
    version INTEGER NOT NULL DEFAULT 1,
    updated_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (poll_interval_seconds BETWEEN 1 AND 300),
    CHECK (max_threads_per_checkpoint BETWEEN 1 AND 20),
    CHECK (version > 0)
);

COMMENT ON TABLE position_auto_reply_configs IS '岗位自动回复开关和独立配置';
COMMENT ON COLUMN position_auto_reply_configs.position_id IS '关联岗位标识，一岗一份自动回复配置';
COMMENT ON COLUMN position_auto_reply_configs.tenant_id IS '岗位当前所属团队标识';
COMMENT ON COLUMN position_auto_reply_configs.company_profile_id IS '岗位选择的团队公司档案标识';
COMMENT ON COLUMN position_auto_reply_configs.enabled IS '岗位是否开启自动回复';
COMMENT ON COLUMN position_auto_reply_configs.position_description IS '供自动回复使用的岗位说明';
COMMENT ON COLUMN position_auto_reply_configs.resume_request_message IS '没有简历时发送给候选人的默认索要话术';
COMMENT ON COLUMN position_auto_reply_configs.poll_interval_seconds IS '独立自动回复无未读时的轮询间隔秒数';
COMMENT ON COLUMN position_auto_reply_configs.max_threads_per_checkpoint IS '与打招呼穿插时单次最多处理的未读会话数';
COMMENT ON COLUMN position_auto_reply_configs.version IS '自动回复配置版本号';
COMMENT ON COLUMN position_auto_reply_configs.updated_by_user_id IS '最近编辑配置的用户标识';
COMMENT ON COLUMN position_auto_reply_configs.created_at IS '配置创建时间';
COMMENT ON COLUMN position_auto_reply_configs.updated_at IS '配置最近更新时间';

CREATE INDEX IF NOT EXISTS idx_position_auto_reply_enabled
    ON position_auto_reply_configs (tenant_id, enabled, updated_at DESC);

CREATE TABLE IF NOT EXISTS position_reply_conditions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    position_id UUID NOT NULL REFERENCES positions(id) ON DELETE CASCADE,
    condition_type TEXT NOT NULL,
    content TEXT NOT NULL,
    dedupe_key TEXT NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    updated_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (condition_type IN ('required', 'confirm', 'bonus')),
    CHECK (sort_order >= 0)
);

COMMENT ON TABLE position_reply_conditions IS '岗位自动回复条件，支持必须满足、需要确认和加分项';
COMMENT ON COLUMN position_reply_conditions.id IS '岗位条件唯一标识';
COMMENT ON COLUMN position_reply_conditions.tenant_id IS '岗位条件所属团队标识';
COMMENT ON COLUMN position_reply_conditions.position_id IS '岗位条件关联的岗位标识';
COMMENT ON COLUMN position_reply_conditions.condition_type IS '条件类型：required必须满足，confirm需要确认，bonus加分项';
COMMENT ON COLUMN position_reply_conditions.content IS '岗位条件正文';
COMMENT ON COLUMN position_reply_conditions.dedupe_key IS '标准化去重键，由云端根据条件正文生成';
COMMENT ON COLUMN position_reply_conditions.sort_order IS '条件在编辑和询问时的排序序号';
COMMENT ON COLUMN position_reply_conditions.enabled IS '条件是否启用';
COMMENT ON COLUMN position_reply_conditions.created_by_user_id IS '创建条件的用户标识';
COMMENT ON COLUMN position_reply_conditions.updated_by_user_id IS '最近编辑条件的用户标识';
COMMENT ON COLUMN position_reply_conditions.created_at IS '条件创建时间';
COMMENT ON COLUMN position_reply_conditions.updated_at IS '条件最近更新时间';

CREATE UNIQUE INDEX IF NOT EXISTS idx_position_reply_conditions_dedupe
    ON position_reply_conditions (position_id, dedupe_key);
CREATE INDEX IF NOT EXISTS idx_position_reply_conditions_order
    ON position_reply_conditions (position_id, enabled, sort_order, created_at);


-- 为已有手机号身份表补齐更新时间；不重建表或修改已有身份归属。
ALTER TABLE candidate_phone_identities ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
COMMENT ON COLUMN candidate_phone_identities.updated_at IS '手机号身份记录最近更新时间';
