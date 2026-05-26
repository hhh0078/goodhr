-- 本迁移新增会员订阅支付记录表，用于记录用户购买套餐和支付回调结果。
CREATE TABLE IF NOT EXISTS payment_orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_no TEXT NOT NULL UNIQUE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_email TEXT NOT NULL,
    plan_id TEXT NOT NULL,
    plan_name TEXT NOT NULL,
    member_type TEXT NOT NULL DEFAULT 'plus',
    duration_days INTEGER NOT NULL DEFAULT 0,
    original_amount_cents INTEGER NOT NULL DEFAULT 0,
    discount_amount_cents INTEGER NOT NULL DEFAULT 0,
    amount_cents INTEGER NOT NULL DEFAULT 0,
    payment_provider TEXT NOT NULL DEFAULT 'haoshoumi',
    trade_no TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    paid_at TIMESTAMPTZ,
    expired_at TIMESTAMPTZ,
    notify_data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE payment_orders IS '会员订阅支付记录表';
COMMENT ON COLUMN payment_orders.id IS '支付记录ID';
COMMENT ON COLUMN payment_orders.order_no IS '本系统生成的订单号';
COMMENT ON COLUMN payment_orders.user_id IS '购买用户ID';
COMMENT ON COLUMN payment_orders.user_email IS '购买用户邮箱，便于后台查看';
COMMENT ON COLUMN payment_orders.plan_id IS '订阅套餐ID';
COMMENT ON COLUMN payment_orders.plan_name IS '订阅套餐名称';
COMMENT ON COLUMN payment_orders.member_type IS '会员类型，例如plus';
COMMENT ON COLUMN payment_orders.duration_days IS '购买套餐增加的会员天数';
COMMENT ON COLUMN payment_orders.original_amount_cents IS '套餐原价，单位分';
COMMENT ON COLUMN payment_orders.discount_amount_cents IS '优惠金额，单位分';
COMMENT ON COLUMN payment_orders.amount_cents IS '实际支付金额，单位分';
COMMENT ON COLUMN payment_orders.payment_provider IS '支付平台标识，例如haoshoumi';
COMMENT ON COLUMN payment_orders.trade_no IS '第三方支付流水号';
COMMENT ON COLUMN payment_orders.status IS '订单状态：pending待支付、paid已支付、closed已关闭';
COMMENT ON COLUMN payment_orders.paid_at IS '支付成功时间';
COMMENT ON COLUMN payment_orders.expired_at IS '订单过期时间';
COMMENT ON COLUMN payment_orders.notify_data IS '支付平台回调原始数据';
COMMENT ON COLUMN payment_orders.created_at IS '创建时间';
COMMENT ON COLUMN payment_orders.updated_at IS '更新时间';

CREATE INDEX IF NOT EXISTS idx_payment_orders_user_created ON payment_orders(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_payment_orders_status ON payment_orders(status);
CREATE INDEX IF NOT EXISTS idx_payment_orders_user_email ON payment_orders(user_email);
