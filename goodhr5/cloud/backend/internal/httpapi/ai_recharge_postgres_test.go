// 本文件仅使用显式指定的本机独立测试库，验证充值赠送迁移、资金事务、并发去重和退款恢复。
package httpapi

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// aiRechargePostgresTestDB 创建独立测试 schema，拒绝连接非回环地址或非测试库。
func aiRechargePostgresTestDB(t *testing.T, beforePromotion bool) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOODHR_RECHARGE_TEST_DSN")
	if dsn == "" {
		t.Skip("未指定本机充值活动 PostgreSQL 测试库")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost") || !strings.HasSuffix(parsed.Path, "_test") {
		t.Fatal("充值数据库测试仅允许回环地址的 _test 库")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if _, err = admin.Exec(`CREATE EXTENSION IF NOT EXISTS pgcrypto`); err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("ai_recharge_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("清理充值测试 schema: %v", err)
		}
	})
	query := parsed.Query()
	query.Set("options", "-csearch_path="+schema+",public")
	parsed.RawQuery = query.Encode()
	db, err := sql.Open("postgres", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(24)
	t.Cleanup(func() { _ = db.Close() })
	if _, err = db.Exec(`
		CREATE TABLE users(id UUID PRIMARY KEY DEFAULT gen_random_uuid(),email TEXT UNIQUE NOT NULL);
		COMMENT ON TABLE users IS '充值集成测试用户';
		COMMENT ON COLUMN users.id IS '测试用户主键';
		COMMENT ON COLUMN users.email IS '测试用户邮箱';
	`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0002_add_system_configs.sql", "0023_payment_orders.sql", "0055_ai_wallet_and_builtin_ai.sql", "0056_ai_wallet_units.sql", "0058_ai_wallet_legacy_cents_defaults.sql", "0080_payment_refunds.sql"} {
		aiRechargeApplyMigration(t, db, name)
	}
	if _, err = db.Exec(`
		ALTER TABLE payment_orders ADD COLUMN upgrade_from_member_type TEXT NOT NULL DEFAULT '', ADD COLUMN upgrade_credit_cents INTEGER NOT NULL DEFAULT 0;
		COMMENT ON COLUMN payment_orders.upgrade_from_member_type IS '测试订单原会员类型';
		COMMENT ON COLUMN payment_orders.upgrade_credit_cents IS '测试订单会员抵扣金额，单位分';
	`); err != nil {
		t.Fatal(err)
	}
	if !beforePromotion {
		aiRechargeApplyMigration(t, db, "0092_ai_recharge_promotions.sql")
	}
	return db
}

// aiRechargeApplyMigration 在当前独立 schema 内执行真实迁移文件。
func aiRechargeApplyMigration(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", name))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(raw)); err != nil {
		t.Fatalf("充值迁移 %s: %v", name, err)
	}
}

// aiRechargeSeedLegacyRecord 模拟旧后端流水及其总余额，保留原流水时间顺序。
func aiRechargeSeedLegacyRecord(t *testing.T, db *sql.DB, email, category, orderNo string, change, balance int64, sequence int) {
	t.Helper()
	userID, err := ensureUserID(context.Background(), db, email)
	if err != nil {
		t.Fatal(err)
	}
	created := time.Now().Add(-time.Hour).Add(time.Duration(sequence) * time.Second)
	if _, err = db.Exec(`INSERT INTO ai_balance_records(user_id,user_email,change_cents,balance_after_cents,change_units,balance_after_units,category,reason,related_order_no,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,'历史测试',$8,$9)`, userID, email, aiUnitsToCents(change), aiUnitsToCents(balance), change, balance, category, orderNo, created); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE users SET ai_balance_units=$2 WHERE id=$1`, userID, balance); err != nil {
		t.Fatal(err)
	}
}

// aiRechargeSeedLegacyOrder 保存迁移前的已支付充值订单，不为缺失流水补造到账余额。
func aiRechargeSeedLegacyOrder(t *testing.T, db *sql.DB, email, orderNo string, amountCents int) {
	t.Helper()
	userID, err := ensureUserID(context.Background(), db, email)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO payment_orders(order_no,order_type,user_id,user_email,plan_id,plan_name,member_type,amount_cents,original_amount_cents,payment_provider,status,paid_at) VALUES($1,'ai_balance',$2,$3,'ai_balance','历史充值','',$4,$4,'wechat','paid',NOW()-INTERVAL '1 hour')`, orderNo, userID, email, amountCents); err != nil {
		t.Fatal(err)
	}
}

// aiRechargePostgresCredit 创建已支付测试订单并把本金和活动赠送一次性写入钱包。
func aiRechargePostgresCredit(t *testing.T, db *sql.DB, email, orderNo string, amountCents, bonusCents int) (*PostgresAIWalletStore, *PostgresPaymentStore) {
	t.Helper()
	orders := NewPostgresPaymentStore(db)
	if _, err := orders.Create(PaymentOrder{OrderNo: orderNo, OrderType: "ai_balance", UserEmail: email, PlanID: "ai_balance", PlanName: "测试充值", AmountCents: amountCents, OriginalAmountCents: amountCents, BonusAmountCents: bonusCents, PaymentProvider: wechatPayProviderName, Status: "paid"}); err != nil {
		t.Fatal(err)
	}
	wallet := NewPostgresAIWalletStore(db)
	if _, err := wallet.AdjustBalance(AIWalletRecord{UserEmail: email, ChangeUnits: centsToAIUnits(amountCents + bonusCents), BonusChangeUnits: centsToAIUnits(bonusCents), Category: "recharge", RelatedOrderNo: orderNo}); err != nil {
		t.Fatal(err)
	}
	return wallet, orders
}

// aiRechargePostgresBalances 读取精确总余额和赠送余额，便于独立核对钱包账本。
func aiRechargePostgresBalances(t *testing.T, db *sql.DB, email string) (int64, int64) {
	t.Helper()
	var total, bonus int64
	if err := db.QueryRow(`SELECT ai_balance_units,ai_bonus_balance_units FROM users WHERE email=$1`, email).Scan(&total, &bonus); err != nil {
		t.Fatal(err)
	}
	return total, bonus
}

// TestAIRechargePostgresHistoricalMigration 验证旧余额不变、注册赠送最后扣、异常历史独立隔离及迁移重跑。
func TestAIRechargePostgresHistoricalMigration(t *testing.T) {
	db := aiRechargePostgresTestDB(t, true)
	for _, email := range []string{"legacy-gift@example.test", "legacy-spent-gift@example.test"} {
		aiRechargeSeedLegacyOrder(t, db, email, email, 10000)
		aiRechargeSeedLegacyRecord(t, db, email, "signup_bonus", "", 7000, 7000, 0)
		sequence := 1
		balance := int64(7000)
		if strings.Contains(email, "spent") {
			aiRechargeSeedLegacyRecord(t, db, email, "ai_usage", "", -7000, 0, sequence)
			sequence++
			balance = 0
		}
		balance += 1000000
		aiRechargeSeedLegacyRecord(t, db, email, "recharge", email, 1000000, balance, sequence)
		aiRechargeSeedLegacyRecord(t, db, email, "ai_usage", "", -900000, balance-900000, sequence+1)
	}
	if _, err := db.Exec(`INSERT INTO users(email,ai_balance_units) VALUES('legacy-missing-ledger@example.test',5555)`); err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 2; pass++ {
		aiRechargeApplyMigration(t, db, "0092_ai_recharge_promotions.sql")
		wallet := NewPostgresAIWalletStore(db)
		for _, tc := range []struct {
			email string
			total int64
			bonus int64
		}{{"legacy-gift@example.test", 107000, 7000}, {"legacy-spent-gift@example.test", 100000, 0}} {
			total, bonus := aiRechargePostgresBalances(t, db, tc.email)
			if total != tc.total || bonus != tc.bonus {
				t.Fatalf("migration pass=%d email=%s total=%d bonus=%d", pass, tc.email, total, bonus)
			}
			if units, err := wallet.RefundableUnits(tc.email, tc.email); err != nil || units != 100000 {
				t.Fatalf("historical refundable=%d err=%v", units, err)
			}
			records, _, err := wallet.ListRecords(tc.email, 100, 0)
			if err != nil || len(records) == 0 {
				t.Fatalf("historical list records=%d err=%v", len(records), err)
			}
			for _, record := range records {
				if record.WalletLedgerVersion != 0 || publicAIWalletRecord(record)["wallet_ledger_version"] != 0 {
					t.Fatalf("migration must retain historical record version zero: %+v", record)
				}
			}
		}
		var blocked string
		var missingTotal int64
		if err := db.QueryRow(`SELECT ai_balance_units,ai_wallet_refund_block_reason FROM users WHERE email='legacy-missing-ledger@example.test'`).Scan(&missingTotal, &blocked); err != nil || blocked == "" || missingTotal != 5555 {
			t.Fatalf("missing historical ledger should preserve total and block only this user: total=%d reason=%q err=%v", missingTotal, blocked, err)
		}
		var lotCount int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ai_wallet_lots WHERE order_no<>''`).Scan(&lotCount); err != nil || lotCount != 2 {
			t.Fatalf("migration duplicated paid lots: count=%d err=%v", lotCount, err)
		}
	}
}

// TestAIRechargePostgresWalletRecordVersions 验证真实钱包新充值、退款预扣和失败返还流水均明确保存并返回分笔算法版本1。
func TestAIRechargePostgresWalletRecordVersions(t *testing.T) {
	db := aiRechargePostgresTestDB(t, false)
	const email = "wallet-record-version@example.test"
	wallet, orders := aiRechargePostgresCredit(t, db, email, "version-order", 10000, 2000)
	refund, err := wallet.ReserveRefund(PaymentRefund{RefundNo: "version-refund", OrderNo: "version-order", UserEmail: email, RefundType: refundTypeAIBalance, AmountCents: 1000}, orders)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = wallet.ResolveRefund(refund.RefundNo, "closed", "", `{}`, orders); err != nil {
		t.Fatal(err)
	}
	records, total, err := wallet.ListRecords(email, 100, 0)
	if err != nil || total != 3 || len(records) != 3 {
		t.Fatalf("new record list=%+v total=%d err=%v", records, total, err)
	}
	for _, record := range records {
		if record.WalletLedgerVersion != 1 || publicAIWalletRecord(record)["wallet_ledger_version"] != 1 {
			t.Fatalf("new record missing version1: %+v", record)
		}
	}
}

// TestAIRechargePostgresOrderCashAndBonusFIFO 验证所有订单本金先用、赠送按时间用，以及新充值不能为已用旧订单退款。
func TestAIRechargePostgresOrderCashAndBonusFIFO(t *testing.T) {
	db := aiRechargePostgresTestDB(t, false)
	email := "order-fifo@example.test"
	wallet, orders := aiRechargePostgresCredit(t, db, email, "fifo-a", 10000, 2000)
	aiRechargePostgresCredit(t, db, email, "fifo-b", 5000, 500)
	if _, err := wallet.AdjustBalance(AIWalletRecord{UserEmail: email, ChangeUnits: -1510000, Category: "ai_usage"}); err != nil {
		t.Fatal(err)
	}
	if total, bonus := aiRechargePostgresBalances(t, db, email); total != 240000 || bonus != 240000 {
		t.Fatalf("all cash first total=%d bonus=%d", total, bonus)
	}
	var giftA, giftB int64
	if err := db.QueryRow(`SELECT remaining_bonus_units FROM ai_wallet_lots WHERE order_no='fifo-a'`).Scan(&giftA); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT remaining_bonus_units FROM ai_wallet_lots WHERE order_no='fifo-b'`).Scan(&giftB); err != nil || giftA != 190000 || giftB != 50000 {
		t.Fatalf("gift FIFO a=%d b=%d err=%v", giftA, giftB, err)
	}
	aiRechargePostgresCredit(t, db, email, "fifo-c", 10000, 2000)
	if _, err := wallet.ReserveRefund(PaymentRefund{RefundNo: "fifo-old-cash-refund", OrderNo: "fifo-a", UserEmail: email, RefundType: refundTypeAIBalance, AmountCents: 100, Status: "pending"}, orders); err == nil {
		t.Fatal("new payment must not enable refund of spent old principal")
	}
	if _, err := wallet.ReserveRefund(PaymentRefund{RefundNo: "fifo-new-cash-refund", OrderNo: "fifo-c", UserEmail: email, RefundType: refundTypeAIBalance, AmountCents: 10000, Status: "pending"}, orders); err != nil {
		t.Fatal(err)
	}
	if total, bonus := aiRechargePostgresBalances(t, db, email); total != 240000 || bonus != 240000 {
		t.Fatalf("new refund should revoke only its own gift: total=%d bonus=%d", total, bonus)
	}
}

// TestAIRechargePostgresConcurrentCredit 验证多个支付回调仅产生一笔本金、赠送和流水。
func TestAIRechargePostgresConcurrentCredit(t *testing.T) {
	db := aiRechargePostgresTestDB(t, false)
	email, orderNo := "concurrent-credit@example.test", "concurrent-credit-order"
	orders := NewPostgresPaymentStore(db)
	if _, err := orders.Create(PaymentOrder{OrderNo: orderNo, OrderType: "ai_balance", UserEmail: email, PlanID: "ai_balance", PlanName: "并发充值", AmountCents: 10000, BonusAmountCents: 2000, PaymentProvider: wechatPayProviderName, Status: "paid"}); err != nil {
		t.Fatal(err)
	}
	wallet := NewPostgresAIWalletStore(db)
	start := make(chan struct{})
	errors := make(chan error, 16)
	var group sync.WaitGroup
	for i := 0; i < 16; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, err := wallet.AdjustBalance(AIWalletRecord{UserEmail: email, ChangeUnits: 1200000, BonusChangeUnits: 200000, Category: "recharge", RelatedOrderNo: orderNo})
			errors <- err
		}()
	}
	close(start)
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if total, bonus := aiRechargePostgresBalances(t, db, email); total != 1200000 || bonus != 200000 {
		t.Fatalf("duplicate callback credited total=%d bonus=%d", total, bonus)
	}
	var recordCount, lotCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_balance_records WHERE category='recharge' AND related_order_no=$1`, orderNo).Scan(&recordCount); err != nil || recordCount != 1 {
		t.Fatalf("credit record count=%d err=%v", recordCount, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_wallet_lots WHERE order_no=$1`, orderNo).Scan(&lotCount); err != nil || lotCount != 1 {
		t.Fatalf("credit lot count=%d err=%v", lotCount, err)
	}
}

// TestAIRechargePostgresRefundReserveAndRecovery 验证部分退款全收剩余赠送、失败恢复一次和成功终态不被逆转。
func TestAIRechargePostgresRefundReserveAndRecovery(t *testing.T) {
	db := aiRechargePostgresTestDB(t, false)
	email, orderNo := "refund-recovery@example.test", "refund-recovery-order"
	wallet, orders := aiRechargePostgresCredit(t, db, email, orderNo, 10000, 2000)
	if _, err := wallet.AdjustBalance(AIWalletRecord{UserEmail: email, ChangeUnits: -900000, Category: "ai_usage"}); err != nil {
		t.Fatal(err)
	}
	refund, err := wallet.ReserveRefund(PaymentRefund{RefundNo: "refund-recovery-first", OrderNo: orderNo, UserEmail: email, RefundType: refundTypeAIBalance, AmountCents: 1000, Status: "pending"}, orders)
	if err != nil || refund.RevokedBonusUnits != 200000 || !refund.RightsReserved {
		t.Fatalf("reserve refund=%+v err=%v", refund, err)
	}
	if total, bonus := aiRechargePostgresBalances(t, db, email); total != 0 || bonus != 0 {
		t.Fatalf("refund should reserve cash and all bonus: total=%d bonus=%d", total, bonus)
	}
	for pass := 0; pass < 2; pass++ {
		if _, err := wallet.ResolveRefund(refund.RefundNo, "failed", "", `{}`, orders); err != nil {
			t.Fatal(err)
		}
		if total, bonus := aiRechargePostgresBalances(t, db, email); total != 300000 || bonus != 200000 {
			t.Fatalf("failed recovery pass=%d total=%d bonus=%d", pass, total, bonus)
		}
	}
	refund, err = wallet.ReserveRefund(PaymentRefund{RefundNo: "refund-recovery-success", OrderNo: orderNo, UserEmail: email, RefundType: refundTypeAIBalance, AmountCents: 1000, Status: "pending"}, orders)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"processing", "success", "success", "failed"} {
		if _, err := wallet.ResolveRefund(refund.RefundNo, status, "provider-test", `{}`, orders); err != nil {
			t.Fatal(err)
		}
	}
	if total, bonus := aiRechargePostgresBalances(t, db, email); total != 0 || bonus != 0 {
		t.Fatalf("success refund must not restore: total=%d bonus=%d", total, bonus)
	}
}

// TestAIRechargePostgresRefundBlocksSecondPending 验证同订单并发退款只有一笔占用本金和赠送。
func TestAIRechargePostgresRefundBlocksSecondPending(t *testing.T) {
	db := aiRechargePostgresTestDB(t, false)
	email, orderNo := "refund-concurrent@example.test", "refund-concurrent-order"
	wallet, orders := aiRechargePostgresCredit(t, db, email, orderNo, 10000, 2000)
	start := make(chan struct{})
	results := make(chan error, 12)
	var group sync.WaitGroup
	for i := 0; i < 12; i++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			<-start
			_, err := wallet.ReserveRefund(PaymentRefund{RefundNo: fmt.Sprintf("concurrent-refund-%d", index), OrderNo: orderNo, UserEmail: email, RefundType: refundTypeAIBalance, AmountCents: 100, Status: "pending"}, orders)
			results <- err
		}(i)
	}
	close(start)
	group.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent pending refunds accepted=%d want 1", successes)
	}
	if total, bonus := aiRechargePostgresBalances(t, db, email); total != 990000 || bonus != 0 {
		t.Fatalf("concurrent refund total=%d bonus=%d", total, bonus)
	}
}

// TestAIRechargePostgresConsumptionRefundRace 验证真实数据库消费和退款预占只产生串行合法结果，不把赠送退成现金。
func TestAIRechargePostgresConsumptionRefundRace(t *testing.T) {
	db := aiRechargePostgresTestDB(t, false)
	for iteration := 0; iteration < 12; iteration++ {
		email := fmt.Sprintf("refund-usage-race-%d@example.test", iteration)
		orderNo := fmt.Sprintf("refund-usage-race-%d", iteration)
		wallet, orders := aiRechargePostgresCredit(t, db, email, orderNo, 10000, 2000)
		if _, err := wallet.AdjustBalance(AIWalletRecord{UserEmail: email, ChangeUnits: -900000, Category: "ai_usage"}); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		refundResult, usageResult := make(chan error, 1), make(chan error, 1)
		go func() {
			<-start
			_, err := wallet.ReserveRefund(PaymentRefund{RefundNo: "race-refund-" + orderNo, OrderNo: orderNo, UserEmail: email, RefundType: refundTypeAIBalance, AmountCents: 1000, Status: "pending"}, orders)
			refundResult <- err
		}()
		go func() {
			<-start
			_, err := wallet.AdjustBalance(AIWalletRecord{UserEmail: email, ChangeUnits: -50000, Category: "ai_usage"})
			usageResult <- err
		}()
		close(start)
		refundErr, usageErr := <-refundResult, <-usageResult
		if usageErr != nil {
			t.Fatal(usageErr)
		}
		total, bonus := aiRechargePostgresBalances(t, db, email)
		if refundErr == nil {
			// AI 按完成后的实际用量计费，已经开始的请求可形成既有债务，但不能凭空恢复本金或赠送。
			if total != -50000 || bonus != 0 {
				t.Fatalf("refund-first race total=%d bonus=%d", total, bonus)
			}
		} else if total != 250000 || bonus != 200000 {
			t.Fatalf("usage-first race total=%d bonus=%d err=%v", total, bonus, refundErr)
		}
		if units, err := wallet.RefundableUnits(email, orderNo); err != nil || units < 0 || units > 50000 {
			t.Fatalf("race refundable units=%d err=%v", units, err)
		}
	}
}

// TestAIRechargePostgresRefundPrecisionAndDebtRecovery 验证不足一分钱不能退款、近全额退款无赠送，以及失败返还先抵期间欠费。
func TestAIRechargePostgresRefundPrecisionAndDebtRecovery(t *testing.T) {
	db := aiRechargePostgresTestDB(t, false)
	email := "subcent-refund@example.test"
	wallet, orders := aiRechargePostgresCredit(t, db, email, "subcent-order", 10000, 2000)
	if _, err := wallet.AdjustBalance(AIWalletRecord{UserEmail: email, ChangeUnits: -899901, Category: "ai_usage"}); err != nil {
		t.Fatal(err)
	}
	refund, err := wallet.ReserveRefund(PaymentRefund{RefundNo: "subcent-refund", OrderNo: "subcent-order", UserEmail: email, RefundType: refundTypeAIBalance, AmountCents: 1000}, orders)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = wallet.ResolveRefund(refund.RefundNo, "success", "subcent-provider", `{}`, orders); err != nil {
		t.Fatal(err)
	}
	if total, bonus := aiRechargePostgresBalances(t, db, email); total != 99 || bonus != 0 {
		t.Fatalf("subcent cash remainder total=%d bonus=%d", total, bonus)
	}
	if _, err = wallet.ReserveRefund(PaymentRefund{RefundNo: "subcent-over-refund", OrderNo: "subcent-order", UserEmail: email, RefundType: refundTypeAIBalance, AmountCents: 1}, orders); err == nil {
		t.Fatal("0.0099 yuan must not be rounded up to refund one cent")
	}
	email = "near-full-refund@example.test"
	wallet, orders = aiRechargePostgresCredit(t, db, email, "near-full-order", 10000, 2000)
	if _, err = wallet.ReserveRefund(PaymentRefund{RefundNo: "near-full-refund", OrderNo: "near-full-order", UserEmail: email, RefundType: refundTypeAIBalance, AmountCents: 9999}, orders); err != nil {
		t.Fatal(err)
	}
	if total, bonus := aiRechargePostgresBalances(t, db, email); total != 100 || bonus != 0 {
		t.Fatalf("near full refund left gift: total=%d bonus=%d", total, bonus)
	}
	email = "debt-recovery@example.test"
	wallet, orders = aiRechargePostgresCredit(t, db, email, "debt-recovery-order", 10000, 2000)
	if _, err = wallet.ReserveRefund(PaymentRefund{RefundNo: "debt-recovery-refund", OrderNo: "debt-recovery-order", UserEmail: email, RefundType: refundTypeAIBalance, AmountCents: 10000}, orders); err != nil {
		t.Fatal(err)
	}
	if _, err = wallet.AdjustBalance(AIWalletRecord{UserEmail: email, ChangeUnits: -50000, Category: "ai_usage"}); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err = wallet.ResolveRefund("debt-recovery-refund", "failed", "", `{}`, orders); err != nil {
			t.Fatal(err)
		}
		if total, bonus := aiRechargePostgresBalances(t, db, email); total != 1150000 || bonus != 200000 {
			t.Fatalf("failed refund debt settlement attempt=%d total=%d bonus=%d", attempt, total, bonus)
		}
		if units, err := wallet.RefundableUnits(email, "debt-recovery-order"); err != nil || units != 950000 {
			t.Fatalf("restored principal must exclude debt: units=%d err=%v", units, err)
		}
	}
}

// TestAIRechargePostgresHistoricalRefundIsolation 验证旧退款有扣款流水才能占用本金，无法核对的旧退款只隔离该用户。
func TestAIRechargePostgresHistoricalRefundIsolation(t *testing.T) {
	db := aiRechargePostgresTestDB(t, true)
	for _, email := range []string{"historical-refund-success@example.test", "historical-refund-missing@example.test"} {
		aiRechargeSeedLegacyOrder(t, db, email, email, 10000)
		aiRechargeSeedLegacyRecord(t, db, email, "recharge", email, 1000000, 1000000, 0)
		userID, err := ensureUserID(context.Background(), db, email)
		if err != nil {
			t.Fatal(err)
		}
		status := "processing"
		if strings.Contains(email, "success") {
			status = "success"
		}
		if _, err = db.Exec(`INSERT INTO payment_refunds(refund_no,order_no,user_id,user_email,refund_type,amount_cents,status) VALUES($1,$1,$2,$1,'ai_balance',500,$3)`, email, userID, status); err != nil {
			t.Fatal(err)
		}
		if status == "success" {
			aiRechargeSeedLegacyRecord(t, db, email, "refund", email, -50000, 950000, 1)
		}
	}
	aiRechargeApplyMigration(t, db, "0092_ai_recharge_promotions.sql")
	wallet := NewPostgresAIWalletStore(db)
	if units, err := wallet.RefundableUnits("historical-refund-success@example.test", "historical-refund-success@example.test"); err != nil || units != 950000 {
		t.Fatalf("historical confirmed refund units=%d err=%v", units, err)
	}
	if units, err := wallet.RefundableUnits("historical-refund-missing@example.test", "historical-refund-missing@example.test"); err == nil || units != 0 {
		t.Fatalf("unconfirmed historical refund should be blocked units=%d err=%v", units, err)
	}
	if total, _ := aiRechargePostgresBalances(t, db, "historical-refund-missing@example.test"); total != 1000000 {
		t.Fatalf("isolated historical wallet changed total=%d", total)
	}
	if _, err := wallet.AdjustBalance(AIWalletRecord{UserEmail: "historical-refund-missing@example.test", ChangeUnits: -100, Category: "ai_usage"}); err != nil {
		t.Fatalf("isolated history should allow existing balance consumption: %v", err)
	}
}

// TestAIRechargePostgresPartialInitializationIsolation 验证版本不完整的钱包保留总额和原资金来源，仅隔离退款而不阻止消费充值。
func TestAIRechargePostgresPartialInitializationIsolation(t *testing.T) {
	db := aiRechargePostgresTestDB(t, false)
	email := "partial-initialization@example.test"
	wallet, _ := aiRechargePostgresCredit(t, db, email, "partial-init-order", 10000, 2000)
	if _, err := db.Exec(`UPDATE users SET ai_wallet_ledger_version=0,ai_bonus_balance_units=0 WHERE email=$1`, email); err != nil {
		t.Fatal(err)
	}
	aiRechargeApplyMigration(t, db, "0092_ai_recharge_promotions.sql")
	if total, bonus := aiRechargePostgresBalances(t, db, email); total != 1200000 || bonus != 1200000 {
		t.Fatalf("partially initialized balance must be preserved: total=%d bonus=%d", total, bonus)
	}
	if _, err := wallet.RefundableUnits(email, "partial-init-order"); err == nil {
		t.Fatal("partially initialized historical principal must remain blocked")
	}
	if _, err := wallet.AdjustBalance(AIWalletRecord{UserEmail: email, ChangeUnits: -10000, Category: "ai_usage"}); err != nil {
		t.Fatalf("partial initialization must not block usage: %v", err)
	}
	aiRechargePostgresCredit(t, db, email, "partial-init-new-order", 100, 0)
	if total, bonus := aiRechargePostgresBalances(t, db, email); total != 1200000 || bonus != 1190000 {
		t.Fatalf("partial initialization must allow new payment: total=%d bonus=%d", total, bonus)
	}
	var retained int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_wallet_lots WHERE order_no='partial-init-order' AND principal_units=1000000 AND bonus_units=200000`).Scan(&retained); err != nil || retained != 1 {
		t.Fatalf("original incomplete source must remain auditable: count=%d err=%v", retained, err)
	}
}
