-- 本文件新增团队公司信息、独立招聘邮件模板与原子幂等发送账本，使用平台发件账号。
CREATE TABLE IF NOT EXISTS recruitment_company_settings (
 tenant_id UUID PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
 company_name TEXT NOT NULL DEFAULT '',
 contact_name TEXT NOT NULL DEFAULT '',
 contact_phone TEXT NOT NULL DEFAULT '',
 contact_email TEXT NOT NULL DEFAULT '',
 company_address TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE recruitment_company_settings IS '团队招聘公司署名与联系方式，首次创建时初始化团队邮件模板';
COMMENT ON COLUMN recruitment_company_settings.tenant_id IS '所属团队标识';
COMMENT ON COLUMN recruitment_company_settings.company_name IS '用于招聘邮件标题及正文的公司名称';
COMMENT ON COLUMN recruitment_company_settings.contact_name IS '招聘联系人姓名';
COMMENT ON COLUMN recruitment_company_settings.contact_phone IS '招聘联系电话，可不填';
COMMENT ON COLUMN recruitment_company_settings.contact_email IS '候选人回复招聘邮件的联系邮箱';
COMMENT ON COLUMN recruitment_company_settings.company_address IS '公司办公或入职报到地址';
COMMENT ON COLUMN recruitment_company_settings.created_at IS '首次创建时间';
COMMENT ON COLUMN recruitment_company_settings.updated_at IS '最近更新时间';

CREATE TABLE IF NOT EXISTS recruitment_mail_templates (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 name TEXT NOT NULL,
 category TEXT NOT NULL DEFAULT 'custom',
 subject TEXT NOT NULL,
 body TEXT NOT NULL,
 is_builtin BOOLEAN NOT NULL DEFAULT false,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE recruitment_mail_templates IS '团队独立维护的招聘纯文本邮件模板，预置模板可编辑删除';
COMMENT ON COLUMN recruitment_mail_templates.id IS '邮件模板标识';
COMMENT ON COLUMN recruitment_mail_templates.tenant_id IS '所属团队标识';
COMMENT ON COLUMN recruitment_mail_templates.name IS '模板名称';
COMMENT ON COLUMN recruitment_mail_templates.category IS '招聘场景分类';
COMMENT ON COLUMN recruitment_mail_templates.subject IS '包含中文占位符的邮件标题';
COMMENT ON COLUMN recruitment_mail_templates.body IS '包含中文占位符的纯文本正文';
COMMENT ON COLUMN recruitment_mail_templates.is_builtin IS '是否来源于初始化预置模板，仅作来源标记';
COMMENT ON COLUMN recruitment_mail_templates.updated_at IS '最近更新时间';
CREATE INDEX IF NOT EXISTS idx_recruitment_mail_templates_tenant ON recruitment_mail_templates(tenant_id);

CREATE TABLE IF NOT EXISTS recruitment_mail_deliveries (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 actor_email TEXT NOT NULL,
 application_id TEXT NOT NULL,
 interview_id TEXT NOT NULL DEFAULT '',
 template_id TEXT NOT NULL DEFAULT '',
 idempotency_key TEXT NOT NULL,
 fingerprint TEXT NOT NULL,
 snapshot JSONB NOT NULL,
 snapshot_ready BOOLEAN NOT NULL DEFAULT false,
 status TEXT NOT NULL DEFAULT 'sending' CHECK(status IN ('sending','sent','failed','uncertain')),
 error_message TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 finished_at TIMESTAMPTZ,
 UNIQUE(tenant_id,actor_email,idempotency_key)
);
COMMENT ON TABLE recruitment_mail_deliveries IS '招聘邮件发送快照与幂等账本，发送结果不确定时禁止自动重发';
COMMENT ON COLUMN recruitment_mail_deliveries.id IS '发送记录标识';
COMMENT ON COLUMN recruitment_mail_deliveries.tenant_id IS '所属团队标识';
COMMENT ON COLUMN recruitment_mail_deliveries.actor_email IS '发起发送的团队成员邮箱';
COMMENT ON COLUMN recruitment_mail_deliveries.application_id IS '关联候选人与岗位的应聘记录标识';
COMMENT ON COLUMN recruitment_mail_deliveries.interview_id IS '关联面试场次标识，可为空';
COMMENT ON COLUMN recruitment_mail_deliveries.template_id IS '使用的邮件模板标识快照，可为空';
COMMENT ON COLUMN recruitment_mail_deliveries.idempotency_key IS '客户端提交的发送幂等编号，限同团队同操作者';
COMMENT ON COLUMN recruitment_mail_deliveries.fingerprint IS '发送请求内容的SHA256指纹，拒绝同编号不同内容';
COMMENT ON COLUMN recruitment_mail_deliveries.snapshot IS '发送时收件人、回复邮箱、标题、纯文本、转义HTML及流程版本的不可变快照';
COMMENT ON COLUMN recruitment_mail_deliveries.snapshot_ready IS '最终确认链接与正文是否已持久保存，只有准备完成后才允许发送';
COMMENT ON COLUMN recruitment_mail_deliveries.status IS '发送结果：sending发送中、sent邮件服务已接收、failed明确失败、uncertain结果不确定';
COMMENT ON COLUMN recruitment_mail_deliveries.error_message IS '发送失败或结果不确定的简短说明，不含凭据';
COMMENT ON COLUMN recruitment_mail_deliveries.created_at IS '抢占发送权并持久保存快照的时间';
COMMENT ON COLUMN recruitment_mail_deliveries.finished_at IS '发送结果保存时间';
CREATE INDEX IF NOT EXISTS idx_recruitment_mail_deliveries_application ON recruitment_mail_deliveries(tenant_id,application_id,created_at DESC);
