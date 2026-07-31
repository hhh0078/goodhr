-- 本迁移为历史已支付订单补齐会员权益发放记录，防止支付平台重复通知时再次增加会员时间。
INSERT INTO subscription_order_grants (
    order_no,
    grant_type,
    user_id,
    user_email,
    member_type,
    duration_days,
    replace_from_now,
    subscription_expires_at,
    applied_at
)
SELECT
    payment.order_no,
    'buyer',
    payment.user_id,
    payment.user_email,
    COALESCE(NULLIF(LOWER(payment.member_type), ''), NULLIF(LOWER(account.subscription->>'member_type'), ''), 'max'),
    payment.duration_days,
    false,
    COALESCE(NULLIF(account.subscription->>'expires_at', '')::timestamptz, payment.paid_at, payment.updated_at),
    COALESCE(payment.paid_at, payment.updated_at)
FROM payment_orders AS payment
INNER JOIN users AS account ON account.id = payment.user_id
WHERE payment.status = 'paid'
  AND COALESCE(payment.order_type, 'subscription') = 'subscription'
  AND payment.duration_days > 0
ON CONFLICT (order_no, grant_type) DO NOTHING;

INSERT INTO subscription_order_grants (
    order_no,
    grant_type,
    user_id,
    user_email,
    member_type,
    duration_days,
    replace_from_now,
    subscription_expires_at,
    applied_at
)
SELECT
    payment.order_no,
    'inviter',
    inviter.id,
    inviter.email,
    COALESCE(NULLIF(LOWER(inviter.subscription->>'member_type'), ''), 'max'),
    CASE
        WHEN invite_config.config_value->>'paid_month_reward_days' ~ '^[0-9]+$'
            THEN (invite_config.config_value->>'paid_month_reward_days')::integer
        ELSE 0
    END * GREATEST(payment.duration_days / 30, 1),
    false,
    COALESCE(NULLIF(inviter.subscription->>'expires_at', '')::timestamptz, payment.paid_at, payment.updated_at),
    COALESCE(payment.paid_at, payment.updated_at)
FROM payment_orders AS payment
INNER JOIN users AS invitee ON invitee.id = payment.user_id
INNER JOIN users AS inviter ON inviter.id = invitee.inviter_id
LEFT JOIN system_configs AS invite_config ON invite_config.config_key = 'system.invite_config'
WHERE payment.status = 'paid'
  AND COALESCE(payment.order_type, 'subscription') = 'subscription'
  AND payment.duration_days > 0
ON CONFLICT (order_no, grant_type) DO NOTHING;
