-- 本迁移把旧 Plus 会员兼容为 Max 全能版，并配置免费版、基础包月版和全能包年版。
ALTER TABLE users
    ALTER COLUMN subscription SET DEFAULT jsonb_build_object(
        'member_type', 'max',
        'expires_at', now() + interval '3 days'
    );

COMMENT ON COLUMN users.subscription IS '用户订阅信息JSON，包含member_type会员类型和expires_at到期时间；plus为基础版，max为全能版';

UPDATE users
SET subscription = jsonb_set(subscription, '{member_type}', '"max"'::jsonb, true)
WHERE LOWER(COALESCE(subscription->>'member_type', '')) = 'plus';

UPDATE payment_orders
SET status = 'closed',
    updated_at = now()
WHERE COALESCE(order_type, 'subscription') = 'subscription'
  AND status = 'pending';

ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS upgrade_from_member_type TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS upgrade_credit_cents INTEGER NOT NULL DEFAULT 0;

COMMENT ON COLUMN payment_orders.upgrade_from_member_type IS '升级订单原会员类型，例如plus；普通订阅订单为空';
COMMENT ON COLUMN payment_orders.upgrade_credit_cents IS '升级时按原会员剩余时间抵扣的金额，单位分';

CREATE TABLE IF NOT EXISTS subscription_order_grants (
    order_no TEXT NOT NULL REFERENCES payment_orders(order_no) ON DELETE CASCADE,
    grant_type TEXT NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_email TEXT NOT NULL,
    member_type TEXT NOT NULL,
    duration_days INTEGER NOT NULL,
    replace_from_now BOOLEAN NOT NULL DEFAULT false,
    subscription_expires_at TIMESTAMPTZ NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (order_no, grant_type)
);

COMMENT ON TABLE subscription_order_grants IS '支付订单会员权益发放记录，用于支付回调重试时避免重复增加会员时间';
COMMENT ON COLUMN subscription_order_grants.order_no IS '关联支付订单号';
COMMENT ON COLUMN subscription_order_grants.grant_type IS '权益类型：buyer购买者会员或inviter邀请人奖励';
COMMENT ON COLUMN subscription_order_grants.user_id IS '获得会员权益的用户ID';
COMMENT ON COLUMN subscription_order_grants.user_email IS '获得会员权益的用户邮箱';
COMMENT ON COLUMN subscription_order_grants.member_type IS '本次发放后的会员类型';
COMMENT ON COLUMN subscription_order_grants.duration_days IS '本次发放的会员天数';
COMMENT ON COLUMN subscription_order_grants.replace_from_now IS '是否从支付完成时间重新计算到期时间';
COMMENT ON COLUMN subscription_order_grants.subscription_expires_at IS '本次权益发放后的会员到期时间';
COMMENT ON COLUMN subscription_order_grants.applied_at IS '权益实际发放时间';

CREATE UNIQUE INDEX IF NOT EXISTS idx_ai_balance_records_recharge_order
    ON ai_balance_records(related_order_no)
    WHERE category = 'recharge' AND related_order_no <> '';

INSERT INTO system_configs (config_key, config_value, description, enabled)
VALUES (
    'system.subscription_plans',
    '[
      {
        "id": "free",
        "name": "永久免费版",
        "member_type": "free",
        "duration_days": 0,
        "original_price": 0,
        "discount_amount": 0,
        "allow_auto_reply": false,
        "features": ["多平台账号管理", "关键词筛选", "基础自动打招呼", "每天最多打100个招呼"],
        "description": "关键词筛选和基础自动打招呼可以永久免费使用。",
        "created_at": "2026-07-31"
      },
      {
        "id": "monthly",
        "name": "基础包月版",
        "member_type": "plus",
        "duration_days": 30,
        "original_price": 70,
        "discount_amount": 30,
        "allow_auto_reply": false,
        "features": ["多平台账号管理", "关键词筛选", "AI筛选与详情分析", "AI自动打招呼"],
        "description": "适合日常招聘使用，包含 AI 筛选和自动打招呼，不包含自动回复。",
        "created_at": "2026-07-31"
      },
      {
        "id": "yearly",
        "name": "全能包年版",
        "member_type": "max",
        "duration_days": 365,
        "original_price": 840,
        "discount_amount": 500,
        "allow_auto_reply": true,
        "features": ["多平台账号管理", "关键词筛选", "AI筛选与详情分析", "AI自动打招呼", "AI自动回复"],
        "description": "完整开放现有会员能力，包含自动回复。",
        "created_at": "2026-07-31"
      }
    ]'::jsonb,
    '订阅套餐配置，member_type决定会员类型，allow_auto_reply决定是否允许自动回复',
    true
)
ON CONFLICT (config_key) DO UPDATE
SET config_value = EXCLUDED.config_value,
    description = EXCLUDED.description,
    enabled = EXCLUDED.enabled,
    updated_at = now();

UPDATE system_configs
SET config_value = jsonb_set(
        config_value,
        '{cards}',
        COALESCE(config_value->'cards', '[]'::jsonb) || '[
          {
            "id": "subscription",
            "title": "会员版本",
            "summary": "免费版可做基础招聘，Plus 开放 AI，Max 再开放自动回复。",
            "content": "新用户注册赠送 3 天 Max 全能版。免费版可使用关键词筛选和基础自动打招呼；Plus 基础包月版 40 元/30 天，支持 AI 筛选和 AI 自动打招呼，不支持自动回复；Max 全能包年版 340 元/365 天，开放自动回复。有效 Plus 升级 Max 时，剩余时间会按 40 元/30 天精确抵扣，Max 从付款完成时间重新计算 365 天。有效 Max 暂时不能购买 Plus。"
          }
        ]'::jsonb,
        true
    ),
    updated_at = now()
WHERE config_key = 'system.guide'
  AND NOT EXISTS (
      SELECT 1
      FROM jsonb_array_elements(COALESCE(config_value->'cards', '[]'::jsonb)) AS card
      WHERE card->>'id' = 'subscription'
  );

UPDATE system_configs
SET config_value = jsonb_set(
        config_value,
        '{sections}',
        COALESCE(
            (
                SELECT jsonb_agg(
                    CASE
                        WHEN section->>'id' = 'subscription' THEN
                            jsonb_set(
                                section,
                                '{items}',
                                '[
                                  "新用户注册赠送 3 天 Max 全能版，可以体验包括自动回复在内的全部现有功能。",
                                  "免费版支持关键词筛选和基础自动打招呼；Plus 基础包月版支持 AI 筛选和 AI 自动打招呼；Max 全能包年版额外支持自动回复。",
                                  "Plus 升级 Max 时按 Plus 剩余时间折算抵扣，Max 到期时间从付款完成时间重新计算 365 天。",
                                  "开始 AI 岗位和自动回复前，前端、云端和本地程序都会按统一会员权限检查。",
                                  "支付记录用户可查看自己的记录，超级管理员可查看全部记录。"
                                ]'::jsonb,
                                true
                            )
                        ELSE section
                    END
                    ORDER BY section_index
                )
                FROM jsonb_array_elements(COALESCE(config_value->'sections', '[]'::jsonb))
                     WITH ORDINALITY AS sections(section, section_index)
            ),
            config_value->'sections'
        ),
        true
    ),
    updated_at = now()
WHERE config_key = 'system.guide'
  AND jsonb_typeof(config_value->'sections') = 'array';
