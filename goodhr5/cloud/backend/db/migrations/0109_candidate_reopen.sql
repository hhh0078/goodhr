-- 本迁移为简历删除设置可信时间边界，并记录授权的新入站附件，阻止旧异步任务重新建档。
ALTER TABLE candidate_deletions ADD COLUMN deleted_at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp(),
 ADD COLUMN cutoff_from_migration BOOLEAN NOT NULL DEFAULT TRUE,
 ADD COLUMN contact_hashes JSONB NOT NULL DEFAULT '[]';
ALTER TABLE candidate_deletions ALTER COLUMN deleted_at SET DEFAULT clock_timestamp(),
 ALTER COLUMN cutoff_from_migration SET DEFAULT FALSE;
COMMENT ON COLUMN candidate_deletions.deleted_at IS '新事件为数据库实际登记删除时刻；旧事件采用迁移时刻作为保守下限';
COMMENT ON COLUMN candidate_deletions.cutoff_from_migration IS '为真表示旧版本没有保存原始删除时间，必须以迁移后新消息为准';
COMMENT ON COLUMN candidate_deletions.contact_hashes IS '仅含删除时可验证唯一归属的团队限定联系方式摘要，未知旧记录留空';

CREATE TABLE candidate_reopen_receipts (
 deletion_sequence BIGINT NOT NULL REFERENCES candidate_deletions(sequence) ON DELETE CASCADE,
 source_hash TEXT NOT NULL,
 actor_hash TEXT NOT NULL,
 attachment_at TIMESTAMPTZ NOT NULL,
 proof_hash TEXT NOT NULL,
 candidate_id UUID NOT NULL REFERENCES candidate_profiles(id) ON DELETE CASCADE,
 PRIMARY KEY (deletion_sequence,source_hash)
);
COMMENT ON TABLE candidate_reopen_receipts IS '经绑定设备核实新入站及新附件后，以原来源和删除事件为单位记录本代新简历';
COMMENT ON COLUMN candidate_reopen_receipts.deletion_sequence IS '本次重新建档匹配的最新删除事件';
COMMENT ON COLUMN candidate_reopen_receipts.source_hash IS '确实属于该删除事件的招聘平台和候选人来源摘要';
COMMENT ON COLUMN candidate_reopen_receipts.actor_hash IS '设备所属云账号及招聘账号标记的组合摘要';
COMMENT ON COLUMN candidate_reopen_receipts.attachment_at IS '当前已入库附件在平台实际发送的时间，用于阻止延迟旧附件覆盖';
COMMENT ON COLUMN candidate_reopen_receipts.proof_hash IS '入站和附件两条证据的稳定联合摘要，用于原子幂等重放';
COMMENT ON COLUMN candidate_reopen_receipts.candidate_id IS '此代新建云端简历编号，再次删除时清除收据';
CREATE INDEX idx_candidate_reopen_candidate ON candidate_reopen_receipts(candidate_id);
