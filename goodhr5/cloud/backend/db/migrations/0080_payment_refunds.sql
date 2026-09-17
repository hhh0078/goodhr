-- 本迁移创建自动退款记录表，用于防止同一支付订单重复退款并保存微信退款结果。
CREATE TABLE IF NOT EXISTS payment_refunds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    refund_no TEXT NOT NULL UNIQUE,
    order_no TEXT NOT NULL REFERENCES payment_orders(order_no),
    user_id UUID NOT NULL REFERENCES users(id),
    user_email TEXT NOT NULL,
    refund_type TEXT NOT NULL CHECK (refund_type IN ('membership', 'ai_balance')),
    amount_cents INTEGER NOT NULL CHECK (amount_cents > 0),
    deducted_days INTEGER NOT NULL DEFAULT 0 CHECK (deducted_days >= 0),
    status TEXT NOT NULL DEFAULT 'pending',
    provider_refund_id TEXT NOT NULL DEFAULT '',
    response_data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

COMMENT ON TABLE payment_refunds IS '支付订单自动退款记录';
COMMENT ON COLUMN payment_refunds.id IS '退款记录主键';
COMMENT ON COLUMN payment_refunds.refund_no IS '商户退款单号';
COMMENT ON COLUMN payment_refunds.order_no IS '原支付订单号';
COMMENT ON COLUMN payment_refunds.user_id IS '申请退款的用户主键';
COMMENT ON COLUMN payment_refunds.user_email IS '申请退款的用户邮箱';
COMMENT ON COLUMN payment_refunds.refund_type IS '退款类型：会员或AI余额';
COMMENT ON COLUMN payment_refunds.amount_cents IS '退款金额，单位为分';
COMMENT ON COLUMN payment_refunds.deducted_days IS '会员退款扣减天数';
COMMENT ON COLUMN payment_refunds.status IS '退款状态';
COMMENT ON COLUMN payment_refunds.provider_refund_id IS '微信支付退款单号';
COMMENT ON COLUMN payment_refunds.response_data IS '微信退款接口响应数据';
COMMENT ON COLUMN payment_refunds.created_at IS '退款申请创建时间';
COMMENT ON COLUMN payment_refunds.updated_at IS '退款记录更新时间';
COMMENT ON COLUMN payment_refunds.completed_at IS '退款成功时间';

CREATE INDEX IF NOT EXISTS idx_payment_refunds_order_status
    ON payment_refunds(order_no, status);
CREATE INDEX IF NOT EXISTS idx_payment_refunds_user_created
    ON payment_refunds(user_id, created_at DESC);
