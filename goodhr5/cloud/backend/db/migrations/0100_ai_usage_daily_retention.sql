-- 本迁移新增永久按日消费金额汇总，供删除七天前消费明细后继续准确统计；不修改余额、分笔本金、充值或退款记录。
CREATE TABLE IF NOT EXISTS ai_usage_daily (
    usage_day DATE PRIMARY KEY,
    usage_units BIGINT NOT NULL DEFAULT 0 CHECK (usage_units >= 0),
    record_count BIGINT NOT NULL DEFAULT 0 CHECK (record_count >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
COMMENT ON TABLE ai_usage_daily IS '已清理AI消费明细的北京时间日金额汇总，不含简历、密钥或用户调用详情';
COMMENT ON COLUMN ai_usage_daily.usage_day IS '消费所属北京时间日期，也是日汇总唯一主键';
COMMENT ON COLUMN ai_usage_daily.usage_units IS '已清理明细的消费总金额，单位0.0001元，与保留明细合并计算月度及累计消费';
COMMENT ON COLUMN ai_usage_daily.record_count IS '已清理消费明细的数量，用于核对清理结果';
COMMENT ON COLUMN ai_usage_daily.updated_at IS '最近一次原子汇总消费金额的时间';
CREATE INDEX IF NOT EXISTS idx_ai_balance_records_usage_retention ON ai_balance_records(created_at, id) WHERE category='ai_usage' AND change_units<0;
