-- 本迁移为内置 AI 钱包新增 0.0001 元精度金额字段，并从旧的分字段迁移历史数据。
ALTER TABLE users
ADD COLUMN IF NOT EXISTS ai_balance_units BIGINT NOT NULL DEFAULT 0;

COMMENT ON COLUMN users.ai_balance_units IS '用户内置AI余额，单位为0.0001元';

UPDATE users
SET ai_balance_units = ai_balance_cents::BIGINT * 100
WHERE ai_balance_units = 0
  AND ai_balance_cents <> 0;

ALTER TABLE ai_balance_records
ADD COLUMN IF NOT EXISTS change_units BIGINT NOT NULL DEFAULT 0,
ADD COLUMN IF NOT EXISTS balance_after_units BIGINT NOT NULL DEFAULT 0;

COMMENT ON COLUMN ai_balance_records.change_units IS '本次余额变动金额，单位为0.0001元，正数增加负数扣减';
COMMENT ON COLUMN ai_balance_records.balance_after_units IS '变动后的余额，单位为0.0001元';

UPDATE ai_balance_records
SET change_units = change_cents::BIGINT * 100
WHERE change_units = 0
  AND change_cents <> 0;

UPDATE ai_balance_records
SET balance_after_units = balance_after_cents::BIGINT * 100
WHERE balance_after_units = 0
  AND balance_after_cents <> 0;
