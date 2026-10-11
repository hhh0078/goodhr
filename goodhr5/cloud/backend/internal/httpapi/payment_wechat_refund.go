// 本文件负责微信退款查询、通知验签及明确拒绝的判断，网络异常保持原单待核实。
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/services/refunddomestic"
)

// definiteWechatRefundRejection 仅把文档明确未受理的错误码判作失败，超时和未知错误不释放余额。
func definiteWechatRefundRejection(err error) bool {
	var apiErr *core.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode < 400 || apiErr.StatusCode >= 500 {
		return false
	}
	switch apiErr.Code {
	case "USER_ACCOUNT_ABNORMAL", "NOT_ENOUGH", "PARAM_ERROR", "MCH_NOT_EXISTS", "RESOURCE_NOT_EXISTS", "SIGN_ERROR", "FREQUENCY_LIMITED", "NO_AUTH":
		return true
	}
	// INVALID_REQUEST 也可能是原退款号已存在，先查原单，不能直接释放。
	return false
}

// refundNotifyURL 优先读取显式退款回调地址，标准支付回调地址可直接推导同站退款路径。
func refundNotifyURL(settings wechatPaySettings) string {
	if value := strings.TrimSpace(settings.RefundNotifyURL); value != "" {
		return value
	}
	value := strings.TrimSpace(settings.NotifyURL)
	const suffix = "/api/payment/notify/wechat"
	if strings.HasSuffix(value, suffix) {
		return strings.TrimSuffix(value, suffix) + "/api/payment/refund-notify/wechat"
	}
	return ""
}

// optionalString 把空配置转为 nil，避免向微信提交空的可选通知地址。
func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return core.String(value)
}

// QueryRefund 按原商户退款号核实退款结果，只有确认不存在时允许原参数重试。
func (p *WechatPayProvider) QueryRefund(ctx context.Context, refundNo string) (PaymentProviderRefundResult, error) {
	runtime, err := p.initialize(ctx)
	if err != nil {
		return PaymentProviderRefundResult{}, err
	}
	response, _, err := runtime.refundService.QueryByOutRefundNo(ctx, refunddomestic.QueryByOutRefundNoRequest{OutRefundNo: core.String(refundNo)})
	if err != nil {
		var apiErr *core.APIError
		if errors.As(err, &apiErr) && apiErr.Code == "RESOURCE_NOT_EXISTS" && apiErr.StatusCode == http.StatusNotFound {
			return PaymentProviderRefundResult{}, ErrRefundNotFound
		}
		return PaymentProviderRefundResult{}, fmt.Errorf("微信退款查单暂时未完成")
	}
	return wechatRefundResult(response)
}

// wechatRefundResult 校验 SDK 退款结果必需字段及人民币金额。
func wechatRefundResult(response *refunddomestic.Refund) (PaymentProviderRefundResult, error) {
	if response == nil || response.OutTradeNo == nil || response.OutRefundNo == nil || response.RefundId == nil || response.Status == nil ||
		response.Amount == nil || response.Amount.Refund == nil || response.Amount.Total == nil || response.Amount.Currency == nil ||
		*response.Amount.Currency != "CNY" || *response.Amount.Refund <= 0 || *response.Amount.Total < *response.Amount.Refund {
		return PaymentProviderRefundResult{}, errors.New("微信退款没有返回完整结果")
	}
	return PaymentProviderRefundResult{Provider: wechatPayProviderName, OrderNo: *response.OutTradeNo,
		RefundNo: *response.OutRefundNo, ProviderRefundID: *response.RefundId, Status: string(*response.Status),
		AmountCents: int(*response.Amount.Refund), TotalCents: int(*response.Amount.Total), Raw: response}, nil
}

// wechatRefundNotification 表示验签解密后的退款通知，使用通知专用 refund_status 字段。
type wechatRefundNotification struct {
	MerchantID       string `json:"mchid"`
	OrderNo          string `json:"out_trade_no"`
	RefundNo         string `json:"out_refund_no"`
	ProviderRefundID string `json:"refund_id"`
	Status           string `json:"refund_status"`
	Amount           struct {
		Total  int `json:"total"`
		Refund int `json:"refund"`
	} `json:"amount"`
}

// ParseRefundNotify 复用微信 SDK 验签解密能力并核对商户，拒绝伪造或缺字段通知。
func (p *WechatPayProvider) ParseRefundNotify(ctx context.Context, request *http.Request) (PaymentProviderRefundResult, error) {
	runtime, err := p.initialize(ctx)
	if err != nil {
		return PaymentProviderRefundResult{}, err
	}
	var notification wechatRefundNotification
	if _, err := runtime.notifyHandler.ParseNotifyRequest(ctx, request, &notification); err != nil {
		return PaymentProviderRefundResult{}, errors.New("微信退款通知验签失败")
	}
	if notification.MerchantID != runtime.settings.MerchantID || notification.OrderNo == "" || notification.RefundNo == "" ||
		notification.ProviderRefundID == "" || notification.Amount.Refund <= 0 || notification.Amount.Total < notification.Amount.Refund {
		return PaymentProviderRefundResult{}, errors.New("微信退款通知的商户或金额不正确")
	}
	return PaymentProviderRefundResult{Provider: p.Name(), OrderNo: notification.OrderNo, RefundNo: notification.RefundNo,
		ProviderRefundID: notification.ProviderRefundID, Status: notification.Status,
		AmountCents: notification.Amount.Refund, TotalCents: notification.Amount.Total, Raw: notification}, nil
}
