-- 本迁移新增正式的团队邀请表，并记录用户加入当前团队的时间。

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS tenant_joined_at TIMESTAMPTZ;

COMMENT ON COLUMN users.tenant_joined_at IS '用户加入当前团队的时间，切换团队时由后端更新';

UPDATE users
SET tenant_joined_at = COALESCE(tenant_joined_at, created_at)
WHERE tenant_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS tenant_invitations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    invitee_email TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('admin', 'user')),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'rejected', 'canceled')),
    invited_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    invited_by_email TEXT NOT NULL DEFAULT '',
    email_sent_at TIMESTAMPTZ,
    responded_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE tenant_invitations IS '团队成员邀请记录表，邀请不会提前修改被邀请人的团队';
COMMENT ON COLUMN tenant_invitations.id IS '团队邀请唯一标识';
COMMENT ON COLUMN tenant_invitations.tenant_id IS '邀请加入的目标团队标识';
COMMENT ON COLUMN tenant_invitations.invitee_email IS '被邀请人的标准化邮箱';
COMMENT ON COLUMN tenant_invitations.role IS '接受邀请后获得的团队角色：admin管理员，user普通成员';
COMMENT ON COLUMN tenant_invitations.status IS '邀请状态：pending待确认，accepted已接受，rejected已拒绝，canceled已取消';
COMMENT ON COLUMN tenant_invitations.invited_by_user_id IS '发起邀请的用户标识';
COMMENT ON COLUMN tenant_invitations.invited_by_email IS '发起邀请时的用户邮箱快照';
COMMENT ON COLUMN tenant_invitations.email_sent_at IS '最近一次邀请邮件发送成功时间';
COMMENT ON COLUMN tenant_invitations.responded_at IS '被邀请人接受或拒绝的时间';
COMMENT ON COLUMN tenant_invitations.created_at IS '邀请创建时间';
COMMENT ON COLUMN tenant_invitations.updated_at IS '邀请最近更新时间';

CREATE UNIQUE INDEX IF NOT EXISTS idx_tenant_invitations_pending_email
    ON tenant_invitations (LOWER(invitee_email))
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_tenant_invitations_tenant_created
    ON tenant_invitations (tenant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_tenant_invitations_invitee_status
    ON tenant_invitations (LOWER(invitee_email), status, created_at DESC);

-- 把旧实现直接写进 users 的 pending 成员恢复为正式邀请，再还原为个人团队。
INSERT INTO tenant_invitations (
    tenant_id,
    invitee_email,
    role,
    status,
    invited_by_email,
    created_at,
    updated_at
)
SELECT
    tenant_id,
    LOWER(TRIM(email)),
    CASE WHEN role = 'admin' THEN 'admin' ELSE 'user' END,
    'pending',
    invited_by,
    created_at,
    now()
FROM users
WHERE status = 'pending'
  AND tenant_id IS NOT NULL
ON CONFLICT DO NOTHING;

DO $$
DECLARE
    pending_user RECORD;
    personal_tenant_id UUID;
BEGIN
    FOR pending_user IN
        SELECT id, email, tenant_id
        FROM users
        WHERE status = 'pending'
    LOOP
        SELECT id
        INTO personal_tenant_id
        FROM tenants
        WHERE LOWER(owner_email) = LOWER(pending_user.email)
          AND id <> pending_user.tenant_id
        ORDER BY created_at
        LIMIT 1;

        IF personal_tenant_id IS NULL THEN
            INSERT INTO tenants (name, owner_email)
            VALUES (pending_user.email, pending_user.email)
            RETURNING id INTO personal_tenant_id;
        END IF;

        UPDATE users
        SET tenant_id = personal_tenant_id,
            role = 'admin',
            status = 'active',
            invited_by = '',
            tenant_joined_at = COALESCE(tenant_joined_at, created_at)
        WHERE id = pending_user.id;
    END LOOP;
END $$;
