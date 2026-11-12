-- 0086: WorkBuddy 连接器 OAuth 授权数据表。
-- 用途：保存动态注册的 MCP 客户端、授权码和刷新令牌，支撑 WorkBuddy 开放平台 server-side OAuth 接入。

CREATE TABLE IF NOT EXISTS workbuddy_oauth_clients (
    client_id      TEXT PRIMARY KEY,              -- OAuth 客户端 ID，动态注册生成
    client_name    TEXT NOT NULL DEFAULT '',      -- 客户端名称，展示在授权确认页
    redirect_uris  TEXT NOT NULL DEFAULT '',      -- 允许的回调地址列表，JSON 数组字符串
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(), -- 注册时间
    last_used_at   TIMESTAMPTZ                    -- 最近一次授权使用时间
);
COMMENT ON TABLE workbuddy_oauth_clients IS 'WorkBuddy 连接器 OAuth 动态注册客户端表';
COMMENT ON COLUMN workbuddy_oauth_clients.client_id IS 'OAuth 客户端 ID，动态注册生成';
COMMENT ON COLUMN workbuddy_oauth_clients.client_name IS '客户端名称，展示在授权确认页';
COMMENT ON COLUMN workbuddy_oauth_clients.redirect_uris IS '允许的回调地址列表，JSON 数组字符串';
COMMENT ON COLUMN workbuddy_oauth_clients.created_at IS '注册时间';
COMMENT ON COLUMN workbuddy_oauth_clients.last_used_at IS '最近一次授权使用时间';

CREATE TABLE IF NOT EXISTS workbuddy_oauth_codes (
    code            TEXT PRIMARY KEY,             -- 授权码本身
    client_id       TEXT NOT NULL,                -- 发起授权的客户端 ID
    user_email      TEXT NOT NULL,                -- 同意授权的云端用户邮箱
    redirect_uri    TEXT NOT NULL,                -- 本次授权允许的回调地址
    scope           TEXT NOT NULL DEFAULT '',     -- 授权范围
    code_challenge  TEXT NOT NULL DEFAULT '',     -- PKCE S256 的 code_challenge
    expires_at      TIMESTAMPTZ NOT NULL,         -- 过期时间，默认 5 分钟
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now() -- 创建时间
);
COMMENT ON TABLE workbuddy_oauth_codes IS 'WorkBuddy 连接器 OAuth 一次性授权码表';
COMMENT ON COLUMN workbuddy_oauth_codes.code IS '授权码本身';
COMMENT ON COLUMN workbuddy_oauth_codes.client_id IS '发起授权的客户端 ID';
COMMENT ON COLUMN workbuddy_oauth_codes.user_email IS '同意授权的云端用户邮箱';
COMMENT ON COLUMN workbuddy_oauth_codes.redirect_uri IS '本次授权允许的回调地址';
COMMENT ON COLUMN workbuddy_oauth_codes.scope IS '授权范围';
COMMENT ON COLUMN workbuddy_oauth_codes.code_challenge IS 'PKCE S256 的 code_challenge';
COMMENT ON COLUMN workbuddy_oauth_codes.expires_at IS '过期时间，默认 5 分钟';
COMMENT ON COLUMN workbuddy_oauth_codes.created_at IS '创建时间';

CREATE TABLE IF NOT EXISTS workbuddy_oauth_refresh_tokens (
    token       TEXT PRIMARY KEY,                 -- 刷新令牌
    client_id   TEXT NOT NULL,                    -- 所属客户端 ID
    user_email  TEXT NOT NULL,                    -- 所属用户邮箱
    scope       TEXT NOT NULL DEFAULT '',         -- 授权范围
    expires_at  TIMESTAMPTZ NOT NULL,             -- 过期时间
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(), -- 创建时间
    rotated_at  TIMESTAMPTZ                       -- 被轮换消费的时间，非空表示已失效
);
COMMENT ON TABLE workbuddy_oauth_refresh_tokens IS 'WorkBuddy 连接器 OAuth 刷新令牌表';
COMMENT ON COLUMN workbuddy_oauth_refresh_tokens.token IS '刷新令牌';
COMMENT ON COLUMN workbuddy_oauth_refresh_tokens.client_id IS '所属客户端 ID';
COMMENT ON COLUMN workbuddy_oauth_refresh_tokens.user_email IS '所属用户邮箱';
COMMENT ON COLUMN workbuddy_oauth_refresh_tokens.scope IS '授权范围';
COMMENT ON COLUMN workbuddy_oauth_refresh_tokens.expires_at IS '过期时间';
COMMENT ON COLUMN workbuddy_oauth_refresh_tokens.created_at IS '创建时间';
COMMENT ON COLUMN workbuddy_oauth_refresh_tokens.rotated_at IS '被轮换消费的时间，非空表示已失效';

CREATE INDEX IF NOT EXISTS idx_workbuddy_oauth_refresh_user ON workbuddy_oauth_refresh_tokens (user_email);
