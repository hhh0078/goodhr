// 本文件负责定义支付平台共用的数据结构，具体支付方式分别放在独立文件中实现。
package httpapi

import (
	"context"
	"fmt"
	"net/http"
)

// PaymentProvider 定义支付平台下单、查单和回调解析能力。
type PaymentProvider interface {
	// Name 返回支付平台标识。
	Name() string
	// CreateOrder 创建第三方支付订单。
	CreateOrder(ctx context.Context, input PaymentProviderOrderInput) (PaymentProviderOrderResult, error)
	// QueryOrder 主动查询第三方订单状态。
	QueryOrder(ctx context.Context, orderNo string) (PaymentProviderTransactionResult, error)
	// ParseNotify 验证并解析第三方支付回调。
	ParseNotify(ctx context.Context, request *http.Request) (PaymentProviderTransactionResult, error)
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
	Provider string `json:"provider"`
	OrderNo  string `json:"order_no"`
	CodeURL  string `json:"code_url"`
}

// PaymentProviderTransactionResult 表示第三方支付通知或查单的标准结果。
type PaymentProviderTransactionResult struct {
	OrderNo     string
	TradeNo     string
	AmountCents int
	Paid        bool
	Raw         any
}

// centsToYuanString 将分转换为两位小数元字符串。
func centsToYuanString(cents int) string {
	return fmt.Sprintf("%.2f", float64(cents)/100)
}

// centsToAIUnits 将分转换成 AI 钱包 0.0001 元精度单位。
func centsToAIUnits(cents int) int64 {
	return int64(cents) * aiWalletUnitsPerCent
}

// aiUnitsToCents 将 AI 钱包余额按分返回，主要兼容旧前端字段。
func aiUnitsToCents(units int64) int64 {
	return units / aiWalletUnitsPerCent
}

// aiUnitsToYuanString 将 AI 钱包 0.0001 元单位转换为四位小数元字符串。
func aiUnitsToYuanString(units int64) string {
	sign := ""
	if units < 0 {
		sign = "-"
		units = -units
	}
	return fmt.Sprintf("%s%d.%04d", sign, units/aiWalletUnitsPerYuan, units%aiWalletUnitsPerYuan)
}
