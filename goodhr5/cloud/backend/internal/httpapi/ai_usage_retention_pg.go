// 本文件在一个PostgreSQL事务中汇总并删除过期AI消费明细，按日保留金额，不触碰用户余额、分笔资金、充值退款记录。
package httpapi

import (
	"context"
	"database/sql"
	"time"
)

// PruneAIUsage 每批锁定有限消费记录并原子汇总、删除；并发清理跳过已锁记录，重复调用不会重复累计消费。
func (s *PostgresAIWalletStore) PruneAIUsage(ctx context.Context, cutoff time.Time) (AIUsageCleanupResult, error) {
	result := AIUsageCleanupResult{Cutoff: cutoff}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `
	WITH selected AS (
	 SELECT r.id FROM ai_balance_records r JOIN users u ON u.id=r.user_id
	 WHERE r.category='ai_usage' AND r.change_units<0 AND r.created_at<$1 AND u.ai_wallet_ledger_version=1
	 ORDER BY r.created_at,r.id LIMIT $2 FOR UPDATE OF r SKIP LOCKED
	), removed AS (
	 DELETE FROM ai_balance_records r USING selected s WHERE r.id=s.id
	 RETURNING r.created_at,r.change_units
	), archived AS (
	 INSERT INTO ai_usage_daily(usage_day,usage_units,record_count,updated_at)
	 SELECT (created_at AT TIME ZONE 'Asia/Shanghai')::date,SUM(-change_units::numeric),COUNT(*),NOW() FROM removed
	 GROUP BY (created_at AT TIME ZONE 'Asia/Shanghai')::date
	 ORDER BY (created_at AT TIME ZONE 'Asia/Shanghai')::date
	 ON CONFLICT(usage_day) DO UPDATE SET usage_units=ai_usage_daily.usage_units+EXCLUDED.usage_units,
	 record_count=ai_usage_daily.record_count+EXCLUDED.record_count,updated_at=NOW()
	 RETURNING usage_day
	)
	SELECT COUNT(*),COALESCE(SUM(-change_units::numeric),0) FROM removed
	`, cutoff, aiUsageCleanupBatch).Scan(&result.DeletedRecords, &result.ArchivedUnits)
	if err != nil {
		return result, err
	}
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ai_balance_records r JOIN users u ON u.id=r.user_id WHERE r.category='ai_usage' AND r.change_units<0 AND r.created_at<$1 AND u.ai_wallet_ledger_version=1), (SELECT COUNT(*) FROM ai_balance_records r JOIN users u ON u.id=r.user_id WHERE r.category='ai_usage' AND r.change_units<0 AND r.created_at<$1 AND u.ai_wallet_ledger_version<>1)`, cutoff).Scan(&result.HasMore, &result.BlockedCount)
	if err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	result.HasMore = result.HasMore || result.BlockedCount > 0
	return result, nil
}
