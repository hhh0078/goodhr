-- 文件作用说明：按业务实体保存自动回复本地数据，保留旧账号快照用于可校验迁移。


CREATE TABLE IF NOT EXISTS auto_reply_candidates (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT, -- 数据首次写入顺序，消息按此稳定排序
    owner_id TEXT NOT NULL, -- 已认证用户的本地隔离键
    id TEXT NOT NULL, -- 业务实体稳定标识
    payload_json TEXT NOT NULL CHECK(json_valid(payload_json)), -- 当前单条实体的强类型JSON内容，不包含其他实体集合
    revision INTEGER NOT NULL DEFAULT 1, -- 当前记录的乐观版本号
    updated_at TEXT NOT NULL, -- 最近写入时间
    phone TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.Phone')) STORED, -- 候选人可选手机号，仅作身份线索
    UNIQUE(owner_id,id)
);
CREATE INDEX IF NOT EXISTS idx_ar_candidates_phone ON auto_reply_candidates(owner_id,phone);

CREATE TABLE IF NOT EXISTS auto_reply_identities (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT, -- 数据首次写入顺序，消息按此稳定排序
    owner_id TEXT NOT NULL, -- 已认证用户的本地隔离键
    id TEXT NOT NULL, -- 业务实体稳定标识
    payload_json TEXT NOT NULL CHECK(json_valid(payload_json)), -- 当前单条实体的强类型JSON内容，不包含其他实体集合
    revision INTEGER NOT NULL DEFAULT 1, -- 当前记录的乐观版本号
    updated_at TEXT NOT NULL, -- 最近写入时间
    platform_id TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.platform_id')) STORED, -- 招聘平台标识
    account_id TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.platform_account_id')) STORED, -- 已确认的招聘账号标识
    platform_candidate_id TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.platform_candidate_id')) STORED, -- 平台候选人标识
    candidate_id TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.candidate_id')) STORED, -- 本地稳定候选人标识
    UNIQUE(owner_id,id)
);
CREATE INDEX IF NOT EXISTS idx_ar_identities_platform_id ON auto_reply_identities(owner_id,platform_id);
CREATE INDEX IF NOT EXISTS idx_ar_identities_account_id ON auto_reply_identities(owner_id,account_id);
CREATE INDEX IF NOT EXISTS idx_ar_identities_platform_candidate_id ON auto_reply_identities(owner_id,platform_candidate_id);
CREATE INDEX IF NOT EXISTS idx_ar_identities_candidate_id ON auto_reply_identities(owner_id,candidate_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_ar_identity_source ON auto_reply_identities(owner_id,platform_id,account_id,platform_candidate_id);

CREATE TABLE IF NOT EXISTS auto_reply_conversations (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT, -- 数据首次写入顺序，消息按此稳定排序
    owner_id TEXT NOT NULL, -- 已认证用户的本地隔离键
    id TEXT NOT NULL, -- 业务实体稳定标识
    payload_json TEXT NOT NULL CHECK(json_valid(payload_json)), -- 当前单条实体的强类型JSON内容，不包含其他实体集合
    revision INTEGER NOT NULL DEFAULT 1, -- 当前记录的乐观版本号
    updated_at TEXT NOT NULL, -- 最近写入时间
    platform_id TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.platform_id')) STORED, -- 招聘平台标识
    account_id TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.platform_account_id')) STORED, -- 已确认的招聘账号标识
    thread_id TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.platform_thread_id')) STORED, -- 平台会话标识
    identity_id TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.platform_identity_id')) STORED, -- 平台身份关联标识
    candidate_id TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.candidate_id')) STORED, -- 本地稳定候选人标识
    position_id TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.position_id')) STORED, -- 岗位标识
    UNIQUE(owner_id,id)
);
CREATE INDEX IF NOT EXISTS idx_ar_conversations_platform_id ON auto_reply_conversations(owner_id,platform_id);
CREATE INDEX IF NOT EXISTS idx_ar_conversations_account_id ON auto_reply_conversations(owner_id,account_id);
CREATE INDEX IF NOT EXISTS idx_ar_conversations_thread_id ON auto_reply_conversations(owner_id,thread_id);
CREATE INDEX IF NOT EXISTS idx_ar_conversations_identity_id ON auto_reply_conversations(owner_id,identity_id);
CREATE INDEX IF NOT EXISTS idx_ar_conversations_candidate_id ON auto_reply_conversations(owner_id,candidate_id);
CREATE INDEX IF NOT EXISTS idx_ar_conversations_position_id ON auto_reply_conversations(owner_id,position_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_ar_conversation_source ON auto_reply_conversations(owner_id,platform_id,account_id,thread_id);

CREATE TABLE IF NOT EXISTS auto_reply_messages (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT, -- 数据首次写入顺序，消息按此稳定排序
    owner_id TEXT NOT NULL, -- 已认证用户的本地隔离键
    id TEXT NOT NULL, -- 业务实体稳定标识
    payload_json TEXT NOT NULL CHECK(json_valid(payload_json)), -- 当前单条实体的强类型JSON内容，不包含其他实体集合
    revision INTEGER NOT NULL DEFAULT 1, -- 当前记录的乐观版本号
    updated_at TEXT NOT NULL, -- 最近写入时间
    conversation_id TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.conversation_id')) STORED, -- 本地会话标识
    message_key TEXT GENERATED ALWAYS AS (COALESCE(NULLIF(json_extract(payload_json,'$.platform_message_id'),''),json_extract(payload_json,'$.fingerprint'))) STORED, -- 平台消息键或稳定指纹
    direction TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.direction')) STORED, -- 消息方向
    message_order REAL NOT NULL DEFAULT 0, -- 消息在会话中的稳定先后顺序，补历史可插到已知边界之前
    processed_status TEXT NOT NULL DEFAULT '', -- 单条消息处理结果，空值表示仍待处理
    UNIQUE(owner_id,id)
);
CREATE INDEX IF NOT EXISTS idx_ar_messages_conversation_id ON auto_reply_messages(owner_id,conversation_id);
CREATE INDEX IF NOT EXISTS idx_ar_messages_message_key ON auto_reply_messages(owner_id,message_key);
CREATE INDEX IF NOT EXISTS idx_ar_messages_direction ON auto_reply_messages(owner_id,direction);
CREATE UNIQUE INDEX IF NOT EXISTS idx_ar_messages_key ON auto_reply_messages(owner_id,conversation_id,message_key);

CREATE TABLE IF NOT EXISTS auto_reply_confirmations (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT, -- 数据首次写入顺序，消息按此稳定排序
    owner_id TEXT NOT NULL, -- 已认证用户的本地隔离键
    id TEXT NOT NULL, -- 业务实体稳定标识
    payload_json TEXT NOT NULL CHECK(json_valid(payload_json)), -- 当前单条实体的强类型JSON内容，不包含其他实体集合
    revision INTEGER NOT NULL DEFAULT 1, -- 当前记录的乐观版本号
    updated_at TEXT NOT NULL, -- 最近写入时间
    candidate_id TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.candidate_id')) STORED, -- 本地稳定候选人标识
    conversation_id TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.conversation_id')) STORED, -- 本地会话标识
    position_id TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.position_id')) STORED, -- 岗位标识
    source_type TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.source_type')) STORED, -- 确认项来源类型
    condition_id TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.position_condition_id')) STORED, -- 来源岗位条件标识
    UNIQUE(owner_id,id)
);
CREATE INDEX IF NOT EXISTS idx_ar_confirmations_candidate_id ON auto_reply_confirmations(owner_id,candidate_id);
CREATE INDEX IF NOT EXISTS idx_ar_confirmations_conversation_id ON auto_reply_confirmations(owner_id,conversation_id);
CREATE INDEX IF NOT EXISTS idx_ar_confirmations_position_id ON auto_reply_confirmations(owner_id,position_id);
CREATE INDEX IF NOT EXISTS idx_ar_confirmations_source_type ON auto_reply_confirmations(owner_id,source_type);
CREATE INDEX IF NOT EXISTS idx_ar_confirmations_condition_id ON auto_reply_confirmations(owner_id,condition_id);

CREATE TABLE IF NOT EXISTS auto_reply_attachments (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT, -- 数据首次写入顺序，消息按此稳定排序
    owner_id TEXT NOT NULL, -- 已认证用户的本地隔离键
    id TEXT NOT NULL, -- 业务实体稳定标识
    payload_json TEXT NOT NULL CHECK(json_valid(payload_json)), -- 当前单条实体的强类型JSON内容，不包含其他实体集合
    revision INTEGER NOT NULL DEFAULT 1, -- 当前记录的乐观版本号
    updated_at TEXT NOT NULL, -- 最近写入时间
    candidate_id TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.candidate_id')) STORED, -- 本地稳定候选人标识
    conversation_id TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.conversation_id')) STORED, -- 本地会话标识
    UNIQUE(owner_id,id)
);
CREATE INDEX IF NOT EXISTS idx_ar_attachments_candidate_id ON auto_reply_attachments(owner_id,candidate_id);
CREATE INDEX IF NOT EXISTS idx_ar_attachments_conversation_id ON auto_reply_attachments(owner_id,conversation_id);

CREATE TABLE IF NOT EXISTS auto_reply_runs (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT, -- 数据首次写入顺序，消息按此稳定排序
    owner_id TEXT NOT NULL, -- 已认证用户的本地隔离键
    id TEXT NOT NULL, -- 业务实体稳定标识
    payload_json TEXT NOT NULL CHECK(json_valid(payload_json)), -- 当前单条实体的强类型JSON内容，不包含其他实体集合
    revision INTEGER NOT NULL DEFAULT 1, -- 当前记录的乐观版本号
    updated_at TEXT NOT NULL, -- 最近写入时间
    conversation_id TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.conversation_id')) STORED, -- 本地会话标识
    UNIQUE(owner_id,id)
);
CREATE INDEX IF NOT EXISTS idx_ar_runs_conversation_id ON auto_reply_runs(owner_id,conversation_id);

CREATE TABLE IF NOT EXISTS auto_reply_tools (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT, -- 数据首次写入顺序，消息按此稳定排序
    owner_id TEXT NOT NULL, -- 已认证用户的本地隔离键
    id TEXT NOT NULL, -- 业务实体稳定标识
    payload_json TEXT NOT NULL CHECK(json_valid(payload_json)), -- 当前单条实体的强类型JSON内容，不包含其他实体集合
    revision INTEGER NOT NULL DEFAULT 1, -- 当前记录的乐观版本号
    updated_at TEXT NOT NULL, -- 最近写入时间
    run_id TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.ai_run_id')) STORED, -- 关联AI运行标识
    UNIQUE(owner_id,id)
);
CREATE INDEX IF NOT EXISTS idx_ar_tools_run_id ON auto_reply_tools(owner_id,run_id);

CREATE TABLE IF NOT EXISTS auto_reply_suggestions (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT, -- 数据首次写入顺序，消息按此稳定排序
    owner_id TEXT NOT NULL, -- 已认证用户的本地隔离键
    id TEXT NOT NULL, -- 业务实体稳定标识
    payload_json TEXT NOT NULL CHECK(json_valid(payload_json)), -- 当前单条实体的强类型JSON内容，不包含其他实体集合
    revision INTEGER NOT NULL DEFAULT 1, -- 当前记录的乐观版本号
    updated_at TEXT NOT NULL, -- 最近写入时间
    conversation_id TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.conversation_id')) STORED, -- 本地会话标识
    position_id TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.position_id')) STORED, -- 岗位标识
    UNIQUE(owner_id,id)
);
CREATE INDEX IF NOT EXISTS idx_ar_suggestions_conversation_id ON auto_reply_suggestions(owner_id,conversation_id);
CREATE INDEX IF NOT EXISTS idx_ar_suggestions_position_id ON auto_reply_suggestions(owner_id,position_id);

CREATE TABLE IF NOT EXISTS auto_reply_reports (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT, -- 数据首次写入顺序，消息按此稳定排序
    owner_id TEXT NOT NULL, -- 已认证用户的本地隔离键
    id TEXT NOT NULL, -- 业务实体稳定标识
    payload_json TEXT NOT NULL CHECK(json_valid(payload_json)), -- 当前单条实体的强类型JSON内容，不包含其他实体集合
    revision INTEGER NOT NULL DEFAULT 1, -- 当前记录的乐观版本号
    updated_at TEXT NOT NULL, -- 最近写入时间
    UNIQUE(owner_id,id)
);

CREATE TABLE IF NOT EXISTS auto_reply_recommendations (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT, -- 数据首次写入顺序，消息按此稳定排序
    owner_id TEXT NOT NULL, -- 已认证用户的本地隔离键
    id TEXT NOT NULL, -- 业务实体稳定标识
    payload_json TEXT NOT NULL CHECK(json_valid(payload_json)), -- 当前单条实体的强类型JSON内容，不包含其他实体集合
    revision INTEGER NOT NULL DEFAULT 1, -- 当前记录的乐观版本号
    updated_at TEXT NOT NULL, -- 最近写入时间
    status TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.status')) STORED, -- 持久任务状态
    UNIQUE(owner_id,id)
);
CREATE INDEX IF NOT EXISTS idx_ar_recommendations_status ON auto_reply_recommendations(owner_id,status);

CREATE TABLE IF NOT EXISTS auto_reply_outbox (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT, -- 数据首次写入顺序，消息按此稳定排序
    owner_id TEXT NOT NULL, -- 已认证用户的本地隔离键
    id TEXT NOT NULL, -- 业务实体稳定标识
    payload_json TEXT NOT NULL CHECK(json_valid(payload_json)), -- 当前单条实体的强类型JSON内容，不包含其他实体集合
    revision INTEGER NOT NULL DEFAULT 1, -- 当前记录的乐观版本号
    updated_at TEXT NOT NULL, -- 最近写入时间
    status TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.status')) STORED, -- 持久任务状态
    UNIQUE(owner_id,id)
);
CREATE INDEX IF NOT EXISTS idx_ar_outbox_status ON auto_reply_outbox(owner_id,status);

CREATE TABLE IF NOT EXISTS auto_reply_cloud_links (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT, -- 数据首次写入顺序，消息按此稳定排序
    owner_id TEXT NOT NULL, -- 已认证用户的本地隔离键
    id TEXT NOT NULL, -- 业务实体稳定标识
    payload_json TEXT NOT NULL CHECK(json_valid(payload_json)), -- 当前单条实体的强类型JSON内容，不包含其他实体集合
    revision INTEGER NOT NULL DEFAULT 1, -- 当前记录的乐观版本号
    updated_at TEXT NOT NULL, -- 最近写入时间
    UNIQUE(owner_id,id)
);

CREATE TABLE IF NOT EXISTS auto_reply_sends (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT, -- 数据首次写入顺序，消息按此稳定排序
    owner_id TEXT NOT NULL, -- 已认证用户的本地隔离键
    id TEXT NOT NULL, -- 业务实体稳定标识
    payload_json TEXT NOT NULL CHECK(json_valid(payload_json)), -- 当前单条实体的强类型JSON内容，不包含其他实体集合
    revision INTEGER NOT NULL DEFAULT 1, -- 当前记录的乐观版本号
    updated_at TEXT NOT NULL, -- 最近写入时间
    conversation_id TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.conversation_id')) STORED, -- 本地会话标识
    message_key TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.message_key')) STORED, -- 平台消息键或稳定指纹
    status TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.status')) STORED, -- 持久任务状态
    UNIQUE(owner_id,id)
);
CREATE INDEX IF NOT EXISTS idx_ar_sends_conversation_id ON auto_reply_sends(owner_id,conversation_id);
CREATE INDEX IF NOT EXISTS idx_ar_sends_message_key ON auto_reply_sends(owner_id,message_key);
CREATE INDEX IF NOT EXISTS idx_ar_sends_status ON auto_reply_sends(owner_id,status);

CREATE TABLE IF NOT EXISTS auto_reply_migrations (
    owner_id TEXT PRIMARY KEY, -- 已迁移账号的隔离键
    source_sha256 TEXT NOT NULL, -- 旧账号快照原文校验值
    migrated_at TEXT NOT NULL -- 全部实体原子导入完成时间
);

CREATE TABLE IF NOT EXISTS auto_reply_proposal_receipts (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT, -- 判断提交的首次顺序
 owner_id TEXT NOT NULL, -- 已认证用户的本地隔离键
 id TEXT NOT NULL, -- 会话消息与配置版本的稳定收据标识
 payload_json TEXT NOT NULL CHECK(json_valid(payload_json)), -- 单次已提交判断的恢复信息，不保存凭证
 revision INTEGER NOT NULL DEFAULT 1, -- 收据状态版本
 updated_at TEXT NOT NULL, -- 最近状态写入时间
 status TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.status')) STORED, -- 是否已检查并登记后续推荐
 UNIQUE(owner_id,id)
);
CREATE INDEX IF NOT EXISTS idx_ar_proposal_pending ON auto_reply_proposal_receipts(owner_id,status);
