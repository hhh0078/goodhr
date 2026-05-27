-- 本迁移为 Local Agent 增加公钥字段，用于 cookie 共享时为每台机器加密数据密钥。
ALTER TABLE local_agents
    ADD COLUMN IF NOT EXISTS public_key TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN local_agents.public_key IS 'Local Agent 公钥，用于为该机器加密 cookie 数据密钥';
