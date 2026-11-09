-- 文件作用：保存每个成员导入旧设备自动回复资料的校验回执，响应丢失也可安全重试。
CREATE TABLE auto_reply_import_receipts(
 tenant_id uuid NOT NULL,user_id uuid NOT NULL,digest text NOT NULL,accepted integer NOT NULL,reconciled integer NOT NULL,
 suppressed integer NOT NULL,file_count integer NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,user_id,digest),FOREIGN KEY(tenant_id,user_id) REFERENCES auto_reply_state_owners(tenant_id,user_id) ON DELETE CASCADE
);
COMMENT ON TABLE auto_reply_import_receipts IS '已完整处理的数据包回执，只保存摘要与计数';
COMMENT ON COLUMN auto_reply_import_receipts.tenant_id IS '归属团队';
COMMENT ON COLUMN auto_reply_import_receipts.user_id IS '归属招聘成员';
COMMENT ON COLUMN auto_reply_import_receipts.digest IS '整份旧资料ZIP的SHA256摘要';
COMMENT ON COLUMN auto_reply_import_receipts.accepted IS '新写入云端的实体数量';
COMMENT ON COLUMN auto_reply_import_receipts.reconciled IS '已确认有权威记录的实体数量';
COMMENT ON COLUMN auto_reply_import_receipts.suppressed IS '已删除身份和关联后代被拦截的数量';
COMMENT ON COLUMN auto_reply_import_receipts.file_count IS '导入前已验证内容摘要和大小的唯一文件路径数量';
COMMENT ON COLUMN auto_reply_import_receipts.created_at IS '事务完成时间';
