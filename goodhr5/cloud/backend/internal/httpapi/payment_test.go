// 本文件负责测试会员订阅支付订单和支付回调。
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestPaymentOrderAndNotify 验证创建支付订单后，好收米回调会标记订单已支付。
func TestPaymentOrderAndNotify(t *testing.T) {
	t.Setenv("GOODHR_HAOSHOUMI_MERCHANT_ID", "pid-test")
	t.Setenv("GOODHR_HAOSHOUMI_MERCHANT_KEY", "key-test")
	t.Setenv("GOODHR_HAOSHOUMI_NOTIFY_URL", "https://goodhr.test/api/payment/notify/haoshoumi")
	t.Setenv("GOODHR_HAOSHOUMI_RETURN_URL", "https://goodhr.test/subscription")

	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "payment@example.com")

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
	if createPayload.Order.OrderNo == "" || createPayload.Order.Amount != "70.00" {
		t.Fatalf("unexpected order payload: %+v", createPayload.Order)
	}

	values := map[string]string{
		"pid":          "pid-test",
		"out_trade_no": createPayload.Order.OrderNo,
		"trade_no":     "trade-test",
		"trade_status": "TRADE_SUCCESS",
		"money":        "70.00",
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
