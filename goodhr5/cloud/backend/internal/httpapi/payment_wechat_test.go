// 本文件负责测试微信支付回调验签、解密和交易归属校验。
package httpapi

import (
	"bytes"
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestWechatPayParseNotify 验证微信支付回调必须通过签名校验和 AES-GCM 解密。
func TestWechatPayParseNotify(t *testing.T) {
	merchantKey := generatePaymentRSAKey(t)
	wechatKey := generatePaymentRSAKey(t)
	apiV3Key := "0123456789abcdef0123456789abcdef"
	config := Config{
		WechatPayAppID:            "wx-test-app",
		WechatPayMerchantID:       "1900000001",
		WechatPayMerchantSerialNo: "merchant-serial",
		WechatPayPrivateKeyBase64: base64.StdEncoding.EncodeToString(paymentPrivateKeyPEM(t, merchantKey)),
		WechatPayAPIV3Key:         apiV3Key,
		WechatPayPublicKeyID:      "PUB_KEY_ID_3000000001",
		WechatPayPublicKeyBase64:  base64.StdEncoding.EncodeToString(paymentPublicKeyPEM(t, &wechatKey.PublicKey)),
		WechatPayNotifyURL:        "https://goodhr.test/api/payment/notify/wechat",
	}
	provider := NewWechatPayProvider(config)
	body := paymentNotifyBody(t, apiV3Key, map[string]any{
		"appid":          config.WechatPayAppID,
		"mchid":          config.WechatPayMerchantID,
		"out_trade_no":   "S123",
		"transaction_id": "wx-trade-123",
		"trade_state":    "SUCCESS",
		"trade_type":     "NATIVE",
		"amount":         map[string]any{"total": 4000, "currency": "CNY"},
	})
	request := httptest.NewRequest(http.MethodPost, "/api/payment/notify/wechat", bytes.NewReader(body))
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	nonce := "notify-header-nonce"
	request.Header.Set("Wechatpay-Serial", config.WechatPayPublicKeyID)
	request.Header.Set("Wechatpay-Timestamp", timestamp)
	request.Header.Set("Wechatpay-Nonce", nonce)
	request.Header.Set("Wechatpay-Signature", paymentNotifySignature(t, wechatKey, timestamp, nonce, body))

	result, err := provider.ParseNotify(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Paid || result.OrderNo != "S123" || result.TradeNo != "wx-trade-123" || result.AmountCents != 4000 {
		t.Fatalf("微信支付回调解析结果不正确: %+v", result)
	}
}

// generatePaymentRSAKey 生成支付单元测试使用的 RSA 密钥。
func generatePaymentRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// paymentPrivateKeyPEM 将测试商户私钥编码为 PEM。
func paymentPrivateKeyPEM(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	data, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: data})
}

// paymentPublicKeyPEM 将测试微信支付公钥编码为 PEM。
func paymentPublicKeyPEM(t *testing.T, key *rsa.PublicKey) []byte {
	t.Helper()
	data, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: data})
}

// paymentNotifyBody 创建经过 APIv3 密钥加密的微信支付通知报文。
func paymentNotifyBody(t *testing.T, apiV3Key string, transaction map[string]any) []byte {
	t.Helper()
	plaintext, err := json.Marshal(transaction)
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher([]byte(apiV3Key))
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := []byte("0123456789ab")
	associatedData := "transaction"
	ciphertext := gcm.Seal(nil, nonce, plaintext, []byte(associatedData))
	body, err := json.Marshal(map[string]any{
		"id":            "notify-id",
		"create_time":   time.Now().Format(time.RFC3339),
		"event_type":    "TRANSACTION.SUCCESS",
		"resource_type": "encrypt-resource",
		"summary":       "支付成功",
		"resource": map[string]string{
			"algorithm":       "AEAD_AES_256_GCM",
			"ciphertext":      base64.StdEncoding.EncodeToString(ciphertext),
			"nonce":           string(nonce),
			"associated_data": associatedData,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// paymentNotifySignature 按微信支付规则生成测试回调签名。
func paymentNotifySignature(t *testing.T, key *rsa.PrivateKey, timestamp string, nonce string, body []byte) string {
	t.Helper()
	digest := sha256.Sum256([]byte(timestamp + "\n" + nonce + "\n" + string(body) + "\n"))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(signature)
}
