-- 候选人表：保存打招呼后入库的候选人结构化信息与AI评分结果。
-- 说明：本表用于任务过程留痕与后续日志/复核，不保存浏览器运行态字段（如 element_ref）。

CREATE TABLE IF NOT EXISTS task_candidates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL REFERENCES task_runs(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    platform_id TEXT NOT NULL,
    platform_candidate_id TEXT NOT NULL DEFAULT '',
    candidate_name TEXT NOT NULL DEFAULT '',
    birth_ym TEXT NOT NULL DEFAULT '',
    phone TEXT NOT NULL DEFAULT '',
    email TEXT NOT NULL DEFAULT '',
    work_region TEXT NOT NULL DEFAULT '',
    work_years TEXT NOT NULL DEFAULT '',
    expected_salary_min INTEGER,
    expected_salary_max INTEGER,
    personal_description TEXT NOT NULL DEFAULT '',
    work_status TEXT NOT NULL DEFAULT '',
    expected_position TEXT NOT NULL DEFAULT '',
    online_status TEXT NOT NULL DEFAULT '',
    education_level TEXT NOT NULL DEFAULT '',
    basic_info TEXT NOT NULL DEFAULT '',
    raw_text TEXT NOT NULL DEFAULT '',
    filter_text TEXT NOT NULL DEFAULT '',
    work_experiences JSONB NOT NULL DEFAULT '[]'::jsonb,
    educations JSONB NOT NULL DEFAULT '[]'::jsonb,
    certificates JSONB NOT NULL DEFAULT '[]'::jsonb,
    honors JSONB NOT NULL DEFAULT '[]'::jsonb,
    project_experiences JSONB NOT NULL DEFAULT '[]'::jsonb,
    colleague_communications JSONB NOT NULL DEFAULT '[]'::jsonb,
    resume_attachment_url TEXT NOT NULL DEFAULT '',
    resume_attachment_extracted_text TEXT NOT NULL DEFAULT '',
    ai_detail_reason TEXT NOT NULL DEFAULT '',
    ai_detail_score NUMERIC(6, 2),
    ai_greet_reason TEXT NOT NULL DEFAULT '',
    ai_greet_score NUMERIC(6, 2),
    ai_review_reason TEXT NOT NULL DEFAULT '',
    ai_review_score NUMERIC(6, 2),
    ext JSONB NOT NULL DEFAULT '{}'::jsonb,
    first_seen_at TIMESTAMPTZ,
    detail_fetched_at TIMESTAMPTZ,
    greeted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE task_candidates IS '任务候选人入库表，保存打招呼后候选人信息与AI评分';
COMMENT ON COLUMN task_candidates.task_id IS '所属任务ID';
COMMENT ON COLUMN task_candidates.user_id IS '所属用户ID';
COMMENT ON COLUMN task_candidates.platform_id IS '招聘平台标识，如 boss/zhaopin/liepin';
COMMENT ON COLUMN task_candidates.platform_candidate_id IS '平台侧候选人原始ID';
COMMENT ON COLUMN task_candidates.candidate_name IS '候选人姓名，用于日志与列表展示';
COMMENT ON COLUMN task_candidates.birth_ym IS '出生年月，格式建议 YYYY-MM';
COMMENT ON COLUMN task_candidates.phone IS '手机号原文';
COMMENT ON COLUMN task_candidates.email IS '邮箱';
COMMENT ON COLUMN task_candidates.work_region IS '工作地区';
COMMENT ON COLUMN task_candidates.work_years IS '工作年限文本，如 3年/3-5年';
COMMENT ON COLUMN task_candidates.expected_salary_min IS '期望薪资最低值，单位建议元/月';
COMMENT ON COLUMN task_candidates.expected_salary_max IS '期望薪资最高值，单位建议元/月';
COMMENT ON COLUMN task_candidates.personal_description IS '个人描述';
COMMENT ON COLUMN task_candidates.work_status IS '工作状态，如 离职/在职/看机会';
COMMENT ON COLUMN task_candidates.expected_position IS '期望职位';
COMMENT ON COLUMN task_candidates.online_status IS '在线状态文本，如 在线/10分钟前在线/无';
COMMENT ON COLUMN task_candidates.education_level IS '学历主字段，如 本科/硕士';
COMMENT ON COLUMN task_candidates.basic_info IS '兼容字段：候选人基础信息摘要';
COMMENT ON COLUMN task_candidates.raw_text IS '兼容字段：平台抓取原始拼接文本';
COMMENT ON COLUMN task_candidates.filter_text IS '兼容字段：用于筛选流程的文本';
COMMENT ON COLUMN task_candidates.work_experiences IS '工作经历数组JSON';
COMMENT ON COLUMN task_candidates.educations IS '教育经历数组JSON';
COMMENT ON COLUMN task_candidates.certificates IS '资格证书数组JSON';
COMMENT ON COLUMN task_candidates.honors IS '所得荣誉数组JSON';
COMMENT ON COLUMN task_candidates.project_experiences IS '项目经验数组JSON';
COMMENT ON COLUMN task_candidates.colleague_communications IS '同事沟通记录数组JSON';
COMMENT ON COLUMN task_candidates.resume_attachment_url IS '简历附件文件URL';
COMMENT ON COLUMN task_candidates.resume_attachment_extracted_text IS '简历附件提取文本';
COMMENT ON COLUMN task_candidates.ai_detail_reason IS 'AI详情阶段原因';
COMMENT ON COLUMN task_candidates.ai_detail_score IS 'AI详情阶段分数';
COMMENT ON COLUMN task_candidates.ai_greet_reason IS 'AI打招呼阶段原因';
COMMENT ON COLUMN task_candidates.ai_greet_score IS 'AI打招呼阶段分数';
COMMENT ON COLUMN task_candidates.ai_review_reason IS 'AI复核阶段原因';
COMMENT ON COLUMN task_candidates.ai_review_score IS 'AI复核阶段分数';
COMMENT ON COLUMN task_candidates.ext IS '扩展字段，存放平台个性化信息';
COMMENT ON COLUMN task_candidates.first_seen_at IS '候选人首次发现时间';
COMMENT ON COLUMN task_candidates.detail_fetched_at IS '候选人详情抓取完成时间';
COMMENT ON COLUMN task_candidates.greeted_at IS '候选人打招呼成功时间';
COMMENT ON COLUMN task_candidates.created_at IS '记录创建时间';
COMMENT ON COLUMN task_candidates.updated_at IS '记录更新时间';

CREATE INDEX IF NOT EXISTS idx_task_candidates_task_created_at
    ON task_candidates(task_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_task_candidates_user_created_at
    ON task_candidates(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_task_candidates_platform_name
    ON task_candidates(platform_id, candidate_name);
