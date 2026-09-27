-- 文件作用说明：保存岗位排障链接的临时日志快照，链接只读并在七天后失效。
CREATE TABLE IF NOT EXISTS position_log_shares (
    token_hash TEXT PRIMARY KEY,
    position_id UUID NOT NULL UNIQUE REFERENCES positions(id) ON DELETE CASCADE,
    user_email TEXT NOT NULL,
    snapshot JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);
COMMENT ON TABLE position_log_shares IS '岗位排障分享链接的临时快照';
COMMENT ON COLUMN position_log_shares.token_hash IS '随机分享令牌的 SHA256 摘要，明文不落库';
COMMENT ON COLUMN position_log_shares.position_id IS '分享日志所属岗位标识';
COMMENT ON COLUMN position_log_shares.user_email IS '创建分享链接的岗位用户邮箱';
COMMENT ON COLUMN position_log_shares.snapshot IS '创建链接时固定的最多一千条脱敏岗位日志';
COMMENT ON COLUMN position_log_shares.created_at IS '分享链接创建时间';
COMMENT ON COLUMN position_log_shares.expires_at IS '分享链接过期时间';
