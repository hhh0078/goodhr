-- 本迁移在公众号永久奖励账本上增加可靠邮件通知状态，既有奖励跳过，迁移后新旧后端新增奖励均待通知。
BEGIN;

ALTER TABLE wechat_follow_rewards
    ADD COLUMN IF NOT EXISTS mail_status TEXT NOT NULL DEFAULT 'skipped',
    ADD COLUMN IF NOT EXISTS mail_attempts INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS mail_next_attempt_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS mail_claim_token TEXT,
    ADD COLUMN IF NOT EXISTS mail_lease_until TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS mail_sent_at TIMESTAMPTZ;

-- 按当前表检查约束，使迁移执行成功但版本登记失败后的重复启动仍然安全。
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='wechat_follow_rewards'::regclass AND conname='wechat_reward_notice_status_check') THEN
        ALTER TABLE wechat_follow_rewards ADD CONSTRAINT wechat_reward_notice_status_check
            CHECK (mail_status IN ('skipped','pending','sending','retry','sent','failed','uncertain'));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='wechat_follow_rewards'::regclass AND conname='wechat_reward_notice_attempts_check') THEN
        ALTER TABLE wechat_follow_rewards ADD CONSTRAINT wechat_reward_notice_attempts_check CHECK (mail_attempts >= 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='wechat_follow_rewards'::regclass AND conname='wechat_reward_notice_token_check') THEN
        ALTER TABLE wechat_follow_rewards ADD CONSTRAINT wechat_reward_notice_token_check
            CHECK (mail_claim_token IS NULL OR mail_claim_token ~ '^[0-9a-f]{64}$');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='wechat_follow_rewards'::regclass AND conname='wechat_reward_notice_lease_check') THEN
        ALTER TABLE wechat_follow_rewards ADD CONSTRAINT wechat_reward_notice_lease_check CHECK (
            (mail_status='sending' AND mail_claim_token IS NOT NULL AND mail_lease_until IS NOT NULL)
            OR (mail_status<>'sending' AND mail_claim_token IS NULL AND mail_lease_until IS NULL)
        );
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='wechat_follow_rewards'::regclass AND conname='wechat_reward_notice_next_attempt_check') THEN
        ALTER TABLE wechat_follow_rewards ADD CONSTRAINT wechat_reward_notice_next_attempt_check
            CHECK (mail_status NOT IN ('pending','retry') OR mail_next_attempt_at IS NOT NULL);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='wechat_follow_rewards'::regclass AND conname='wechat_reward_notice_sent_at_check') THEN
        ALTER TABLE wechat_follow_rewards ADD CONSTRAINT wechat_reward_notice_sent_at_check CHECK (mail_status<>'sent' OR mail_sent_at IS NOT NULL);
    END IF;
END;
$$;

COMMENT ON COLUMN wechat_follow_rewards.mail_status IS '会员奖励邮件状态：旧记录skipped，待发送pending，已独占领取sending，待重试retry，SMTP已接受sent，明确失败failed，发送结果不确定uncertain';
COMMENT ON COLUMN wechat_follow_rewards.mail_attempts IS '邮件被独占领取的累计次数，非负，账号或邮箱失效时不增加次数';
COMMENT ON COLUMN wechat_follow_rewards.mail_next_attempt_at IS '待发送或安全重试通知的下次可领取时间，不确定通知不能自动重试';
COMMENT ON COLUMN wechat_follow_rewards.mail_claim_token IS '本次独占领取的256位随机令牌，旧工作进程不能覆盖新的发送结果';
COMMENT ON COLUMN wechat_follow_rewards.mail_lease_until IS '独占发送租约到期时间，过期发送标为不确定而非自动重发';
COMMENT ON COLUMN wechat_follow_rewards.mail_sent_at IS 'SMTP接受会员奖励通知的记录时间，不代表收件箱已送达';

CREATE INDEX IF NOT EXISTS idx_wechat_reward_notice_ready
    ON wechat_follow_rewards (mail_next_attempt_at,applied_at,user_email)
    WHERE mail_status IN ('pending','retry');
CREATE INDEX IF NOT EXISTS idx_wechat_reward_notice_sending_lease
    ON wechat_follow_rewards (mail_lease_until)
    WHERE mail_status='sending';

-- 初次添加列时既有行已得到skipped；只改变以后INSERT的默认值，不补发历史奖励。
-- 候选版本迁移之后、正式切换之前，旧后端仍可能发奖，默认pending保证该窗口及回退期间的通知不会丢失。
ALTER TABLE wechat_follow_rewards
    ALTER COLUMN mail_status SET DEFAULT 'pending',
    ALTER COLUMN mail_next_attempt_at SET DEFAULT now();

COMMIT;
