-- 本迁移新增团队隔离的猎头甲方客户、独立业务岗位与平台岗位绑定，不修改既有岗位和推荐历史。
CREATE TABLE IF NOT EXISTS headhunter_customers (
 id TEXT PRIMARY KEY,
 tenant_id UUID NOT NULL REFERENCES tenants(id),
 name TEXT NOT NULL,
 code TEXT NOT NULL DEFAULT '',
 owner_email TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'potential',
 revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
 payload JSONB NOT NULL DEFAULT '{}'::jsonb,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,id)
);
COMMENT ON TABLE headhunter_customers IS '猎头团队的甲方公司客户，联系人和合同资料按负责人权限返回';
COMMENT ON COLUMN headhunter_customers.id IS '客户编号';
COMMENT ON COLUMN headhunter_customers.tenant_id IS '所属团队编号';
COMMENT ON COLUMN headhunter_customers.name IS '甲方公司名称';
COMMENT ON COLUMN headhunter_customers.code IS '团队内客户编码，可不填写';
COMMENT ON COLUMN headhunter_customers.owner_email IS '当前负责人邮箱';
COMMENT ON COLUMN headhunter_customers.status IS '客户状态：potential潜在、active签约、paused暂停';
COMMENT ON COLUMN headhunter_customers.revision IS '并发编辑修订号';
COMMENT ON COLUMN headhunter_customers.payload IS '客户完整资料JSON，含多联系人和用户提供的合同链接';
COMMENT ON COLUMN headhunter_customers.created_at IS '创建时间';
COMMENT ON COLUMN headhunter_customers.updated_at IS '最后修改时间';
CREATE UNIQUE INDEX IF NOT EXISTS idx_headhunter_customers_name ON headhunter_customers(tenant_id,lower(name));
CREATE UNIQUE INDEX IF NOT EXISTS idx_headhunter_customers_code ON headhunter_customers(tenant_id,lower(code)) WHERE code <> '';
CREATE INDEX IF NOT EXISTS idx_headhunter_customers_list ON headhunter_customers(tenant_id,updated_at DESC,id);

CREATE TABLE IF NOT EXISTS headhunter_jobs (
 id TEXT PRIMARY KEY,
 tenant_id UUID NOT NULL REFERENCES tenants(id),
 customer_id TEXT NOT NULL,
 name TEXT NOT NULL,
 owner_email TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'open',
 revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
 payload JSONB NOT NULL DEFAULT '{}'::jsonb,
 position_history JSONB NOT NULL DEFAULT '[]'::jsonb,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,id),
 FOREIGN KEY(tenant_id,customer_id) REFERENCES headhunter_customers(tenant_id,id)
);
COMMENT ON TABLE headhunter_jobs IS '甲方独立业务岗位JD，可绑定多个既有平台自动招聘岗位';
COMMENT ON COLUMN headhunter_jobs.id IS '甲方业务岗位编号';
COMMENT ON COLUMN headhunter_jobs.tenant_id IS '所属团队编号';
COMMENT ON COLUMN headhunter_jobs.customer_id IS '所属甲方客户编号，与团队组成外键防止串团队';
COMMENT ON COLUMN headhunter_jobs.name IS '业务岗位名称';
COMMENT ON COLUMN headhunter_jobs.owner_email IS '岗位负责人邮箱';
COMMENT ON COLUMN headhunter_jobs.status IS '岗位状态：open招聘中、paused暂停、closed关闭';
COMMENT ON COLUMN headhunter_jobs.revision IS '并发编辑修订号';
COMMENT ON COLUMN headhunter_jobs.payload IS '完整岗位JD；salary_min与salary_max均为年薪万元，零表示未知';
COMMENT ON COLUMN headhunter_jobs.position_history IS '历次绑定过的平台岗位编号，仅用于删除前核对业务历史';
COMMENT ON COLUMN headhunter_jobs.created_at IS '创建时间';
COMMENT ON COLUMN headhunter_jobs.updated_at IS '最后修改时间';
CREATE INDEX IF NOT EXISTS idx_headhunter_jobs_list ON headhunter_jobs(tenant_id,updated_at DESC,id);
CREATE INDEX IF NOT EXISTS idx_headhunter_jobs_customer ON headhunter_jobs(tenant_id,customer_id);

CREATE TABLE IF NOT EXISTS headhunter_position_bindings (
 tenant_id UUID NOT NULL REFERENCES tenants(id),
 job_id TEXT NOT NULL,
 position_id UUID NOT NULL REFERENCES positions(id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,job_id,position_id),
 FOREIGN KEY(tenant_id,job_id) REFERENCES headhunter_jobs(tenant_id,id)
);
COMMENT ON TABLE headhunter_position_bindings IS '甲方业务岗位与既有平台岗位的绑定关系，不复制或删除平台配置';
COMMENT ON COLUMN headhunter_position_bindings.tenant_id IS '所属团队编号';
COMMENT ON COLUMN headhunter_position_bindings.job_id IS '甲方业务岗位编号';
COMMENT ON COLUMN headhunter_position_bindings.position_id IS '同团队有效成员拥有的平台岗位编号';
COMMENT ON COLUMN headhunter_position_bindings.created_at IS '绑定时间';
CREATE INDEX IF NOT EXISTS idx_headhunter_binding_position ON headhunter_position_bindings(tenant_id,position_id);
