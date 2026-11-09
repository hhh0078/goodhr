-- 为独立于岗位运行的附件清理任务和最后引用核验建立部分索引。
CREATE INDEX ar_cloud_pending_file_cleanup ON auto_reply_cloud_entities(updated_at,tenant_id,user_id,id) WHERE kind='purge_files';
COMMENT ON INDEX ar_cloud_pending_file_cleanup IS '后台按待办和重试时间领取自动回复附件清理任务';
CREATE INDEX ar_cloud_attachment_file_ref ON auto_reply_cloud_entities(tenant_id,user_id,(payload->>'relative_path')) WHERE kind='attachments';
COMMENT ON INDEX ar_cloud_attachment_file_ref IS '删除前在成员锁内查找同一私有附件的剩余引用';
