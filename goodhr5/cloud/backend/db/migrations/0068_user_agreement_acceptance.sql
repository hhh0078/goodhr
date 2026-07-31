-- 本迁移为用户表增加协议同意时间，用于控制登录前协议确认只出现一次。
ALTER TABLE users
ADD COLUMN IF NOT EXISTS agreement_accepted_at TIMESTAMPTZ;

COMMENT ON COLUMN users.agreement_accepted_at IS '用户首次同意 GoodHR 使用协议与隐私说明的时间';
