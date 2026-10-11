// 本文件使用独立内存资金夹具测试充值赠送分桶、分笔退款、欠费抵扣、失败恢复及消费退款并发。
package httpapi

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
)

// walletLedgerFixture 为资金测试创建独立的用户、钱包和已支付订单。
type walletLedgerFixture struct {
	wallet *MemoryAIWalletStore
	orders *MemoryPaymentStore
	email  string
}

// newWalletLedgerFixture 创建不读取环境变量、不连接数据库或支付渠道的内存夹具。
func newWalletLedgerFixture() walletLedgerFixture {
	return walletLedgerFixture{wallet: NewMemoryAIWalletStore(), orders: NewMemoryPaymentStore(), email: "wallet-ledger@example.test"}
}

// credit 创建已支付订单并通过真实钱包接口一次性入账本金和赠送。
func (f walletLedgerFixture) credit(t *testing.T, orderNo string, principalCents, bonusCents int) {
	t.Helper()
	if _, err := f.orders.Create(PaymentOrder{OrderNo: orderNo, UserEmail: f.email, OrderType: "ai_balance", AmountCents: principalCents, BonusAmountCents: bonusCents, Status: "paid", PaymentProvider: wechatPayProviderName}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.wallet.AdjustBalance(AIWalletRecord{UserEmail: f.email, ChangeUnits: centsToAIUnits(principalCents + bonusCents), BonusChangeUnits: centsToAIUnits(bonusCents), Category: "recharge", RelatedOrderNo: orderNo}); err != nil {
		t.Fatal(err)
	}
}

// consume 扣除已经完成的 AI 实际费用。
func (f walletLedgerFixture) consume(t *testing.T, units int64) {
	t.Helper()
	if _, err := f.wallet.AdjustBalance(AIWalletRecord{UserEmail: f.email, ChangeUnits: -units, Category: "ai_usage"}); err != nil {
		t.Fatal(err)
	}
}

// assertFunds 同时检查总余额、赠送缓存与分笔合计。
func (f walletLedgerFixture) assertFunds(t *testing.T, total, bonus int64) {
	t.Helper()
	actualTotal, err := f.wallet.BalanceUnits(f.email)
	if err != nil {
		t.Fatal(err)
	}
	actualBonus, err := f.wallet.BonusBalanceUnits(f.email)
	if err != nil {
		t.Fatal(err)
	}
	if actualTotal != total || actualBonus != bonus {
		t.Fatalf("total=%d bonus=%d; want total=%d bonus=%d", actualTotal, actualBonus, total, bonus)
	}
	f.wallet.mu.Lock()
	defer f.wallet.mu.Unlock()
	if err = f.wallet.memoryLedgerLocked(f.email).validate(); err != nil {
		t.Fatal(err)
	}
}

// refund 构造指定订单的本金退款请求。
func (f walletLedgerFixture) refund(refundNo, orderNo string, cents int) PaymentRefund {
	return PaymentRefund{RefundNo: refundNo, OrderNo: orderNo, UserEmail: f.email, RefundType: refundTypeAIBalance, AmountCents: cents}
}

// TestAIWalletLedgerRefundExamples 验证用户要求的100送20余额30只退10且收回20，余额10全部赠送不可退。
func TestAIWalletLedgerRefundExamples(t *testing.T) {
	for _, tc := range []struct {
		name        string
		cost        int64
		refundCents int
		allowed     bool
	}{
		{"thirty_remains_refund_ten", 900000, 1000, true},
		{"only_ten_bonus_remains", 1100000, 1, false},
		{"cannot_refund_bonus", 900000, 1001, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newWalletLedgerFixture()
			f.credit(t, "paid", 10000, 2000)
			f.consume(t, tc.cost)
			before, _ := f.wallet.BalanceUnits(f.email)
			refund, err := f.wallet.ReserveRefund(f.refund("refund", "paid", tc.refundCents), f.orders)
			if tc.allowed {
				if err != nil || refund.RevokedBonusUnits != 200000 || !refund.RightsReserved {
					t.Fatalf("refund=%+v err=%v", refund, err)
				}
				f.assertFunds(t, 0, 0)
			} else {
				if err == nil {
					t.Fatal("gift or spent principal refunded")
				}
				after, _ := f.wallet.BalanceUnits(f.email)
				if after != before {
					t.Fatal("rejected refund changed funds")
				}
				if _, err = f.orders.ByRefundNo("refund"); !errors.Is(err, ErrNotFound) {
					t.Fatalf("rejected refund created record: %v", err)
				}
			}
		})
	}
}

// TestAIWalletLedgerAllPrincipalBeforeBonusFIFO 验证所有充值本金先消费，随后赠送先进先出，新充值不能替旧已消费订单退款。
func TestAIWalletLedgerAllPrincipalBeforeBonusFIFO(t *testing.T) {
	f := newWalletLedgerFixture()
	if _, err := f.wallet.AdjustBalance(AIWalletRecord{UserEmail: f.email, ChangeUnits: 7000, Category: "signup_bonus"}); err != nil {
		t.Fatal(err)
	}
	f.credit(t, "a", 10000, 2000)
	f.credit(t, "b", 5000, 500)
	f.consume(t, 1000000)
	if cash, err := f.wallet.RefundableUnits(f.email, "a"); err != nil || cash != 0 {
		t.Fatalf("old cash=%d err=%v", cash, err)
	}
	if cash, err := f.wallet.RefundableUnits(f.email, "b"); err != nil || cash != 500000 {
		t.Fatalf("new cash=%d err=%v", cash, err)
	}
	f.assertFunds(t, 757000, 257000)
	f.consume(t, 510000)
	f.assertFunds(t, 247000, 247000)
	f.wallet.mu.Lock()
	ledger := f.wallet.memoryLedgerLocked(f.email)
	f.wallet.mu.Unlock()
	if ledger.Lots[0].RemainingBonusUnits != 0 || ledger.Lots[1].RemainingBonusUnits != 197000 || ledger.Lots[2].RemainingBonusUnits != 50000 {
		t.Fatalf("bonus order wrong: %+v", ledger.Lots)
	}
	f.credit(t, "c", 10000, 2000)
	if _, err := f.wallet.ReserveRefund(f.refund("old-refund", "a", 1), f.orders); err == nil {
		t.Fatal("new cash reopened spent old principal")
	}
	if _, err := f.wallet.ReserveRefund(f.refund("new-refund", "c", 10000), f.orders); err != nil {
		t.Fatal(err)
	}
	f.assertFunds(t, 247000, 247000)
}

// TestAIWalletLedgerPartialRefundRevokesWholeOrderGift 验证小额部分退款也收回原订单全部未用赠送，其他订单赠送保留。
func TestAIWalletLedgerPartialRefundRevokesWholeOrderGift(t *testing.T) {
	f := newWalletLedgerFixture()
	f.credit(t, "a", 10000, 2000)
	f.credit(t, "b", 5000, 500)
	refund, err := f.wallet.ReserveRefund(f.refund("partial", "a", 100), f.orders)
	if err != nil || refund.RevokedBonusUnits != 200000 {
		t.Fatalf("refund=%+v err=%v", refund, err)
	}
	f.assertFunds(t, 1540000, 50000)
	if _, err = f.wallet.ResolveRefund("partial", "success", "wx-refund", `{}`, f.orders); err != nil {
		t.Fatal(err)
	}
	if _, err = f.wallet.ReserveRefund(f.refund("remaining", "a", 9900), f.orders); err != nil {
		t.Fatal(err)
	}
	f.assertFunds(t, 550000, 50000)
}

// TestAIWalletLedgerRefundUnknownFailureAndTerminal 验证未知及处理中保留占用，失败返还一次，成功终态不回退。
func TestAIWalletLedgerRefundUnknownFailureAndTerminal(t *testing.T) {
	f := newWalletLedgerFixture()
	f.credit(t, "a", 10000, 2000)
	f.consume(t, 900000)
	refund, err := f.wallet.ReserveRefund(f.refund("reserved", "a", 1000), f.orders)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate, err := f.wallet.ReserveRefund(f.refund("reserved", "a", 1000), f.orders); err != nil || duplicate.RefundNo != refund.RefundNo {
		t.Fatalf("duplicate=%+v err=%v", duplicate, err)
	}
	for _, status := range []string{"unknown", "processing", "ABNORMAL"} {
		if _, err = f.wallet.ResolveRefund("reserved", status, "wx", `{}`, f.orders); err != nil {
			t.Fatal(err)
		}
		f.assertFunds(t, 0, 0)
	}
	for range 2 {
		resolved, err := f.wallet.ResolveRefund("reserved", "closed", "wx", `{}`, f.orders)
		if err != nil || resolved.RightsReserved {
			t.Fatalf("closed=%+v err=%v", resolved, err)
		}
		f.assertFunds(t, 300000, 200000)
	}
	if _, err = f.wallet.ResolveRefund("reserved", "processing", "wx", `{}`, f.orders); err != nil {
		t.Fatal(err)
	}
	f.assertFunds(t, 300000, 200000)
	if _, err = f.wallet.ReserveRefund(f.refund("succeeded", "a", 1000), f.orders); err != nil {
		t.Fatal(err)
	}
	if _, err = f.wallet.ResolveRefund("succeeded", "success", "wx-ok", `{}`, f.orders); err != nil {
		t.Fatal(err)
	}
	if item, err := f.wallet.ResolveRefund("succeeded", "failed", "wx-ok", `{}`, f.orders); err != nil || item.Status != "success" {
		t.Fatalf("success reverted=%+v err=%v", item, err)
	}
	f.assertFunds(t, 0, 0)
	records, _, _ := f.wallet.ListRecords(f.email, 100, 0)
	var releases int
	for _, item := range records {
		if item.Category == "refund_release" {
			releases++
		}
	}
	if releases != 1 {
		t.Fatalf("duplicate release records=%d", releases)
	}
}

// TestAIWalletLedgerDebtUsesIncomingFunds 验证真实欠费先抵新本金再抵赠送，任何时候赠送都不超过可用总余额。
func TestAIWalletLedgerDebtUsesIncomingFunds(t *testing.T) {
	for _, debt := range []int64{100000, 1100000, 1300000} {
		t.Run(fmt.Sprint(debt), func(t *testing.T) {
			f := newWalletLedgerFixture()
			f.consume(t, debt)
			f.credit(t, "a", 10000, 2000)
			wantTotal := int64(1200000) - debt
			wantBonus := min(int64(200000), max(int64(0), wantTotal))
			f.assertFunds(t, wantTotal, wantBonus)
			cash, err := f.wallet.RefundableUnits(f.email, "a")
			if err != nil || cash != max(int64(0), wantTotal-wantBonus) {
				t.Fatalf("cash=%d err=%v", cash, err)
			}
		})
	}
}

// TestAIWalletLedgerFailedRefundPaysNewDebt 验证退款等待期间产生的欠费由失败返还资金先抵扣，不能把原赠送恢复成负数或现金。
func TestAIWalletLedgerFailedRefundPaysNewDebt(t *testing.T) {
	f := newWalletLedgerFixture()
	f.credit(t, "a", 10000, 2000)
	if _, err := f.wallet.ReserveRefund(f.refund("pending", "a", 10000), f.orders); err != nil {
		t.Fatal(err)
	}
	f.consume(t, 1100000)
	if _, err := f.wallet.ResolveRefund("pending", "failed", "", `{}`, f.orders); err != nil {
		t.Fatal(err)
	}
	f.assertFunds(t, 100000, 100000)
	if units, err := f.wallet.RefundableUnits(f.email, "a"); err != nil || units != 0 {
		t.Fatalf("cash restored over debt=%d err=%v", units, err)
	}
}

// TestAIWalletLedgerGiftsAreNotRefundable 验证注册、管理员赠送及普通赠送均不会变为充值本金。
func TestAIWalletLedgerGiftsAreNotRefundable(t *testing.T) {
	f := newWalletLedgerFixture()
	for _, category := range []string{"signup_bonus", "admin_adjust", "gift"} {
		if _, err := f.wallet.AdjustBalance(AIWalletRecord{UserEmail: f.email, ChangeUnits: 10000, Category: category, RelatedOrderNo: "fake-order"}); err != nil {
			t.Fatal(err)
		}
	}
	f.assertFunds(t, 30000, 30000)
	if cash, err := f.wallet.RefundableUnits(f.email, "fake-order"); err != nil || cash != 0 {
		t.Fatalf("gift refundable=%d err=%v", cash, err)
	}
}

// TestAIWalletLedgerOnlyOnePendingPerOrder 验证同单第二笔处理中退款被拒绝，防止先失败的退款复活已被另一退款收回赠送。
func TestAIWalletLedgerOnlyOnePendingPerOrder(t *testing.T) {
	f := newWalletLedgerFixture()
	f.credit(t, "a", 10000, 2000)
	if _, err := f.wallet.ReserveRefund(f.refund("one", "a", 100), f.orders); err != nil {
		t.Fatal(err)
	}
	if units, err := f.wallet.RefundableUnits(f.email, "a"); err != nil || units != 0 {
		t.Fatalf("pending refund still quoted cash=%d err=%v", units, err)
	}
	if _, err := f.wallet.ReserveRefund(f.refund("two", "a", 100), f.orders); err == nil {
		t.Fatal("second pending refund accepted")
	}
	f.assertFunds(t, 990000, 0)
	if _, err := f.wallet.ResolveRefund("one", "failed", "", `{}`, f.orders); err != nil {
		t.Fatal(err)
	}
	f.assertFunds(t, 1200000, 200000)
}

// TestAIWalletLedgerFractionalCashAndLegacyUnreservedRefund 验证不足一分钱不退款，旧未预扣的退款失败也不能凭空返还现金。
func TestAIWalletLedgerFractionalCashAndLegacyUnreservedRefund(t *testing.T) {
	f := newWalletLedgerFixture()
	f.credit(t, "a", 100, 0)
	f.consume(t, 9901)
	if units, err := f.wallet.RefundableUnits(f.email, "a"); err != nil || units != 99 {
		t.Fatalf("fractional cash=%d err=%v", units, err)
	}
	if _, err := f.wallet.ReserveRefund(f.refund("too-small", "a", 1), f.orders); err == nil {
		t.Fatal("fractional balance rounded up to a refundable cent")
	}
	if _, err := f.orders.CreateRefund(PaymentRefund{RefundNo: "legacy-not-deducted", OrderNo: "a", UserEmail: f.email, RefundType: refundTypeAIBalance, AmountCents: 1, Status: "action_required", RightsReserved: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.wallet.ResolveRefund("legacy-not-deducted", "closed", "", `{}`, f.orders); err != nil {
		t.Fatal(err)
	}
	f.assertFunds(t, 99, 0)
}

// TestAIWalletLedgerRefundOrderValidation 验证越权、未支付、错类型和超额请求不会扣款或写入退款。
func TestAIWalletLedgerRefundOrderValidation(t *testing.T) {
	for _, name := range []string{"wrong_user", "unpaid", "membership", "too_much", "wrong_refund_type"} {
		t.Run(name, func(t *testing.T) {
			f := newWalletLedgerFixture()
			f.credit(t, "a", 10000, 2000)
			request := f.refund("invalid", "a", 1)
			f.orders.mu.Lock()
			order := f.orders.orders["a"]
			switch name {
			case "wrong_user":
				order.UserEmail = "other@example.test"
			case "unpaid":
				order.Status = "pending"
			case "membership":
				order.OrderType = "subscription"
			case "too_much":
				request.AmountCents = 10001
			case "wrong_refund_type":
				request.RefundType = refundTypeMembership
			}
			f.orders.orders["a"] = order
			f.orders.mu.Unlock()
			if _, err := f.wallet.ReserveRefund(request, f.orders); err == nil {
				t.Fatal("invalid refund accepted")
			}
			f.assertFunds(t, 1200000, 200000)
			if _, err := f.orders.ByRefundNo("invalid"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("invalid request persisted: %v", err)
			}
		})
	}
}

// TestAIWalletLedgerConcurrentCreditAndRefund 验证并发支付回调去重和并发退款仅允许一次占用。
func TestAIWalletLedgerConcurrentCreditAndRefund(t *testing.T) {
	f := newWalletLedgerFixture()
	f.credit(t, "a", 10000, 2000)
	var group sync.WaitGroup
	creditErrors := make(chan error, 24)
	for range 24 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := f.wallet.AdjustBalance(AIWalletRecord{UserEmail: f.email, ChangeUnits: 1200000, BonusChangeUnits: 200000, Category: "recharge", RelatedOrderNo: "a"})
			creditErrors <- err
		}()
	}
	group.Wait()
	close(creditErrors)
	for err := range creditErrors {
		if err != nil {
			t.Fatal(err)
		}
	}
	f.assertFunds(t, 1200000, 200000)
	refundErrors := make(chan error, 24)
	for index := range 24 {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			_, err := f.wallet.ReserveRefund(f.refund(fmt.Sprintf("refund-%d", index), "a", 10000), f.orders)
			refundErrors <- err
		}(index)
	}
	group.Wait()
	close(refundErrors)
	var accepted int
	for err := range refundErrors {
		if err == nil {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted concurrent refunds=%d", accepted)
	}
	f.assertFunds(t, 0, 0)
}

// TestAIWalletLedgerConsumptionRefundRace 验证 AI 真实扣费与退款竞争时不会用不足本金提交退款。
func TestAIWalletLedgerConsumptionRefundRace(t *testing.T) {
	for range 40 {
		f := newWalletLedgerFixture()
		f.credit(t, "a", 10000, 2000)
		start := make(chan struct{})
		refundResult := make(chan error, 1)
		chargeResult := make(chan error, 1)
		go func() {
			<-start
			_, err := f.wallet.ReserveRefund(f.refund("race", "a", 10000), f.orders)
			refundResult <- err
		}()
		go func() {
			<-start
			_, err := f.wallet.AdjustBalance(AIWalletRecord{UserEmail: f.email, ChangeUnits: -100000, Category: "ai_usage"})
			chargeResult <- err
		}()
		close(start)
		refundErr, chargeErr := <-refundResult, <-chargeResult
		if chargeErr != nil {
			t.Fatal(chargeErr)
		}
		if refundErr == nil {
			f.assertFunds(t, -100000, 0)
		} else {
			f.assertFunds(t, 1100000, 200000)
		}
	}
}

// TestAIWalletLedgerUnsafeHistoryAndOverflow 验证缺失分笔的旧余额不可自动退款，金额溢出不改动原资金。
func TestAIWalletLedgerUnsafeHistoryAndOverflow(t *testing.T) {
	f := newWalletLedgerFixture()
	f.wallet.balances[f.email] = 300000
	if _, err := f.wallet.RefundableUnits(f.email, "legacy"); !errors.Is(err, ErrAIWalletRefundBlocked) {
		t.Fatalf("unsafe history err=%v", err)
	}
	if _, err := f.wallet.AdjustBalance(AIWalletRecord{UserEmail: f.email, ChangeUnits: math.MaxInt64, Category: "gift"}); err == nil {
		t.Fatal("overflow accepted")
	}
	f.assertFunds(t, 300000, 300000)
}
