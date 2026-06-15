// 本文件负责保存系统级按日统计数据，例如官网展示的已处理简历数。
package httpapi

import (
	"context"
	"database/sql"
	"time"
)

// SystemDailyStats 表示某一天的系统公开统计。
type SystemDailyStats struct {
	StatDate             string
	ProcessedResumeCount int
}

// SystemDailyStatsStore 定义系统按日统计存储接口。
type SystemDailyStatsStore interface {
	IncrementProcessedResumes(count int) error
	TodayStats() (SystemDailyStats, error)
}

// MemorySystemDailyStatsStore 是系统按日统计的内存实现，供测试和本地开发使用。
type MemorySystemDailyStatsStore struct {
	items map[string]SystemDailyStats
	now   func() time.Time
}

// NewMemorySystemDailyStatsStore 创建内存系统按日统计存储。
func NewMemorySystemDailyStatsStore() *MemorySystemDailyStatsStore {
	return &MemorySystemDailyStatsStore{
		items: map[string]SystemDailyStats{},
		now:   time.Now,
	}
}

// IncrementProcessedResumes 累加当天已处理简历数量。
// count 为本次新增处理简历数量，小于等于 0 时忽略。
func (s *MemorySystemDailyStatsStore) IncrementProcessedResumes(count int) error {
	if count <= 0 {
		return nil
	}
	today := s.now().In(time.Local).Format(time.DateOnly)
	item := s.items[today]
	item.StatDate = today
	item.ProcessedResumeCount += count
	s.items[today] = item
	return nil
}

// TodayStats 返回当天系统统计。
func (s *MemorySystemDailyStatsStore) TodayStats() (SystemDailyStats, error) {
	today := s.now().In(time.Local).Format(time.DateOnly)
	item := s.items[today]
	item.StatDate = today
	return item, nil
}

// PostgresSystemDailyStatsStore 是系统按日统计的 PostgreSQL 实现。
type PostgresSystemDailyStatsStore struct {
	db *sql.DB
}

// NewPostgresSystemDailyStatsStore 创建 PostgreSQL 系统按日统计存储。
func NewPostgresSystemDailyStatsStore(db *sql.DB) *PostgresSystemDailyStatsStore {
	return &PostgresSystemDailyStatsStore{db: db}
}

// IncrementProcessedResumes 累加 PostgreSQL 中当天已处理简历数量。
// count 为本次新增处理简历数量，小于等于 0 时忽略。
func (s *PostgresSystemDailyStatsStore) IncrementProcessedResumes(count int) error {
	if count <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO system_daily_stats (stat_date, processed_resume_count, created_at, updated_at)
		VALUES (CURRENT_DATE, $1, now(), now())
		ON CONFLICT (stat_date)
		DO UPDATE SET
			processed_resume_count = system_daily_stats.processed_resume_count + EXCLUDED.processed_resume_count,
			updated_at = now()
	`, count)
	return err
}

// TodayStats 返回 PostgreSQL 中当天系统统计。
func (s *PostgresSystemDailyStatsStore) TodayStats() (SystemDailyStats, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var item SystemDailyStats
	err := s.db.QueryRowContext(ctx, `
		SELECT CURRENT_DATE::text, COALESCE(processed_resume_count, 0)::int
		FROM system_daily_stats
		WHERE stat_date = CURRENT_DATE
	`).Scan(&item.StatDate, &item.ProcessedResumeCount)
	if err == sql.ErrNoRows {
		item.StatDate = time.Now().In(time.Local).Format(time.DateOnly)
		return item, nil
	}
	return item, err
}
