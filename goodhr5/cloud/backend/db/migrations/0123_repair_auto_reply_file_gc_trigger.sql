-- 文件作用：为已经执行旧0112的数据库前向升级简历删除触发器，在删除附件元数据前持久登记既有GC待办，继续按最后引用和摘要核验回收文件。
-- 不回放历史0112、不扫描或删除文件、不改变候选人数据；已有共享引用与账号范围继续由原GC校验。
CREATE OR REPLACE FUNCTION purge_cloud_auto_reply_candidate() RETURNS trigger LANGUAGE plpgsql AS $$
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
COMMENT ON FUNCTION purge_cloud_auto_reply_candidate() IS '删除简历时先持久登记关联附件GC任务，再清理对应成员实体并封印来源；共享文件由既有GC在最后引用消失后回收';
