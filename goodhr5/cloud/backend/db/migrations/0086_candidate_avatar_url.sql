-- 为候选人简历增加招聘平台头像地址，供简历库列表和详情统一展示。

ALTER TABLE candidate_profiles
    ADD COLUMN IF NOT EXISTS avatar_url TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN candidate_profiles.avatar_url IS '候选人在招聘平台显示的头像地址';
