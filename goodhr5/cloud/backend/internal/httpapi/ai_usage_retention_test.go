// 本文件验证七天消费明细物理清理、日金额连续性、资金及幂等保持、清理授权和数据库事务，不连接生产。
package httpapi

import (
	"context"
	"encoding/json"
	"math"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestUsageRetentionPreservesFunds 验证只删除过期消费，余额、赠送、可退本金、充值退款回执和月金额保持一致。
func TestUsageRetentionPreservesFunds(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, chinaLocation())
	f := newWalletLedgerFixture()
	f.credit(t, "retention-order", 10000, 2000)
	f.consume(t, 1000)
	f.wallet.records[len(f.wallet.records)-1].CreatedAt = now.Add(-8 * 24 * time.Hour)
	f.consume(t, 2000)
	f.wallet.records[len(f.wallet.records)-1].CreatedAt = now.Add(-aiUsageRetention)
	f.consume(t, 3000)
	f.wallet.records[len(f.wallet.records)-1].CreatedAt = now.Add(-24 * time.Hour)
	refund, err := f.wallet.ReserveRefund(f.refund("retention-refund", "retention-order", 1000), f.orders)
	if err != nil {
		t.Fatal(err)
	}
	// 充值和退款已经很旧也必须留下，删除消费不能导致未来退款核对失败。
	f.wallet.records[0].CreatedAt = now.Add(-30 * 24 * time.Hour)
	f.wallet.records[len(f.wallet.records)-1].CreatedAt = now.Add(-30 * 24 * time.Hour)
	beforeTotal, beforeBonus, _ := f.wallet.BalanceSnapshot(f.email)
	beforeRefundable, _ := f.wallet.RefundableUnits(f.email, "retention-order")
	month := time.Date(2026, 10, 1, 0, 0, 0, 0, chinaLocation())
	beforeUsage, err := f.wallet.UsageUnitsBetween(month, now)
	if err != nil || beforeUsage != 6000 {
		t.Fatalf("清理前统计=%d err=%v", beforeUsage, err)
	}
	result, err := f.wallet.PruneAIUsage(context.Background(), now.Add(-aiUsageRetention))
	if err != nil || result.DeletedRecords != 1 || result.ArchivedUnits != 1000 || result.HasMore {
		t.Fatalf("清理结果=%+v err=%v", result, err)
	}
	afterTotal, afterBonus, _ := f.wallet.BalanceSnapshot(f.email)
	afterRefundable, _ := f.wallet.RefundableUnits(f.email, "retention-order")
	afterUsage, err := f.wallet.UsageUnitsBetween(month, now)
	if err != nil || beforeTotal != afterTotal || beforeBonus != afterBonus || beforeRefundable != afterRefundable || beforeUsage != afterUsage {
		t.Fatal("清理改变了资金或月消费")
	}
	if len(f.wallet.records) != 4 || len(f.wallet.usageDaily) != 1 {
		t.Fatal("没有物理删除明细或未保留汇总")
	}
	again, err := f.wallet.PruneAIUsage(context.Background(), now.Add(-aiUsageRetention))
	if err != nil || again.DeletedRecords != 0 || again.ArchivedUnits != 0 {
		t.Fatal("重复清理重复累计")
	}
	if _, err = f.wallet.AdjustBalance(AIWalletRecord{UserEmail: f.email, Category: "recharge", RelatedOrderNo: "retention-order", ChangeUnits: 1200000, BonusChangeUnits: 200000}); err != nil {
		t.Fatal(err)
	}
	if balance, _ := f.wallet.BalanceUnits(f.email); balance != beforeTotal {
		t.Fatal("清理后充值回调再次到账")
	}
	if _, err = f.wallet.ResolveRefund(refund.RefundNo, "failed", "", "{}", f.orders); err != nil {
		t.Fatal(err)
	}
	returned, _ := f.wallet.BalanceUnits(f.email)
	if _, err = f.wallet.ResolveRefund(refund.RefundNo, "failed", "", "{}", f.orders); err != nil {
		t.Fatal(err)
	}
	if balance, _ := f.wallet.BalanceUnits(f.email); balance != returned {
		t.Fatal("清理后退款重复返还")
	}
}

// TestUsageRetentionAtomicAndBounded 验证取消、溢出均保留全部明细，批次有界且金额只汇总一次。
func TestUsageRetentionAtomicAndBounded(t *testing.T) {
	now := time.Date(2026, 10, 22, 0, 0, 0, 0, chinaLocation())
	for _, bad := range []int64{math.MinInt64, -math.MaxInt64} {
		wallet := NewMemoryAIWalletStore()
		wallet.records = []AIWalletRecord{{Category: "ai_usage", ChangeUnits: -2, CreatedAt: now.Add(-10 * 24 * time.Hour)}, {Category: "ai_usage", ChangeUnits: bad, CreatedAt: now.Add(-10 * 24 * time.Hour)}}
		if _, err := wallet.PruneAIUsage(context.Background(), now.Add(-aiUsageRetention)); err == nil || len(wallet.records) != 2 || len(wallet.usageDaily) != 0 {
			t.Fatal("异常清理没有回滚")
		}
	}
	wallet := NewMemoryAIWalletStore()
	for i := 0; i < aiUsageCleanupBatch+1; i++ {
		wallet.records = append(wallet.records, AIWalletRecord{Category: "ai_usage", ChangeUnits: -1, CreatedAt: now.Add(-10 * 24 * time.Hour)})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := wallet.PruneAIUsage(ctx, now.Add(-aiUsageRetention)); err == nil || len(wallet.records) != aiUsageCleanupBatch+1 {
		t.Fatal("已取消清理修改了数据")
	}
	first, err := wallet.PruneAIUsage(context.Background(), now.Add(-aiUsageRetention))
	if err != nil || first.DeletedRecords != aiUsageCleanupBatch || !first.HasMore {
		t.Fatal("清理没有按批限制")
	}
	last, err := wallet.PruneAIUsage(context.Background(), now.Add(-aiUsageRetention))
	if err != nil || last.DeletedRecords != 1 || last.HasMore || len(wallet.records) != 0 {
		t.Fatal("后续批次没有清完")
	}
	units, err := wallet.UsageUnitsBetween(now.Add(-30*24*time.Hour), now)
	if err != nil || units != aiUsageCleanupBatch+1 {
		t.Fatalf("分批汇总金额=%d err=%v", units, err)
	}
	if _, err := wallet.UsageUnitsBetween(now.Add(-10*24*time.Hour).Add(time.Hour), now); err == nil {
		t.Fatal("日汇总不应假装支持未知的小时明细")
	}
}

// TestUsageRetentionAuthorization 验证GET和POST都只接受请求头密钥，URL和普通登录令牌不能触发删除。
func TestUsageRetentionAuthorization(t *testing.T) {
	const token = "cleanup-test-secret-0123456789abcdef"
	t.Setenv("GOODHR_AI_USAGE_CLEANUP_TOKEN", token)
	service := NewAIUsageRetentionService(NewMemoryAIWalletStore())
	for _, item := range []struct {
		method, header, value, path string
		want                        int
	}{{"GET", "", "", "/cleanup?token=" + token, 401}, {"POST", "Authorization", "Bearer wrong", "/cleanup", 401}, {"POST", "Authorization", "Bearer " + token, "/cleanup", 200}, {"GET", "X-Cleanup-Token", token, "/cleanup", 200}, {"DELETE", "Authorization", "Bearer " + token, "/cleanup", 405}} {
		req := httptest.NewRequest(item.method, item.path, nil)
		if item.header != "" {
			req.Header.Set(item.header, item.value)
		}
		response := httptest.NewRecorder()
		service.Cleanup(response, req)
		if response.Code != item.want || strings.Contains(response.Body.String(), token) || !strings.Contains(response.Header().Get("Cache-Control"), "no-store") {
			t.Fatalf("授权响应不正确=%d %s", response.Code, response.Body.String())
		}
		if item.want == 200 {
			var body AIUsageCleanupResult
			if json.Unmarshal(response.Body.Bytes(), &body) != nil || body.HasMore {
				t.Fatal("清理回执格式错误")
			}
		}
	}
	t.Setenv("GOODHR_AI_USAGE_CLEANUP_TOKEN", "short")
	response := httptest.NewRecorder()
	service.Cleanup(response, httptest.NewRequest("POST", "/cleanup", nil))
	if response.Code != 503 {
		t.Fatal("短密钥仍启用了清理")
	}
}

// TestUsageRetentionPostgres 验证真实事务原子汇总、并发去重及完整充值退款凭据；仅显式本机测试DSN可运行。
func TestUsageRetentionPostgres(t *testing.T) {
	db := aiRechargePostgresTestDB(t, false)
	aiRechargeApplyMigration(t, db, "0100_ai_usage_daily_retention.sql")
	email := "retention-pg@example.test"
	wallet, orders := aiRechargePostgresCredit(t, db, email, "retention-pg-order", 10000, 2000)
	for _, amount := range []int64{1234, 5678} {
		if _, err := wallet.AdjustBalance(AIWalletRecord{UserEmail: email, ChangeUnits: -amount, Category: "ai_usage"}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, chinaLocation())
	if _, err := db.Exec(`UPDATE ai_balance_records SET created_at=$1 WHERE category='ai_usage'`, now.Add(-9*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	total, bonus, _ := wallet.BalanceSnapshot(email)
	refundable, _ := wallet.RefundableUnits(email, "retention-pg-order")
	var group sync.WaitGroup
	errors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := wallet.PruneAIUsage(context.Background(), now.Add(-aiUsageRetention))
			errors <- err
		}()
	}
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	var details, count int
	var units int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_balance_records WHERE category='ai_usage'`).Scan(&details); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COALESCE(SUM(usage_units),0),COALESCE(SUM(record_count),0) FROM ai_usage_daily`).Scan(&units, &count); err != nil || details != 0 || units != 6912 || count != 2 {
		t.Fatalf("并发汇总=%d/%d，明细=%d，err=%v", units, count, details, err)
	}
	gotTotal, gotBonus, _ := wallet.BalanceSnapshot(email)
	gotRefundable, _ := wallet.RefundableUnits(email, "retention-pg-order")
	if total != gotTotal || bonus != gotBonus || refundable != gotRefundable {
		t.Fatal("数据库清理改变了资金")
	}
	month := time.Date(2026, 10, 1, 0, 0, 0, 0, chinaLocation())
	if got, err := wallet.UsageUnitsBetween(month, now); err != nil || got != 6912 {
		t.Fatalf("归档月统计=%d err=%v", got, err)
	}
	if _, err := wallet.AdjustBalance(AIWalletRecord{UserEmail: email, ChangeUnits: 1200000, BonusChangeUnits: 200000, Category: "recharge", RelatedOrderNo: "retention-pg-order"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := wallet.BalanceUnits(email); got != total {
		t.Fatal("归档后重复充值生效")
	}
	refund := PaymentRefund{RefundNo: "retention-pg-refund", OrderNo: "retention-pg-order", UserEmail: email, RefundType: refundTypeAIBalance, AmountCents: 1000}
	stored, err := wallet.ReserveRefund(refund, orders)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = wallet.ResolveRefund(stored.RefundNo, "failed", "", "{}", orders); err != nil {
		t.Fatal(err)
	}
	returned, err := wallet.BalanceUnits(email)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = wallet.ResolveRefund(stored.RefundNo, "failed", "", "{}", orders); err != nil {
		t.Fatal(err)
	}
	if repeated, err := wallet.BalanceUnits(email); err != nil || repeated != returned {
		t.Fatal("归档后重复退款返还改变余额")
	}
}

// TestUsageRetentionPostgresBlockedLedger 验证未升级分笔账本必须显式阻塞，不能删除旧记录或偷偷重算资金。
func TestUsageRetentionPostgresBlockedLedger(t *testing.T) {
	db := aiRechargePostgresTestDB(t, false)
	aiRechargeApplyMigration(t, db, "0100_ai_usage_daily_retention.sql")
	email := "retention-blocked@example.test"
	wallet, _ := aiRechargePostgresCredit(t, db, email, "retention-blocked-order", 10000, 2000)
	if _, err := wallet.AdjustBalance(AIWalletRecord{UserEmail: email, ChangeUnits: -1234, Category: "ai_usage"}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, chinaLocation())
	if _, err := db.Exec(`UPDATE ai_balance_records SET created_at=$1 WHERE category='ai_usage'`, now.Add(-9*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE ai_usage_daily ADD CONSTRAINT retention_test_reject CHECK (usage_units<1000)`); err != nil {
		t.Fatal(err)
	}
	if _, err := wallet.PruneAIUsage(context.Background(), now.Add(-aiUsageRetention)); err == nil {
		t.Fatal("汇总失败仍提交了清理事务")
	}
	var retained int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_balance_records WHERE category='ai_usage'`).Scan(&retained); err != nil || retained != 1 {
		t.Fatal("汇总失败没有回滚明细删除")
	}
	if _, err := db.Exec(`ALTER TABLE ai_usage_daily DROP CONSTRAINT retention_test_reject`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE users SET ai_wallet_ledger_version=0 WHERE email=$1`, email); err != nil {
		t.Fatal(err)
	}
	var beforeTotal, beforeBonus int64
	if err := db.QueryRow(`SELECT ai_balance_units,ai_bonus_balance_units FROM users WHERE email=$1`, email).Scan(&beforeTotal, &beforeBonus); err != nil {
		t.Fatal(err)
	}
	result, err := wallet.PruneAIUsage(context.Background(), now.Add(-aiUsageRetention))
	if err != nil || result.DeletedRecords != 0 || result.BlockedCount != 1 || !result.HasMore {
		t.Fatalf("旧账本没有显式阻塞: %+v err=%v", result, err)
	}
	var details, aggregates int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_balance_records WHERE category='ai_usage'`).Scan(&details); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_usage_daily`).Scan(&aggregates); err != nil || details != 1 || aggregates != 0 {
		t.Fatal("阻塞状态仍删除或汇总了旧账本")
	}
	var afterTotal, afterBonus int64
	if err := db.QueryRow(`SELECT ai_balance_units,ai_bonus_balance_units FROM users WHERE email=$1`, email).Scan(&afterTotal, &afterBonus); err != nil || beforeTotal != afterTotal || beforeBonus != afterBonus {
		t.Fatal("清理擅自迁移旧用户余额")
	}
	// 测试账户原本已建立完整账本；只恢复标志模拟人工核验完成，清理本身不负责执行历史迁移。
	if _, err := db.Exec(`UPDATE users SET ai_wallet_ledger_version=1 WHERE email=$1`, email); err != nil {
		t.Fatal(err)
	}
	result, err = wallet.PruneAIUsage(context.Background(), now.Add(-aiUsageRetention))
	if err != nil || result.DeletedRecords != 1 || result.BlockedCount != 0 || result.HasMore {
		t.Fatalf("已核验账本无法清理: %+v err=%v", result, err)
	}
	month := time.Date(2026, 10, 1, 0, 0, 0, 0, chinaLocation())
	if used, err := wallet.UsageUnitsBetween(month, now); err != nil || used != 1234 {
		t.Fatalf("清理后本月消费=%d err=%v", used, err)
	}
}
