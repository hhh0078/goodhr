// 本文件仅在显式回环测试库内验证全套迁移、统计金额、漏斗、数据清理及重传去重，不连接生产数据库。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// statisticsPostgresFixture 在独立测试schema执行真实迁移，退出仅删除本次创建的schema。
func statisticsPostgresFixture(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOODHR_STATISTICS_TEST_DSN")
	if dsn == "" {
		t.Skip("未指定全站统计回环测试库")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") || !strings.HasSuffix(u.Path, "_test") {
		t.Fatal("全站统计测试只允许回环_test库")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	if _, err = admin.Exec(`CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("system_statistics_%d", time.Now().UnixNano())
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Error(err)
		}
	})
	q := u.Query()
	q.Set("options", "-csearch_path="+schema+",public")
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	files, err := os.ReadDir(filepath.Join("..", "..", "db", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".sql") && !strings.HasSuffix(file.Name(), ".down.sql") {
			aiRechargeApplyMigration(t, db, file.Name())
		}
	}
	return db
}

// statisticsTestExec 执行仅限独立schema的合成数据写入，任何错误立即终止本用例。
func statisticsTestExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

// TestSystemStatisticsPostgres 验证北京日界线、金额精度、同批漏斗、成功支付退款及未知历史的准确处理。
func TestSystemStatisticsPostgres(t *testing.T) {
	db := statisticsPostgresFixture(t)
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, aiUsageDay(time.Now()).Location())
	start, end := aiUsageDay(now), aiUsageDay(now).AddDate(0, 0, 1)
	statisticsTestExec(t, db, `UPDATE system_stat_coverage SET complete_from=$1`, start.AddDate(0, 0, -30))
	var tenant, user, other, position string
	if err := db.QueryRow(`INSERT INTO tenants(name,owner_email) VALUES('合成统计团队','stats@example.test') RETURNING id::text`).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	state := defaultUserFlowState()
	var err error
	for _, step := range userFlowStepOrder {
		state, err = applyUserFlowUpdate(state, UserFlowUpdate{Step: step, Status: "completed", OccurredAt: start.Add(time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := json.Marshal(state)
	if err := db.QueryRow(`INSERT INTO users(email,tenant_id,created_at,flow_state,ai_balance_units,ai_bonus_balance_units,ai_wallet_ledger_version) VALUES('stats@example.test',$1,$2,$3::jsonb,20000,3000,1) RETURNING id::text`, tenant, start, string(raw)).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users(email,tenant_id,created_at) VALUES('other-stats@example.test',$1,$2) RETURNING id::text`, tenant, start.Add(time.Hour)).Scan(&other); err != nil {
		t.Fatal(err)
	}
	statisticsTestExec(t, db, `INSERT INTO users(email,created_at,flow_state) VALUES('old-stats@example.test',$1,$2::jsonb)`, start.Add(-time.Second), string(raw))
	if err := db.QueryRow(`INSERT INTO positions(user_id,name,platform_id,created_at) VALUES($1,'合成统计岗位','boss',$2) RETURNING id::text`, user, start).Scan(&position); err != nil {
		t.Fatal(err)
	}
	statisticsTestExec(t, db, `UPDATE users SET last_login_at=$2 WHERE id=$1`, user, start.Add(time.Hour))
	statisticsTestExec(t, db, `UPDATE users SET last_login_at=$2 WHERE id=$1`, user, start.Add(2*time.Hour))
	for _, at := range []time.Time{start.Add(-time.Second), start, start.Add(2 * time.Hour), end} {
		statisticsTestExec(t, db, `INSERT INTO ai_balance_records(user_id,user_email,change_cents,balance_after_cents,change_units,balance_after_units,bonus_change_units,wallet_ledger_version,category,model_id,prompt_tokens,completion_tokens,created_at) VALUES($1,'stats@example.test',-1,0,-1234,0,-234,1,'ai_usage','test-model',100,20,$2)`, user, at)
	}
	statisticsTestExec(t, db, `UPDATE positions SET daily_greeted_count=3,daily_greeted_date=$2 WHERE id=$1`, position, start.Format(time.DateOnly))
	statisticsTestExec(t, db, `UPDATE positions SET daily_greeted_count=3 WHERE id=$1`, position)
	statisticsTestExec(t, db, `INSERT INTO payment_orders(order_no,user_id,user_email,plan_id,plan_name,amount_cents,status,order_type,paid_at) VALUES('stats-paid',$1,'stats@example.test','ai','AI',123,'paid','ai_balance',$2),('stats-pending',$1,'stats@example.test','ai','AI',999,'pending','ai_balance',$2)`, user, start)
	statisticsTestExec(t, db, `INSERT INTO payment_refunds(refund_no,order_no,user_id,user_email,refund_type,amount_cents,status,completed_at) VALUES('stats-refund','stats-paid',$1,'stats@example.test','ai_balance',23,'success',$2)`, user, start)
	statisticsTestExec(t, db, `INSERT INTO system_daily_stats(stat_date,processed_resume_count) VALUES($1,45)`, start.Format(time.DateOnly))
	store := &postgresSystemStatistics{db: db}
	r, err := store.Summary(context.Background(), start, end, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Amounts["consumption"] != "0.2468" || r.Amounts["bonus_consumption"] != "0.0468" || r.Amounts["principal_consumption"] != "0.2000" || r.Amounts["receipts"] != "1.23" || r.Amounts["refunds"] != "0.23" || r.Amounts["net_receipts"] != "1.00" {
		t.Fatalf("money mismatch: %+v", r.Amounts)
	}
	if r.Metrics["registered"] != 2 || r.Metrics["charged_calls"] != 2 || r.Metrics["prompt_tokens"] != 200 || r.Metrics["active_users"] != 1 || r.Metrics["greeted"] != 3 || r.Metrics["processed_resumes"] != 45 {
		t.Fatalf("metric mismatch: %+v", r.Metrics)
	}
	if len(r.Curve) != 7 || r.Curve[6].Amount != "0.2468" || r.Curve[0].Amount != "0.0000" {
		t.Fatalf("curve mismatch: %+v", r.Curve)
	}
	if len(r.Funnel) != 8 || r.Funnel[0].Count != 2 || r.Funnel[7].Count != 1 {
		t.Fatalf("cohort funnel mismatch: %+v", r.Funnel)
	}
	if r.CurrentAmounts["principal"] != "1.7000" {
		t.Fatalf("current principal mismatch: %+v", r.CurrentAmounts)
	}
	statisticsTestExec(t, db, `INSERT INTO ai_balance_records(user_id,user_email,change_cents,balance_after_cents,change_units,balance_after_units,wallet_ledger_version,category,created_at) VALUES($1,'stats@example.test',50,50,5000,5000,0,'signup_bonus',$2)`, user, start)
	r, err = store.Summary(context.Background(), start, end, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Amounts["gift"]; ok {
		t.Fatal("legacy bonus split became a falsely complete total")
	}
	// 权威账本多出一笔已清理历史时，金额仍完整，缺少的Token和本金拆分不能当零。
	statisticsTestExec(t, db, `INSERT INTO ai_usage_daily(usage_day,usage_units,record_count) VALUES($1,1,1)`, start.Format(time.DateOnly))
	r, err = store.Summary(context.Background(), start, end, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Amounts["consumption"] != "0.2469" || r.Metrics["charged_calls"] != 3 {
		t.Fatal("archived amount lost")
	}
	if _, ok := r.Metrics["prompt_tokens"]; ok {
		t.Fatal("missing historical token data became a complete total")
	}
	if _, ok := r.Amounts["principal_consumption"]; ok {
		t.Fatal("missing historical principal data became a complete total")
	}
	if len(r.Warnings) == 0 {
		t.Fatal("missing history warning absent")
	}
	// 空注册批次必须显示真实零，不可被LEFT JOIN虚拟行计成一人。
	r, err = store.Summary(context.Background(), start.AddDate(0, 0, -20), start.AddDate(0, 0, -19), now)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range r.Funnel {
		if step.Count != 0 {
			t.Fatalf("empty funnel=%+v", r.Funnel)
		}
	}
}

// TestSystemStatisticsPostgresFacts 验证统计事实跨明细删除保留、运行重传去重及筛选修订只更新一次。
func TestSystemStatisticsPostgresFacts(t *testing.T) {
	db := statisticsPostgresFixture(t)
	var tenant, user, position string
	if err := db.QueryRow(`INSERT INTO tenants(name,owner_email) VALUES('合成团队','facts@example.test') RETURNING id::text`).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users(email,tenant_id) VALUES('facts@example.test',$1) RETURNING id::text`, tenant).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO positions(user_id,name,platform_id) VALUES($1,'合成岗位','boss') RETURNING id::text`, user).Scan(&position); err != nil {
		t.Fatal(err)
	}
	const event = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for i := 0; i < 2; i++ {
		statisticsTestExec(t, db, `INSERT INTO browser_events(event_id,user_id,payload,created_at) VALUES($1,$2,'{"action":"browser.exit","is_error":true}',now()) ON CONFLICT DO NOTHING`, event, user)
		statisticsTestExec(t, db, `DELETE FROM browser_events WHERE event_id=$1`, event)
	}
	var count int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM system_stat_facts WHERE kind='runtime'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("runtime replay counted twice: %d %v", count, err)
	}
	statisticsTestExec(t, db, `INSERT INTO position_screening_records(tenant_id,position_id,owner_user_id,attempt_id,revision,occurred_at,snapshot) VALUES($1,$2,$3,'11111111-1111-4111-8111-111111111111',1,now(),'{"platform_id":"boss","first":{"status":"running"},"second":{"status":"not_executed"}}')`, tenant, position, user)
	statisticsTestExec(t, db, `UPDATE position_screening_records SET revision=2,snapshot='{"platform_id":"boss","first":{"status":"completed","accepted":true,"duration_ms":100},"second":{"status":"not_executed"}}'`)
	statisticsTestExec(t, db, `UPDATE position_screening_records SET revision=1,snapshot='{"platform_id":"boss","first":{"status":"running"}}'`)
	statisticsTestExec(t, db, `DELETE FROM position_screening_records`)
	var passed int64
	if err := db.QueryRow(`SELECT COUNT(*),SUM((metrics->>'first_pass')::int) FROM system_stat_facts WHERE kind='screening'`).Scan(&count, &passed); err != nil || count != 1 || passed != 1 {
		t.Fatalf("screening revision or pruning damaged aggregate: %d %d %v", count, passed, err)
	}
	statisticsTestExec(t, db, `UPDATE positions SET daily_greeted_date='2026-11-09',daily_greeted_count=2 WHERE id=$1`, position)
	statisticsTestExec(t, db, `UPDATE positions SET daily_greeted_date='2026-11-10',daily_greeted_count=1 WHERE id=$1`, position)
	statisticsTestExec(t, db, `UPDATE positions SET daily_greeted_count=2 WHERE id=$1`, position)
	if err := db.QueryRow(`SELECT SUM((metrics->>'count')::int) FROM system_stat_facts WHERE kind='greeting'`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("cross-day greeting total=%d err=%v", count, err)
	}
	// 原事务回滚时不能留下统计数字。
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO browser_events(event_id,user_id,payload,created_at) VALUES('bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',$1,'{"action":"browser.exit","is_error":true}',now())`, user); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM system_stat_facts WHERE kind='runtime'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("rolled back event leaked: %d %v", count, err)
	}
	// 迁移重复执行不得重置采集起点，或把已记录的事实再次累计。
	var before, after time.Time
	if err = db.QueryRow(`SELECT MAX(complete_from) FROM system_stat_coverage`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	aiRechargeApplyMigration(t, db, "0124_system_statistics.sql")
	if err = db.QueryRow(`SELECT MAX(complete_from) FROM system_stat_coverage`).Scan(&after); err != nil || !before.Equal(after) {
		t.Fatal("migration reset collection coverage")
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM system_stat_facts WHERE kind='runtime'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("migration duplicated facts")
	}
}

// TestSystemStatisticsPostgresAICleanup 验证七日清理前后消费曲线、金额、Token及模型总量完全一致。
func TestSystemStatisticsPostgresAICleanup(t *testing.T) {
	db := statisticsPostgresFixture(t)
	at := aiUsageDay(time.Now()).AddDate(0, 0, -9)
	var user string
	if err := db.QueryRow(`INSERT INTO users(email,ai_wallet_ledger_version) VALUES('cleanup-stats@example.test',1) RETURNING id::text`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	statisticsTestExec(t, db, `INSERT INTO ai_balance_records(user_id,user_email,change_cents,balance_after_cents,change_units,balance_after_units,bonus_change_units,wallet_ledger_version,category,model_id,prompt_tokens,completion_tokens,created_at) VALUES($1,'cleanup-stats@example.test',-1,0,-1234,0,-234,1,'ai_usage','cleanup-model',100,20,$2)`, user, at)
	store := &postgresSystemStatistics{db: db}
	before, err := store.Summary(context.Background(), at, at.AddDate(0, 0, 1), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cleanup, err := NewPostgresAIWalletStore(db).PruneAIUsage(context.Background(), at.AddDate(0, 0, 1))
	if err != nil || cleanup.DeletedRecords != 1 {
		t.Fatalf("cleanup=%+v err=%v", cleanup, err)
	}
	after, err := store.Summary(context.Background(), at, at.AddDate(0, 0, 1), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if before.Amounts["consumption"] != after.Amounts["consumption"] || after.Metrics["prompt_tokens"] != 100 || after.Amounts["bonus_consumption"] != "0.0234" || len(after.Models) != 1 || after.Models[0].Amount != "0.1234" || after.Curve[6].Amount != "0.1234" {
		t.Fatalf("cleanup changed statistics: before=%+v after=%+v", before, after)
	}
}
