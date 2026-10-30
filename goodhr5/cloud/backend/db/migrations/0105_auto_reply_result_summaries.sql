-- 本迁移保存本地生成且获岗位授权的结构化报告和简短通知状态，不保存聊天、附件或AI输入输出。
CREATE TABLE IF NOT EXISTS candidate_auto_reply_reports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    public_id TEXT NOT NULL UNIQUE,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    candidate_id UUID NOT NULL REFERENCES candidate_profiles(id) ON DELETE CASCADE,
    position_id UUID NOT NULL REFERENCES positions(id) ON DELETE CASCADE,
    version INTEGER NOT NULL CHECK(version > 0),
    input_hash TEXT NOT NULL,
    report_data JSONB NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    token_usage INTEGER NOT NULL DEFAULT 0 CHECK(token_usage >= 0),
    share_enabled BOOLEAN NOT NULL DEFAULT false,
    generated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id,candidate_id,position_id,input_hash)
);
COMMENT ON TABLE candidate_auto_reply_reports IS '本地生成并经岗位结构化开关授权同步的自动回复推荐报告，与人工推荐关系表独立';
COMMENT ON COLUMN candidate_auto_reply_reports.id IS '自动回复报告唯一标识';
COMMENT ON COLUMN candidate_auto_reply_reports.public_id IS '随机公开分享标识，默认关闭分享';
COMMENT ON COLUMN candidate_auto_reply_reports.tenant_id IS '报告所属团队';
COMMENT ON COLUMN candidate_auto_reply_reports.candidate_id IS '授权入库的候选人';
COMMENT ON COLUMN candidate_auto_reply_reports.position_id IS '报告关联的授权岗位';
COMMENT ON COLUMN candidate_auto_reply_reports.version IS '同候选人与岗位的报告版本';
COMMENT ON COLUMN candidate_auto_reply_reports.input_hash IS '本地输入摘要，仅用于重试去重';
COMMENT ON COLUMN candidate_auto_reply_reports.report_data IS '本地生成的结构化报告，不含聊天消息数组和原始附件';
COMMENT ON COLUMN candidate_auto_reply_reports.model IS '本地使用的模型名称';
COMMENT ON COLUMN candidate_auto_reply_reports.token_usage IS '本地报告生成使用的令牌数量';
COMMENT ON COLUMN candidate_auto_reply_reports.share_enabled IS '用户是否明确启用公开分享';
COMMENT ON COLUMN candidate_auto_reply_reports.generated_at IS '本地报告生成时间';
COMMENT ON COLUMN candidate_auto_reply_reports.created_at IS '报告同步时间';
COMMENT ON COLUMN candidate_auto_reply_reports.updated_at IS '分享状态最近更新时间';
CREATE INDEX IF NOT EXISTS idx_auto_reply_reports_candidate ON candidate_auto_reply_reports(tenant_id,candidate_id,created_at DESC);

CREATE TABLE IF NOT EXISTS auto_reply_notice_summaries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    position_id UUID NOT NULL REFERENCES positions(id) ON DELETE CASCADE,
    recipient_email TEXT NOT NULL,
    dedupe_key TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','sent','failed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id,recipient_email,dedupe_key)
);
COMMENT ON TABLE auto_reply_notice_summaries IS '自动回复人工接管邮件去重及状态，不保存候选人消息正文';
COMMENT ON COLUMN auto_reply_notice_summaries.id IS '通知记录唯一标识';
COMMENT ON COLUMN auto_reply_notice_summaries.tenant_id IS '通知所属团队';
COMMENT ON COLUMN auto_reply_notice_summaries.position_id IS '通知关联岗位';
COMMENT ON COLUMN auto_reply_notice_summaries.recipient_email IS '已登录且拥有岗位的收件人邮箱';
COMMENT ON COLUMN auto_reply_notice_summaries.dedupe_key IS '本地通知标识的不可反推摘要';
COMMENT ON COLUMN auto_reply_notice_summaries.status IS '邮件状态：pending发送中、sent已发送、failed失败';
COMMENT ON COLUMN auto_reply_notice_summaries.created_at IS '通知登记时间';
COMMENT ON COLUMN auto_reply_notice_summaries.updated_at IS '通知状态最近更新时间';
