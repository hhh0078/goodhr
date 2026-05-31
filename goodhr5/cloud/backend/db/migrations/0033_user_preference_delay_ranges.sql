-- 本迁移新增清晰的任务延时配置，并调整摸鱼休息默认范围。
ALTER TABLE user_preferences
    ADD COLUMN IF NOT EXISTS detail_open_delay_min DOUBLE PRECISION NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS detail_open_delay_max DOUBLE PRECISION NOT NULL DEFAULT 2,
    ADD COLUMN IF NOT EXISTS detail_close_delay_min DOUBLE PRECISION NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS detail_close_delay_max DOUBLE PRECISION NOT NULL DEFAULT 2,
    ADD COLUMN IF NOT EXISTS greet_before_delay_min DOUBLE PRECISION NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS greet_before_delay_max DOUBLE PRECISION NOT NULL DEFAULT 2;

COMMENT ON COLUMN user_preferences.detail_open_delay_min IS '点击候选人详情前的最小等待秒数';
COMMENT ON COLUMN user_preferences.detail_open_delay_max IS '点击候选人详情前的最大等待秒数';
COMMENT ON COLUMN user_preferences.detail_close_delay_min IS '关闭候选人详情前的最小等待秒数';
COMMENT ON COLUMN user_preferences.detail_close_delay_max IS '关闭候选人详情前的最大等待秒数';
COMMENT ON COLUMN user_preferences.greet_before_delay_min IS '点击打招呼前的最小等待秒数';
COMMENT ON COLUMN user_preferences.greet_before_delay_max IS '点击打招呼前的最大等待秒数';
COMMENT ON COLUMN user_preferences.rest_after_candidates_min IS '处理多少候选人后休息的最小人数';
COMMENT ON COLUMN user_preferences.rest_after_candidates_max IS '处理多少候选人后休息的最大人数';
COMMENT ON COLUMN user_preferences.rest_times_min IS '单次任务最多休息次数的最小值';
COMMENT ON COLUMN user_preferences.rest_times_max IS '单次任务最多休息次数的最大值';
COMMENT ON COLUMN user_preferences.rest_duration_min IS '每次休息的最小分钟数';
COMMENT ON COLUMN user_preferences.rest_duration_max IS '每次休息的最大分钟数';

ALTER TABLE user_preferences
    ALTER COLUMN rest_after_candidates_min SET DEFAULT 40,
    ALTER COLUMN rest_after_candidates_max SET DEFAULT 70,
    ALTER COLUMN rest_times_min SET DEFAULT 2,
    ALTER COLUMN rest_times_max SET DEFAULT 3,
    ALTER COLUMN rest_duration_min SET DEFAULT 2,
    ALTER COLUMN rest_duration_max SET DEFAULT 7;

UPDATE user_preferences
SET rest_after_candidates_min = 40,
    rest_after_candidates_max = 70,
    rest_times_min = 2,
    rest_times_max = 3,
    rest_duration_min = 2,
    rest_duration_max = 7,
    updated_at = now()
WHERE rest_after_candidates_min = 0
  AND rest_after_candidates_max = 0
  AND rest_times_min = 0
  AND rest_times_max = 0
  AND rest_duration_min = 0
  AND rest_duration_max = 0;
