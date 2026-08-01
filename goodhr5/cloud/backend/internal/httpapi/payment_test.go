// 本文件负责测试会员订阅支付订单和支付回调。
package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestPaymentOrderAndNotify 验证创建支付订单后，好收米回调会标记订单已支付。
func TestPaymentOrderAndNotify(t *testing.T) {
	t.Setenv("GOODHR_HAOSHOUMI_MERCHANT_ID", "pid-test")
	t.Setenv("GOODHR_HAOSHOUMI_MERCHANT_KEY", "key-test")
	t.Setenv("GOODHR_HAOSHOUMI_NOTIFY_URL", "https://goodhr.test/api/payment/notify/haoshoumi")
	t.Setenv("GOODHR_HAOSHOUMI_RETURN_URL", "https://goodhr.test/subscription")

	server := mustNewServer(t)
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
	}
	if err := json.NewDecoder(createResp.Body).Decode(&createPayload); err != nil {
		t.Fatal(err)
	}
	if createPayload.Order.OrderNo == "" || createPayload.Order.Amount != "40.00" {
		t.Fatalf("unexpected order payload: %+v", createPayload.Order)
	}

	values := map[string]string{
		"pid":          "pid-test",
		"out_trade_no": createPayload.Order.OrderNo,
		"trade_no":     "trade-test",
		"trade_status": "TRADE_SUCCESS",
		"money":        "40.00",
		"param":        "test",
	}
	values["sign"] = NewHaoshoumiProvider(LoadConfigFromEnv()).sign(values)
	values["sign_type"] = "MD5"

	form := url.Values{}
	for key, value := range values {
		form.Set(key, value)
	}
	notifyReq := httptest.NewRequest(http.MethodPost, "/api/payment/notify/haoshoumi", strings.NewReader(form.Encode()))
	notifyReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	notifyResp := httptest.NewRecorder()
	routes.ServeHTTP(notifyResp, notifyReq)
	if notifyResp.Code != http.StatusOK {
		t.Fatalf("notify status = %d, body = %s", notifyResp.Code, notifyResp.Body.String())
	}
	firstSubscription, err := server.payments.subscriptions.UserSubscription(email)
	if err != nil {
		t.Fatal(err)
	}

	retryReq := httptest.NewRequest(http.MethodPost, "/api/payment/notify/haoshoumi", strings.NewReader(form.Encode()))
	retryReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	retryResp := httptest.NewRecorder()
	routes.ServeHTTP(retryResp, retryReq)
	if retryResp.Code != http.StatusOK {
		t.Fatalf("retry notify status = %d, body = %s", retryResp.Code, retryResp.Body.String())
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
