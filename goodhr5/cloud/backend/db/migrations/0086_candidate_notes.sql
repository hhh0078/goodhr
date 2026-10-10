-- 文件作用：将人工候选人备注迁移到专用表，保留旧编号、时间、内容和作者，自动事件继续使用事件表。
CREATE TABLE IF NOT EXISTS candidate_notes (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 candidate_id UUID NOT NULL REFERENCES candidate_profiles(id) ON DELETE CASCADE,
 author_email TEXT NOT NULL DEFAULT '',
 author_name TEXT NOT NULL DEFAULT '',
 content TEXT NOT NULL,
 occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 idempotency_key TEXT NOT NULL DEFAULT ''
);
COMMENT ON TABLE candidate_notes IS '候选人独立人工备注历史，只追加记录，不混入自动招聘事件';
COMMENT ON COLUMN candidate_notes.id IS '备注编号，迁移时保留原人工事件编号';
COMMENT ON COLUMN candidate_notes.tenant_id IS '所属团队编号';
COMMENT ON COLUMN candidate_notes.candidate_id IS '所属候选人编号';
COMMENT ON COLUMN candidate_notes.author_email IS '操作人邮箱，来自登录会话';
COMMENT ON COLUMN candidate_notes.author_name IS '操作人姓名快照，缺失时显示邮箱';
COMMENT ON COLUMN candidate_notes.content IS '人工备注正文，保留换行';
COMMENT ON COLUMN candidate_notes.occurred_at IS '实际沟通或记录发生时间，旧备注沿用原记录时间';
COMMENT ON COLUMN candidate_notes.created_at IS '备注入库时间，迁移时保留原创建时间';
COMMENT ON COLUMN candidate_notes.idempotency_key IS '操作人范围内的请求幂等键，历史重复键追加原编号以保留全部记录';
CREATE UNIQUE INDEX IF NOT EXISTS idx_candidate_notes_request ON candidate_notes(tenant_id,candidate_id,author_email,idempotency_key) WHERE idempotency_key<>'';
CREATE INDEX IF NOT EXISTS idx_candidate_notes_timeline ON candidate_notes(tenant_id,candidate_id,occurred_at DESC,created_at DESC,id DESC);
-- 相同正文也不合并；历史重复请求键只保留第一条原键，其余以原编号登记，所有备注内容和时间都保留。
WITH legacy AS (
 SELECT e.*,COALESCE(metadata->>'author_email','') AS note_author,
  COALESCE(NULLIF(metadata->>'idempotency_key',''),idempotency_key,'') AS note_key,
  row_number() OVER(PARTITION BY tenant_id,candidate_id,COALESCE(metadata->>'author_email',''),COALESCE(NULLIF(metadata->>'idempotency_key',''),idempotency_key,'') ORDER BY created_at,id) AS key_rank
 FROM candidate_events e WHERE event_type='manual_note'
)
INSERT INTO candidate_notes(id,tenant_id,candidate_id,author_email,author_name,content,occurred_at,created_at,idempotency_key)
 SELECT id,tenant_id,candidate_id,note_author,COALESCE(NULLIF(metadata->>'author_name',''),note_author,''),message_text,created_at,created_at,
  CASE WHEN note_key<>'' AND key_rank>1 THEN 'migrated:'||id::text ELSE note_key END
 FROM legacy ON CONFLICT(id) DO NOTHING;
-- 保留旧事件供历史回溯和回退；业务展示与后续备注写入只使用 candidate_notes，避免显示两次。
ALTER TABLE candidate_recommendation_events ADD COLUMN IF NOT EXISTS occurred_at TIMESTAMPTZ;
UPDATE candidate_recommendation_events SET occurred_at=created_at WHERE occurred_at IS NULL;
ALTER TABLE candidate_recommendation_events ALTER COLUMN occurred_at SET DEFAULT now(), ALTER COLUMN occurred_at SET NOT NULL;
COMMENT ON COLUMN candidate_recommendation_events.occurred_at IS '推荐或跟进实际发生时间，独立于入库时间，旧记录沿用创建时间';
COMMENT ON COLUMN candidate_recommendation_events.created_at IS '推荐跟进事件入库时间，独立于实际发生时间';
CREATE INDEX IF NOT EXISTS idx_recommendation_events_occurred ON candidate_recommendation_events(recommendation_id,occurred_at DESC,created_at DESC,id DESC);
