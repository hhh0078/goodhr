// 本文件负责测试会员订阅支付订单和支付回调。
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestPaymentOrderAndNotify 验证创建微信支付订单后，支付结果会标记订单已支付且重复处理不重复到账。
func TestPaymentOrderAndNotify(t *testing.T) {
	server := mustNewServer(t)
	provider := &fakeWechatPaymentProvider{}
	server.payments.providers = map[string]PaymentProvider{provider.Name(): provider}
	routes := server.Routes()
	email := "payment@example.com"
	token := loginForTest(t, routes, email)
	if _, err := server.payments.subscriptions.AdjustSubscriptionDays(email, memberTypeMax, -10); err != nil {
		t.Fatal(err)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/payment/orders", bytes.NewBufferString(`{"plan_id":"monthly"}`))
	createReq.Header.Set("Authorization", "Bearer "+token)
	createResp := httptest.NewRecorder()
	routes.ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusOK {
		t.Fatalf("create payment status = %d, body = %s", createResp.Code, createResp.Body.String())
	}

	var createPayload struct {
		Order struct {
			OrderNo string `json:"order_no"`
			Amount  string `json:"amount"`
		} `json:"order"`
		Payment struct {
			CodeURL string `json:"code_url"`
		} `json:"payment"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&createPayload); err != nil {
		t.Fatal(err)
	}
	if createPayload.Order.OrderNo == "" || createPayload.Order.Amount != "40.00" || createPayload.Payment.CodeURL == "" {
		t.Fatalf("unexpected order payload: %+v", createPayload.Order)
	}

	result := PaymentProviderTransactionResult{
		OrderNo:     createPayload.Order.OrderNo,
		TradeNo:     "trade-test",
		AmountCents: 4000,
		Paid:        true,
		Raw:         map[string]string{"trade_state": "SUCCESS"},
	}
	if err := server.payments.completeProviderTransaction(provider.Name(), result); err != nil {
		t.Fatal(err)
	}
	firstSubscription, err := server.payments.subscriptions.UserSubscription(email)
	if err != nil {
		t.Fatal(err)
	}

	if err := server.payments.completeProviderTransaction(provider.Name(), result); err != nil {
		t.Fatal(err)
	}
	retriedSubscription, err := server.payments.subscriptions.UserSubscription(email)
	if err != nil {
		t.Fatal(err)
	}
	if !retriedSubscription.ExpiresAt.Equal(firstSubscription.ExpiresAt) {
		t.Fatalf("重复支付回调不应再次增加会员时间: first=%s retry=%s", firstSubscription.ExpiresAt, retriedSubscription.ExpiresAt)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/payment/orders", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listResp := httptest.NewRecorder()
	routes.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list payment status = %d, body = %s", listResp.Code, listResp.Body.String())
	}

	var listPayload struct {
		Orders []struct {
			Status  string `json:"status"`
			TradeNo string `json:"trade_no"`
		} `json:"orders"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listPayload); err != nil {
		t.Fatal(err)
	}
	if len(listPayload.Orders) != 1 || listPayload.Orders[0].Status != "paid" || listPayload.Orders[0].TradeNo != "trade-test" {
		t.Fatalf("unexpected paid order payload: %+v", listPayload.Orders)
	}
}

// fakeWechatPaymentProvider 为支付业务测试提供不访问微信网络的实现。
type fakeWechatPaymentProvider struct{}

// Name 返回测试使用的微信支付标识。
func (p *fakeWechatPaymentProvider) Name() string { return wechatPayProviderName }

// CreateOrder 返回固定格式的微信二维码内容。
func (p *fakeWechatPaymentProvider) CreateOrder(_ context.Context, input PaymentProviderOrderInput) (PaymentProviderOrderResult, error) {
	return PaymentProviderOrderResult{Provider: p.Name(), OrderNo: input.OrderNo, CodeURL: "weixin://wxpay/test"}, nil
}

// QueryOrder 返回未支付，避免列表测试触发真实到账。
func (p *fakeWechatPaymentProvider) QueryOrder(_ context.Context, orderNo string) (PaymentProviderTransactionResult, error) {
	return PaymentProviderTransactionResult{OrderNo: orderNo}, nil
}

// ParseNotify 返回测试中预设的支付结果，本测试不通过 HTTP 回调调用。
func (p *fakeWechatPaymentProvider) ParseNotify(_ context.Context, _ *http.Request) (PaymentProviderTransactionResult, error) {
	return PaymentProviderTransactionResult{}, errors.New("not implemented in fake provider")
}

// TestApplyPaidSubscriptionOrderIgnoresMailFailure 验证邮件失败不会阻止会员到账或导致重试重复加时长。
func TestApplyPaidSubscriptionOrderIgnoresMailFailure(t *testing.T) {
	server := mustNewServer(t)
	mailer := &recordingMailer{subscriptionRewardErr: errors.New("smtp unavailable")}
	server.payments.mailer = mailer
	order := PaymentOrder{
		OrderNo:      "subscription-mail-failure",
		UserEmail:    "subscription-mail-failure@example.com",
		MemberType:   memberTypeMax,
		DurationDays: 365,
	}
	if err := server.payments.applyPaidSubscriptionOrder(order); err != nil {
		t.Fatalf("邮件失败不应阻止会员到账: %v", err)
	}
	first, err := server.payments.subscriptions.UserSubscription(order.UserEmail)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.payments.applyPaidSubscriptionOrder(order); err != nil {
		t.Fatalf("同一订单重试应成功: %v", err)
	}
	retried, err := server.payments.subscriptions.UserSubscription(order.UserEmail)
	if err != nil {
		t.Fatal(err)
	}
	if !retried.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatalf("同一订单重试不应重复增加会员时间: first=%s retry=%s", first.ExpiresAt, retried.ExpiresAt)
	}
}

// TestMemoryPaymentStoreDoesNotReopenClosedOrder 验证已关闭订单收到迟到回调时不会重新变成已支付。
func TestMemoryPaymentStoreDoesNotReopenClosedOrder(t *testing.T) {
	store := NewMemoryPaymentStore()
	order, err := store.Create(PaymentOrder{
		OrderNo: "closed-order",
		Status:  "closed",
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, changed, err := store.MarkPaid(order.OrderNo, "late-trade", "{}")
	if err != nil {
		t.Fatal(err)
	}
	if changed || updated.Status != "closed" {
		t.Fatalf("已关闭订单不应重新标记为已支付: changed=%v status=%s", changed, updated.Status)
	}
}

// TestBuildSubscriptionPaymentQuoteProratesPlusUpgrade 验证 Plus 剩余时间会按包月价格抵扣 Max 实付金额。
func TestBuildSubscriptionPaymentQuoteProratesPlusUpgrade(t *testing.T) {
	plans, err := loadSubscriptionPlans(NewMemorySystemConfigStore())
	if err != nil {
		t.Fatal(err)
	}
	target, ok := subscriptionPlanByMemberType(plans, memberTypeMax)
	if !ok {
		t.Fatal("Max 套餐不存在")
	}
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	quote, err := buildSubscriptionPaymentQuote(plans, Subscription{
		MemberType: memberTypePlus,
		ExpiresAt:  now.Add(15 * 24 * time.Hour),
	}, target, now)
	if err != nil {
		t.Fatal(err)
	}
	if quote.UpgradeFromType != memberTypePlus || quote.UpgradeCreditCents != 2000 || quote.AmountCents != 32000 {
		t.Fatalf("unexpected upgrade quote: %+v", quote)
	}
}

// TestApplyPlusUpgradeReplacesExpiryFromNow 验证 Plus 升 Max 后从付款时间重新计算 365 天。
func TestApplyPlusUpgradeReplacesExpiryFromNow(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	store := NewMemorySubscriptionStore()
	store.now = func() time.Time { return now }
	email := "upgrade-from-now@example.com"
	store.items[email] = Subscription{
		MemberType: memberTypePlus,
		ExpiresAt:  now.Add(15 * 24 * time.Hour),
	}
	service := &PaymentService{
		subscriptions: store,
		systemConfigs: NewMemorySystemConfigStore(),
	}
	if err := service.applyPaidSubscriptionOrder(PaymentOrder{
		OrderNo:         "plus-upgrade",
		UserEmail:       email,
		MemberType:      memberTypeMax,
		DurationDays:    365,
		UpgradeFromType: memberTypePlus,
	}); err != nil {
		t.Fatal(err)
	}
	subscription, err := store.UserSubscription(email)
	if err != nil {
		t.Fatal(err)
	}
	want := now.Add(365 * 24 * time.Hour)
	if !subscription.ExpiresAt.Equal(want) || subscription.MemberType != memberTypeMax {
		t.Fatalf("Plus 升 Max 到期时间不正确: got=%+v want=%s", subscription, want)
	}
}

// TestBuildSubscriptionPaymentQuoteAllowsMaxToPlus 验证有效 Max 可以原价切换 Plus。
func TestBuildSubscriptionPaymentQuoteAllowsMaxToPlus(t *testing.T) {
	plans, err := loadSubscriptionPlans(NewMemorySystemConfigStore())
	if err != nil {
		t.Fatal(err)
	}
	target, ok := subscriptionPlanByMemberType(plans, memberTypePlus)
	if !ok {
		t.Fatal("Plus 套餐不存在")
	}
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	quote, err := buildSubscriptionPaymentQuote(plans, Subscription{
		MemberType: memberTypeMax,
		ExpiresAt:  now.Add(24 * time.Hour),
	}, target, now)
	if err != nil {
		t.Fatal(err)
	}
	if quote.UpgradeFromType != memberTypeMax || quote.UpgradeCreditCents != 0 || quote.AmountCents != 4000 {
		t.Fatalf("Max 切换 Plus 报价不正确: %+v", quote)
	}
}

// TestApplyMaxToPlusReplacesExpiryFromNow 验证 Max 切换 Plus 后从付款时间重新计算 30 天。
func TestApplyMaxToPlusReplacesExpiryFromNow(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	store := NewMemorySubscriptionStore()
	store.now = func() time.Time { return now }
	email := "max-to-plus@example.com"
	store.items[email] = Subscription{MemberType: memberTypeMax, ExpiresAt: now.Add(72 * time.Hour)}
	service := &PaymentService{subscriptions: store, systemConfigs: NewMemorySystemConfigStore()}
	if err := service.applyPaidSubscriptionOrder(PaymentOrder{
		OrderNo: "max-to-plus", UserEmail: email, MemberType: memberTypePlus,
		DurationDays: 30, UpgradeFromType: memberTypeMax,
	}); err != nil {
		t.Fatal(err)
	}
	subscription, err := store.UserSubscription(email)
	if err != nil {
		t.Fatal(err)
	}
	want := now.Add(30 * 24 * time.Hour)
	if subscription.MemberType != memberTypePlus || !subscription.ExpiresAt.Equal(want) {
		t.Fatalf("Max 切换 Plus 到期时间不正确: got=%+v want=%s", subscription, want)
	}
}
