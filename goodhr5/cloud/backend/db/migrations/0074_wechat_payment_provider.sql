-- 本迁移将支付订单默认平台切换为微信支付，并保留历史订单原有平台标识。
ALTER TABLE payment_orders
    ALTER COLUMN payment_provider SET DEFAULT 'wechat';

COMMENT ON COLUMN payment_orders.payment_provider IS '支付平台标识，例如wechat或未来接入的其他平台';
