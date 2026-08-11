-- 本迁移把候选人头像限制为 GoodHR 自托管公开路径，并清理旧招聘平台远程地址。

-- 清空候选人主体中已有的远程头像，等待本地 Agent 下次处理时重新同步。
UPDATE candidate_profiles
SET avatar_url = '', updated_at = now()
WHERE avatar_url <> ''
  AND avatar_url !~ '^/api/public/candidate-avatars/[0-9a-f]{64}[.]png$';

-- 清空旧推荐快照中的远程头像，避免公开报告继续直接访问招聘平台资源。
UPDATE candidate_recommendations
SET report_data = jsonb_set(report_data, '{candidate,avatar_url}', to_jsonb(''::text), true),
    updated_at = now()
WHERE COALESCE(report_data #>> '{candidate,avatar_url}', '') <> ''
  AND (report_data #>> '{candidate,avatar_url}') !~ '^/api/public/candidate-avatars/[0-9a-f]{64}[.]png$';

-- 数据库最终兜底：头像只允许为空或保存 GoodHR 自托管路径。
ALTER TABLE candidate_profiles
    DROP CONSTRAINT IF EXISTS candidate_profiles_avatar_url_self_hosted;

ALTER TABLE candidate_profiles
    ADD CONSTRAINT candidate_profiles_avatar_url_self_hosted
    CHECK (avatar_url = '' OR avatar_url ~ '^/api/public/candidate-avatars/[0-9a-f]{64}[.]png$');

COMMENT ON COLUMN candidate_profiles.avatar_url IS 'GoodHR自托管候选人头像公开路径，只允许系统生成的PNG地址';
