-- 本迁移新增系统按日统计表，用于官网展示全站已处理简历数量。
CREATE TABLE IF NOT EXISTS system_daily_stats (
    stat_date DATE PRIMARY KEY,
    processed_resume_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE system_daily_stats IS '系统按日统计表，用于保存官网公开展示的全站运营统计';
COMMENT ON COLUMN system_daily_stats.stat_date IS '统计日期';
COMMENT ON COLUMN system_daily_stats.processed_resume_count IS '当天已处理简历数量，由本地程序候选人去重后上报累加';
COMMENT ON COLUMN system_daily_stats.created_at IS '统计记录创建时间';
COMMENT ON COLUMN system_daily_stats.updated_at IS '统计记录更新时间';
