-- 删除最后一条原话后，移除候选人原问与 HR 私人答复；仅保留下游岗位／公司使用的已归档招聘事实。
CREATE FUNCTION remove_orphan_auto_reply_question(qid uuid) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
 PERFORM 1 FROM auto_reply_cloud_questions WHERE id=qid FOR UPDATE;
 IF NOT FOUND OR EXISTS(SELECT 1 FROM auto_reply_cloud_question_sources WHERE question_id=qid) THEN RETURN; END IF;
 DELETE FROM auto_reply_cloud_questions WHERE id=qid AND (status<>'resolved' OR fact='' OR fact ~ '[[:alnum:]_.+-]+@[[:alnum:].-]+\.[[:alpha:]]{2,}|1[3-9][0-9]{9}');
 UPDATE auto_reply_cloud_questions SET question='',answer='',error='',dedupe_key='archived:'||id::text,version=version+1,updated_at=now()
 WHERE id=qid AND (question<>'' OR answer<>'' OR error<>'');
END $$;
COMMENT ON FUNCTION remove_orphan_auto_reply_question(uuid) IS '最后一条来源删除后清除提问与原答复，独立事实继续供岗位使用';
CREATE FUNCTION cleanup_auto_reply_question_source() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM remove_orphan_auto_reply_question(OLD.question_id);
 RETURN OLD;
END $$;
CREATE TRIGGER ar_question_source_removed AFTER DELETE ON auto_reply_cloud_question_sources FOR EACH ROW EXECUTE FUNCTION cleanup_auto_reply_question_source();
-- 升级时补清以往已失去全部来源的记录，不动仍有其他候选人来信的问题。
SELECT remove_orphan_auto_reply_question(q.id) FROM auto_reply_cloud_questions q WHERE NOT EXISTS(SELECT 1 FROM auto_reply_cloud_question_sources s WHERE s.question_id=q.id);
