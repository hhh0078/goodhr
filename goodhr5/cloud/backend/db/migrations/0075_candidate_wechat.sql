-- 本迁移为候选人简历补充微信号字段，使自动回复从简历中识别出的联系方式可以长期保存。

ALTER TABLE candidate_profiles
    ADD COLUMN IF NOT EXISTS wechat TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN candidate_profiles.wechat IS '候选人微信号，来源于在线简历、附件简历或AI结构化结果';
