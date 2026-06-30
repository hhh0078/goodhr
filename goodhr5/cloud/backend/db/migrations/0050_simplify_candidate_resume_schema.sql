-- 本迁移将候选人简历库收敛为标准简历模型：基础字段扁平化，经历类字段 JSONB，AI 两次分析和原文单独保存。

ALTER TABLE candidate_profiles
  ADD COLUMN IF NOT EXISTS ai_detail_score DOUBLE PRECISION,
  ADD COLUMN IF NOT EXISTS ai_detail_reason TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS ai_greet_score DOUBLE PRECISION,
  ADD COLUMN IF NOT EXISTS ai_greet_reason TEXT NOT NULL DEFAULT '';

ALTER TABLE candidate_profiles
  DROP COLUMN IF EXISTS filter_text,
  DROP COLUMN IF EXISTS resume_attachment_url,
  DROP COLUMN IF EXISTS resume_attachment_extracted_text,
  DROP COLUMN IF EXISTS ext;

COMMENT ON COLUMN candidate_profiles.candidate_name IS '候选人姓名';
COMMENT ON COLUMN candidate_profiles.birth_ym IS '出生年月，格式 YYYY-MM';
COMMENT ON COLUMN candidate_profiles.phone IS '候选人手机号';
COMMENT ON COLUMN candidate_profiles.email IS '候选人邮箱';
COMMENT ON COLUMN candidate_profiles.work_region IS '当前或期望工作地区';
COMMENT ON COLUMN candidate_profiles.work_years IS '工作年限文本';
COMMENT ON COLUMN candidate_profiles.expected_salary_min IS '期望最低薪资，单位 K';
COMMENT ON COLUMN candidate_profiles.expected_salary_max IS '期望最高薪资，单位 K';
COMMENT ON COLUMN candidate_profiles.education_level IS '最高学历';
COMMENT ON COLUMN candidate_profiles.expected_position IS '期望岗位';
COMMENT ON COLUMN candidate_profiles.online_status IS '平台在线状态';
COMMENT ON COLUMN candidate_profiles.personal_description IS '个人优势或自我介绍';
COMMENT ON COLUMN candidate_profiles.work_status IS '求职状态';
COMMENT ON COLUMN candidate_profiles.work_experiences IS '工作经历数组，字段为 company_name、position_name、content、start_ym、end_ym';
COMMENT ON COLUMN candidate_profiles.educations IS '教育经历数组，字段为 school_name、major_name、education_level、start_ym、end_ym';
COMMENT ON COLUMN candidate_profiles.certificates IS '证书数组，字段为 certificate_name、issued_by、issued_ym';
COMMENT ON COLUMN candidate_profiles.honors IS '荣誉数组，字段为 honor_name、issued_by、issued_ym、description';
COMMENT ON COLUMN candidate_profiles.project_experiences IS '项目经历数组，字段为 project_name、role_name、content、start_ym、end_ym';
COMMENT ON COLUMN candidate_profiles.colleague_communications IS '沟通记录数组，字段为 communicator_name、communicated_at、content';
COMMENT ON COLUMN candidate_profiles.ai_detail_score IS '第一次详情分析分数';
COMMENT ON COLUMN candidate_profiles.ai_detail_reason IS '第一次详情分析原因';
COMMENT ON COLUMN candidate_profiles.ai_greet_score IS '第二次打招呼分析分数';
COMMENT ON COLUMN candidate_profiles.ai_greet_reason IS '第二次打招呼分析原因';
COMMENT ON COLUMN candidate_profiles.raw_text IS '平台简历原文，只保存一份';
