// 本文件独立验证退款通知和自动对账的资金边界，支付提供方使用现有测试实现，不访问微信网络。
package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// refundReconcileReviewProvider 复用已有假支付提供方，仅控制退款查询、通知和原单重试结果。
type refundReconcileReviewProvider struct {
	fakeWechatPaymentProvider
	queryResult   PaymentProviderRefundResult
	queryError    error
	queryRefundNo string
	refundError   error
	refundStatus  string
	refundInputs  []PaymentProviderRefundInput
	notifyResult  PaymentProviderRefundResult
	notifyError   error
}

// QueryRefund 记录查询使用的原退款号，并返回可控的第三方结果。
func (p *refundReconcileReviewProvider) QueryRefund(_ context.Context, refundNo string) (PaymentProviderRefundResult, error) {
	p.queryRefundNo = refundNo
	return p.queryResult, p.queryError
}

// Refund 记录完整原单重试参数，按配置返回受理状态或明确拒绝。
func (p *refundReconcileReviewProvider) Refund(_ context.Context, input PaymentProviderRefundInput) (PaymentProviderRefundResult, error) {
	p.refundInputs = append(p.refundInputs, input)
	if p.refundError != nil {
		return PaymentProviderRefundResult{}, p.refundError
	}
	status := p.refundStatus
	if status == "" {
		status = "PROCESSING"
	}
	return PaymentProviderRefundResult{Provider: p.Name(), OrderNo: input.OrderNo, RefundNo: input.RefundNo, ProviderRefundID: "review-provider-refund", AmountCents: input.AmountCents, TotalCents: input.TotalCents, Status: status}, nil
}

// ParseRefundNotify 模拟支付提供方验签后的通知，业务层仍需独立校验订单和金额。
func (p *refundReconcileReviewProvider) ParseRefundNotify(_ context.Context, _ *http.Request) (PaymentProviderRefundResult, error) {
	return p.notifyResult, p.notifyError
}

// refundReconcileReviewFixture 创建一笔本金 100 元、赠送 20 元的独立钱包，可选择是否预占 10 元退款。
func refundReconcileReviewFixture(t *testing.T, reserve bool) (*PaymentService, *MemoryAIWalletStore, PaymentRefund, *refundReconcileReviewProvider) {
	t.Helper()
	email, orderNo := "refund-review@example.test", "refund-review-order"
	orders := NewMemoryPaymentStore()
	if _, err := orders.Create(PaymentOrder{OrderNo: orderNo, OrderType: "ai_balance", UserEmail: email, PlanID: "ai_balance", PlanName: "退款审查测试", AmountCents: 10000, BonusAmountCents: 2000, PaymentProvider: wechatPayProviderName, Status: "paid"}); err != nil {
		t.Fatal(err)
	}
	wallet := NewMemoryAIWalletStore()
	if _, err := wallet.AdjustBalance(AIWalletRecord{UserEmail: email, ChangeUnits: 1200000, BonusChangeUnits: 200000, Category: "recharge", RelatedOrderNo: orderNo}); err != nil {
		t.Fatal(err)
	}
	refund := PaymentRefund{RefundNo: "refund-review-original", OrderNo: orderNo, UserEmail: email, RefundType: refundTypeAIBalance, AmountCents: 1000, Status: "pending"}
	var err error
	if reserve {
		refund, err = wallet.ReserveRefund(refund, orders)
	} else {
		refund, err = orders.CreateRefund(refund)
	}
	if err != nil {
		t.Fatal(err)
	}
	provider := &refundReconcileReviewProvider{}
	service := &PaymentService{orders: orders, aiWallet: wallet, providers: map[string]PaymentProvider{wechatPayProviderName: provider}}
	return service, wallet, refund, provider
}

// refundReconcileReviewResult 生成和原订单、退款号及实付金额一致的查询或通知结果。
func refundReconcileReviewResult(refund PaymentRefund, status string) PaymentProviderRefundResult {
	return PaymentProviderRefundResult{Provider: wechatPayProviderName, OrderNo: refund.OrderNo, RefundNo: refund.RefundNo, ProviderRefundID: "review-provider-refund", AmountCents: refund.AmountCents, TotalCents: 10000, Status: status}
}

// refundReconcileReviewBalance 独立校验总余额和赠送余额，确保查单结果不会重复扣返资金。
func refundReconcileReviewBalance(t *testing.T, wallet *MemoryAIWalletStore, email string, wantTotal, wantBonus int64) {
	t.Helper()
	total, bonus, err := wallet.BalanceSnapshot(email)
	if err != nil || total != wantTotal || bonus != wantBonus {
		t.Fatalf("refund review total=%d bonus=%d want total=%d bonus=%d err=%v", total, bonus, wantTotal, wantBonus, err)
	}
}

// TestRefundReconcileUnknownRetriesOriginal 验证未知结果确认不存在后仅重试原退款号，处理中查单不存在不能释放或重新提交。
func TestRefundReconcileUnknownRetriesOriginal(t *testing.T) {
	service, wallet, refund, provider := refundReconcileReviewFixture(t, true)
	var err error
	refund, err = wallet.ResolveRefund(refund.RefundNo, "unknown", "", `{}`, service.orders)
	if err != nil {
		t.Fatal(err)
	}
	provider.queryError = ErrRefundNotFound
	if err := service.reconcileAIRefund(context.Background(), refund); err != nil {
		t.Fatal(err)
	}
	if provider.queryRefundNo != refund.RefundNo || len(provider.refundInputs) != 1 {
		t.Fatalf("query=%q retry calls=%d", provider.queryRefundNo, len(provider.refundInputs))
	}
	input := provider.refundInputs[0]
	if input.OrderNo != refund.OrderNo || input.RefundNo != refund.RefundNo || input.AmountCents != 1000 || input.TotalCents != 10000 {
		t.Fatalf("original retry input changed: %+v", input)
	}
	refund, err = service.orders.ByRefundNo(refund.RefundNo)
	if err != nil || refund.Status != "processing" || !refund.RightsReserved {
		t.Fatalf("retry refund=%+v err=%v", refund, err)
	}
	refundReconcileReviewBalance(t, wallet, refund.UserEmail, 900000, 0)
	if err = service.reconcileAIRefund(context.Background(), refund); err == nil || len(provider.refundInputs) != 1 {
		t.Fatalf("processing missing query must keep hold and avoid retry: calls=%d err=%v", len(provider.refundInputs), err)
	}
	provider.queryError = nil
	provider.queryResult = refundReconcileReviewResult(refund, "SUCCESS")
	if err = service.reconcileAIRefund(context.Background(), refund); err != nil {
		t.Fatal(err)
	}
	refundReconcileReviewBalance(t, wallet, refund.UserEmail, 900000, 0)
	stored, err := service.orders.ByRefundNo(refund.RefundNo)
	if err != nil || stored.Status != "success" {
		t.Fatalf("confirmed refund=%+v err=%v", stored, err)
	}
}

// TestRefundReconcileQueryFailureAndRejectedRetry 验证网络查单失败保持占用，原单明确拒绝后仅恢复一次本金和赠送。
func TestRefundReconcileQueryFailureAndRejectedRetry(t *testing.T) {
	service, wallet, refund, provider := refundReconcileReviewFixture(t, true)
	provider.queryError = errors.New("simulated query timeout")
	if err := service.reconcileAIRefund(context.Background(), refund); err == nil || len(provider.refundInputs) != 0 {
		t.Fatalf("query timeout must not submit another refund: err=%v calls=%d", err, len(provider.refundInputs))
	}
	refundReconcileReviewBalance(t, wallet, refund.UserEmail, 900000, 0)
	provider.queryError = ErrRefundNotFound
	provider.refundError = ErrRefundRejected
	for repeat := 0; repeat < 2; repeat++ {
		if err := service.reconcileAIRefund(context.Background(), refund); err != nil {
			t.Fatal(err)
		}
		refundReconcileReviewBalance(t, wallet, refund.UserEmail, 1200000, 200000)
	}
	stored, err := service.orders.ByRefundNo(refund.RefundNo)
	if err != nil || stored.Status != "failed" || stored.RightsReserved {
		t.Fatalf("rejected original refund=%+v err=%v", stored, err)
	}
}

// TestRefundReconcileMismatchKeepsReservation 验证通知或查询的归属、金额、退款号和状态不一致时不释放资金。
func TestRefundReconcileMismatchKeepsReservation(t *testing.T) {
	for _, mismatch := range []string{"order", "refund", "amount", "total", "provider", "provider_id", "status"} {
		t.Run(mismatch, func(t *testing.T) {
			service, wallet, refund, provider := refundReconcileReviewFixture(t, true)
			result := refundReconcileReviewResult(refund, "CLOSED")
			switch mismatch {
			case "order":
				result.OrderNo = "different-order"
			case "refund":
				result.RefundNo = "different-refund"
			case "amount":
				result.AmountCents++
			case "total":
				result.TotalCents++
			case "provider":
				result.Provider = "different-provider"
			case "provider_id":
				result.ProviderRefundID = ""
			case "status":
				result.Status = "UNRECOGNIZED"
			}
			provider.queryResult = result
			if err := service.reconcileAIRefund(context.Background(), refund); err == nil {
				t.Fatal("mismatched query must not resolve refund")
			}
			provider.notifyResult = result
			response := httptest.NewRecorder()
			service.WechatRefundNotify(response, httptest.NewRequest(http.MethodPost, "/api/payment/refund-notify/wechat", nil))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("mismatched callback status=%d body=%s", response.Code, response.Body.String())
			}
			refundReconcileReviewBalance(t, wallet, refund.UserEmail, 900000, 0)
			stored, err := service.orders.ByRefundNo(refund.RefundNo)
			if err != nil || !stored.RightsReserved || stored.Status != "pending" {
				t.Fatalf("mismatch changed reservation=%+v err=%v", stored, err)
			}
		})
	}
}

// TestRefundReconcileLegacyUnreservedNeedsAction 验证旧未预扣退款即使渠道成功也只能进入人工核对，不宣告成功或凭空返还。
func TestRefundReconcileLegacyUnreservedNeedsAction(t *testing.T) {
	service, wallet, refund, provider := refundReconcileReviewFixture(t, false)
	provider.queryResult = refundReconcileReviewResult(refund, "SUCCESS")
	if err := service.reconcileAIRefund(context.Background(), refund); err != nil {
		t.Fatal(err)
	}
	stored, err := service.orders.ByRefundNo(refund.RefundNo)
	if err != nil || stored.Status != "action_required" || stored.RightsReserved {
		t.Fatalf("old refund must require manual verification: %+v err=%v", stored, err)
	}
	refundReconcileReviewBalance(t, wallet, refund.UserEmail, 1200000, 200000)
	provider.notifyResult = refundReconcileReviewResult(stored, "CLOSED")
	response := httptest.NewRecorder()
	service.WechatRefundNotify(response, httptest.NewRequest(http.MethodPost, "/api/payment/refund-notify/wechat", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("legacy verified notification status=%d", response.Code)
	}
	refundReconcileReviewBalance(t, wallet, refund.UserEmail, 1200000, 200000)
	stored, err = service.orders.ByRefundNo(refund.RefundNo)
	if err != nil || stored.Status != "action_required" {
		t.Fatalf("legacy closure must not auto-release: %+v err=%v", stored, err)
	}
}

// TestRefundNotifyClosedAndDuplicateSuccess 验证已验签失败通知不改资金，关闭通知恢复一次，成功终态不接受迟到关闭通知返还。
func TestRefundNotifyClosedAndDuplicateSuccess(t *testing.T) {
	service, wallet, refund, provider := refundReconcileReviewFixture(t, true)
	provider.notifyError = errors.New("simulated signature error")
	response := httptest.NewRecorder()
	service.WechatRefundNotify(response, httptest.NewRequest(http.MethodPost, "/api/payment/refund-notify/wechat", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("signature error status=%d", response.Code)
	}
	refundReconcileReviewBalance(t, wallet, refund.UserEmail, 900000, 0)
	provider.notifyError = nil
	provider.notifyResult = refundReconcileReviewResult(refund, "CLOSED")
	for repeat := 0; repeat < 2; repeat++ {
		response = httptest.NewRecorder()
		service.WechatRefundNotify(response, httptest.NewRequest(http.MethodPost, "/api/payment/refund-notify/wechat", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("closed notification status=%d", response.Code)
		}
		refundReconcileReviewBalance(t, wallet, refund.UserEmail, 1200000, 200000)
	}
	service, wallet, refund, provider = refundReconcileReviewFixture(t, true)
	for _, status := range []string{"SUCCESS", "SUCCESS", "CLOSED"} {
		provider.notifyResult = refundReconcileReviewResult(refund, status)
		response = httptest.NewRecorder()
		service.WechatRefundNotify(response, httptest.NewRequest(http.MethodPost, "/api/payment/refund-notify/wechat", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("terminal notification status=%s response=%d", status, response.Code)
		}
		refundReconcileReviewBalance(t, wallet, refund.UserEmail, 900000, 0)
	}
}
