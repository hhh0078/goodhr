-- 文件作用：按已认证团队成员保存服务器自动回复业务实体、处理水位、岗位问题与人工归档事实。
CREATE TABLE auto_reply_state_owners (
 tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE, user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 version bigint NOT NULL DEFAULT 0, PRIMARY KEY(tenant_id,user_id)
);
COMMENT ON TABLE auto_reply_state_owners IS '自动回复账号状态版本，服务器流程和简历删除共用事务边界';
COMMENT ON COLUMN auto_reply_state_owners.tenant_id IS '团队编号';
COMMENT ON COLUMN auto_reply_state_owners.user_id IS '实际执行招聘任务的团队成员';
COMMENT ON COLUMN auto_reply_state_owners.version IS '账号状态乐观锁版本';
CREATE TABLE auto_reply_cloud_entities (
 tenant_id uuid NOT NULL, user_id uuid NOT NULL, kind text NOT NULL, id text NOT NULL, payload jsonb NOT NULL,
 revision bigint NOT NULL, sequence bigint NOT NULL, message_order double precision NOT NULL DEFAULT 0, processed_status text NOT NULL DEFAULT '',
 updated_at text NOT NULL, parents text[] NOT NULL, PRIMARY KEY(tenant_id,user_id,kind,id),
 FOREIGN KEY(tenant_id,user_id) REFERENCES auto_reply_state_owners(tenant_id,user_id) ON DELETE CASCADE
);
COMMENT ON TABLE auto_reply_cloud_entities IS '服务器流程的权威业务实体，包括沟通原文、AI执行和外发收据';
COMMENT ON COLUMN auto_reply_cloud_entities.tenant_id IS '团队编号';
COMMENT ON COLUMN auto_reply_cloud_entities.user_id IS '资料所属招聘成员';
COMMENT ON COLUMN auto_reply_cloud_entities.kind IS '业务实体或辅助状态类型';
COMMENT ON COLUMN auto_reply_cloud_entities.id IS '流程使用的稳定实体编号';
COMMENT ON COLUMN auto_reply_cloud_entities.payload IS '按业务实体类型保存的强类型记录';
COMMENT ON COLUMN auto_reply_cloud_entities.revision IS '实体每次写入递增的版本';
COMMENT ON COLUMN auto_reply_cloud_entities.sequence IS '记录在同类型内首次写入的顺序';
COMMENT ON COLUMN auto_reply_cloud_entities.message_order IS '包含补采历史在内的稳定聊天顺序';
COMMENT ON COLUMN auto_reply_cloud_entities.processed_status IS '该消息已经处理的结果';
COMMENT ON COLUMN auto_reply_cloud_entities.updated_at IS '旧记录及运行时实体最后写入时间原值';
COMMENT ON COLUMN auto_reply_cloud_entities.parents IS '按字段提取的父实体关联，用于准确级联删除和封堵在途回写';
CREATE INDEX ar_cloud_parents ON auto_reply_cloud_entities USING gin(parents);
CREATE INDEX ar_cloud_thread ON auto_reply_cloud_entities(tenant_id,user_id,kind,(payload->>'conversation_id'),message_order,sequence);
CREATE INDEX ar_cloud_position ON auto_reply_cloud_entities(tenant_id,user_id,kind,(payload->>'position_id'));
CREATE INDEX ar_cloud_candidate ON auto_reply_cloud_entities(tenant_id,user_id,kind,(payload->>'candidate_id'));
CREATE INDEX ar_cloud_link ON auto_reply_cloud_entities(tenant_id,kind,(payload->>'value')) WHERE kind='cloud_links';
CREATE TABLE auto_reply_deleted_entity_keys (
 tenant_id uuid NOT NULL,user_id uuid NOT NULL,key_hash text NOT NULL,
 PRIMARY KEY(tenant_id,user_id,key_hash), FOREIGN KEY(tenant_id,user_id) REFERENCES auto_reply_state_owners(tenant_id,user_id) ON DELETE CASCADE
);
COMMENT ON TABLE auto_reply_deleted_entity_keys IS '已清除实体及根的单向摘要，拒绝迟到任务重建旧数据';
COMMENT ON COLUMN auto_reply_deleted_entity_keys.tenant_id IS '归属团队';
COMMENT ON COLUMN auto_reply_deleted_entity_keys.user_id IS '归属成员';
COMMENT ON COLUMN auto_reply_deleted_entity_keys.key_hash IS '实体类型和稳定编号的SHA256摘要';
CREATE TABLE auto_reply_cloud_questions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 position_id uuid NOT NULL REFERENCES positions(id) ON DELETE CASCADE, dedupe_key text NOT NULL, question text NOT NULL,
 answer text NOT NULL DEFAULT '', status text NOT NULL DEFAULT 'pending', version integer NOT NULL DEFAULT 1,
 publish_target text NOT NULL DEFAULT '', fact text NOT NULL DEFAULT '', company_profile_id uuid REFERENCES tenant_company_profiles(id) ON DELETE SET NULL,
 error text NOT NULL DEFAULT '', updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(tenant_id,position_id,dedupe_key),
 CHECK (status IN ('pending','processing','failed','resolved'))
);
COMMENT ON TABLE auto_reply_cloud_questions IS '岗位共用待确认问题与当前HR答复；归档事实按稳定问题号替换';
COMMENT ON COLUMN auto_reply_cloud_questions.id IS '跨会话稳定问题编号';
COMMENT ON COLUMN auto_reply_cloud_questions.tenant_id IS '团队编号';
COMMENT ON COLUMN auto_reply_cloud_questions.position_id IS '提问所对应的岗位';
COMMENT ON COLUMN auto_reply_cloud_questions.dedupe_key IS '规范题义的精确去重摘要';
COMMENT ON COLUMN auto_reply_cloud_questions.question IS 'AI归纳的招聘问题';
COMMENT ON COLUMN auto_reply_cloud_questions.answer IS 'HR当前答复';
COMMENT ON COLUMN auto_reply_cloud_questions.status IS '待答、归档中、失败或已归档';
COMMENT ON COLUMN auto_reply_cloud_questions.version IS 'HR答复乐观锁版本';
COMMENT ON COLUMN auto_reply_cloud_questions.publish_target IS '上一版已确认事实适用于岗位、公司或两者';
COMMENT ON COLUMN auto_reply_cloud_questions.fact IS 'HR答复支持的公开招聘事实';
COMMENT ON COLUMN auto_reply_cloud_questions.company_profile_id IS '归档时核对的公司编号';
COMMENT ON COLUMN auto_reply_cloud_questions.error IS '最近一次归档失败说明';
COMMENT ON COLUMN auto_reply_cloud_questions.updated_at IS '最近一次答复或状态变更时间';
CREATE INDEX ar_question_company ON auto_reply_cloud_questions(tenant_id,company_profile_id) WHERE fact<>'';
CREATE TABLE auto_reply_cloud_question_sources (
 question_id uuid NOT NULL REFERENCES auto_reply_cloud_questions(id) ON DELETE CASCADE,tenant_id uuid NOT NULL,user_id uuid NOT NULL,
 kind text NOT NULL DEFAULT 'messages' CHECK(kind='messages'),message_id text NOT NULL,
 PRIMARY KEY(question_id,user_id,message_id),FOREIGN KEY(tenant_id,user_id,kind,message_id) REFERENCES auto_reply_cloud_entities(tenant_id,user_id,kind,id) ON DELETE CASCADE
);
COMMENT ON TABLE auto_reply_cloud_question_sources IS '问题与真实来信的多对多关系，删除简历时同时移除引用';
COMMENT ON COLUMN auto_reply_cloud_question_sources.question_id IS '岗位待确认问题编号';
COMMENT ON COLUMN auto_reply_cloud_question_sources.tenant_id IS '消息所属团队';
COMMENT ON COLUMN auto_reply_cloud_question_sources.user_id IS '消息所属招聘成员';
COMMENT ON COLUMN auto_reply_cloud_question_sources.kind IS '引用实体固定为聊天消息';
COMMENT ON COLUMN auto_reply_cloud_question_sources.message_id IS '候选人来信的真实稳定编号';

-- 删除候选人时在原事务删除整个关联树，对缓存重放和迟到的执行结果留不可逆摘要。
CREATE FUNCTION purge_cloud_auto_reply_candidate() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE who record; removed text[];
BEGIN
 FOR who IN SELECT DISTINCT e.user_id FROM auto_reply_cloud_entities e WHERE e.tenant_id=OLD.tenant_id AND ((e.kind='cloud_links' AND left(e.id,10)='candidate:' AND e.payload->>'value'=OLD.id::text) OR (e.kind='identities' AND e.payload->>'platform_candidate_id'<>'' AND ((OLD.source_platform_id=e.payload->>'platform_id' AND OLD.source_platform_candidate_id=e.payload->>'platform_candidate_id') OR EXISTS(SELECT 1 FROM candidate_profile_sources s WHERE s.tenant_id=OLD.tenant_id AND s.candidate_id=OLD.id AND s.platform_id=e.payload->>'platform_id' AND s.platform_candidate_id=e.payload->>'platform_candidate_id')))) ORDER BY e.user_id LOOP
   UPDATE auto_reply_state_owners SET version=version+1 WHERE tenant_id=OLD.tenant_id AND user_id=who.user_id;
   WITH RECURSIVE roots(k) AS (
     SELECT 'cloud:'||OLD.id::text UNION SELECT 'candidates:'||substr(id,11) FROM auto_reply_cloud_entities WHERE tenant_id=OLD.tenant_id AND user_id=who.user_id AND kind='cloud_links' AND left(id,10)='candidate:' AND payload->>'value'=OLD.id::text
     UNION SELECT root FROM auto_reply_cloud_entities e CROSS JOIN LATERAL unnest(ARRAY[e.kind||':'||e.id,'candidates:'||nullif(e.payload->>'candidate_id','')]) root WHERE root IS NOT NULL AND e.tenant_id=OLD.tenant_id AND e.user_id=who.user_id AND e.kind='identities' AND e.payload->>'platform_candidate_id'<>'' AND ((OLD.source_platform_id=e.payload->>'platform_id' AND OLD.source_platform_candidate_id=e.payload->>'platform_candidate_id') OR EXISTS(SELECT 1 FROM candidate_profile_sources s WHERE s.tenant_id=OLD.tenant_id AND s.candidate_id=OLD.id AND s.platform_id=e.payload->>'platform_id' AND s.platform_candidate_id=e.payload->>'platform_candidate_id'))
   ), tree(k) AS (
     SELECT k FROM roots UNION SELECT e.kind||':'||e.id FROM auto_reply_cloud_entities e JOIN tree t ON e.parents @> ARRAY[t.k] WHERE e.tenant_id=OLD.tenant_id AND e.user_id=who.user_id
   ) SELECT array_agg(k) INTO removed FROM tree;
   INSERT INTO auto_reply_deleted_entity_keys(tenant_id,user_id,key_hash) SELECT OLD.tenant_id,who.user_id,encode(sha256(convert_to(k,'UTF8')),'hex') FROM unnest(removed) k ON CONFLICT DO NOTHING;
   -- 交给服务器既有哈希校验及最后引用清理流程，可随账号任务重启恢复。
   INSERT INTO auto_reply_cloud_entities(tenant_id,user_id,kind,id,payload,revision,sequence,updated_at,parents)
   SELECT DISTINCT OLD.tenant_id,who.user_id,'purge_files',replace(jsonb_build_array('cloud:'||OLD.id::text,e.payload->>'relative_path')::text,', ',','),
     jsonb_build_object('event_key','cloud:'||OLD.id::text,'path',e.payload->>'relative_path','sha256',e.payload->>'sha256'),0,0,'','{}'::text[]
   FROM auto_reply_cloud_entities e WHERE e.tenant_id=OLD.tenant_id AND e.user_id=who.user_id AND e.kind='attachments' AND 'attachments:'||e.id=ANY(removed) AND nullif(e.payload->>'relative_path','') IS NOT NULL
   ON CONFLICT DO NOTHING;
   DELETE FROM auto_reply_cloud_entities WHERE tenant_id=OLD.tenant_id AND user_id=who.user_id AND kind||':'||id=ANY(removed);
 END LOOP;
 DELETE FROM auto_reply_cloud_questions q WHERE q.tenant_id=OLD.tenant_id AND q.fact='' AND NOT EXISTS(SELECT 1 FROM auto_reply_cloud_question_sources s WHERE s.question_id=q.id);
 RETURN OLD;
END $$;
CREATE TRIGGER ar_purge_cloud_candidate BEFORE DELETE ON candidate_profiles FOR EACH ROW EXECUTE FUNCTION purge_cloud_auto_reply_candidate();
