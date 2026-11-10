-- 文件作用：按确切的成员、会话封印和已删除会话回收旧服务器遗留的操作审计；有效会话及账号级日志继续保留。
CREATE FUNCTION purge_sealed_auto_reply_audits(p_tenant uuid, p_user uuid) RETURNS integer LANGUAGE plpgsql AS $$
DECLARE removed integer;
BEGIN
 PERFORM 1 FROM auto_reply_state_owners o WHERE o.tenant_id=p_tenant AND o.user_id=p_user FOR UPDATE;
 IF NOT FOUND THEN RETURN 0; END IF;
 DELETE FROM auto_reply_cloud_entities a USING auto_reply_cloud_entities seal
 WHERE a.tenant_id=p_tenant AND a.user_id=p_user AND a.kind IN ('runtime_events','runtime_commands')
   AND nullif(a.payload->>'conversation_id','') IS NOT NULL
   AND seal.tenant_id=a.tenant_id AND seal.user_id=a.user_id AND seal.kind='purge_keys'
   AND seal.payload->>'entity_key'='conversations:'||(a.payload->>'conversation_id')
   AND NOT EXISTS(SELECT 1 FROM auto_reply_cloud_entities live WHERE live.tenant_id=a.tenant_id AND live.user_id=a.user_id
       AND live.kind='conversations' AND live.id=a.payload->>'conversation_id');
 GET DIAGNOSTICS removed=ROW_COUNT;
 IF removed>0 THEN UPDATE auto_reply_state_owners SET version=version+1 WHERE tenant_id=p_tenant AND user_id=p_user; END IF;
 RETURN removed;
END $$;
COMMENT ON FUNCTION purge_sealed_auto_reply_audits(uuid,uuid) IS '在成员锁内精准回收可证明属于已封印会话的孤立操作记录，返回实际清理条数';

CREATE FUNCTION apply_sealed_auto_reply_audit_cleanup() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE who uuid;
BEGIN
 FOR who IN SELECT u.id FROM users u JOIN auto_reply_state_owners o ON o.tenant_id=u.tenant_id AND o.user_id=u.id
   WHERE u.tenant_id=NEW.tenant_id AND (
     NEW.audiences ? encode(sha256(convert_to(lower(u.email),'UTF8')),'hex') OR
     EXISTS(SELECT 1 FROM auto_reply_deleted_entity_keys k WHERE k.tenant_id=u.tenant_id AND k.user_id=u.id
         AND k.key_hash=encode(sha256(convert_to('cloud:'||NEW.candidate_id::text,'UTF8')),'hex')))
   ORDER BY u.id LOOP
   PERFORM purge_sealed_auto_reply_audits(NEW.tenant_id,who);
 END LOOP;
 RETURN NEW;
END $$;
COMMENT ON FUNCTION apply_sealed_auto_reply_audit_cleanup() IS '在授权的正式简历删除事务内回收相关成员已经封印的孤立审计';
CREATE TRIGGER ar_cloud_reply_sealed_audit_cleanup AFTER INSERT ON candidate_deletions
 FOR EACH ROW EXECUTE FUNCTION apply_sealed_auto_reply_audit_cleanup();

-- 首次执行只访问同时持有候选操作审计和对应会话封印的成员，清理结果按账号反映到权威状态版本。
SELECT purge_sealed_auto_reply_audits(a.tenant_id,a.user_id) FROM (
 SELECT DISTINCT e.tenant_id,e.user_id FROM auto_reply_cloud_entities e
 JOIN auto_reply_cloud_entities seal ON seal.tenant_id=e.tenant_id AND seal.user_id=e.user_id AND seal.kind='purge_keys'
  AND seal.payload->>'entity_key'='conversations:'||(e.payload->>'conversation_id')
 WHERE e.kind IN ('runtime_commands','runtime_events') AND nullif(e.payload->>'conversation_id','') IS NOT NULL
 ORDER BY e.tenant_id,e.user_id
) a;
