-- 文件作用：把HR已核实事实一次性写入真实可编辑资料，并记录已合并版本，防止人工删改后旧事实复活。
ALTER TABLE auto_reply_cloud_questions ADD COLUMN applied_version integer NOT NULL DEFAULT 0;
ALTER TABLE auto_reply_cloud_questions ADD COLUMN applied_fact text NOT NULL DEFAULT '';
ALTER TABLE auto_reply_cloud_questions ADD COLUMN applied_target text NOT NULL DEFAULT '';
ALTER TABLE auto_reply_cloud_questions ADD COLUMN applied_company_profile_id uuid REFERENCES tenant_company_profiles(id) ON DELETE SET NULL;
COMMENT ON COLUMN auto_reply_cloud_questions.applied_version IS '已经合入可编辑资料的答复版本，旧答复重放不能重复写入';
COMMENT ON COLUMN auto_reply_cloud_questions.applied_fact IS '本问题上次实际写入正文的事实片段，仅用于同问题新版精确替换';
COMMENT ON COLUMN auto_reply_cloud_questions.applied_target IS '上次实际写入岗位、公司或两者的归属';
COMMENT ON COLUMN auto_reply_cloud_questions.applied_company_profile_id IS '上次写入的公司档案，用于识别岗位换公司后的历史片段';

CREATE TABLE auto_reply_question_answer_history (
 tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 question_id uuid NOT NULL REFERENCES auto_reply_cloud_questions(id) ON DELETE CASCADE,
 version integer NOT NULL CHECK(version>0),answer text NOT NULL,fact text NOT NULL,publish_target text NOT NULL,
 company_profile_id uuid REFERENCES tenant_company_profiles(id) ON DELETE SET NULL,
 created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(question_id,version)
);
COMMENT ON TABLE auto_reply_question_answer_history IS 'HR已归档答复的版本审计，不作为下一轮招聘事实来源';
COMMENT ON COLUMN auto_reply_question_answer_history.tenant_id IS '答复所属团队';
COMMENT ON COLUMN auto_reply_question_answer_history.question_id IS '原岗位问题编号，问题删除时级联清理';
COMMENT ON COLUMN auto_reply_question_answer_history.version IS '本次已完成归档的答复版本';
COMMENT ON COLUMN auto_reply_question_answer_history.answer IS 'HR原始答复，最后候选来源清理后同步清空';
COMMENT ON COLUMN auto_reply_question_answer_history.fact IS '本版通过校验的可公开事实，仅保留归档审计';
COMMENT ON COLUMN auto_reply_question_answer_history.publish_target IS '本版归入岗位、公司或两者';
COMMENT ON COLUMN auto_reply_question_answer_history.company_profile_id IS '本版事实归入的公司';
COMMENT ON COLUMN auto_reply_question_answer_history.created_at IS '本版归档完成时间';
INSERT INTO auto_reply_question_answer_history(tenant_id,question_id,version,answer,fact,publish_target,company_profile_id)
SELECT tenant_id,id,version,answer,fact,publish_target,company_profile_id FROM auto_reply_cloud_questions WHERE status='resolved' AND fact<>'';

-- 原问题失去最后候选来源时，审计中的原始答复遵循相同的正文清理规则。
CREATE FUNCTION clear_reply_answer_history_private_text() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.question='' THEN UPDATE auto_reply_question_answer_history SET answer='' WHERE question_id=NEW.id; END IF;
 RETURN NEW;
END;
$$;
COMMENT ON FUNCTION clear_reply_answer_history_private_text() IS '清理失去候选来源的问题时，同步清空历版HR原始答复';
CREATE TRIGGER clear_reply_answer_history_private_text AFTER UPDATE OF question ON auto_reply_cloud_questions FOR EACH ROW EXECUTE FUNCTION clear_reply_answer_history_private_text();

-- 保留原正文，只把尚未出现的旧已确认事实追加一次；迁移后人工可直接修改或删除这些文字。
WITH additions AS (
 SELECT q.position_id,string_agg(DISTINCT q.fact,E'\n' ORDER BY q.fact) AS facts
 FROM auto_reply_cloud_questions q JOIN position_auto_reply_configs c ON c.position_id=q.position_id AND c.tenant_id=q.tenant_id
 WHERE q.status='resolved' AND q.fact<>'' AND q.publish_target IN ('position','both') AND strpos(c.position_description,q.fact)=0
 GROUP BY q.position_id
)
UPDATE position_auto_reply_configs c SET position_description=concat_ws(E'\n\n',nullif(btrim(c.position_description),''),'【HR 已确认补充】'||E'\n'||a.facts),version=c.version+1,updated_at=now()
FROM additions a WHERE c.position_id=a.position_id;

WITH additions AS (
 SELECT q.company_profile_id,string_agg(DISTINCT q.fact,E'\n' ORDER BY q.fact) AS facts
 FROM auto_reply_cloud_questions q JOIN tenant_company_profiles c ON c.id=q.company_profile_id AND c.tenant_id=q.tenant_id
 WHERE q.status='resolved' AND q.fact<>'' AND q.publish_target IN ('company','both') AND strpos(c.extra_info,q.fact)=0
 GROUP BY q.company_profile_id
)
UPDATE tenant_company_profiles c SET extra_info=concat_ws(E'\n\n',nullif(btrim(c.extra_info),''),'【HR 已确认补充】'||E'\n'||a.facts),updated_at=now()
FROM additions a WHERE c.id=a.company_profile_id;

UPDATE auto_reply_cloud_questions q SET applied_version=version,applied_fact=fact,applied_target=publish_target,applied_company_profile_id=company_profile_id
WHERE q.status='resolved' AND q.fact<>'';
