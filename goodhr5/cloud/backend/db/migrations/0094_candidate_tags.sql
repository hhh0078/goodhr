-- 文件作用：新增个人和团队简历标签及独立关联，旧标签仅回填创建者个人标签，不修改原简历行。
CREATE TABLE IF NOT EXISTS candidate_tags (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 50 AND name=btrim(name)),
 scope TEXT NOT NULL CHECK (scope IN ('personal','team')),
 created_by_email TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,id)
);
COMMENT ON TABLE candidate_tags IS '受管理的简历标签，个人仅本人可见，团队对当前团队成员可见';
COMMENT ON COLUMN candidate_tags.id IS '标签唯一ID';
COMMENT ON COLUMN candidate_tags.tenant_id IS '所属团队ID';
COMMENT ON COLUMN candidate_tags.name IS '标签名称，同一可见范围内不区分大小写且唯一';
COMMENT ON COLUMN candidate_tags.scope IS '可见范围：personal个人标签，team团队标签';
COMMENT ON COLUMN candidate_tags.created_by_email IS '标签创建者邮箱，个人标签的唯一可见用户';
COMMENT ON COLUMN candidate_tags.created_at IS '标签创建时间';
COMMENT ON COLUMN candidate_tags.updated_at IS '标签最后编辑时间';
CREATE UNIQUE INDEX IF NOT EXISTS idx_candidate_tags_personal_name ON candidate_tags(tenant_id,created_by_email,lower(name)) WHERE scope='personal';
CREATE UNIQUE INDEX IF NOT EXISTS idx_candidate_tags_team_name ON candidate_tags(tenant_id,lower(name)) WHERE scope='team';
CREATE UNIQUE INDEX IF NOT EXISTS idx_candidate_profiles_tenant_identity ON candidate_profiles(tenant_id,id);

CREATE TABLE IF NOT EXISTS candidate_tag_links (
 tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 tag_id UUID NOT NULL,
 candidate_id UUID NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(tag_id,candidate_id),
 CONSTRAINT fk_candidate_tag_links_tag FOREIGN KEY(tenant_id,tag_id) REFERENCES candidate_tags(tenant_id,id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
 CONSTRAINT fk_candidate_tag_links_candidate FOREIGN KEY(tenant_id,candidate_id) REFERENCES candidate_profiles(tenant_id,id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED
);
COMMENT ON TABLE candidate_tag_links IS '候选人与标签的独立关联，删除标签仅删除关联，保留简历';
COMMENT ON COLUMN candidate_tag_links.tenant_id IS '所属团队ID，与标签和候选人团队必须一致';
COMMENT ON COLUMN candidate_tag_links.tag_id IS '已存在且有权限选择的标签ID';
COMMENT ON COLUMN candidate_tag_links.candidate_id IS '关联候选人ID，删除标签不会删除候选人';
COMMENT ON COLUMN candidate_tag_links.created_at IS '标签关联时间';
CREATE INDEX IF NOT EXISTS idx_candidate_tag_links_candidate ON candidate_tag_links(tenant_id,candidate_id);

-- 历史自由文本标签归原简历创建者个人所有，不把其发布为团队标签。
INSERT INTO candidate_tags(tenant_id,name,scope,created_by_email)
 SELECT DISTINCT ON (cp.tenant_id,u.email,lower(btrim(label.value #>> '{}')))
 cp.tenant_id,btrim(label.value #>> '{}'),'personal',u.email
 FROM candidate_profiles cp JOIN users u ON u.id=cp.created_by_user_id
 CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(cp.library_fields->'tags')='array' THEN cp.library_fields->'tags' ELSE '[]'::jsonb END) label(value)
 WHERE jsonb_typeof(label.value)='string' AND char_length(btrim(label.value #>> '{}')) BETWEEN 1 AND 50
 ORDER BY cp.tenant_id,u.email,lower(btrim(label.value #>> '{}')),btrim(label.value #>> '{}')
 ON CONFLICT DO NOTHING;
INSERT INTO candidate_tag_links(tenant_id,tag_id,candidate_id)
 SELECT DISTINCT cp.tenant_id,t.id,cp.id FROM candidate_profiles cp JOIN users u ON u.id=cp.created_by_user_id
 CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(cp.library_fields->'tags')='array' THEN cp.library_fields->'tags' ELSE '[]'::jsonb END) label(value)
 JOIN candidate_tags t ON t.tenant_id=cp.tenant_id AND t.scope='personal' AND t.created_by_email=u.email AND lower(t.name)=lower(btrim(label.value #>> '{}'))
 WHERE jsonb_typeof(label.value)='string' ON CONFLICT DO NOTHING;
