-- 文件作用：保存实测旧库已执行0112后的旧删除触发器形状，仅供隔离PG回归复现无GC待办的问题；不含业务数据。
CREATE OR REPLACE FUNCTION purge_cloud_auto_reply_candidate()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
DECLARE who record; removed text[];
BEGIN
 FOR who IN SELECT DISTINCT user_id FROM auto_reply_cloud_entities WHERE tenant_id=OLD.tenant_id AND kind='cloud_links' AND left(id,10)='candidate:' AND payload->>'value'=OLD.id::text ORDER BY user_id LOOP
   UPDATE auto_reply_state_owners SET version=version+1 WHERE tenant_id=OLD.tenant_id AND user_id=who.user_id;
   WITH RECURSIVE roots(k) AS (
     SELECT 'cloud:'||OLD.id::text UNION SELECT 'candidates:'||substr(id,11) FROM auto_reply_cloud_entities WHERE tenant_id=OLD.tenant_id AND user_id=who.user_id AND kind='cloud_links' AND left(id,10)='candidate:' AND payload->>'value'=OLD.id::text
   ), tree(k) AS (
     SELECT k FROM roots UNION SELECT e.kind||':'||e.id FROM auto_reply_cloud_entities e JOIN tree t ON e.parents @> ARRAY[t.k] WHERE e.tenant_id=OLD.tenant_id AND e.user_id=who.user_id
   ) SELECT array_agg(k) INTO removed FROM tree;
   INSERT INTO auto_reply_deleted_entity_keys(tenant_id,user_id,key_hash) SELECT OLD.tenant_id,who.user_id,encode(sha256(convert_to(k,'UTF8')),'hex') FROM unnest(removed) k ON CONFLICT DO NOTHING;
   DELETE FROM auto_reply_cloud_entities WHERE tenant_id=OLD.tenant_id AND user_id=who.user_id AND kind||':'||id=ANY(removed);
 END LOOP;
 DELETE FROM auto_reply_cloud_questions q WHERE q.tenant_id=OLD.tenant_id AND q.fact='' AND NOT EXISTS(SELECT 1 FROM auto_reply_cloud_question_sources s WHERE s.question_id=q.id);
 RETURN OLD;
END $function$
