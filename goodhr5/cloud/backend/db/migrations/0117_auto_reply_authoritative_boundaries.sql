-- 正常简历删除事务同时更新云编排使用的账号边界和封印；岗位停止、没有本机订阅或刚上云时均可生效。
CREATE FUNCTION sync_cloud_reply_deletion_boundary(d candidate_deletions) RETURNS void LANGUAGE plpgsql AS $$
DECLARE who uuid; source_id text; changed integer; affected integer;
BEGIN
 FOR who IN SELECT u.id FROM users u WHERE u.tenant_id=d.tenant_id AND (
   d.audiences ? encode(sha256(convert_to(lower(u.email),'UTF8')),'hex') OR
   EXISTS(SELECT 1 FROM auto_reply_deleted_entity_keys k WHERE k.tenant_id=d.tenant_id AND k.user_id=u.id
          AND k.key_hash=encode(sha256(convert_to('cloud:'||d.candidate_id::text,'UTF8')),'hex')))
   ORDER BY u.id LOOP
   INSERT INTO auto_reply_state_owners(tenant_id,user_id) VALUES(d.tenant_id,who) ON CONFLICT DO NOTHING;
   PERFORM 1 FROM auto_reply_state_owners WHERE tenant_id=d.tenant_id AND user_id=who FOR UPDATE;
   INSERT INTO auto_reply_cloud_entities(tenant_id,user_id,kind,id,payload,revision,sequence,updated_at,parents)
   VALUES(d.tenant_id,who,'purge_keys',jsonb_build_array('cloud:'||d.candidate_id::text)::text,jsonb_build_object('entity_key','cloud:'||d.candidate_id::text),0,0,'','{}') ON CONFLICT DO NOTHING;
   GET DIAGNOSTICS changed=ROW_COUNT;
   FOR source_id IN SELECT jsonb_array_elements_text(d.source_hashes) LOOP
     INSERT INTO auto_reply_cloud_entities AS e(tenant_id,user_id,kind,id,payload,revision,sequence,updated_at,parents)
     VALUES(d.tenant_id,who,'deletion_boundaries',jsonb_build_array(source_id)::text,
       jsonb_build_object('source_hash',source_id,'deletion_sequence',d.sequence,'deleted_at',to_char(d.deleted_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')),0,0,'','{}')
     ON CONFLICT(tenant_id,user_id,kind,id) DO UPDATE SET payload=excluded.payload
      WHERE (e.payload->>'deleted_at')::timestamptz<d.deleted_at OR ((e.payload->>'deleted_at')::timestamptz=d.deleted_at AND (e.payload->>'deletion_sequence')::bigint<d.sequence);
     GET DIAGNOSTICS affected=ROW_COUNT;
     changed:=changed+affected;
   END LOOP;
   IF changed>0 THEN UPDATE auto_reply_state_owners SET version=version+1 WHERE tenant_id=d.tenant_id AND user_id=who; END IF;
 END LOOP;
END $$;
COMMENT ON FUNCTION sync_cloud_reply_deletion_boundary(candidate_deletions) IS '将真实删除截止时间写到授权成员的云编排状态；较新的本机证据下界不会被旧事件降低';
CREATE FUNCTION apply_new_cloud_reply_deletion_boundary() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM sync_cloud_reply_deletion_boundary(NEW);
 RETURN NEW;
END $$;
CREATE TRIGGER ar_cloud_reply_deletion_boundary AFTER INSERT ON candidate_deletions FOR EACH ROW EXECUTE FUNCTION apply_new_cloud_reply_deletion_boundary();
-- 为启用服务器编排前已删除的简历补记边界，不触碰任何在用的替代简历。
SELECT sync_cloud_reply_deletion_boundary(d) FROM candidate_deletions d ORDER BY d.sequence;
