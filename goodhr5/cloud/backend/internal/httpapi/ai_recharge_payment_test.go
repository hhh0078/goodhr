// 本文件验证充值活动下单快照、实际收款、赠送到账和用户退款示例，不调用真实支付渠道。
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// rechargeOrderProvider 记录下单和退款的真实本金参数，用于核对赠送不会传给支付渠道。
type rechargeOrderProvider struct {
	fakeWechatPaymentProvider
	orderInput   PaymentProviderOrderInput
	refundInputs []PaymentProviderRefundInput
}

// CreateOrder 记录实付下单参数并返回模拟二维码。
func (p *rechargeOrderProvider) CreateOrder(ctx context.Context, input PaymentProviderOrderInput) (PaymentProviderOrderResult, error) {
	p.orderInput = input
	return p.fakeWechatPaymentProvider.CreateOrder(ctx, input)
}

// Refund 记录实际退回金额及原实付金额，返回确定成功结果。
func (p *rechargeOrderProvider) Refund(ctx context.Context, input PaymentProviderRefundInput) (PaymentProviderRefundResult, error) {
	p.refundInputs = append(p.refundInputs, input)
	return p.fakeWechatPaymentProvider.Refund(ctx, input)
}

// newRechargeHTTPTest 创建严格不连接业务数据库、不发送真实邮件的充值测试服务。
func newRechargeHTTPTest(t *testing.T) (*Server, http.Handler, string, *rechargeOrderProvider) {
	t.Helper()
	t.Setenv("GOODHR_PG_DSN", "")
	t.Setenv("GOODHR_REDIS_ADDR", "")
	t.Setenv("GOODHR_SMTP_HOST", "")
	t.Setenv("GOODHR_SUPER_ADMINS", "recharge-http@example.com")
	server := mustNewServer(t)
	provider := &rechargeOrderProvider{}
	server.payments.providers = map[string]PaymentProvider{provider.Name(): provider}
	routes := server.Routes()
	token := loginForTest(t, routes, "recharge-http@example.com")
	// 登录赠送属于另一条业务；资金用例使用独立空钱包，精确复现用户给出的金额。
	wallet := NewMemoryAIWalletStore()
	server.payments.aiWallet, server.aiWallet.wallet = wallet, wallet
	return server, routes, token, provider
}

// rechargeTestRequest 通过真实路由提交已登录请求并保留响应以核对金额和状态。
func rechargeTestRequest(routes http.Handler, token, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	routes.ServeHTTP(response, request)
	return response
}

// TestAIRechargePromotionOrderSnapshot 验证最高档不叠加、渠道仅收本金、修改活动不影响旧订单和重复到账。
func TestAIRechargePromotionOrderSnapshot(t *testing.T) {
	server, routes, token, provider := newRechargeHTTPTest(t)
	err := server.systemConfigs.Save(SystemConfig{ConfigKey: "system.app_config", Enabled: true, ConfigValue: `{"ai_recharge_promotions":[{"amount_cents":20000,"bonus_cents":5000},{"amount_cents":5000,"bonus_cents":500},{"amount_cents":10000,"bonus_cents":2000}]}`})
	if err != nil {
		t.Fatal(err)
	}
	response := rechargeTestRequest(routes, token, http.MethodPost, "/api/payment/ai-balance", `{"amount_yuan":"150.00","bonus_amount_cents":999999}`)
	if response.Code != http.StatusOK {
		t.Fatalf("create=%d %s", response.Code, response.Body.String())
	}
	var created struct {
		Order PaymentOrder `json:"order"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Order.AmountCents != 15000 || created.Order.BonusAmountCents != 2000 || provider.orderInput.AmountCents != 15000 {
		t.Fatalf("本金与赠送快照或渠道收款错误: order=%+v provider=%+v", created.Order, provider.orderInput)
	}
	if err = server.systemConfigs.Save(SystemConfig{ConfigKey: "system.app_config", Enabled: true, ConfigValue: `{"ai_recharge_promotions":[{"amount_cents":10000,"bonus_cents":9999}]}`}); err != nil {
		t.Fatal(err)
	}
	result := PaymentProviderTransactionResult{OrderNo: created.Order.OrderNo, TradeNo: "wx-snapshot", AmountCents: 15000, Paid: true}
	for range 2 {
		if err = server.payments.completeProviderTransaction(provider.Name(), result); err != nil {
			t.Fatal(err)
		}
	}
	balance, bonus, err := server.payments.aiWallet.BalanceSnapshot(created.Order.UserEmail)
	if err != nil || balance != centsToAIUnits(17000) || bonus != centsToAIUnits(2000) {
		t.Fatalf("snapshot balance=%d bonus=%d err=%v", balance, bonus, err)
	}
	result.AmountCents = 17000
	if err = server.payments.completeProviderTransaction(provider.Name(), result); err == nil {
		t.Fatal("不得把含赠送金额当作实际支付金额")
	}
}

// TestAIRechargeRefundHTTPExamples 验证实际退款本金与撤回赠送分开，并禁止仅有赠送时退款。
func TestAIRechargeRefundHTTPExamples(t *testing.T) {
	cases := []struct {
		name                  string
		consumed, refundCents int
		remaining             int64
		allowed               bool
	}{
		{"余30退10并撤20", 9000, 1000, 0, true},
		{"只余10赠送不退款", 11000, 1, centsToAIUnits(1000), false},
		{"未使用全退并撤20", 0, 10000, 0, true},
		{"部分退也撤全赠送", 0, 100, centsToAIUnits(9900), true},
		{"几乎全退不留赠送", 0, 9999, centsToAIUnits(1), true},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			server, routes, token, provider := newRechargeHTTPTest(t)
			order, err := server.payments.orders.Create(PaymentOrder{OrderNo: "http-refund", OrderType: "ai_balance", UserEmail: "recharge-http@example.com", AmountCents: 10000, BonusAmountCents: 2000, PaymentProvider: provider.Name(), Status: "pending"})
			if err != nil {
				t.Fatal(err)
			}
			if err = server.payments.completeProviderTransaction(provider.Name(), PaymentProviderTransactionResult{OrderNo: order.OrderNo, TradeNo: "wx-http-refund", AmountCents: 10000, Paid: true}); err != nil {
				t.Fatal(err)
			}
			if item.consumed > 0 {
				if _, err = server.payments.aiWallet.AdjustBalance(AIWalletRecord{UserEmail: order.UserEmail, Category: "ai_usage", ChangeUnits: -centsToAIUnits(item.consumed)}); err != nil {
					t.Fatal(err)
				}
			}
			body, _ := json.Marshal(map[string]any{"refund_type": "ai_balance", "amount_cents": item.refundCents})
			response := rechargeTestRequest(routes, token, http.MethodPost, "/api/payment/refund", string(body))
			if item.allowed {
				if response.Code != http.StatusOK {
					t.Fatalf("refund=%d %s", response.Code, response.Body.String())
				}
				if len(provider.refundInputs) != 1 || provider.refundInputs[0].AmountCents != item.refundCents || provider.refundInputs[0].TotalCents != 10000 {
					t.Fatalf("微信退款参数=%+v", provider.refundInputs)
				}
				var result struct {
					Refund PaymentRefund `json:"refund"`
				}
				if err = json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.Refund.RevokedBonusUnits != centsToAIUnits(2000) || result.Refund.Status != "success" {
					t.Fatalf("refund=%+v", result.Refund)
				}
			} else if response.Code != http.StatusConflict || len(provider.refundInputs) != 0 {
				t.Fatalf("只有赠送不能调用微信退款: %d %+v", response.Code, provider.refundInputs)
			}
			balance, err := server.payments.aiWallet.BalanceUnits(order.UserEmail)
			if err != nil || balance != item.remaining {
				t.Fatalf("balance=%d want=%d err=%v", balance, item.remaining, err)
			}
		})
	}
}

// TestAIRechargeConfigAndAmounts 验证管理端配置校验和金额精度，兼容原管理员负数调整。
func TestAIRechargeConfigAndAmounts(t *testing.T) {
	server, routes, token, provider := newRechargeHTTPTest(t)
	invalid := []string{
		`{"ai_recharge_promotions":null}`,
		`{"ai_recharge_promotions":[{"amount_cents":10000,"bonus_cents":0}]}`,
		`{"ai_recharge_promotions":[{"amount_cents":10000,"bonus_cents":1},{"amount_cents":10000,"bonus_cents":2}]}`,
		`{"ai_recharge_promotions":[{"amount_cents":100.1,"bonus_cents":100}]}`,
	}
	for _, raw := range invalid {
		body, _ := json.Marshal(map[string]string{"config_value": raw})
		response := rechargeTestRequest(routes, token, http.MethodPut, "/api/admin/system/configs/system.app_config", string(body))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid config=%s status=%d body=%s", raw, response.Code, response.Body.String())
		}
	}
	for _, amount := range []string{"NaN", "Inf", "1e2", "10.001", "-10", "0"} {
		body, _ := json.Marshal(map[string]string{"amount_yuan": amount})
		response := rechargeTestRequest(routes, token, http.MethodPost, "/api/payment/ai-balance", string(body))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid amount %q status=%d", amount, response.Code)
		}
	}
	if provider.orderInput.OrderNo != "" {
		t.Fatal("错误金额不应调用第三方下单")
	}
	orders, err := server.payments.orders.ListByUser("recharge-http@example.com")
	if err != nil || len(orders) != 0 {
		t.Fatalf("invalid orders=%+v err=%v", orders, err)
	}
	for value, want := range map[string]int{"12.50": 1250, "-12.50": -1250, "+10": 1000, "0.01": 1} {
		got, err := yuanTextToCents(value)
		if err != nil || got != want {
			t.Fatalf("parse(%s)=%d want=%d err=%v", value, got, want, err)
		}
	}
}
