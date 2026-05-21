CREATE TABLE IF NOT EXISTS user_preferences (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    ai_model TEXT NOT NULL DEFAULT '',
    click_frequency INTEGER NOT NULL DEFAULT 80,
    scroll_delay_min INTEGER NOT NULL DEFAULT 3,
    scroll_delay_max INTEGER NOT NULL DEFAULT 8,
    list_view_delay_min DOUBLE PRECISION NOT NULL DEFAULT 1,
    list_view_delay_max DOUBLE PRECISION NOT NULL DEFAULT 2,
    detail_view_delay_min DOUBLE PRECISION NOT NULL DEFAULT 1,
    detail_view_delay_max DOUBLE PRECISION NOT NULL DEFAULT 2,
    greet_delay_min DOUBLE PRECISION NOT NULL DEFAULT 1,
    greet_delay_max DOUBLE PRECISION NOT NULL DEFAULT 2,
    rest_after_candidates_min INTEGER NOT NULL DEFAULT 0,
    rest_after_candidates_max INTEGER NOT NULL DEFAULT 0,
    rest_times_min INTEGER NOT NULL DEFAULT 0,
    rest_times_max INTEGER NOT NULL DEFAULT 0,
    rest_duration_min DOUBLE PRECISION NOT NULL DEFAULT 0,
    rest_duration_max DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE user_preferences IS '用户个人运行配置';
COMMENT ON COLUMN user_preferences.ai_model IS '个人默认 AI 模型';
