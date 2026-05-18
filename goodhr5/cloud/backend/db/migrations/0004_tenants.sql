-- 租户系统: tenants 表 + users 改造
CREATE TABLE IF NOT EXISTS tenants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL DEFAULT '',
    owner_email TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE tenants IS '租户表';
COMMENT ON COLUMN tenants.owner_email IS '创建者邮箱';

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'admin',
    ADD COLUMN IF NOT EXISTS invited_by TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active';
COMMENT ON COLUMN users.tenant_id IS '所属租户';
COMMENT ON COLUMN users.role IS 'admin或user';
COMMENT ON COLUMN users.invited_by IS '邀请者邮箱';
COMMENT ON COLUMN users.status IS 'active或pending';
CREATE INDEX IF NOT EXISTS idx_users_tenant_id ON users(tenant_id);
