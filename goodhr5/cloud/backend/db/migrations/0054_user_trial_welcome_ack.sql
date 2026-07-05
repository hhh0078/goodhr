-- 本迁移用于记录用户是否已确认新用户 3 天体验会员到账提醒。
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS trial_welcome_ack_at TIMESTAMPTZ;

COMMENT ON COLUMN users.trial_welcome_ack_at IS '新用户3天体验会员到账弹框确认时间';

UPDATE users
SET trial_welcome_ack_at = COALESCE(trial_welcome_ack_at, now())
WHERE trial_welcome_ack_at IS NULL;
