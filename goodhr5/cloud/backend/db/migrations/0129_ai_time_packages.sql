-- 文件作用：新增时间套餐独立额度、下单快照和幂等发放记录；不迁移旧会员、不动按量余额。
CREATE TABLE ai_time_accounts (
 id TEXT PRIMARY KEY REFERENCES payment_orders(order_no),
 user_email TEXT NOT NULL REFERENCES users(email) ON DELETE CASCADE,
 plan_id TEXT NOT NULL,
 plan_name TEXT NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 balance_units BIGINT NOT NULL DEFAULT 0,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE ai_time_accounts IS '时间套餐独立使用额度，非充值本金';
COMMENT ON COLUMN ai_time_accounts.id IS '账期编号，取本账期首笔支付订单号';
COMMENT ON COLUMN ai_time_accounts.user_email IS '套餐所属账号邮箱';
COMMENT ON COLUMN ai_time_accounts.plan_id IS '最近一次购买的套餐编号';
COMMENT ON COLUMN ai_time_accounts.plan_name IS '最近一次购买的套餐名称';
COMMENT ON COLUMN ai_time_accounts.expires_at IS '本账期到期时间';
COMMENT ON COLUMN ai_time_accounts.balance_units IS '可用额度，万分之一元；到期回收至零';
COMMENT ON COLUMN ai_time_accounts.created_at IS '账期创建时间';
COMMENT ON COLUMN ai_time_accounts.updated_at IS '账期最近变更时间';
CREATE INDEX ai_time_accounts_user_expiry ON ai_time_accounts(user_email,expires_at DESC);
CREATE INDEX ai_time_accounts_expiry ON ai_time_accounts(expires_at) WHERE balance_units<>0;
CREATE TABLE ai_time_order_terms (
 order_no TEXT PRIMARY KEY REFERENCES payment_orders(order_no),
 balance_units BIGINT NOT NULL CHECK (balance_units>0)
);
COMMENT ON TABLE ai_time_order_terms IS '时间套餐下单时不可变的额度快照';
COMMENT ON COLUMN ai_time_order_terms.order_no IS '支付订单号';
COMMENT ON COLUMN ai_time_order_terms.balance_units IS '付款后应补足的额度，万分之一元';
CREATE TABLE ai_time_grants (
 order_no TEXT PRIMARY KEY REFERENCES payment_orders(order_no),
 account_id TEXT NOT NULL REFERENCES ai_time_accounts(id),
 granted_units BIGINT NOT NULL CHECK (granted_units>=0),
 applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE ai_time_grants IS '套餐支付幂等发放凭据，重复回调不重复增加期限或额度';
COMMENT ON COLUMN ai_time_grants.order_no IS '已发放权益的支付订单号';
COMMENT ON COLUMN ai_time_grants.account_id IS '权益发放到的账期编号';
COMMENT ON COLUMN ai_time_grants.granted_units IS '本次实际补足额度，万分之一元';
COMMENT ON COLUMN ai_time_grants.applied_at IS '权益发放时间';
-- 沿用现有月/年套餐实付价格；季度取月费的三倍，所有价格、天数和额度可在后台调整。
INSERT INTO system_configs(config_key,config_value,description,enabled)
SELECT 'system.ai_time_plans',jsonb_build_array(
 jsonb_build_object('id','monthly','name','包月套餐','duration_days',30,'price_cents',prices.monthly,'balance_cents',99900,'enabled',true),
 jsonb_build_object('id','quarterly','name','季度套餐','duration_days',90,'price_cents',prices.monthly*3,'balance_cents',99900,'enabled',true),
 jsonb_build_object('id','yearly','name','包年套餐','duration_days',365,'price_cents',prices.yearly,'balance_cents',99900,'enabled',true)
),'AI时间套餐：价格、天数和独立可用额度（金额单位分）',true
FROM (SELECT
 COALESCE((SELECT round(GREATEST(0,(p->>'original_price')::numeric-COALESCE((p->>'discount_amount')::numeric,0))*100)::bigint FROM system_configs c CROSS JOIN LATERAL jsonb_array_elements(c.config_value) p WHERE c.config_key='system.subscription_plans' AND p->>'member_type'='plus' LIMIT 1),4000) AS monthly,
 COALESCE((SELECT round(GREATEST(0,(p->>'original_price')::numeric-COALESCE((p->>'discount_amount')::numeric,0))*100)::bigint FROM system_configs c CROSS JOIN LATERAL jsonb_array_elements(c.config_value) p WHERE c.config_key='system.subscription_plans' AND p->>'member_type'='max' LIMIT 1),34000) AS yearly
) prices
ON CONFLICT(config_key) DO NOTHING;
