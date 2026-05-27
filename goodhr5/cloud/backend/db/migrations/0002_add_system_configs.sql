-- 本文件定义 GoodHR 5 云端系统配置表。
CREATE TABLE IF NOT EXISTS system_configs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    config_key TEXT NOT NULL UNIQUE,
    config_value JSONB NOT NULL DEFAULT '{}'::jsonb,
    description TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE system_configs IS '云端系统配置表';
COMMENT ON COLUMN system_configs.config_key IS '配置键';
COMMENT ON COLUMN system_configs.config_value IS 'JSONB 格式配置';
CREATE INDEX IF NOT EXISTS idx_system_configs_key ON system_configs(config_key);
