-- 本迁移保留单份简历删除事件及离线上传识别摘要，云端和已绑定设备均可安全重试。
CREATE TABLE candidate_deletions (
 sequence BIGSERIAL PRIMARY KEY,
 tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 candidate_id UUID NOT NULL,
 row_version BIGINT NOT NULL,
 owner_hash TEXT NOT NULL,
 audiences JSONB NOT NULL,
 source_hashes JSONB NOT NULL,
 UNIQUE(tenant_id,candidate_id)
);
COMMENT ON TABLE candidate_deletions IS '按团队串行发布简历删除通知，保留离线设备可追赶的游标';
COMMENT ON COLUMN candidate_deletions.sequence IS '删除事件单调序号，同团队按提交顺序分配';
COMMENT ON COLUMN candidate_deletions.tenant_id IS '删除时的团队标识';
COMMENT ON COLUMN candidate_deletions.candidate_id IS '已删除的云端简历编号，不关联已经移除的主体';
COMMENT ON COLUMN candidate_deletions.row_version IS '用户确认删除时的主体版本，用于幂等重试';
COMMENT ON COLUMN candidate_deletions.owner_hash IS '简历创建者身份摘要，用于校验重复请求权限';
COMMENT ON COLUMN candidate_deletions.audiences IS '已知与此简历有关的账号摘要，仅这些账号的绑定设备可领取事件';
COMMENT ON COLUMN candidate_deletions.source_hashes IS '招聘平台及候选标识的组合摘要，不保存原号码、姓名或平台编号';
CREATE INDEX idx_candidate_deletions_cursor ON candidate_deletions(tenant_id,sequence);

CREATE TABLE candidate_deletion_keys (
 tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 kind TEXT NOT NULL,
 identity_hash TEXT NOT NULL,
 replacement_candidate_id UUID REFERENCES candidate_profiles(id) ON DELETE SET NULL,
 PRIMARY KEY(tenant_id,kind,identity_hash)
);
COMMENT ON TABLE candidate_deletion_keys IS '防止离线采集、筛选或面试邮件在删除后重新写回已清除的数据';
COMMENT ON COLUMN candidate_deletion_keys.tenant_id IS '识别摘要仅对本团队生效';
COMMENT ON COLUMN candidate_deletion_keys.kind IS '来源、手机号、筛选尝试或应聘实例的标识类型';
COMMENT ON COLUMN candidate_deletion_keys.identity_hash IS '对应来源或标识的不可逆摘要';

CREATE TABLE candidate_attachment_purge_jobs (
 deletion_sequence BIGINT NOT NULL REFERENCES candidate_deletions(sequence) ON DELETE CASCADE,
 owner_hash TEXT NOT NULL,
 anchor_ids JSONB NOT NULL DEFAULT '{}',
 scan_cursor TEXT NOT NULL DEFAULT '0',
 removing BOOLEAN NOT NULL DEFAULT FALSE,
 next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(deletion_sequence,owner_hash)
);
COMMENT ON TABLE candidate_attachment_purge_jobs IS '插件附件临时回执的持久清理进度；先核实快照，再清同账号同快照的临时资料';
COMMENT ON COLUMN candidate_attachment_purge_jobs.deletion_sequence IS '同事务登记的简历删除事件编号';
COMMENT ON COLUMN candidate_attachment_purge_jobs.owner_hash IS '插件回执账号摘要，仅扫描此账号的专用键空间';
COMMENT ON COLUMN candidate_attachment_purge_jobs.anchor_ids IS '已被该账号成功入库回执证明的插件任务编号，不含附件资料';
COMMENT ON COLUMN candidate_attachment_purge_jobs.scan_cursor IS 'Redis 扫描本账号临时记录的续读游标';
COMMENT ON COLUMN candidate_attachment_purge_jobs.removing IS '是否已找齐可证明属于该简历的捕获记录，进入物理清理阶段';
COMMENT ON COLUMN candidate_attachment_purge_jobs.next_attempt_at IS '后台公平重试时间，依赖异常时保留下次处理机会';
CREATE INDEX idx_candidate_attachment_purge_due ON candidate_attachment_purge_jobs(next_attempt_at);

CREATE TABLE candidate_attachment_revocations (
 owner_hash TEXT NOT NULL,
 key_hash TEXT NOT NULL,
 PRIMARY KEY(owner_hash,key_hash)
);
COMMENT ON TABLE candidate_attachment_revocations IS '已明确删除的附件任务或捕获快照摘要，防止迟到的解析及重试再次提供结果';
COMMENT ON COLUMN candidate_attachment_revocations.owner_hash IS '附件任务原账号摘要，与插件原有缓存命名相同';
COMMENT ON COLUMN candidate_attachment_revocations.key_hash IS '原有任务编号或同账号捕获编号和正文摘要的专用哈希，无简历正文';

COMMENT ON COLUMN candidate_deletion_keys.replacement_candidate_id IS '经人工明确重新关联同来源的新简历，可阻止缓存据旧来源误删新资料';
CREATE INDEX idx_candidate_deletions_sources ON candidate_deletions USING GIN(source_hashes);
