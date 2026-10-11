// 本文件验证微信退款通知的真实 RSA 验签、AES-GCM 解密和商户金额校验。
package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestWechatPayParseRefundNotify 使用本地测试密钥，验证退款专用通知字段且拒绝伪造通知。
func TestWechatPayParseRefundNotify(t *testing.T) {
	merchantKey := generatePaymentRSAKey(t)
	wechatKey := generatePaymentRSAKey(t)
	apiV3Key := "0123456789abcdef0123456789abcdef"
	settings := wechatPaySettings{
		AppID: "wx-test-refund-app", MerchantID: "1900000001", MerchantSerialNo: "merchant-serial",
		PrivateKeyBase64: base64.StdEncoding.EncodeToString(paymentPrivateKeyPEM(t, merchantKey)),
		APIV3Key:         apiV3Key, PublicKeyID: "PUB_KEY_ID_3000000001",
		PublicKeyBase64: base64.StdEncoding.EncodeToString(paymentPublicKeyPEM(t, &wechatKey.PublicKey)),
		NotifyURL:       "https://goodhr.test/api/payment/notify/wechat",
	}
	provider := NewWechatPayProvider(newTestWechatPayConfigStore(t, settings))
	for _, tc := range []struct {
		name, merchant string
		amount         int
		badSignature   bool
		valid          bool
	}{
		{name: "真实签名退款通知", merchant: settings.MerchantID, amount: 1000, valid: true},
		{name: "其他商户通知", merchant: "other-merchant", amount: 1000},
		{name: "伪造签名通知", merchant: settings.MerchantID, amount: 1000, badSignature: true},
		{name: "缺少实际退款金额", merchant: settings.MerchantID},
		{name: "退款超过原实付", merchant: settings.MerchantID, amount: 10001},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := paymentNotifyBody(t, apiV3Key, map[string]any{
				"mchid": tc.merchant, "out_trade_no": "A-refund-test", "out_refund_no": "R-refund-test",
				"refund_id": "wx-refund-test", "refund_status": "SUCCESS",
				"amount": map[string]any{"total": 10000, "refund": tc.amount, "payer_total": 10000, "payer_refund": tc.amount},
			})
			var envelope map[string]any
			if err := json.Unmarshal(body, &envelope); err != nil {
				t.Fatal(err)
			}
			envelope["event_type"], envelope["summary"] = "REFUND.SUCCESS", "退款成功"
			body, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/api/payment/refund-notify/wechat", bytes.NewReader(body))
			timestamp, nonce := fmt.Sprint(time.Now().Unix()), "refund-notify-nonce"
			request.Header.Set("Wechatpay-Serial", settings.PublicKeyID)
			request.Header.Set("Wechatpay-Timestamp", timestamp)
			request.Header.Set("Wechatpay-Nonce", nonce)
			signature := paymentNotifySignature(t, wechatKey, timestamp, nonce, body)
			if tc.badSignature {
				signature = base64.StdEncoding.EncodeToString([]byte("forged-signature"))
			}
			request.Header.Set("Wechatpay-Signature", signature)
			result, err := provider.ParseRefundNotify(context.Background(), request)
			if !tc.valid {
				if err == nil {
					t.Fatalf("不应接受不可信退款通知: %+v", result)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != "SUCCESS" || result.OrderNo != "A-refund-test" || result.RefundNo != "R-refund-test" || result.ProviderRefundID != "wx-refund-test" || result.AmountCents != 1000 || result.TotalCents != 10000 {
				t.Fatalf("退款通知字段不正确: %+v", result)
			}
		})
	}
}
