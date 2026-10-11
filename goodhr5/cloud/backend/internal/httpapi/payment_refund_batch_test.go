// 本文件使用内存和本机独立 PostgreSQL，验证退款批次先筛到期再限量，且用户全部活跃退款仍用于阻止重复申请。
package httpapi

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// refundBatchFixture 为两个存储实现提供相同的订单、钱包和退款时间控制。
type refundBatchFixture struct {
	orders   PaymentStore
	wallet   AIWalletStore
	setTimes func(string, time.Time, time.Time)
}

// newRefundBatchFixture 创建资金测试夹具，PostgreSQL 仅使用已有的显式本机测试库入口。
func newRefundBatchFixture(t *testing.T, postgres bool) refundBatchFixture {
	t.Helper()
	if postgres {
		db := aiRechargePostgresTestDB(t, false)
		return refundBatchFixture{
			orders: NewPostgresPaymentStore(db), wallet: NewPostgresAIWalletStore(db),
			setTimes: func(refundNo string, created, updated time.Time) {
				t.Helper()
				if _, err := db.Exec(`UPDATE payment_refunds SET created_at=$2,updated_at=$3 WHERE refund_no=$1`, refundNo, created, updated); err != nil {
					t.Fatal(err)
				}
			},
		}
	}
	orders := NewMemoryPaymentStore()
	return refundBatchFixture{
		orders: orders, wallet: NewMemoryAIWalletStore(),
		setTimes: func(refundNo string, created, updated time.Time) {
			orders.mu.Lock()
			defer orders.mu.Unlock()
			item := orders.refunds[refundNo]
			item.CreatedAt, item.UpdatedAt = created, updated
			orders.refunds[refundNo] = item
		},
	}
}

// seedOrder 创建固定实付金额的已支付 AI 订单，可选择是否进行真实现金赠送入账。
func (f refundBatchFixture) seedOrder(t *testing.T, email, orderNo string, credit bool) {
	t.Helper()
	if _, err := f.orders.Create(PaymentOrder{OrderNo: orderNo, OrderType: "ai_balance", UserEmail: email, PlanID: "ai_balance", PlanName: "退款批次测试", AmountCents: 10000, BonusAmountCents: 2000, PaymentProvider: wechatPayProviderName, Status: "paid"}); err != nil {
		t.Fatal(err)
	}
	if credit {
		if _, err := f.wallet.AdjustBalance(AIWalletRecord{UserEmail: email, ChangeUnits: 1200000, BonusChangeUnits: 200000, Category: "recharge", RelatedOrderNo: orderNo}); err != nil {
			t.Fatal(err)
		}
	}
}

// runRefundBatchDueBeforeLimit 验证50个较老但未到期的退款不会阻塞第51个已经到期的新未知退款。
func runRefundBatchDueBeforeLimit(t *testing.T, postgres bool) {
	t.Helper()
	f := newRefundBatchFixture(t, postgres)
	const email = "refund-batch@example.test"
	f.seedOrder(t, email, "old-order", false)
	f.seedOrder(t, email, "due-order", true)
	now := time.Now()
	for index := range aiRefundReconcileBatchLimit {
		refundNo := fmt.Sprintf("old-not-due-%02d", index)
		if _, err := f.orders.CreateRefund(PaymentRefund{RefundNo: refundNo, OrderNo: "old-order", UserEmail: email, RefundType: refundTypeAIBalance, AmountCents: 1, Status: "processing"}); err != nil {
			t.Fatal(err)
		}
		// 创建超过24小时的退款每30分钟核实；其updated_at仍比新单旧，会先占满旧SQL的LIMIT。
		f.setTimes(refundNo, now.Add(-25*time.Hour), now.Add(-25*time.Minute))
	}
	refund, err := f.wallet.ReserveRefund(PaymentRefund{RefundNo: "new-due-unknown", OrderNo: "due-order", UserEmail: email, RefundType: refundTypeAIBalance, AmountCents: 1000}, f.orders)
	if err != nil {
		t.Fatal(err)
	}
	refund, err = f.wallet.ResolveRefund(refund.RefundNo, "unknown", "", `{}`, f.orders)
	if err != nil {
		t.Fatal(err)
	}
	f.setTimes(refund.RefundNo, now.Add(-2*time.Minute), now.Add(-2*time.Minute))
	batch, err := f.orders.ListOpenRefunds("")
	if err != nil || len(batch) != 1 || batch[0].RefundNo != refund.RefundNo {
		t.Fatalf("eligible batch=%+v err=%v", batch, err)
	}
	active, err := f.orders.ListOpenRefunds(email)
	if err != nil || len(active) != aiRefundReconcileBatchLimit+1 {
		t.Fatalf("user active refunds truncated or filtered: count=%d err=%v", len(active), err)
	}
	provider := &refundReconcileReviewProvider{queryResult: refundReconcileReviewResult(refund, "SUCCESS")}
	service := &PaymentService{orders: f.orders, aiWallet: f.wallet, providers: map[string]PaymentProvider{wechatPayProviderName: provider}}
	service.reconcileAIRefunds(context.Background())
	if provider.queryRefundNo != refund.RefundNo {
		t.Fatalf("batch did not query due new refund: got=%q want=%q", provider.queryRefundNo, refund.RefundNo)
	}
	stored, err := f.orders.ByRefundNo(refund.RefundNo)
	if err != nil || stored.Status != "success" {
		t.Fatalf("due refund not reconciled: %+v err=%v", stored, err)
	}
	total, bonus, err := f.wallet.BalanceSnapshot(email)
	if err != nil || total != 900000 || bonus != 0 {
		t.Fatalf("reconciliation changed held money: total=%d bonus=%d err=%v", total, bonus, err)
	}
}

// TestMemoryRefundBatchDueBeforeLimit 在内存中复现51条退款的到期筛选与真实核实路径。
func TestMemoryRefundBatchDueBeforeLimit(t *testing.T) { runRefundBatchDueBeforeLimit(t, false) }

// TestPostgresRefundBatchDueBeforeLimit 在真实PostgreSQL中验证SQL先筛到期再LIMIT，并完成新未知退款核实。
func TestPostgresRefundBatchDueBeforeLimit(t *testing.T) { runRefundBatchDueBeforeLimit(t, true) }

// runRefundBatchPendingGuard 验证未到期pending不会进入后台批次，但用户查找仍包含它并拒绝同单重复退款。
func runRefundBatchPendingGuard(t *testing.T, postgres bool) {
	t.Helper()
	f := newRefundBatchFixture(t, postgres)
	const email = "refund-pending-guard@example.test"
	f.seedOrder(t, email, "pending-order", true)
	first, err := f.wallet.ReserveRefund(PaymentRefund{RefundNo: "pending-first", OrderNo: "pending-order", UserEmail: email, RefundType: refundTypeAIBalance, AmountCents: 100}, f.orders)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := f.orders.ListOpenRefunds("")
	if err != nil || len(batch) != 0 {
		t.Fatalf("fresh pending should not be due: %+v err=%v", batch, err)
	}
	active, err := f.orders.ListOpenRefunds(email)
	if err != nil || len(active) != 1 || active[0].RefundNo != first.RefundNo {
		t.Fatalf("fresh pending missing from user guard: %+v err=%v", active, err)
	}
	if _, err = f.wallet.ReserveRefund(PaymentRefund{RefundNo: "pending-second", OrderNo: "pending-order", UserEmail: email, RefundType: refundTypeAIBalance, AmountCents: 100}, f.orders); err == nil {
		t.Fatal("not-yet-due pending allowed another same-order reservation")
	}
	total, bonus, err := f.wallet.BalanceSnapshot(email)
	if err != nil || total != 990000 || bonus != 0 {
		t.Fatalf("rejected duplicate changed wallet: total=%d bonus=%d err=%v", total, bonus, err)
	}
}

// TestMemoryRefundBatchPendingGuard 验证内存到期筛选没有削弱未到期退款的防重复检查。
func TestMemoryRefundBatchPendingGuard(t *testing.T) { runRefundBatchPendingGuard(t, false) }

// TestPostgresRefundBatchPendingGuard 验证真实数据库仍能阻止未到期退款的同单重复申请。
func TestPostgresRefundBatchPendingGuard(t *testing.T) { runRefundBatchPendingGuard(t, true) }

// runRefundBatchLimitAndOrder 验证全为到期记录时仍限制50条，且用户查找不截断第51条活跃记录。
func runRefundBatchLimitAndOrder(t *testing.T, postgres bool) {
	t.Helper()
	f := newRefundBatchFixture(t, postgres)
	const email = "refund-batch-limit@example.test"
	f.seedOrder(t, email, "batch-order", false)
	now := time.Now()
	for index := range aiRefundReconcileBatchLimit + 1 {
		refundNo := fmt.Sprintf("due-%02d", index)
		if _, err := f.orders.CreateRefund(PaymentRefund{RefundNo: refundNo, OrderNo: "batch-order", UserEmail: email, RefundType: refundTypeAIBalance, AmountCents: 1, Status: "unknown"}); err != nil {
			t.Fatal(err)
		}
		f.setTimes(refundNo, now.Add(-3*time.Minute), now.Add(-2*time.Minute))
	}
	batch, err := f.orders.ListOpenRefunds("")
	if err != nil || len(batch) != aiRefundReconcileBatchLimit {
		t.Fatalf("due batch limit count=%d err=%v", len(batch), err)
	}
	for index, item := range batch {
		if want := fmt.Sprintf("due-%02d", index); item.RefundNo != want {
			t.Fatalf("unstable equal-time batch: index=%d got=%q want=%q", index, item.RefundNo, want)
		}
	}
	active, err := f.orders.ListOpenRefunds(email)
	if err != nil || len(active) != aiRefundReconcileBatchLimit+1 {
		t.Fatalf("user lookup must include all active: count=%d err=%v", len(active), err)
	}
}

// TestMemoryRefundBatchLimitAndOrder 验证内存批次限量与相同更新时间的稳定顺序。
func TestMemoryRefundBatchLimitAndOrder(t *testing.T) { runRefundBatchLimitAndOrder(t, false) }

// TestPostgresRefundBatchLimitAndOrder 验证数据库批次限量与相同更新时间的稳定顺序。
func TestPostgresRefundBatchLimitAndOrder(t *testing.T) { runRefundBatchLimitAndOrder(t, true) }
