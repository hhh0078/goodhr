-- 文件作用说明：保存公众号免密登录身份与现有账号的关联，保留邮箱登录和会员、岗位等原有归属。
CREATE TABLE IF NOT EXISTS wechat_login_identities (
    app_id TEXT NOT NULL,
    openid_fingerprint TEXT NOT NULL CHECK (openid_fingerprint ~ '^[a-f0-9]{64}$'),
    user_email TEXT NOT NULL REFERENCES users(email) ON UPDATE CASCADE ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (app_id, openid_fingerprint)
);
COMMENT ON TABLE wechat_login_identities IS '公众号登录身份关联，取消关注不删除账号及其数据，原始OpenID不保存在此表';
COMMENT ON COLUMN wechat_login_identities.app_id IS '公众号AppID，同一微信在不同公众号下分别核验';
COMMENT ON COLUMN wechat_login_identities.openid_fingerprint IS '经过回调签名验证的OpenID的SHA256指纹';
COMMENT ON COLUMN wechat_login_identities.user_email IS '复用现有账号关联键，邮箱账号为真实邮箱，免邮箱账号为随机内部标识';
COMMENT ON COLUMN wechat_login_identities.created_at IS '首次保存登录身份关联的时间';
