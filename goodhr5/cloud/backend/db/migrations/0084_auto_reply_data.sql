-- 本迁移建立自动回复所需的团队公司档案、岗位配置、会话消息、简历附件、确认项和 AI 审计数据模型。

ALTER TABLE candidate_profiles
    ADD COLUMN IF NOT EXISTS gender TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS birth_ym_precision TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS normalized_phone TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN candidate_profiles.gender IS '候选人性别，只允许男、女或空字符串';
COMMENT ON COLUMN candidate_profiles.birth_ym_precision IS '出生年月精度：month精确到月，year_estimated按年龄估算年份，空字符串表示未知';
COMMENT ON COLUMN candidate_profiles.normalized_phone IS '标准化手机号：中国大陆手机号去掉86或0086国家码，其他号码仅保留数字，用于团队内跨平台候选人合并';

ALTER TABLE candidate_profiles
    DROP CONSTRAINT IF EXISTS chk_candidate_profiles_gender;
ALTER TABLE candidate_profiles
    ADD CONSTRAINT chk_candidate_profiles_gender CHECK (gender IN ('', '男', '女'));

ALTER TABLE candidate_profiles
    DROP CONSTRAINT IF EXISTS chk_candidate_profiles_birth_precision;
ALTER TABLE candidate_profiles
    ADD CONSTRAINT chk_candidate_profiles_birth_precision CHECK (birth_ym_precision IN ('', 'month', 'year_estimated'));

WITH normalized_candidate_phones AS (
    SELECT id, regexp_replace(phone, '[^0-9]', '', 'g') AS digits
    FROM candidate_profiles
    WHERE normalized_phone = '' AND phone <> ''
)
UPDATE candidate_profiles AS candidate
SET normalized_phone = CASE
    WHEN normalized.digits ~ '^861[0-9]{10}$' THEN substring(normalized.digits FROM 3)
    WHEN normalized.digits ~ '^00861[0-9]{10}$' THEN substring(normalized.digits FROM 5)
    ELSE normalized.digits
END
FROM normalized_candidate_phones AS normalized
WHERE candidate.id = normalized.id;

CREATE INDEX IF NOT EXISTS idx_candidate_profiles_tenant_phone
    ON candidate_profiles (tenant_id, normalized_phone)
    WHERE normalized_phone <> '';

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

CREATE TABLE IF NOT EXISTS candidate_platform_identities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    candidate_id UUID REFERENCES candidate_profiles(id) ON DELETE CASCADE,
    platform_id TEXT NOT NULL,
    platform_account_id UUID REFERENCES platform_accounts(id) ON DELETE SET NULL,
    platform_candidate_id TEXT NOT NULL DEFAULT '',
    candidate_name TEXT NOT NULL DEFAULT '',
    gender TEXT NOT NULL DEFAULT '',
    normalized_phone TEXT NOT NULL DEFAULT '',
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (gender IN ('', '男', '女'))
);

COMMENT ON TABLE candidate_platform_identities IS '候选人在各招聘平台账号下的身份，可在正式简历入库前存在';
COMMENT ON COLUMN candidate_platform_identities.id IS '平台候选人身份唯一标识';
COMMENT ON COLUMN candidate_platform_identities.tenant_id IS '平台候选人身份所属团队标识';
COMMENT ON COLUMN candidate_platform_identities.candidate_id IS '关联正式候选人标识，手机号未获得时为空';
COMMENT ON COLUMN candidate_platform_identities.platform_id IS '招聘平台标识';
COMMENT ON COLUMN candidate_platform_identities.platform_account_id IS '招聘平台账号标识，对应platform_accounts.id';
COMMENT ON COLUMN candidate_platform_identities.platform_candidate_id IS '平台可稳定读取的候选人标识';
COMMENT ON COLUMN candidate_platform_identities.candidate_name IS '页面可见候选人名称';
COMMENT ON COLUMN candidate_platform_identities.gender IS '页面或简历可见性别，只允许男、女或空字符串';
COMMENT ON COLUMN candidate_platform_identities.normalized_phone IS '平台身份最近获得的标准化手机号，中国大陆手机号不保留国家码';
COMMENT ON COLUMN candidate_platform_identities.first_seen_at IS '首次发现该平台身份的时间';
COMMENT ON COLUMN candidate_platform_identities.last_seen_at IS '最近看见该平台身份的时间';
COMMENT ON COLUMN candidate_platform_identities.created_at IS '平台身份记录创建时间';
COMMENT ON COLUMN candidate_platform_identities.updated_at IS '平台身份记录最近更新时间';

CREATE UNIQUE INDEX IF NOT EXISTS idx_candidate_platform_identity_stable
    ON candidate_platform_identities (tenant_id, platform_id, COALESCE(platform_account_id, '00000000-0000-0000-0000-000000000000'::uuid), platform_candidate_id)
    WHERE platform_candidate_id <> '';
CREATE INDEX IF NOT EXISTS idx_candidate_platform_identity_phone
    ON candidate_platform_identities (tenant_id, normalized_phone)
    WHERE normalized_phone <> '';

CREATE TABLE IF NOT EXISTS candidate_phone_identities (
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    normalized_phone TEXT NOT NULL,
    candidate_id UUID NOT NULL REFERENCES candidate_profiles(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, normalized_phone)
);

COMMENT ON TABLE candidate_phone_identities IS '团队内标准化手机号到正式候选人的唯一映射';
COMMENT ON COLUMN candidate_phone_identities.tenant_id IS '手机号身份所属团队标识';
COMMENT ON COLUMN candidate_phone_identities.normalized_phone IS '标准化手机号，中国大陆手机号不保留国家码，其他号码仅保留数字';
COMMENT ON COLUMN candidate_phone_identities.candidate_id IS '手机号对应的正式候选人标识';
COMMENT ON COLUMN candidate_phone_identities.created_at IS '手机号身份创建时间';
COMMENT ON COLUMN candidate_phone_identities.updated_at IS '手机号身份最近更新时间';

INSERT INTO candidate_phone_identities (tenant_id, normalized_phone, candidate_id)
SELECT tenant_id, normalized_phone, id
FROM (
    SELECT tenant_id, normalized_phone, id,
           ROW_NUMBER() OVER (PARTITION BY tenant_id, normalized_phone ORDER BY updated_at DESC, created_at, id) AS row_no
    FROM candidate_profiles
    WHERE normalized_phone <> ''
) ranked
WHERE row_no = 1
ON CONFLICT (tenant_id, normalized_phone) DO NOTHING;

INSERT INTO candidate_platform_identities (
    tenant_id, candidate_id, platform_id, platform_candidate_id,
    candidate_name, gender, normalized_phone, first_seen_at, last_seen_at
)
SELECT tenant_id, id, source_platform_id, source_platform_candidate_id,
       candidate_name, gender, normalized_phone,
       COALESCE(first_seen_at, created_at), updated_at
FROM candidate_profiles
WHERE source_platform_id <> '' AND source_platform_candidate_id <> ''
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS candidate_conversations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    candidate_id UUID REFERENCES candidate_profiles(id) ON DELETE SET NULL,
    platform_identity_id UUID REFERENCES candidate_platform_identities(id) ON DELETE SET NULL,
    engagement_id UUID REFERENCES candidate_engagements(id) ON DELETE SET NULL,
    position_id UUID REFERENCES positions(id) ON DELETE SET NULL,
    platform_account_id UUID REFERENCES platform_accounts(id) ON DELETE SET NULL,
    platform_id TEXT NOT NULL,
    platform_thread_id TEXT NOT NULL DEFAULT '',
    candidate_name TEXT NOT NULL DEFAULT '',
    gender TEXT NOT NULL DEFAULT '',
    page_position_text TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    history_complete BOOLEAN NOT NULL DEFAULT false,
    last_synced_message_key TEXT NOT NULL DEFAULT '',
    last_candidate_message_key TEXT NOT NULL DEFAULT '',
    unresolved_reason TEXT NOT NULL DEFAULT '',
    last_checked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (gender IN ('', '男', '女')),
    CHECK (status IN ('active', 'waiting_resume', 'ready', 'unresolved', 'ended'))
);

COMMENT ON TABLE candidate_conversations IS '自动回复会话主体，允许在正式候选人和岗位未解析前暂存聊天';
COMMENT ON COLUMN candidate_conversations.id IS '自动回复会话唯一标识';
COMMENT ON COLUMN candidate_conversations.tenant_id IS '会话所属团队标识';
COMMENT ON COLUMN candidate_conversations.candidate_id IS '关联正式候选人标识，未取得手机号时为空';
COMMENT ON COLUMN candidate_conversations.platform_identity_id IS '关联平台候选人身份标识';
COMMENT ON COLUMN candidate_conversations.engagement_id IS '关联候选人岗位触达标识';
COMMENT ON COLUMN candidate_conversations.position_id IS '页面和触达记录唯一解析出的岗位标识';
COMMENT ON COLUMN candidate_conversations.platform_account_id IS '会话所在招聘平台账号标识，对应platform_accounts.id';
COMMENT ON COLUMN candidate_conversations.platform_id IS '招聘平台标识';
COMMENT ON COLUMN candidate_conversations.platform_thread_id IS '平台可稳定读取的会话标识';
COMMENT ON COLUMN candidate_conversations.candidate_name IS '页面可见候选人名称';
COMMENT ON COLUMN candidate_conversations.gender IS '页面或简历可见性别，只允许男、女或空字符串';
COMMENT ON COLUMN candidate_conversations.page_position_text IS '会话页面显示的沟通岗位原文';
COMMENT ON COLUMN candidate_conversations.status IS '会话状态：active、waiting_resume、ready、unresolved或ended';
COMMENT ON COLUMN candidate_conversations.history_complete IS '首次历史是否已经同步到明确起点';
COMMENT ON COLUMN candidate_conversations.last_synced_message_key IS '最近同步成功的页面消息键';
COMMENT ON COLUMN candidate_conversations.last_candidate_message_key IS '最近候选人消息键，用于AI和发送幂等';
COMMENT ON COLUMN candidate_conversations.unresolved_reason IS '会话无法唯一识别或无法自动处理的原因';
COMMENT ON COLUMN candidate_conversations.last_checked_at IS '本地程序最近检查会话的时间';
COMMENT ON COLUMN candidate_conversations.created_at IS '会话创建时间';
COMMENT ON COLUMN candidate_conversations.updated_at IS '会话最近更新时间';

CREATE UNIQUE INDEX IF NOT EXISTS idx_candidate_conversations_thread
    ON candidate_conversations (tenant_id, platform_id, COALESCE(platform_account_id, '00000000-0000-0000-0000-000000000000'::uuid), platform_thread_id)
    WHERE platform_thread_id <> '';
CREATE INDEX IF NOT EXISTS idx_candidate_conversations_position_status
    ON candidate_conversations (tenant_id, position_id, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS candidate_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    conversation_id UUID NOT NULL REFERENCES candidate_conversations(id) ON DELETE CASCADE,
    platform_message_id TEXT NOT NULL DEFAULT '',
    fingerprint TEXT NOT NULL,
    direction TEXT NOT NULL,
    message_type TEXT NOT NULL DEFAULT 'text',
    text_content TEXT NOT NULL DEFAULT '',
    card_content JSONB NOT NULL DEFAULT '{}'::jsonb,
    sender_name TEXT NOT NULL DEFAULT '',
    platform_sent_at TIMESTAMPTZ,
    ingested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (direction IN ('candidate', 'self', 'system'))
);

COMMENT ON TABLE candidate_messages IS '自动回复聊天消息，按平台消息标识或稳定指纹幂等同步';
COMMENT ON COLUMN candidate_messages.id IS '聊天消息唯一标识';
COMMENT ON COLUMN candidate_messages.tenant_id IS '聊天消息所属团队标识';
COMMENT ON COLUMN candidate_messages.conversation_id IS '聊天消息所属自动回复会话标识';
COMMENT ON COLUMN candidate_messages.platform_message_id IS '平台可稳定读取的消息标识';
COMMENT ON COLUMN candidate_messages.fingerprint IS '无平台消息标识时使用的稳定消息指纹';
COMMENT ON COLUMN candidate_messages.direction IS '消息方向：candidate候选人、self己方、system系统';
COMMENT ON COLUMN candidate_messages.message_type IS '消息类型：text、resume_card、file、image、voice、system等';
COMMENT ON COLUMN candidate_messages.text_content IS '页面可见消息正文或摘要';
COMMENT ON COLUMN candidate_messages.card_content IS '卡片类消息的结构化页面可见字段';
COMMENT ON COLUMN candidate_messages.sender_name IS '页面可见发送人名称';
COMMENT ON COLUMN candidate_messages.platform_sent_at IS '页面可见平台发送时间';
COMMENT ON COLUMN candidate_messages.ingested_at IS '消息首次同步到云端的时间';
COMMENT ON COLUMN candidate_messages.created_at IS '消息记录创建时间';

CREATE UNIQUE INDEX IF NOT EXISTS idx_candidate_messages_platform_id
    ON candidate_messages (conversation_id, platform_message_id)
    WHERE platform_message_id <> '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_candidate_messages_fingerprint
    ON candidate_messages (conversation_id, fingerprint);
CREATE INDEX IF NOT EXISTS idx_candidate_messages_conversation_time
    ON candidate_messages (conversation_id, COALESCE(platform_sent_at, ingested_at), id);

CREATE TABLE IF NOT EXISTS candidate_resume_attachments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    candidate_id UUID REFERENCES candidate_profiles(id) ON DELETE CASCADE,
    conversation_id UUID REFERENCES candidate_conversations(id) ON DELETE CASCADE,
    source_message_id UUID REFERENCES candidate_messages(id) ON DELETE SET NULL,
    platform_id TEXT NOT NULL,
    original_name TEXT NOT NULL,
    storage_path TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    extracted_text TEXT NOT NULL DEFAULT '',
    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (size_bytes >= 0 AND size_bytes <= 20971520),
    CHECK (candidate_id IS NOT NULL OR conversation_id IS NOT NULL)
);

COMMENT ON TABLE candidate_resume_attachments IS '自动回复取得的简历附件元数据，文件内容保存在云端持久化目录';
COMMENT ON COLUMN candidate_resume_attachments.id IS '简历附件唯一标识';
COMMENT ON COLUMN candidate_resume_attachments.tenant_id IS '简历附件所属团队标识';
COMMENT ON COLUMN candidate_resume_attachments.candidate_id IS '附件关联的正式候选人标识';
COMMENT ON COLUMN candidate_resume_attachments.conversation_id IS '手机号未获得前附件关联的临时会话标识';
COMMENT ON COLUMN candidate_resume_attachments.source_message_id IS '附件来源聊天消息标识';
COMMENT ON COLUMN candidate_resume_attachments.platform_id IS '附件来源招聘平台标识';
COMMENT ON COLUMN candidate_resume_attachments.original_name IS '候选人附件原始文件名';
COMMENT ON COLUMN candidate_resume_attachments.storage_path IS '云端持久化目录中的相对文件路径';
COMMENT ON COLUMN candidate_resume_attachments.sha256 IS '附件文件SHA256哈希';
COMMENT ON COLUMN candidate_resume_attachments.mime_type IS '附件MIME类型';
COMMENT ON COLUMN candidate_resume_attachments.size_bytes IS '附件字节大小，最大20MB';
COMMENT ON COLUMN candidate_resume_attachments.extracted_text IS '附件解析或AI识别出的文本';
COMMENT ON COLUMN candidate_resume_attachments.created_by_user_id IS '上传附件的本地Agent所属用户标识';
COMMENT ON COLUMN candidate_resume_attachments.created_at IS '附件记录创建时间';

CREATE UNIQUE INDEX IF NOT EXISTS idx_candidate_resume_attachments_hash
    ON candidate_resume_attachments (tenant_id, sha256);
CREATE INDEX IF NOT EXISTS idx_candidate_resume_attachments_candidate
    ON candidate_resume_attachments (candidate_id, created_at DESC);

CREATE TABLE IF NOT EXISTS candidate_confirmation_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    conversation_id UUID NOT NULL REFERENCES candidate_conversations(id) ON DELETE CASCADE,
    candidate_id UUID REFERENCES candidate_profiles(id) ON DELETE SET NULL,
    position_id UUID REFERENCES positions(id) ON DELETE SET NULL,
    item_type TEXT NOT NULL,
    content TEXT NOT NULL,
    dedupe_key TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    source_type TEXT NOT NULL,
    source_ref TEXT NOT NULL DEFAULT '',
    evidence_text TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    created_by_kind TEXT NOT NULL DEFAULT 'ai',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (item_type IN ('required', 'confirm', 'bonus')),
    CHECK (status IN ('pending', 'matched', 'unmatched', 'not_applicable', 'conflicted')),
    CHECK (source_type IN ('position', 'resume', 'chat', 'ai')),
    CHECK (created_by_kind IN ('system', 'ai', 'user'))
);

COMMENT ON TABLE candidate_confirmation_items IS '候选人与岗位之间可审计的确认项，不保存AI隐藏思考过程';
COMMENT ON COLUMN candidate_confirmation_items.id IS '候选人确认项唯一标识';
COMMENT ON COLUMN candidate_confirmation_items.tenant_id IS '候选人确认项所属团队标识';
COMMENT ON COLUMN candidate_confirmation_items.conversation_id IS '确认项所属自动回复会话标识';
COMMENT ON COLUMN candidate_confirmation_items.candidate_id IS '确认项关联的正式候选人标识';
COMMENT ON COLUMN candidate_confirmation_items.position_id IS '确认项关联的岗位标识';
COMMENT ON COLUMN candidate_confirmation_items.item_type IS '确认项类型：required、confirm或bonus';
COMMENT ON COLUMN candidate_confirmation_items.content IS '需要判断或确认的条件正文';
COMMENT ON COLUMN candidate_confirmation_items.dedupe_key IS '同一会话内稳定去重键';
COMMENT ON COLUMN candidate_confirmation_items.status IS '确认项状态：pending、matched、unmatched、not_applicable或conflicted';
COMMENT ON COLUMN candidate_confirmation_items.source_type IS '确认项来源：position、resume、chat或ai';
COMMENT ON COLUMN candidate_confirmation_items.source_ref IS '来源条件、消息或简历字段标识';
COMMENT ON COLUMN candidate_confirmation_items.evidence_text IS '支持当前状态的简短证据';
COMMENT ON COLUMN candidate_confirmation_items.summary IS 'AI或用户给出的简短结论';
COMMENT ON COLUMN candidate_confirmation_items.created_by_kind IS '确认项创建来源：system、ai或user';
COMMENT ON COLUMN candidate_confirmation_items.created_at IS '确认项创建时间';
COMMENT ON COLUMN candidate_confirmation_items.updated_at IS '确认项最近更新时间';

CREATE UNIQUE INDEX IF NOT EXISTS idx_candidate_confirmation_items_dedupe
    ON candidate_confirmation_items (conversation_id, dedupe_key);
CREATE INDEX IF NOT EXISTS idx_candidate_confirmation_items_status
    ON candidate_confirmation_items (conversation_id, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS candidate_confirmation_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    confirmation_item_id UUID NOT NULL REFERENCES candidate_confirmation_items(id) ON DELETE CASCADE,
    old_status TEXT NOT NULL DEFAULT '',
    new_status TEXT NOT NULL,
    evidence_text TEXT NOT NULL DEFAULT '',
    source_ref TEXT NOT NULL DEFAULT '',
    changed_by_kind TEXT NOT NULL DEFAULT 'ai',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (old_status IN ('', 'pending', 'matched', 'unmatched', 'not_applicable', 'conflicted')),
    CHECK (new_status IN ('pending', 'matched', 'unmatched', 'not_applicable', 'conflicted')),
    CHECK (changed_by_kind IN ('system', 'ai', 'user'))
);

COMMENT ON TABLE candidate_confirmation_events IS '候选人确认项每次状态变化和证据流水';
COMMENT ON COLUMN candidate_confirmation_events.id IS '确认项变化事件唯一标识';
COMMENT ON COLUMN candidate_confirmation_events.tenant_id IS '确认项变化事件所属团队标识';
COMMENT ON COLUMN candidate_confirmation_events.confirmation_item_id IS '关联候选人确认项标识';
COMMENT ON COLUMN candidate_confirmation_events.old_status IS '变化前状态，首次创建时为空';
COMMENT ON COLUMN candidate_confirmation_events.new_status IS '变化后状态';
COMMENT ON COLUMN candidate_confirmation_events.evidence_text IS '本次状态变化依据';
COMMENT ON COLUMN candidate_confirmation_events.source_ref IS '本次变化来源消息或简历字段标识';
COMMENT ON COLUMN candidate_confirmation_events.changed_by_kind IS '变更来源：system、ai或user';
COMMENT ON COLUMN candidate_confirmation_events.created_at IS '状态变化时间';

CREATE INDEX IF NOT EXISTS idx_candidate_confirmation_events_item
    ON candidate_confirmation_events (confirmation_item_id, created_at DESC);

CREATE TABLE IF NOT EXISTS auto_reply_ai_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    conversation_id UUID NOT NULL REFERENCES candidate_conversations(id) ON DELETE CASCADE,
    candidate_id UUID REFERENCES candidate_profiles(id) ON DELETE SET NULL,
    position_id UUID REFERENCES positions(id) ON DELETE SET NULL,
    trace_id TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'running',
    based_on_message_key TEXT NOT NULL,
    input_messages JSONB NOT NULL DEFAULT '[]'::jsonb,
    output_message JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    token_usage INTEGER NOT NULL DEFAULT 0,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + INTERVAL '180 days'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (status IN ('running', 'completed', 'failed', 'notified')),
    CHECK (token_usage >= 0)
);

COMMENT ON TABLE auto_reply_ai_runs IS '自动回复每次AI请求、返回和错误的总审计记录';
COMMENT ON COLUMN auto_reply_ai_runs.id IS 'AI运行唯一标识';
COMMENT ON COLUMN auto_reply_ai_runs.tenant_id IS 'AI运行所属团队标识';
COMMENT ON COLUMN auto_reply_ai_runs.conversation_id IS 'AI运行关联的自动回复会话标识';
COMMENT ON COLUMN auto_reply_ai_runs.candidate_id IS 'AI运行关联的正式候选人标识';
COMMENT ON COLUMN auto_reply_ai_runs.position_id IS 'AI运行关联的岗位标识';
COMMENT ON COLUMN auto_reply_ai_runs.trace_id IS '本地程序生成的整次AI调用追踪标识';
COMMENT ON COLUMN auto_reply_ai_runs.model IS '本次AI调用模型名称';
COMMENT ON COLUMN auto_reply_ai_runs.status IS 'AI运行状态：running、completed、failed或notified';
COMMENT ON COLUMN auto_reply_ai_runs.based_on_message_key IS '本次决策依据的最新候选人消息键';
COMMENT ON COLUMN auto_reply_ai_runs.input_messages IS '发送给AI的消息数组审计数据';
COMMENT ON COLUMN auto_reply_ai_runs.output_message IS 'AI完整返回审计数据';
COMMENT ON COLUMN auto_reply_ai_runs.error_code IS 'AI或流程失败的稳定错误码';
COMMENT ON COLUMN auto_reply_ai_runs.error_message IS 'AI或流程失败的安全错误说明';
COMMENT ON COLUMN auto_reply_ai_runs.token_usage IS '本次AI调用Token使用量';
COMMENT ON COLUMN auto_reply_ai_runs.started_at IS 'AI运行开始时间';
COMMENT ON COLUMN auto_reply_ai_runs.completed_at IS 'AI运行结束时间';
COMMENT ON COLUMN auto_reply_ai_runs.expires_at IS 'AI审计到期清理时间，默认180天';
COMMENT ON COLUMN auto_reply_ai_runs.created_at IS 'AI运行记录创建时间';

CREATE UNIQUE INDEX IF NOT EXISTS idx_auto_reply_ai_runs_trace
    ON auto_reply_ai_runs (tenant_id, trace_id);
CREATE INDEX IF NOT EXISTS idx_auto_reply_ai_runs_conversation
    ON auto_reply_ai_runs (conversation_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_auto_reply_ai_runs_expiry
    ON auto_reply_ai_runs (expires_at);

CREATE TABLE IF NOT EXISTS auto_reply_tool_calls (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    ai_run_id UUID NOT NULL REFERENCES auto_reply_ai_runs(id) ON DELETE CASCADE,
    tool_call_id TEXT NOT NULL,
    sequence_no INTEGER NOT NULL,
    tool_name TEXT NOT NULL,
    arguments_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    result_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'running',
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (sequence_no BETWEEN 1 AND 8),
    CHECK (status IN ('running', 'completed', 'failed'))
);

COMMENT ON TABLE auto_reply_tool_calls IS '自动回复AI工具名称、参数、结果和错误审计';
COMMENT ON COLUMN auto_reply_tool_calls.id IS 'AI工具调用唯一标识';
COMMENT ON COLUMN auto_reply_tool_calls.tenant_id IS 'AI工具调用所属团队标识';
COMMENT ON COLUMN auto_reply_tool_calls.ai_run_id IS '关联AI运行标识';
COMMENT ON COLUMN auto_reply_tool_calls.tool_call_id IS '模型返回的工具调用标识';
COMMENT ON COLUMN auto_reply_tool_calls.sequence_no IS '单条候选人消息内的工具调用顺序，最大8次';
COMMENT ON COLUMN auto_reply_tool_calls.tool_name IS '工具名称';
COMMENT ON COLUMN auto_reply_tool_calls.arguments_json IS '经过审计的工具参数';
COMMENT ON COLUMN auto_reply_tool_calls.result_json IS '工具执行结果或安全错误结果';
COMMENT ON COLUMN auto_reply_tool_calls.status IS '工具调用状态：running、completed或failed';
COMMENT ON COLUMN auto_reply_tool_calls.error_code IS '工具调用失败的稳定错误码';
COMMENT ON COLUMN auto_reply_tool_calls.error_message IS '工具调用失败的安全错误说明';
COMMENT ON COLUMN auto_reply_tool_calls.started_at IS '工具调用开始时间';
COMMENT ON COLUMN auto_reply_tool_calls.completed_at IS '工具调用结束时间';
COMMENT ON COLUMN auto_reply_tool_calls.created_at IS '工具调用记录创建时间';

CREATE UNIQUE INDEX IF NOT EXISTS idx_auto_reply_tool_calls_call
    ON auto_reply_tool_calls (ai_run_id, tool_call_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_auto_reply_tool_calls_sequence
    ON auto_reply_tool_calls (ai_run_id, sequence_no);

CREATE TABLE IF NOT EXISTS auto_reply_config_suggestions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    conversation_id UUID REFERENCES candidate_conversations(id) ON DELETE SET NULL,
    position_id UUID REFERENCES positions(id) ON DELETE CASCADE,
    company_profile_id UUID REFERENCES tenant_company_profiles(id) ON DELETE CASCADE,
    suggestion_type TEXT NOT NULL,
    operation TEXT NOT NULL,
    target_id TEXT NOT NULL DEFAULT '',
    proposed_value JSONB NOT NULL DEFAULT '{}'::jsonb,
    reason TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    reviewed_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (suggestion_type IN ('position', 'company')),
    CHECK (operation IN ('create', 'update', 'delete')),
    CHECK (status IN ('pending', 'approved', 'rejected')),
    CHECK (position_id IS NOT NULL OR company_profile_id IS NOT NULL)
);

COMMENT ON TABLE auto_reply_config_suggestions IS 'AI从聊天学习后提交的岗位或公司资料待审核修改建议';
COMMENT ON COLUMN auto_reply_config_suggestions.id IS '配置修改建议唯一标识';
COMMENT ON COLUMN auto_reply_config_suggestions.tenant_id IS '配置修改建议所属团队标识';
COMMENT ON COLUMN auto_reply_config_suggestions.conversation_id IS '建议来源自动回复会话标识';
COMMENT ON COLUMN auto_reply_config_suggestions.position_id IS '建议关联岗位标识';
COMMENT ON COLUMN auto_reply_config_suggestions.company_profile_id IS '建议关联公司档案标识';
COMMENT ON COLUMN auto_reply_config_suggestions.suggestion_type IS '建议类型：position岗位或company公司';
COMMENT ON COLUMN auto_reply_config_suggestions.operation IS '建议操作：create、update或delete';
COMMENT ON COLUMN auto_reply_config_suggestions.target_id IS '建议修改的条件或字段标识';
COMMENT ON COLUMN auto_reply_config_suggestions.proposed_value IS 'AI提出的结构化修改内容';
COMMENT ON COLUMN auto_reply_config_suggestions.reason IS 'AI提出建议的可复核原因';
COMMENT ON COLUMN auto_reply_config_suggestions.status IS '审核状态：pending、approved或rejected';
COMMENT ON COLUMN auto_reply_config_suggestions.reviewed_by_user_id IS '审核建议的用户标识';
COMMENT ON COLUMN auto_reply_config_suggestions.reviewed_at IS '建议审核时间';
COMMENT ON COLUMN auto_reply_config_suggestions.created_at IS '建议创建时间';
COMMENT ON COLUMN auto_reply_config_suggestions.updated_at IS '建议最近更新时间';

CREATE INDEX IF NOT EXISTS idx_auto_reply_config_suggestions_pending
    ON auto_reply_config_suggestions (tenant_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS auto_reply_notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    conversation_id UUID NOT NULL REFERENCES candidate_conversations(id) ON DELETE CASCADE,
    position_id UUID REFERENCES positions(id) ON DELETE SET NULL,
    based_on_message_key TEXT NOT NULL,
    reason_key TEXT NOT NULL,
    candidate_name TEXT NOT NULL DEFAULT '',
    gender TEXT NOT NULL DEFAULT '',
    platform_id TEXT NOT NULL,
    reason TEXT NOT NULL,
    recipient_email TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    error_message TEXT NOT NULL DEFAULT '',
    sent_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + INTERVAL '180 days'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (gender IN ('', '男', '女')),
    CHECK (status IN ('pending', 'sent', 'failed'))
);

COMMENT ON TABLE auto_reply_notifications IS '自动回复无法处理时的人工接管邮件记录和幂等凭据';
COMMENT ON COLUMN auto_reply_notifications.id IS '人工接管通知唯一标识';
COMMENT ON COLUMN auto_reply_notifications.tenant_id IS '人工接管通知所属团队标识';
COMMENT ON COLUMN auto_reply_notifications.conversation_id IS '通知关联的自动回复会话标识';
COMMENT ON COLUMN auto_reply_notifications.position_id IS '通知关联的岗位标识';
COMMENT ON COLUMN auto_reply_notifications.based_on_message_key IS '触发通知的候选人最新消息键';
COMMENT ON COLUMN auto_reply_notifications.reason_key IS '标准化通知原因去重键';
COMMENT ON COLUMN auto_reply_notifications.candidate_name IS '邮件展示的候选人名称';
COMMENT ON COLUMN auto_reply_notifications.gender IS '邮件展示的候选人性别，只允许男、女或空字符串';
COMMENT ON COLUMN auto_reply_notifications.platform_id IS '邮件展示的招聘平台标识';
COMMENT ON COLUMN auto_reply_notifications.reason IS '无法自动处理的可读原因';
COMMENT ON COLUMN auto_reply_notifications.recipient_email IS '接收人工接管通知的HR邮箱';
COMMENT ON COLUMN auto_reply_notifications.status IS '邮件状态：pending待发送、sent已发送或failed发送失败';
COMMENT ON COLUMN auto_reply_notifications.error_message IS '邮件发送失败时的安全错误说明';
COMMENT ON COLUMN auto_reply_notifications.sent_at IS '邮件实际发送成功时间';
COMMENT ON COLUMN auto_reply_notifications.expires_at IS '通知幂等记录到期清理时间，默认180天';
COMMENT ON COLUMN auto_reply_notifications.created_at IS '通知记录创建时间';
COMMENT ON COLUMN auto_reply_notifications.updated_at IS '通知记录最近更新时间';

CREATE UNIQUE INDEX IF NOT EXISTS idx_auto_reply_notifications_dedupe
    ON auto_reply_notifications (tenant_id, conversation_id, based_on_message_key, reason_key);
CREATE INDEX IF NOT EXISTS idx_auto_reply_notifications_expiry
    ON auto_reply_notifications (expires_at);
