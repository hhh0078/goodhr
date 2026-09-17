// 本文件负责微信支付 V3 Native 下单、主动查单以及支付回调验签解密。
// 微信支付参数保存在系统配置表 system.payment_wechat 中，超管在后台修改后立即生效。
package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/core/auth/verifiers"
	"github.com/wechatpay-apiv3/wechatpay-go/core/notify"
	"github.com/wechatpay-apiv3/wechatpay-go/core/option"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"
	"github.com/wechatpay-apiv3/wechatpay-go/services/refunddomestic"
	"github.com/wechatpay-apiv3/wechatpay-go/utils"
)

const (
	// wechatPayProviderName 微信支付平台标识。
	wechatPayProviderName = "wechat"
	// wechatPayConfigKey 系统配置表中微信支付配置的键名。
	wechatPayConfigKey = "system.payment_wechat"
)

// wechatPaySettings 表示 system.payment_wechat 中的微信支付商户配置。
type wechatPaySettings struct {
	AppID            string `json:"app_id"`
	MerchantID       string `json:"mch_id"`
	MerchantSerialNo string `json:"merchant_serial_no"`
	PrivateKeyPath   string `json:"private_key_path"`
	PrivateKeyBase64 string `json:"private_key_base64"`
	APIV3Key         string `json:"api_v3_key"`
	PublicKeyID      string `json:"public_key_id"`
	PublicKeyPath    string `json:"public_key_path"`
	PublicKeyBase64  string `json:"public_key_base64"`
	NotifyURL        string `json:"notify_url"`
}

// wechatPayRuntime 保存按当前配置初始化好的微信支付 SDK 客户端和回调处理器。
type wechatPayRuntime struct {
	settings      wechatPaySettings
	service       *native.NativeApiService
	refundService *refunddomestic.RefundsApiService
	notifyHandler *notify.Handler
}

// Refund 调用微信支付退款接口并返回退款受理状态。
func (p *WechatPayProvider) Refund(ctx context.Context, input PaymentProviderRefundInput) (PaymentProviderRefundResult, error) {
	runtime, err := p.initialize(ctx)
	if err != nil {
		return PaymentProviderRefundResult{}, err
	}
	if input.AmountCents <= 0 || input.TotalCents <= 0 || input.AmountCents > input.TotalCents {
		return PaymentProviderRefundResult{}, fmt.Errorf("微信退款金额不正确")
	}
	response, _, err := runtime.refundService.Create(ctx, refunddomestic.CreateRequest{
		OutTradeNo:  core.String(strings.TrimSpace(input.OrderNo)),
		OutRefundNo: core.String(strings.TrimSpace(input.RefundNo)),
		Reason:      core.String(paymentTitle(input.Reason)),
		Amount: &refunddomestic.AmountReq{
			Currency: core.String("CNY"),
			Refund:   core.Int64(int64(input.AmountCents)),
			Total:    core.Int64(int64(input.TotalCents)),
		},
	})
	if err != nil {
		return PaymentProviderRefundResult{}, fmt.Errorf("微信退款申请失败: %w", err)
	}
	if response == nil || response.RefundId == nil || response.Status == nil {
		return PaymentProviderRefundResult{}, fmt.Errorf("微信退款没有返回完整结果")
	}
	return PaymentProviderRefundResult{
		Provider:         p.Name(),
		OrderNo:          input.OrderNo,
		RefundNo:         input.RefundNo,
		ProviderRefundID: strings.TrimSpace(*response.RefundId),
		Status:           string(*response.Status),
		Raw:              response,
	}, nil
}

// WechatPayProvider 实现微信支付 V3 Native 支付，配置每次请求前从系统配置表读取。
type WechatPayProvider struct {
	systemConfigs SystemConfigStore
	mu            sync.Mutex
	signature     string
	runtime       *wechatPayRuntime
}

// NewWechatPayProvider 创建微信 Native 支付实现，支付参数来自系统配置表。
func NewWechatPayProvider(systemConfigs SystemConfigStore) *WechatPayProvider {
	return &WechatPayProvider{systemConfigs: systemConfigs}
}

// Name 返回微信支付平台标识。
func (p *WechatPayProvider) Name() string {
	return wechatPayProviderName
}

// CreateOrder 调用微信 Native 下单接口并返回二维码内容链接。
func (p *WechatPayProvider) CreateOrder(ctx context.Context, input PaymentProviderOrderInput) (PaymentProviderOrderResult, error) {
	runtime, err := p.initialize(ctx)
	if err != nil {
		return PaymentProviderOrderResult{}, err
	}
	if input.AmountCents <= 0 {
		return PaymentProviderOrderResult{}, fmt.Errorf("微信支付金额必须大于 0")
	}
	response, _, err := runtime.service.Prepay(ctx, native.PrepayRequest{
		Appid:       core.String(strings.TrimSpace(runtime.settings.AppID)),
		Mchid:       core.String(strings.TrimSpace(runtime.settings.MerchantID)),
		Description: core.String(paymentTitle(input.Title)),
		OutTradeNo:  core.String(strings.TrimSpace(input.OrderNo)),
		TimeExpire:  core.Time(time.Now().Add(30 * time.Minute)),
		NotifyUrl:   core.String(strings.TrimSpace(runtime.settings.NotifyURL)),
		Amount: &native.Amount{
			Currency: core.String("CNY"),
			Total:    core.Int64(int64(input.AmountCents)),
		},
	})
	if err != nil {
		return PaymentProviderOrderResult{}, fmt.Errorf("微信支付下单失败: %w", err)
	}
	if response == nil || response.CodeUrl == nil || strings.TrimSpace(*response.CodeUrl) == "" {
		return PaymentProviderOrderResult{}, fmt.Errorf("微信支付没有返回二维码内容")
	}
	return PaymentProviderOrderResult{
		Provider: p.Name(),
		OrderNo:  input.OrderNo,
		CodeURL:  strings.TrimSpace(*response.CodeUrl),
	}, nil
}

// QueryOrder 按商户订单号主动查询微信支付状态。
func (p *WechatPayProvider) QueryOrder(ctx context.Context, orderNo string) (PaymentProviderTransactionResult, error) {
	runtime, err := p.initialize(ctx)
	if err != nil {
		return PaymentProviderTransactionResult{}, err
	}
	transaction, _, err := runtime.service.QueryOrderByOutTradeNo(ctx, native.QueryOrderByOutTradeNoRequest{
		OutTradeNo: core.String(strings.TrimSpace(orderNo)),
		Mchid:      core.String(strings.TrimSpace(runtime.settings.MerchantID)),
	})
	if err != nil {
		return PaymentProviderTransactionResult{}, fmt.Errorf("微信支付查单失败: %w", err)
	}
	return transactionResult(runtime.settings, transaction)
}

// ParseNotify 对微信回调验签、解密并转换为统一支付结果。
func (p *WechatPayProvider) ParseNotify(ctx context.Context, request *http.Request) (PaymentProviderTransactionResult, error) {
	runtime, err := p.initialize(ctx)
	if err != nil {
		return PaymentProviderTransactionResult{}, err
	}
	transaction := new(payments.Transaction)
	if _, err := runtime.notifyHandler.ParseNotifyRequest(ctx, request, transaction); err != nil {
		return PaymentProviderTransactionResult{}, fmt.Errorf("微信支付回调校验失败: %w", err)
	}
	result, err := transactionResult(runtime.settings, transaction)
	if err != nil {
		return PaymentProviderTransactionResult{}, err
	}
	if !result.Paid {
		return PaymentProviderTransactionResult{}, fmt.Errorf("微信支付回调状态不是支付成功")
	}
	return result, nil
}

// loadWechatPaySettings 从系统配置表读取微信支付配置并解析为结构体。
func (p *WechatPayProvider) loadWechatPaySettings() (wechatPaySettings, error) {
	if p.systemConfigs == nil {
		return wechatPaySettings{}, fmt.Errorf("微信支付配置存储没有准备好，请稍后再试")
	}
	cfg, err := p.systemConfigs.Get(wechatPayConfigKey)
	if err != nil {
		if errors.Is(err, ErrConfigNotFound) {
			return wechatPaySettings{}, fmt.Errorf("微信支付配置还没在后台填写，请先到系统配置里保存微信支付参数")
		}
		return wechatPaySettings{}, fmt.Errorf("微信支付配置读取失败: %w", err)
	}
	var settings wechatPaySettings
	if err := json.Unmarshal([]byte(cfg.ConfigValue), &settings); err != nil {
		return wechatPaySettings{}, fmt.Errorf("微信支付配置格式不正确: %w", err)
	}
	return settings, nil
}

// validateWechatPaySettings 校验微信支付配置必填字段，缺哪个用中文提示哪个。
func validateWechatPaySettings(settings wechatPaySettings) error {
	required := []struct {
		name  string
		value string
	}{
		{"应用 ID（app_id）", settings.AppID},
		{"商户号（mch_id）", settings.MerchantID},
		{"商户证书序列号（merchant_serial_no）", settings.MerchantSerialNo},
		{"APIv3 密钥（api_v3_key）", settings.APIV3Key},
		{"微信支付公钥 ID（public_key_id）", settings.PublicKeyID},
		{"回调地址（notify_url）", settings.NotifyURL},
	}
	for _, item := range required {
		if strings.TrimSpace(item.value) == "" {
			return fmt.Errorf("微信支付配置里 %s 还没填", item.name)
		}
	}
	if strings.TrimSpace(settings.PrivateKeyPath) == "" && strings.TrimSpace(settings.PrivateKeyBase64) == "" {
		return fmt.Errorf("微信支付配置里商户私钥文件路径（private_key_path）还没填")
	}
	if strings.TrimSpace(settings.PublicKeyPath) == "" && strings.TrimSpace(settings.PublicKeyBase64) == "" {
		return fmt.Errorf("微信支付配置里微信支付公钥文件路径（public_key_path）还没填")
	}
	return nil
}

// wechatPayConfigSignature 拼接配置和证书内容作为指纹，替换证书文件后会自动重建客户端。
func wechatPayConfigSignature(settings wechatPaySettings, privateKeyText string, publicKeyText string) string {
	return strings.Join([]string{
		settings.AppID,
		settings.MerchantID,
		settings.MerchantSerialNo,
		settings.PrivateKeyPath,
		settings.PrivateKeyBase64,
		settings.APIV3Key,
		settings.PublicKeyID,
		settings.PublicKeyPath,
		settings.PublicKeyBase64,
		settings.NotifyURL,
		privateKeyText,
		publicKeyText,
	}, "\n")
}

// initialize 读取系统配置表中的微信支付配置；配置变化时自动重建 SDK 客户端，修改后立即生效。
func (p *WechatPayProvider) initialize(ctx context.Context) (*wechatPayRuntime, error) {
	settings, err := p.loadWechatPaySettings()
	if err != nil {
		return nil, err
	}
	if err := validateWechatPaySettings(settings); err != nil {
		return nil, err
	}
	privateKeyText, err := loadPaymentPEM(settings.PrivateKeyPath, settings.PrivateKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("微信支付商户私钥读取失败: %w", err)
	}
	publicKeyText, err := loadPaymentPEM(settings.PublicKeyPath, settings.PublicKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("微信支付公钥读取失败: %w", err)
	}
	signature := wechatPayConfigSignature(settings, privateKeyText, publicKeyText)

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.runtime != nil && p.signature == signature {
		return p.runtime, nil
	}
	privateKey, err := utils.LoadPrivateKey(privateKeyText)
	if err != nil {
		return nil, fmt.Errorf("微信支付商户私钥格式不正确: %w", err)
	}
	publicKey, err := utils.LoadPublicKey(publicKeyText)
	if err != nil {
		return nil, fmt.Errorf("微信支付公钥格式不正确: %w", err)
	}
	client, err := core.NewClient(ctx, option.WithWechatPayPublicKeyAuthCipher(
		strings.TrimSpace(settings.MerchantID),
		strings.TrimSpace(settings.MerchantSerialNo),
		privateKey,
		strings.TrimSpace(settings.PublicKeyID),
		publicKey,
	))
	if err != nil {
		return nil, fmt.Errorf("微信支付客户端初始化失败: %w", err)
	}
	handler, err := notify.NewRSANotifyHandler(
		strings.TrimSpace(settings.APIV3Key),
		verifiers.NewSHA256WithRSAPubkeyVerifier(strings.TrimSpace(settings.PublicKeyID), *publicKey),
	)
	if err != nil {
		return nil, fmt.Errorf("微信支付回调处理器初始化失败: %w", err)
	}
	p.runtime = &wechatPayRuntime{
		settings:      settings,
		service:       &native.NativeApiService{Client: client},
		refundService: &refunddomestic.RefundsApiService{Client: client},
		notifyHandler: handler,
	}
	p.signature = signature
	return p.runtime, nil
}

// transactionResult 校验微信交易归属并转换支付状态。
func transactionResult(settings wechatPaySettings, transaction *payments.Transaction) (PaymentProviderTransactionResult, error) {
	if transaction == nil || transaction.OutTradeNo == nil || strings.TrimSpace(*transaction.OutTradeNo) == "" || transaction.Amount == nil || transaction.Amount.Total == nil || *transaction.Amount.Total <= 0 {
		return PaymentProviderTransactionResult{}, fmt.Errorf("微信支付交易数据不完整")
	}
	if transaction.Appid == nil || strings.TrimSpace(*transaction.Appid) != strings.TrimSpace(settings.AppID) {
		return PaymentProviderTransactionResult{}, fmt.Errorf("微信支付 APPID 不匹配")
	}
	if transaction.Mchid == nil || strings.TrimSpace(*transaction.Mchid) != strings.TrimSpace(settings.MerchantID) {
		return PaymentProviderTransactionResult{}, fmt.Errorf("微信支付商户号不匹配")
	}
	if transaction.Amount.Currency == nil || strings.TrimSpace(*transaction.Amount.Currency) != "CNY" {
		return PaymentProviderTransactionResult{}, fmt.Errorf("微信支付币种不匹配")
	}
	if transaction.TradeType == nil || strings.TrimSpace(*transaction.TradeType) != "NATIVE" {
		return PaymentProviderTransactionResult{}, fmt.Errorf("微信支付交易类型不匹配")
	}
	result := PaymentProviderTransactionResult{
		OrderNo:     strings.TrimSpace(*transaction.OutTradeNo),
		AmountCents: int(*transaction.Amount.Total),
		Raw:         transaction,
	}
	if transaction.TransactionId != nil {
		result.TradeNo = strings.TrimSpace(*transaction.TransactionId)
	}
	result.Paid = transaction.TradeState != nil && strings.TrimSpace(*transaction.TradeState) == "SUCCESS"
	if result.Paid && result.TradeNo == "" {
		return PaymentProviderTransactionResult{}, fmt.Errorf("微信支付流水号为空")
	}
	return result, nil
}

// loadPaymentPEM 优先读取部署机上的 PEM 文件，并兼容旧的 Base64 或 PEM 文本配置。
func loadPaymentPEM(path string, legacyValue string) (string, error) {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath != "" {
		data, err := os.ReadFile(trimmedPath)
		if err != nil {
			return "", fmt.Errorf("文件 %s 无法读取: %w", trimmedPath, err)
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			return "", fmt.Errorf("文件 %s 是空文件", trimmedPath)
		}
		return content, nil
	}
	return decodePaymentPEM(legacyValue)
}

// decodePaymentPEM 从 Base64 配置读取 PEM，也兼容直接填写 PEM 文本。
func decodePaymentPEM(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if strings.Contains(trimmed, "-----BEGIN") {
		return strings.ReplaceAll(trimmed, `\n`, "\n"), nil
	}
	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

// paymentTitle 限制微信支付商品描述长度并提供非空兜底。
func paymentTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "GoodHR 服务"
	}
	runes := []rune(title)
	if len(runes) > 42 {
		return string(runes[:42])
	}
	return title
}
