-- 本迁移为内置 AI 余额流水旧精度字段补充默认值，避免只写 0.0001 元字段时被 NOT NULL 约束拦截。
ALTER TABLE ai_balance_records
ALTER COLUMN change_cents SET DEFAULT 0,
ALTER COLUMN balance_after_cents SET DEFAULT 0;

COMMENT ON COLUMN ai_balance_records.change_cents IS '本次余额变动金额，单位为分，兼容旧字段，精确扣费以 change_units 为准';
COMMENT ON COLUMN ai_balance_records.balance_after_cents IS '变动后的余额，单位为分，兼容旧字段，精确余额以 balance_after_units 为准';
