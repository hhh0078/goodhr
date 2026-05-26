// 本文件负责定义支付平台抽象，并实现好收米支付平台。
package httpapi

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// PaymentProvider 定义不同支付平台必须实现的统一接口。
type PaymentProvider interface {
	// Name 返回支付平台标识。
	Name() string
	// CreateOrder 创建第三方支付订单，并返回前端需要提交的支付参数。
	CreateOrder(input PaymentProviderOrderInput) (PaymentProviderOrderResult, error)
	// VerifyNotify 校验第三方支付回调签名，并返回标准化回调结果。
	VerifyNotify(values map[string]string) (PaymentProviderNotifyResult, error)
}

// PaymentProviderOrderInput 表示创建第三方支付订单所需参数。
type PaymentProviderOrderInput struct {
	OrderNo     string
	Title       string
	AmountCents int
	Remark      string
}

// PaymentProviderOrderResult 表示第三方支付下单结果。
type PaymentProviderOrderResult struct {
	Provider     string            `json:"provider"`
	OrderNo      string            `json:"order_no"`
	SubmitURL    string            `json:"submit_url"`
	SubmitMethod string            `json:"submit_method"`
	SubmitFields map[string]string `json:"submit_fields"`
}

// PaymentProviderNotifyResult 表示第三方支付回调校验后的标准结果。
type PaymentProviderNotifyResult struct {
	OrderNo     string
	TradeNo     string
	AmountCents int
	Raw         map[string]string
}

// HaoshoumiProvider 实现好收米支付平台。
type HaoshoumiProvider struct {
	config Config
	client *http.Client
}

// NewHaoshoumiProvider 创建好收米支付平台实现。
func NewHaoshoumiProvider(config Config) *HaoshoumiProvider {
	return &HaoshoumiProvider{
		config: config,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// Name 返回好收米支付平台标识。
func (p *HaoshoumiProvider) Name() string {
	return "haoshoumi"
}

// CreateOrder 创建好收米支付订单。
func (p *HaoshoumiProvider) CreateOrder(input PaymentProviderOrderInput) (PaymentProviderOrderResult, error) {
	if strings.TrimSpace(p.config.HaoshoumiMerchantID) == "" {
		return PaymentProviderOrderResult{}, fmt.Errorf("missing haoshoumi merchant id")
	}
	if strings.TrimSpace(p.config.HaoshoumiMerchantKey) == "" {
		return PaymentProviderOrderResult{}, fmt.Errorf("missing haoshoumi merchant key")
	}
	if strings.TrimSpace(p.config.HaoshoumiNotifyURL) == "" {
		return PaymentProviderOrderResult{}, fmt.Errorf("missing haoshoumi notify url")
	}
	if strings.TrimSpace(p.config.HaoshoumiReturnURL) == "" {
		return PaymentProviderOrderResult{}, fmt.Errorf("missing haoshoumi return url")
	}

	submitURL := strings.TrimSpace(p.config.HaoshoumiAPIURL)
	if submitURL == "" || strings.Contains(submitURL, "/mapi.php") {
		submitURL = "https://api.kuaixiaopu.com/submit.php"
	}
	fields := map[string]string{
		"pid":          p.config.HaoshoumiMerchantID,
		"out_trade_no": input.OrderNo,
		"notify_url":   p.config.HaoshoumiNotifyURL,
		"return_url":   p.config.HaoshoumiReturnURL,
		"name":         input.Title,
		"money":        centsToYuanString(input.AmountCents),
		"param":        input.Remark,
		"sign_type":    "MD5",
	}
	if paymentType := strings.TrimSpace(p.config.HaoshoumiDefaultPaymentType); paymentType != "" {
		fields["type"] = paymentType
	}
	fields["sign"] = p.sign(fields)

	return PaymentProviderOrderResult{
		Provider:     p.Name(),
		OrderNo:      input.OrderNo,
		SubmitURL:    submitURL,
		SubmitMethod: "POST",
		SubmitFields: fields,
	}, nil
}

// VerifyNotify 校验好收米支付回调。
func (p *HaoshoumiProvider) VerifyNotify(values map[string]string) (PaymentProviderNotifyResult, error) {
	if strings.TrimSpace(values["sign"]) == "" {
		return PaymentProviderNotifyResult{}, fmt.Errorf("missing sign")
	}
	if strings.TrimSpace(values["trade_status"]) != "TRADE_SUCCESS" {
		return PaymentProviderNotifyResult{}, fmt.Errorf("trade not successful")
	}
	if p.sign(values) != strings.TrimSpace(values["sign"]) {
		return PaymentProviderNotifyResult{}, fmt.Errorf("invalid sign")
	}
	amountCents, err := yuanTextToCents(values["money"])
	if err != nil {
		return PaymentProviderNotifyResult{}, err
	}
	return PaymentProviderNotifyResult{
		OrderNo:     strings.TrimSpace(values["out_trade_no"]),
		TradeNo:     strings.TrimSpace(values["trade_no"]),
		AmountCents: amountCents,
		Raw:         values,
	}, nil
}

// sign 按好收米规则生成 MD5 签名。
func (p *HaoshoumiProvider) sign(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || key == "sign" || key == "sign_type" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values[key])
	}
	sum := md5.Sum([]byte(strings.Join(parts, "&") + p.config.HaoshoumiMerchantKey))
	return hex.EncodeToString(sum[:])
}

// centsToYuanString 将分转换为两位小数元字符串。
func centsToYuanString(cents int) string {
	return fmt.Sprintf("%.2f", float64(cents)/100)
}
