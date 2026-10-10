-- 本文件新增团队招聘流程模板、候选人岗位流程快照与受限面试邀请；不转换或覆盖历史推荐。
CREATE TABLE IF NOT EXISTS recruitment_workflows (
 id TEXT PRIMARY KEY,
 tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 is_default BOOLEAN NOT NULL DEFAULT FALSE,
 revision BIGINT NOT NULL DEFAULT 1,
 payload JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE recruitment_workflows IS '团队可复用招聘流程模板，模板更新不改变实例快照';
COMMENT ON COLUMN recruitment_workflows.id IS '流程模板随机编号';
COMMENT ON COLUMN recruitment_workflows.tenant_id IS '所属团队编号';
COMMENT ON COLUMN recruitment_workflows.is_default IS '是否为团队默认流程';
COMMENT ON COLUMN recruitment_workflows.revision IS '并发修改校验修订号';
COMMENT ON COLUMN recruitment_workflows.payload IS '流程名称版本和有序节点人员配置';
COMMENT ON COLUMN recruitment_workflows.created_at IS '创建时间';
COMMENT ON COLUMN recruitment_workflows.updated_at IS '最后更新时间';
CREATE UNIQUE INDEX IF NOT EXISTS idx_recruitment_default ON recruitment_workflows(tenant_id) WHERE is_default;
CREATE TABLE IF NOT EXISTS recruitment_applications (
 id TEXT PRIMARY KEY,
 tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 candidate_id UUID NOT NULL,
 position_key TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'active',
 revision BIGINT NOT NULL DEFAULT 1,
 payload JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE recruitment_applications IS '人工启用的候选人岗位HR流程实例，保留独立模板和节点快照';
COMMENT ON COLUMN recruitment_applications.id IS '应聘流程随机编号';
COMMENT ON COLUMN recruitment_applications.tenant_id IS '所属团队编号';
COMMENT ON COLUMN recruitment_applications.candidate_id IS '候选人编号快照，原简历删除后仍保留招聘历史但禁止向旧地址新发邮件';
COMMENT ON COLUMN recruitment_applications.position_key IS '岗位编号快照，岗位删除后仍保留招聘历史';
COMMENT ON COLUMN recruitment_applications.status IS '实例状态：进行中、完成、淘汰或撤回';
COMMENT ON COLUMN recruitment_applications.revision IS '并发动作校验修订号';
COMMENT ON COLUMN recruitment_applications.payload IS '候选人岗位概要、流程版本、节点状态、面试场次、面评和业务历史';
COMMENT ON COLUMN recruitment_applications.created_at IS '明确启用流程时间';
COMMENT ON COLUMN recruitment_applications.updated_at IS '最后推进时间';
CREATE UNIQUE INDEX IF NOT EXISTS idx_recruitment_active_application ON recruitment_applications(tenant_id,candidate_id,position_key) WHERE status='active';
CREATE INDEX IF NOT EXISTS idx_recruitment_application_team_time ON recruitment_applications(tenant_id,updated_at DESC);
CREATE TABLE IF NOT EXISTS recruitment_invitations (
 token_hash TEXT PRIMARY KEY,
 tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 application_id TEXT NOT NULL REFERENCES recruitment_applications(id) ON DELETE CASCADE,
 interview_id TEXT NOT NULL,
 interview_revision BIGINT NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE recruitment_invitations IS '候选人面试确认链接哈希，排期变更或取消后旧版本失效';
COMMENT ON COLUMN recruitment_invitations.token_hash IS '随机邀请令牌SHA256哈希，不保存明文令牌';
COMMENT ON COLUMN recruitment_invitations.tenant_id IS '所属团队编号';
COMMENT ON COLUMN recruitment_invitations.application_id IS '所属应聘流程编号';
COMMENT ON COLUMN recruitment_invitations.interview_id IS '单场面试编号';
COMMENT ON COLUMN recruitment_invitations.interview_revision IS '发邀请时场次版本，仅该版本可确认';
COMMENT ON COLUMN recruitment_invitations.expires_at IS '邀请链接到期时间';
COMMENT ON COLUMN recruitment_invitations.created_at IS '邀请链接生成时间';
CREATE INDEX IF NOT EXISTS idx_recruitment_invitation_expiry ON recruitment_invitations(expires_at);
