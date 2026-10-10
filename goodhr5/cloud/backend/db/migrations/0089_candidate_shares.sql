-- 文件作用说明：保存长期有效的简历分享令牌摘要，取消分享或删除简历后立即失效。
CREATE TABLE IF NOT EXISTS candidate_shares (
    token_hash TEXT PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    candidate_id UUID NOT NULL REFERENCES candidate_profiles(id) ON DELETE CASCADE,
    user_email TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE candidate_shares IS '简历公开只读分享映射，仅保存令牌摘要，持续有效直到取消或删除简历';
COMMENT ON COLUMN candidate_shares.token_hash IS '随机分享令牌的 SHA256 摘要，明文令牌不落库';
COMMENT ON COLUMN candidate_shares.tenant_id IS '被分享简历所属团队编号';
COMMENT ON COLUMN candidate_shares.candidate_id IS '被分享的候选人简历编号，删除简历时级联撤销';
COMMENT ON COLUMN candidate_shares.user_email IS '创建分享链接的登录用户邮箱，仅供内部审计';
COMMENT ON COLUMN candidate_shares.created_at IS '分享链接创建时间';
CREATE INDEX IF NOT EXISTS idx_candidate_shares_candidate ON candidate_shares(tenant_id, candidate_id);
