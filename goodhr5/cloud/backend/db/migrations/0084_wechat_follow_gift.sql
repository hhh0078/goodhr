-- 本文件新增公众号当前绑定、一次性绑定码、关注指纹和永久会员奖励账本；不包含公众号凭据。
BEGIN;

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS wechat_app_id TEXT,
    ADD COLUMN IF NOT EXISTS wechat_openid TEXT,
    ADD COLUMN IF NOT EXISTS wechat_bound_at TIMESTAMPTZ;

COMMENT ON COLUMN users.wechat_app_id IS '当前绑定公众号标识；取消关注后清空';
COMMENT ON COLUMN users.wechat_openid IS '当前绑定微信用户原始OpenID，仅用于当前绑定；取消关注后清空';
COMMENT ON COLUMN users.wechat_bound_at IS '当前公众号绑定完成时间；取消关注后清空';

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_wechat_current_openid
    ON users (wechat_app_id,wechat_openid)
    WHERE wechat_app_id IS NOT NULL AND wechat_openid IS NOT NULL AND wechat_openid <> '';

CREATE TABLE IF NOT EXISTS wechat_follow_states (
    app_id TEXT NOT NULL,
    openid_fingerprint TEXT NOT NULL CHECK (openid_fingerprint ~ '^[0-9a-f]{64}$'),
    followed BOOLEAN NOT NULL,
    event_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (app_id,openid_fingerprint)
);

COMMENT ON TABLE wechat_follow_states IS '公众号最近关注状态，仅保存单向OpenID指纹，不保存原始OpenID';
COMMENT ON COLUMN wechat_follow_states.app_id IS '公众号稳定标识，用于隔离不同公众号的用户身份';
COMMENT ON COLUMN wechat_follow_states.openid_fingerprint IS '原始OpenID的SHA256单向摘要';
COMMENT ON COLUMN wechat_follow_states.followed IS '最近一次有效事件是否为关注';
COMMENT ON COLUMN wechat_follow_states.event_at IS '最近一次有效关注事件时间，旧事件不得覆盖新状态';

CREATE TABLE IF NOT EXISTS wechat_follow_binding_codes (
    code_hash TEXT PRIMARY KEY CHECK (code_hash ~ '^[0-9a-f]{64}$'),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_email TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    invalidated_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    completed_app_id TEXT,
    completed_openid_fingerprint TEXT CHECK (completed_openid_fingerprint IS NULL OR completed_openid_fingerprint ~ '^[0-9a-f]{64}$')
);

COMMENT ON TABLE wechat_follow_binding_codes IS '公众号一次性绑定码摘要及完成记录，用于阻止旧码和安全处理重复回调';
COMMENT ON COLUMN wechat_follow_binding_codes.code_hash IS '一次性绑定码的SHA256摘要，不保存原始码';
COMMENT ON COLUMN wechat_follow_binding_codes.user_id IS '绑定码对应的云端用户ID';
COMMENT ON COLUMN wechat_follow_binding_codes.user_email IS '绑定码所属的规范化邮箱';
COMMENT ON COLUMN wechat_follow_binding_codes.expires_at IS '未完成绑定码的失效时间';
COMMENT ON COLUMN wechat_follow_binding_codes.created_at IS '绑定码摘要创建时间';
COMMENT ON COLUMN wechat_follow_binding_codes.invalidated_at IS '被同账号新码替代的时间；空值表示尚未作废';
COMMENT ON COLUMN wechat_follow_binding_codes.completed_at IS '绑定码成功消费时间；空值表示未完成';
COMMENT ON COLUMN wechat_follow_binding_codes.completed_app_id IS '成功消费该码的公众号标识，用于安全处理重复回调';
COMMENT ON COLUMN wechat_follow_binding_codes.completed_openid_fingerprint IS '成功消费该码的微信用户指纹，不保存原始OpenID';

CREATE INDEX IF NOT EXISTS idx_wechat_follow_binding_pending_email
    ON wechat_follow_binding_codes (user_email)
    WHERE completed_at IS NULL AND invalidated_at IS NULL;

CREATE TABLE IF NOT EXISTS wechat_follow_rewards (
    user_email TEXT PRIMARY KEY,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    app_id TEXT NOT NULL,
    openid_fingerprint TEXT NOT NULL CHECK (openid_fingerprint ~ '^[0-9a-f]{64}$'),
    duration_days INTEGER NOT NULL CHECK (duration_days > 0),
    member_type TEXT NOT NULL,
    subscription_expires_at TIMESTAMPTZ NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (app_id,openid_fingerprint)
);

COMMENT ON TABLE wechat_follow_rewards IS '公众号一次性会员奖励永久账本；取消关注或删除用户不能清除防重复记录';
COMMENT ON COLUMN wechat_follow_rewards.user_email IS '实际领取奖励的规范化邮箱，一邮箱仅领取一次';
COMMENT ON COLUMN wechat_follow_rewards.user_id IS '实际领取用户ID，用户删除后保留邮箱账本并将ID置空';
COMMENT ON COLUMN wechat_follow_rewards.app_id IS '领取奖励时的公众号稳定标识';
COMMENT ON COLUMN wechat_follow_rewards.openid_fingerprint IS '实际领取微信用户的单向OpenID指纹，同一公众号微信用户仅领取一次';
COMMENT ON COLUMN wechat_follow_rewards.duration_days IS '本次赠送的会员天数';
COMMENT ON COLUMN wechat_follow_rewards.member_type IS '本次赠送后的会员类型，保留有效Plus或Max会员类型';
COMMENT ON COLUMN wechat_follow_rewards.subscription_expires_at IS '本次奖励生效后的会员到期时间';
COMMENT ON COLUMN wechat_follow_rewards.applied_at IS '奖励实际入账时间';

INSERT INTO system_configs (config_key,config_value,description,enabled)
VALUES (
    'system.wechat_official_account',
    '{"enabled":false,"account_name":"GoodHR","app_id":"","original_id":"","token":"","encoding_aes_key":"","qr_image_url":""}'::jsonb,
    '公众号关注绑定配置，凭据仅允许超级管理员查看和保存',
    true
)
ON CONFLICT (config_key) DO NOTHING;

COMMIT;
