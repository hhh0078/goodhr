-- 持久保存登录会话，避免后端重启或 Redis 缓存丢失导致未满 30 天提前退出。
CREATE TABLE IF NOT EXISTS auth_sessions (
    token_hash TEXT PRIMARY KEY,
    email TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);
COMMENT ON TABLE auth_sessions IS '登录会话持久记录，保留登录起固定30天有效期';
COMMENT ON COLUMN auth_sessions.token_hash IS '登录凭证的SHA256摘要，不存明文Token';
COMMENT ON COLUMN auth_sessions.email IS '登录用户邮箱';
COMMENT ON COLUMN auth_sessions.created_at IS '本次登录凭证签发时间';
COMMENT ON COLUMN auth_sessions.expires_at IS '本次登录凭证绝对到期时间，访问不续期';
CREATE INDEX IF NOT EXISTS auth_sessions_expires_at_idx ON auth_sessions (expires_at);
