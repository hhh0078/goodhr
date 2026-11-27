-- 文件作用：新增自动回复展示日志，按账号保留最近1000条；权威业务账本及发送去重记录不受清理影响。
CREATE TABLE auto_reply_logs (
 sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 tenant_id uuid NOT NULL,
 user_id uuid NOT NULL,
 kind text NOT NULL DEFAULT 'runtime_events' CHECK(kind='runtime_events'),
 event_id text NOT NULL,
 payload jsonb NOT NULL,
 UNIQUE(tenant_id,user_id,event_id),
 FOREIGN KEY(tenant_id,user_id,kind,event_id) REFERENCES auto_reply_cloud_entities(tenant_id,user_id,kind,id) ON DELETE CASCADE
);
COMMENT ON TABLE auto_reply_logs IS '自动回复工作台独立展示日志，每个登录账号最多1000条，随权威事件删除而清除';
COMMENT ON COLUMN auto_reply_logs.sequence IS '展示日志写入顺序及分页游标';
COMMENT ON COLUMN auto_reply_logs.tenant_id IS '归属团队编号';
COMMENT ON COLUMN auto_reply_logs.user_id IS '归属登录账号编号';
COMMENT ON COLUMN auto_reply_logs.kind IS '来源权威实体类型，固定为运行事件';
COMMENT ON COLUMN auto_reply_logs.event_id IS '来源事件稳定编号，重复提交不增加记录';
COMMENT ON COLUMN auto_reply_logs.payload IS '运行事件快照，接口仅输出脱敏后的允许字段';
CREATE INDEX ar_logs_owner ON auto_reply_logs(tenant_id,user_id,sequence DESC);

-- sync_auto_reply_log 在账号锁内同步展示记录；更新已淘汰事件不会让旧日志重新出现。
CREATE FUNCTION sync_auto_reply_log() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind<>'runtime_events' OR NEW.payload->>'title'='ai_request_log_cutoff' THEN RETURN NEW; END IF;
 PERFORM 1 FROM auto_reply_state_owners WHERE tenant_id=NEW.tenant_id AND user_id=NEW.user_id FOR UPDATE;
 IF TG_OP='INSERT' THEN
  INSERT INTO auto_reply_logs(tenant_id,user_id,event_id,payload) VALUES(NEW.tenant_id,NEW.user_id,NEW.id,NEW.payload)
  ON CONFLICT(tenant_id,user_id,event_id) DO UPDATE SET payload=excluded.payload;
 ELSE
  UPDATE auto_reply_logs SET payload=NEW.payload WHERE tenant_id=NEW.tenant_id AND user_id=NEW.user_id AND event_id=NEW.id;
 END IF;
 DELETE FROM auto_reply_logs WHERE tenant_id=NEW.tenant_id AND user_id=NEW.user_id AND sequence<=
  (SELECT sequence FROM auto_reply_logs WHERE tenant_id=NEW.tenant_id AND user_id=NEW.user_id ORDER BY sequence DESC OFFSET 1000 LIMIT 1);
 RETURN NEW;
END $$;
COMMENT ON FUNCTION sync_auto_reply_log() IS '串行维护账号最近千条展示日志，更新幂等且不清理权威账本';
-- 迁移已有最近日志，让升级后立即可以查看上一次运行；只复制尚未删除的权威事件。
INSERT INTO auto_reply_logs(tenant_id,user_id,event_id,payload)
 SELECT tenant_id,user_id,id,payload FROM (
  SELECT e.*,row_number() OVER(PARTITION BY tenant_id,user_id ORDER BY sequence DESC,id DESC) AS rn
  FROM auto_reply_cloud_entities e WHERE kind='runtime_events' AND coalesce(payload->>'title','')<>'ai_request_log_cutoff'
 ) recent WHERE rn<=1000 ORDER BY tenant_id,user_id,sequence,id;
CREATE TRIGGER ar_display_log AFTER INSERT OR UPDATE ON auto_reply_cloud_entities FOR EACH ROW EXECUTE FUNCTION sync_auto_reply_log();
