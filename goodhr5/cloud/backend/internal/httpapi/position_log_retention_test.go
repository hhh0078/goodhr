// 本文件使用北京时间边界及隔离PostgreSQL合成数据验证七日运行日志清理，不访问生产数据库。
package httpapi

import (
	"context"
	"reflect"
	"testing"
	"time"
)

// TestNextPositionLogCleanupAt 验证UTC输入、05:00边界及跨月仍严格按北京时间安排下一轮。
func TestNextPositionLogCleanupAt(t *testing.T) {
	for _, item := range []struct{ now, expected string }{
		{"2026-11-10T20:59:59Z", "2026-11-11T05:00:00+08:00"},
		{"2026-11-10T21:00:00Z", "2026-11-12T05:00:00+08:00"},
		{"2026-01-31T21:01:00Z", "2026-02-02T05:00:00+08:00"},
		{"2028-02-28T20:00:00Z", "2028-02-29T05:00:00+08:00"},
	} {
		now, _ := time.Parse(time.RFC3339, item.now)
		if got := nextPositionLogCleanupAt(now).Format(time.RFC3339); got != item.expected {
			t.Errorf("now=%s got=%s want=%s", item.now, got, item.expected)
		}
	}
	if newPositionLogRetentionService(NewMemoryPositionLogStore()) != nil {
		t.Fatal("内存存储不应启动PostgreSQL每日清理")
	}
}

// TestPositionLogPostgresRetention 验证跨批清理、七日边界、锁竞争、取消、重复执行及统计账本保持。
func TestPositionLogPostgresRetention(t *testing.T) {
	db := statisticsPostgresFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	cutoff := now.Add(-positionLogRetention)
	var user, position string
	if err := db.QueryRow(`INSERT INTO users(email,ai_wallet_ledger_version) VALUES('log-retention@example.test',1) RETURNING id::text`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO positions(user_id,name,platform_id) VALUES($1,'日志清理合成岗位','boss') RETURNING id::text`, user).Scan(&position); err != nil {
		t.Fatal(err)
	}
	statisticsTestExec(t, db, `INSERT INTO position_logs(position_id,user_id,level,message,created_at)
		SELECT $1,$2,'info','合成旧日志',$3 FROM generate_series(1,$4)`, position, user, cutoff.Add(-time.Second), positionLogCleanupBatch+3)
	for _, at := range []time.Time{cutoff, cutoff.Add(time.Microsecond), now.Add(time.Hour)} {
		statisticsTestExec(t, db, `INSERT INTO position_logs(position_id,user_id,level,message,created_at) VALUES($1,$2,'error','保留边界日志',$3)`, position, user, at)
	}
	statisticsTestExec(t, db, `INSERT INTO position_log_shares(token_hash,position_id,user_email,snapshot,expires_at) VALUES('retention-test',$1,'log-retention@example.test','[]',$2)`, position, now.Add(time.Hour))
	statisticsTestExec(t, db, `INSERT INTO ai_balance_records(user_id,user_email,change_cents,balance_after_cents,change_units,balance_after_units,wallet_ledger_version,category,model_id,prompt_tokens,completion_tokens,created_at)
		VALUES($1,'log-retention@example.test',-1,0,-1234,0,1,'ai_usage','retention-test',100,20,$2)`, user, cutoff.Add(-time.Hour))
	stats := &postgresSystemStatistics{db: db}
	before, err := stats.Summary(context.Background(), aiUsageDay(cutoff).AddDate(0, 0, -1), aiUsageDay(now).AddDate(0, 0, 1), now)
	if err != nil {
		t.Fatal(err)
	}
	store := NewPostgresPositionLogStore(db)
	guard, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = guard.Exec(`SELECT pg_advisory_xact_lock($1)`, positionLogRetentionLock); err != nil {
		t.Fatal(err)
	}
	result, err := store.PrunePositionLogs(context.Background(), cutoff)
	guard.Rollback()
	if err != nil || !result.Skipped || result.DeletedRecords != 0 {
		t.Fatalf("多实例锁未生效：%+v %v", result, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = store.PrunePositionLogs(canceled, cutoff); err == nil {
		t.Fatal("取消后仍继续删除")
	}
	result, err = store.PrunePositionLogs(context.Background(), cutoff)
	if err != nil || result.DeletedRecords != positionLogCleanupBatch+3 || result.Skipped {
		t.Fatalf("跨批清理不正确：%+v %v", result, err)
	}
	var logs, shares, wallet int
	if err = db.QueryRow(`SELECT (SELECT count(*) FROM position_logs),(SELECT count(*) FROM position_log_shares),(SELECT count(*) FROM ai_balance_records)`).Scan(&logs, &shares, &wallet); err != nil || logs != 3 || shares != 1 || wallet != 1 {
		t.Fatalf("保留范围不正确：logs=%d shares=%d wallet=%d err=%v", logs, shares, wallet, err)
	}
	after, err := stats.Summary(context.Background(), aiUsageDay(cutoff).AddDate(0, 0, -1), aiUsageDay(now).AddDate(0, 0, 1), now)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("日志清理改变全站统计：err=%v", err)
	}
	result, err = store.PrunePositionLogs(context.Background(), cutoff)
	if err != nil || result.DeletedRecords != 0 {
		t.Fatalf("重复清理不幂等：%+v %v", result, err)
	}
}
