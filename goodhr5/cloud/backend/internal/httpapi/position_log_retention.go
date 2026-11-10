// 本文件负责北京时间每天05:00分批清理七天前的岗位运行日志，不删除统计汇总、资金流水或日志分享快照。
package httpapi

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

const positionLogRetention = 7 * 24 * time.Hour
const positionLogCleanupBatch = 5000
const positionLogRetentionLock int64 = 0x4748524c4f4737

// PositionLogCleanupResult 记录本轮实际删除数量、固定时间界线及是否由其他实例持锁执行。
type PositionLogCleanupResult struct {
	DeletedRecords int64
	Cutoff         time.Time
	Skipped        bool
}

// PositionLogRetentionService 复用岗位日志存储，只在真实HTTP服务启动后运行单一每日清理协程。
type PositionLogRetentionService struct {
	store *PostgresPositionLogStore
	once  sync.Once
}

// newPositionLogRetentionService 从Redis包装中取得实际PostgreSQL持久层；纯内存存储不启动后台清理。
func newPositionLogRetentionService(store PositionLogStore) *PositionLogRetentionService {
	if cache, ok := store.(*RedisPositionLogStore); ok && cache != nil {
		store = cache.persistent
	}
	postgres, ok := store.(*PostgresPositionLogStore)
	if !ok || postgres == nil || postgres.db == nil {
		return nil
	}
	return &PositionLogRetentionService{store: postgres}
}

// nextPositionLogCleanupAt 按北京时间计算下一次05:00，进程重启不立即删除历史数据。
func nextPositionLogCleanupAt(now time.Time) time.Time {
	local := now.In(chinaLocation())
	next := time.Date(local.Year(), local.Month(), local.Day(), 5, 0, 0, 0, local.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// StartDailyMaintenance 启动正式服务的每日维护；构造测试服务及读取路由时不启动清理任务。
func (s *Server) StartDailyMaintenance(ctx context.Context) {
	if s != nil {
		s.positionLogRetention.Start(ctx)
	}
}

// Start 每天05:00执行一次清理，取消上下文后停止等待，同一服务重复调用不会重复启动。
func (s *PositionLogRetentionService) Start(ctx context.Context) {
	if s == nil || s.store == nil || ctx == nil || ctx.Err() != nil {
		return
	}
	s.once.Do(func() {
		go func() {
			for {
				next := nextPositionLogCleanupAt(time.Now())
				log.Printf("岗位日志定时清理：下次北京时间%s，保留最近7天", next.Format("2006-01-02 15:04:05"))
				timer := time.NewTimer(time.Until(next))
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				started := time.Now()
				job, cancel := context.WithTimeout(ctx, 30*time.Minute)
				result, err := s.store.PrunePositionLogs(job, started.Add(-positionLogRetention))
				cancel()
				if err != nil {
					log.Printf("岗位日志定时清理未完成：已删除%d条，耗时%s，错误：%v", result.DeletedRecords, time.Since(started).Round(time.Second), err)
				} else if result.Skipped {
					log.Print("岗位日志定时清理：其他实例正在处理，本次跳过")
				} else {
					log.Printf("岗位日志定时清理完成：删除%d条，保留%s及之后的记录，耗时%s", result.DeletedRecords, result.Cutoff.In(chinaLocation()).Format("2006-01-02 15:04:05"), time.Since(started).Round(time.Second))
				}
			}
		}()
	})
}

// PrunePositionLogs 使用数据库事务锁防止多实例重复执行，每批独立提交，保留边界及之后的日志。
func (s *PostgresPositionLogStore) PrunePositionLogs(ctx context.Context, cutoff time.Time) (PositionLogCleanupResult, error) {
	result := PositionLogCleanupResult{Cutoff: cutoff}
	if s == nil || s.db == nil || cutoff.IsZero() || !cutoff.Before(time.Now()) {
		return result, errors.New("运行日志清理存储或保留时间无效")
	}
	guard, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer guard.Rollback()
	var locked bool
	if err = guard.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock($1)`, positionLogRetentionLock).Scan(&locked); err != nil {
		return result, err
	}
	if !locked {
		result.Skipped = true
		return result, nil
	}
	for {
		batch, cancel := context.WithTimeout(ctx, 30*time.Second)
		deleted, err := s.db.ExecContext(batch, `
			WITH expired AS (
				SELECT id FROM position_logs WHERE created_at < $1
				ORDER BY created_at, id LIMIT $2 FOR UPDATE SKIP LOCKED
			)
			DELETE FROM position_logs USING expired WHERE position_logs.id=expired.id`, cutoff, positionLogCleanupBatch)
		cancel()
		if err != nil {
			return result, err
		}
		count, err := deleted.RowsAffected()
		if err != nil {
			return result, err
		}
		result.DeletedRecords += count
		if count < positionLogCleanupBatch {
			return result, nil
		}
	}
}
