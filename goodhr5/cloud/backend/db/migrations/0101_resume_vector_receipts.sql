-- 文件作用说明：保存可重建的简历向量及分片成功回执；不修改原始简历和财务记录。
CREATE TABLE IF NOT EXISTS candidate_resume_vectors (
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    candidate_id uuid NOT NULL REFERENCES candidate_profiles(id) ON DELETE CASCADE,
    resume_version bigint NOT NULL,
    profile text NOT NULL,
    complete boolean NOT NULL DEFAULT false,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id,candidate_id)
);
COMMENT ON TABLE candidate_resume_vectors IS '当前简历版本的向量化与索引回执，可从原始资料重建';
COMMENT ON COLUMN candidate_resume_vectors.tenant_id IS '所属团队，查询必须显式限定';
COMMENT ON COLUMN candidate_resume_vectors.candidate_id IS '真实候选人编号，一份简历一条回执';
COMMENT ON COLUMN candidate_resume_vectors.resume_version IS '生成向量时的简历行版本';
COMMENT ON COLUMN candidate_resume_vectors.profile IS '固定模型、维度、分片和归一化方案版本';
COMMENT ON COLUMN candidate_resume_vectors.complete IS '当前简历所有分片已生成并成功写入搜索索引';
COMMENT ON COLUMN candidate_resume_vectors.payload IS '脱敏原文片段及向量，包含各片段成功回执';
COMMENT ON COLUMN candidate_resume_vectors.updated_at IS '最后保存时间，不代表原始简历更新时间';
