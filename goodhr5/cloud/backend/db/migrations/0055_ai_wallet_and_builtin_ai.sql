-- 本迁移新增内置 AI 钱包、余额流水和支付订单类型字段。
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS ai_balance_cents INTEGER NOT NULL DEFAULT 0;

COMMENT ON COLUMN users.ai_balance_cents IS '用户内置AI余额，单位为分';

ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS order_type TEXT NOT NULL DEFAULT 'subscription';

COMMENT ON COLUMN payment_orders.order_type IS '订单类型：subscription会员订阅，ai_balance内置AI余额充值';

CREATE INDEX IF NOT EXISTS idx_payment_orders_order_type ON payment_orders(order_type);

CREATE TABLE IF NOT EXISTS ai_balance_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_email TEXT NOT NULL,
    change_cents INTEGER NOT NULL,
    balance_after_cents INTEGER NOT NULL,
    category TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    related_order_no TEXT NOT NULL DEFAULT '',
    model_id TEXT NOT NULL DEFAULT '',
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE ai_balance_records IS '内置AI余额变动流水表';
COMMENT ON COLUMN ai_balance_records.user_id IS '用户ID';
COMMENT ON COLUMN ai_balance_records.user_email IS '用户邮箱';
COMMENT ON COLUMN ai_balance_records.change_cents IS '本次余额变动金额，单位为分，正数增加负数扣减';
COMMENT ON COLUMN ai_balance_records.balance_after_cents IS '变动后的余额，单位为分';
COMMENT ON COLUMN ai_balance_records.category IS '流水类型：signup_bonus、recharge、admin_adjust、ai_usage';
COMMENT ON COLUMN ai_balance_records.reason IS '余额变动原因';
COMMENT ON COLUMN ai_balance_records.related_order_no IS '关联支付订单号';
COMMENT ON COLUMN ai_balance_records.model_id IS 'AI调用使用的模型ID';
COMMENT ON COLUMN ai_balance_records.prompt_tokens IS 'AI调用输入token数';
COMMENT ON COLUMN ai_balance_records.completion_tokens IS 'AI调用输出token数';
COMMENT ON COLUMN ai_balance_records.created_at IS '流水创建时间';

CREATE INDEX IF NOT EXISTS idx_ai_balance_records_user_created ON ai_balance_records(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_balance_records_order_no ON ai_balance_records(related_order_no);

UPDATE system_configs
SET config_value = jsonb_set(
    config_value,
    '{builtin_ai}',
    COALESCE(config_value->'builtin_ai', '{
        "public_base_url": "https://goodhr5.58it.cn/api/ai-compatible/v1/chat/completions",
        "upstream_base_url": "",
        "upstream_api_key": "",
        "default_model": "qwen3.7-plus",
        "signup_bonus_cents": 70,
        "models": [
            {
                "id": "qwen3.7-plus",
                "name": "通义千问 Plus",
                "description": "适合日常筛选，先稳稳开工",
                "input_price_per_1m_cents": 100,
                "output_price_per_1m_cents": 400
            }
        ]
    }'::jsonb),
    true
)
WHERE config_key = 'system.app_config';
