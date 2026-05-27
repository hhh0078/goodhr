-- 本迁移新增邀请奖励、用户邀请人字段和会员激活码表。
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS inviter_id UUID REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS invite_registered_rewarded_at TIMESTAMPTZ;

COMMENT ON COLUMN users.inviter_id IS '邀请人用户ID，通过邀请链接注册时写入';
COMMENT ON COLUMN users.invite_registered_rewarded_at IS '邀请注册奖励发放时间，避免重复发放';

-- activation_codes 保存超管生成的会员激活码。
CREATE TABLE IF NOT EXISTS activation_codes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code TEXT NOT NULL UNIQUE,
    days INTEGER NOT NULL,
    remark TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'unused',
    used_by UUID REFERENCES users(id) ON DELETE SET NULL,
    used_by_email TEXT NOT NULL DEFAULT '',
    used_at TIMESTAMPTZ,
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE activation_codes IS '会员激活码表';
COMMENT ON COLUMN activation_codes.code IS '激活码内容，用户输入后兑换会员天数';
COMMENT ON COLUMN activation_codes.days IS '激活码可兑换的会员天数';
COMMENT ON COLUMN activation_codes.remark IS '超管生成激活码时填写的备注';
COMMENT ON COLUMN activation_codes.status IS '激活码状态，unused未使用，used已使用';
COMMENT ON COLUMN activation_codes.used_by IS '使用激活码的用户ID';
COMMENT ON COLUMN activation_codes.used_by_email IS '使用激活码的用户邮箱';
COMMENT ON COLUMN activation_codes.used_at IS '激活码使用时间';
COMMENT ON COLUMN activation_codes.created_by IS '创建激活码的超级管理员邮箱';

CREATE INDEX IF NOT EXISTS idx_users_inviter_id ON users(inviter_id);
CREATE INDEX IF NOT EXISTS idx_activation_codes_status ON activation_codes(status);
CREATE INDEX IF NOT EXISTS idx_activation_codes_used_by ON activation_codes(used_by);

INSERT INTO system_configs (config_key, config_value, description, enabled)
VALUES (
  'system.invite_config',
  '{
    "register_reward_days": 3,
    "paid_month_reward_days": 5,
    "activity_title": "邀请好友奖励会员天数",
    "activity_description": "邀请好友注册成功后，邀请人可获得注册奖励；好友充值会员后，邀请人还可按购买月份获得额外会员天数。"
  }'::jsonb,
  '邀请奖励配置，包含注册奖励天数和按月充值奖励天数',
  true
)
ON CONFLICT (config_key) DO UPDATE
SET config_value = EXCLUDED.config_value,
    description = EXCLUDED.description,
    enabled = EXCLUDED.enabled;
