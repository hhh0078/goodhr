// 本文件负责微信支付 V3 Native 下单、主动查单以及支付回调验签解密。
package httpapi

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/core/auth/verifiers"
	"github.com/wechatpay-apiv3/wechatpay-go/core/notify"
	"github.com/wechatpay-apiv3/wechatpay-go/core/option"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"
	"github.com/wechatpay-apiv3/wechatpay-go/utils"
)

const wechatPayProviderName = "wechat"

// WechatPayProvider 实现微信支付 V3 Native 支付。
type WechatPayProvider struct {
	config        Config
	initOnce      sync.Once
	initErr       error
	service       *native.NativeApiService
	notifyHandler *notify.Handler
}

// NewWechatPayProvider 创建微信 Native 支付实现，真实配置在首次支付请求时校验。
func NewWechatPayProvider(config Config) *WechatPayProvider {
	return &WechatPayProvider{config: config}
}

// Name 返回微信支付平台标识。
func (p *WechatPayProvider) Name() string {
	return wechatPayProviderName
}

// CreateOrder 调用微信 Native 下单接口并返回二维码内容链接。
func (p *WechatPayProvider) CreateOrder(ctx context.Context, input PaymentProviderOrderInput) (PaymentProviderOrderResult, error) {
	if err := p.initialize(ctx); err != nil {
		return PaymentProviderOrderResult{}, err
	}
	if input.AmountCents <= 0 {
		return PaymentProviderOrderResult{}, fmt.Errorf("微信支付金额必须大于 0")
	}
	response, _, err := p.service.Prepay(ctx, native.PrepayRequest{
		Appid:       core.String(strings.TrimSpace(p.config.WechatPayAppID)),
		Mchid:       core.String(strings.TrimSpace(p.config.WechatPayMerchantID)),
		Description: core.String(paymentTitle(input.Title)),
		OutTradeNo:  core.String(strings.TrimSpace(input.OrderNo)),
		TimeExpire:  core.Time(time.Now().Add(30 * time.Minute)),
		NotifyUrl:   core.String(strings.TrimSpace(p.config.WechatPayNotifyURL)),
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
	if err := p.initialize(ctx); err != nil {
		return PaymentProviderTransactionResult{}, err
	}
	transaction, _, err := p.service.QueryOrderByOutTradeNo(ctx, native.QueryOrderByOutTradeNoRequest{
		OutTradeNo: core.String(strings.TrimSpace(orderNo)),
		Mchid:      core.String(strings.TrimSpace(p.config.WechatPayMerchantID)),
	})
	if err != nil {
		return PaymentProviderTransactionResult{}, fmt.Errorf("微信支付查单失败: %w", err)
	}
	return p.transactionResult(transaction)
}

// ParseNotify 对微信回调验签、解密并转换为统一支付结果。
func (p *WechatPayProvider) ParseNotify(ctx context.Context, request *http.Request) (PaymentProviderTransactionResult, error) {
	if err := p.initialize(ctx); err != nil {
		return PaymentProviderTransactionResult{}, err
	}
	transaction := new(payments.Transaction)
	if _, err := p.notifyHandler.ParseNotifyRequest(ctx, request, transaction); err != nil {
		return PaymentProviderTransactionResult{}, fmt.Errorf("微信支付回调校验失败: %w", err)
	}
	result, err := p.transactionResult(transaction)
	if err != nil {
		return PaymentProviderTransactionResult{}, err
	}
	if !result.Paid {
		return PaymentProviderTransactionResult{}, fmt.Errorf("微信支付回调状态不是支付成功")
	}
	return result, nil
}

// initialize 延迟加载微信支付密钥并初始化官方 SDK。
func (p *WechatPayProvider) initialize(ctx context.Context) error {
	p.initOnce.Do(func() {
		required := map[string]string{
			"GOODHR_WECHAT_PAY_APP_ID":             p.config.WechatPayAppID,
			"GOODHR_WECHAT_PAY_MCH_ID":             p.config.WechatPayMerchantID,
			"GOODHR_WECHAT_PAY_MERCHANT_SERIAL_NO": p.config.WechatPayMerchantSerialNo,
			"GOODHR_WECHAT_PAY_PRIVATE_KEY_BASE64": p.config.WechatPayPrivateKeyBase64,
			"GOODHR_WECHAT_PAY_API_V3_KEY":         p.config.WechatPayAPIV3Key,
			"GOODHR_WECHAT_PAY_PUBLIC_KEY_ID":      p.config.WechatPayPublicKeyID,
			"GOODHR_WECHAT_PAY_PUBLIC_KEY_BASE64":  p.config.WechatPayPublicKeyBase64,
			"GOODHR_WECHAT_PAY_NOTIFY_URL":         p.config.WechatPayNotifyURL,
		}
		for name, value := range required {
			if strings.TrimSpace(value) == "" {
				p.initErr = fmt.Errorf("微信支付配置缺少 %s", name)
				return
			}
		}
		privateKeyText, err := decodePaymentPEM(p.config.WechatPayPrivateKeyBase64)
		if err != nil {
			p.initErr = fmt.Errorf("微信支付商户私钥读取失败: %w", err)
			return
		}
		privateKey, err := utils.LoadPrivateKey(privateKeyText)
		if err != nil {
			p.initErr = fmt.Errorf("微信支付商户私钥格式不正确: %w", err)
			return
		}
		publicKeyText, err := decodePaymentPEM(p.config.WechatPayPublicKeyBase64)
		if err != nil {
			p.initErr = fmt.Errorf("微信支付公钥读取失败: %w", err)
			return
		}
		publicKey, err := utils.LoadPublicKey(publicKeyText)
		if err != nil {
			p.initErr = fmt.Errorf("微信支付公钥格式不正确: %w", err)
			return
		}
		client, err := core.NewClient(ctx, option.WithWechatPayPublicKeyAuthCipher(
			strings.TrimSpace(p.config.WechatPayMerchantID),
			strings.TrimSpace(p.config.WechatPayMerchantSerialNo),
			privateKey,
			strings.TrimSpace(p.config.WechatPayPublicKeyID),
			publicKey,
		))
		if err != nil {
			p.initErr = fmt.Errorf("微信支付客户端初始化失败: %w", err)
			return
		}
		handler, err := notify.NewRSANotifyHandler(
			strings.TrimSpace(p.config.WechatPayAPIV3Key),
			verifiers.NewSHA256WithRSAPubkeyVerifier(strings.TrimSpace(p.config.WechatPayPublicKeyID), *publicKey),
		)
		if err != nil {
			p.initErr = fmt.Errorf("微信支付回调处理器初始化失败: %w", err)
			return
		}
		p.service = &native.NativeApiService{Client: client}
		p.notifyHandler = handler
	})
	return p.initErr
}

// transactionResult 校验微信交易归属并转换支付状态。
func (p *WechatPayProvider) transactionResult(transaction *payments.Transaction) (PaymentProviderTransactionResult, error) {
	if transaction == nil || transaction.OutTradeNo == nil || strings.TrimSpace(*transaction.OutTradeNo) == "" || transaction.Amount == nil || transaction.Amount.Total == nil || *transaction.Amount.Total <= 0 {
		return PaymentProviderTransactionResult{}, fmt.Errorf("微信支付交易数据不完整")
	}
	if transaction.Appid == nil || strings.TrimSpace(*transaction.Appid) != strings.TrimSpace(p.config.WechatPayAppID) {
		return PaymentProviderTransactionResult{}, fmt.Errorf("微信支付 APPID 不匹配")
	}
	if transaction.Mchid == nil || strings.TrimSpace(*transaction.Mchid) != strings.TrimSpace(p.config.WechatPayMerchantID) {
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

// decodePaymentPEM 从 Base64 环境变量读取 PEM，也兼容直接填写 PEM 文本。
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
