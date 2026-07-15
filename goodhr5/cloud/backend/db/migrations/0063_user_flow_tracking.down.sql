-- 本回滚迁移删除用户招聘流程快照和事件表。
DROP TABLE IF EXISTS user_flow_events;
ALTER TABLE users DROP COLUMN IF EXISTS flow_state;
ALTER TABLE users ADD COLUMN IF NOT EXISTS onboarding JSONB NOT NULL DEFAULT jsonb_build_object(
    'completed', false,
    'completed_at', NULL
);
COMMENT ON COLUMN users.onboarding IS '用户旧版新手教学状态JSON，包含completed是否完成和completed_at完成时间';
