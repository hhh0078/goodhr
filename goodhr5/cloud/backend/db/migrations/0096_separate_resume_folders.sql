-- 文件作用：收藏夹独立于原始简历标签；保留历史物理表供回退使用，只清理0094明确自动回填且未人工维护的分类。
ALTER TABLE candidate_tags ADD COLUMN IF NOT EXISTS source_kind TEXT NOT NULL DEFAULT 'manual';
COMMENT ON TABLE candidate_tags IS '候选人个人或团队收藏夹；历史物理表名保留，原始简历技能标签独立存于candidate_profiles.library_fields.tags';
COMMENT ON COLUMN candidate_tags.id IS '收藏夹唯一ID，保留历史标签表物理列名';
COMMENT ON COLUMN candidate_tags.tenant_id IS '收藏夹所属团队ID';
COMMENT ON COLUMN candidate_tags.name IS '收藏夹名称，同一可见范围内不区分大小写且唯一';
COMMENT ON COLUMN candidate_tags.scope IS '收藏夹可见范围：personal仅本人，team当前团队成员';
COMMENT ON COLUMN candidate_tags.created_by_email IS '收藏夹创建者邮箱，个人收藏夹仅本人可见';
COMMENT ON COLUMN candidate_tags.created_at IS '收藏夹创建时间';
COMMENT ON COLUMN candidate_tags.updated_at IS '收藏夹最后人工编辑时间';
COMMENT ON COLUMN candidate_tags.source_kind IS '来源：manual人工管理收藏夹；legacy_resume_tag为0094从原简历标签自动回填且已核实的旧分类';
COMMENT ON TABLE candidate_tag_links IS '候选人与收藏夹的独立关联；保留历史物理表名，原始简历标签不参与此表';
COMMENT ON COLUMN candidate_tag_links.tenant_id IS '收藏夹关联所属团队ID，与收藏夹和候选人团队一致';
COMMENT ON COLUMN candidate_tag_links.tag_id IS '收藏夹ID，保留历史物理列名tag_id；不表示原始简历标签';
COMMENT ON COLUMN candidate_tag_links.candidate_id IS '收藏夹中的候选人ID，删除收藏夹仅解除关联';
COMMENT ON COLUMN candidate_tag_links.created_at IS '候选人加入收藏夹的时间';

-- 自动执行迁移与记账可能分属两个事务，因此新库用0094之前最近迁移至0094记账之间的时间窗口识别原始回填。
-- 已上线批次经生产只读核实：305个分类和405条关联统一创建于2026-10-14T17:07:42.429804Z。
DO $migration$
DECLARE
 legacy_applied_at TIMESTAMPTZ;
 previous_applied_at TIMESTAMPTZ;
BEGIN
 IF to_regclass('schema_migrations') IS NOT NULL THEN
  SELECT applied_at INTO legacy_applied_at FROM schema_migrations WHERE filename='0094_candidate_tags.sql';
  SELECT max(applied_at) INTO previous_applied_at FROM schema_migrations WHERE filename<'0094_candidate_tags.sql';
 END IF;

 UPDATE candidate_tags t SET source_kind='legacy_resume_tag'
 WHERE t.source_kind='manual' AND t.scope='personal' AND t.updated_at=t.created_at
 AND (
  t.created_at='2026-10-14T17:07:42.429804Z'::timestamptz
  OR (legacy_applied_at IS NOT NULL AND previous_applied_at IS NOT NULL
      AND t.created_at>=previous_applied_at AND t.created_at<=legacy_applied_at
      AND EXISTS (
       SELECT 1 FROM candidate_profiles cp JOIN users u ON u.id=cp.created_by_user_id
       CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(cp.library_fields->'tags')='array' THEN cp.library_fields->'tags' ELSE '[]'::jsonb END) label(value)
       WHERE cp.tenant_id=t.tenant_id AND u.email=t.created_by_email AND jsonb_typeof(label.value)='string'
         AND lower(btrim(label.value #>> '{}'))=lower(t.name)
      ))
 )
 -- 后来人工加入过候选人的分类必须保留，不能仅凭标签本身未改名就当成垃圾。
 AND NOT EXISTS (SELECT 1 FROM candidate_tag_links l WHERE l.tag_id=t.id AND l.created_at<>t.created_at)
 -- 对动态批次再次核实每条关联确实来自同一个原简历标签和原创建者。
 AND NOT EXISTS (
  SELECT 1 FROM candidate_tag_links l JOIN candidate_profiles cp ON cp.id=l.candidate_id JOIN users u ON u.id=cp.created_by_user_id
  WHERE l.tag_id=t.id AND (
   cp.tenant_id<>t.tenant_id OR u.email<>t.created_by_email
   OR NOT EXISTS (
    SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(cp.library_fields->'tags')='array' THEN cp.library_fields->'tags' ELSE '[]'::jsonb END) label(value)
    WHERE jsonb_typeof(label.value)='string' AND lower(btrim(label.value #>> '{}'))=lower(t.name)
   )
  )
 );

 DELETE FROM candidate_tags t
 WHERE t.source_kind='legacy_resume_tag' AND t.updated_at=t.created_at
   AND NOT EXISTS (SELECT 1 FROM candidate_tag_links l WHERE l.tag_id=t.id AND l.created_at<>t.created_at);
END;
$migration$;

-- 不更新candidate_profiles任何字段；原始标签显示、编辑和筛选恢复使用原JSON内容。
