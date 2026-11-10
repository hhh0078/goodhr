-- 文件作用：保存各平台在线简历原始快照与真实沟通时间，并提供可恢复的无手机号简历清理任务。
ALTER TABLE candidate_profiles ADD COLUMN IF NOT EXISTS last_communication_at timestamptz;
COMMENT ON COLUMN candidate_profiles.last_communication_at IS '最后一次真实候选人或招聘方消息的平台时间；轮询、查看和AI运算不更新';
ALTER TABLE candidate_profile_sources ADD COLUMN IF NOT EXISTS online_profile jsonb NOT NULL DEFAULT '{}'::jsonb;
COMMENT ON COLUMN candidate_profile_sources.online_profile IS '本平台可见在线简历的独立快照、内容指纹及严格匹配结果，不覆盖其他来源';
CREATE INDEX IF NOT EXISTS candidate_temporary_retention ON candidate_profiles ((greatest(updated_at,coalesce(last_communication_at,created_at))),id) WHERE normalized_phone='';
CREATE TABLE IF NOT EXISTS candidate_retention_jobs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), request_day date NOT NULL UNIQUE,
 cutoff timestamptz NOT NULL, status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','completed','failed')),
 deleted_count bigint NOT NULL DEFAULT 0, skipped_count bigint NOT NULL DEFAULT 0,
 lease_until timestamptz, error text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE candidate_retention_jobs IS '每日一次的临时简历完整清理任务；重启后按租约继续，不存个人信息';
COMMENT ON COLUMN candidate_retention_jobs.id IS '清理任务编号';
COMMENT ON COLUMN candidate_retention_jobs.request_day IS '北京时间请求日期，同日重复请求复用任务';
COMMENT ON COLUMN candidate_retention_jobs.cutoff IS '本次固定的30天过期边界';
COMMENT ON COLUMN candidate_retention_jobs.status IS '待处理、处理中、已完成或失败';
COMMENT ON COLUMN candidate_retention_jobs.deleted_count IS '已完整删除的简历数量';
COMMENT ON COLUMN candidate_retention_jobs.skipped_count IS '选中后因补齐手机、重新沟通或版本变化而跳过的数量';
COMMENT ON COLUMN candidate_retention_jobs.lease_until IS '当前执行租约截止时间，防止多副本重复领取';
COMMENT ON COLUMN candidate_retention_jobs.error IS '不含个人信息的失败原因';
COMMENT ON COLUMN candidate_retention_jobs.created_at IS '首次请求时间';
COMMENT ON COLUMN candidate_retention_jobs.updated_at IS '最近处理或续租时间';
