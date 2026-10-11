// 本文件负责 AI 退款预留、赠送收回、退款通知和原退款单对账，避免未知结果导致重复退款。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

const (
	aiRefundReconcileInterval   = time.Minute
	aiRefundReconcileBatchLimit = 50
)

// aiRefundReconcileRule 保存退款年龄对应的核实间隔，Go 判断与数据库筛选共用同一组规则。
type aiRefundReconcileRule struct {
	OlderThan time.Duration
	Interval  time.Duration
}

var aiRefundReconcileRules = [...]aiRefundReconcileRule{
	{OlderThan: 24 * time.Hour, Interval: 30 * time.Minute},
	{OlderThan: time.Hour, Interval: 20 * time.Minute},
	{OlderThan: 20 * time.Minute, Interval: 10 * time.Minute},
	{OlderThan: 5 * time.Minute, Interval: 5 * time.Minute},
}

// openRefundStatus 判断退款是否仍在占用余额并等待核实。
func openRefundStatus(status string) bool {
	return status == "pending" || status == "unknown" || status == "processing" || status == "action_required"
}

// createAIRefund 先原子预留本金并收回本单剩余赠送，再提交微信退款。
func (s *PaymentService) createAIRefund(w http.ResponseWriter, r *http.Request, eligibility paymentRefundEligibility, provider PaymentProvider, amountCents int) {
	refund, err := s.aiWallet.ReserveRefund(PaymentRefund{
		RefundNo: generatePaymentRefundNo(), OrderNo: eligibility.Order.OrderNo,
		UserEmail: eligibility.Order.UserEmail, RefundType: refundTypeAIBalance,
		AmountCents: amountCents, Status: "pending",
	}, s.orders)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	result, err := provider.Refund(ctx, refundInput(refund, eligibility.Order))
	if err != nil {
		status := "unknown"
		if errors.Is(err, ErrRefundRejected) {
			status = "failed"
		}
		stored, storeErr := s.aiWallet.ResolveRefund(refund.RefundNo, status, "", "{}", s.orders)
		if storeErr != nil {
			writeError(w, http.StatusInternalServerError, "退款结果暂时没记好，金额已经预留，请先不要重复提交。")
			return
		}
		if status == "failed" {
			writeError(w, http.StatusBadGateway, "微信没有受理这次退款，预留本金和赠送已恢复，请稍后再试。")
			return
		}
		writeAIRefundResponse(w, http.StatusAccepted, stored, "退款结果还在核实，金额已预留，请先不要重复提交。")
		return
	}
	stored, err := s.applyAIRefundResult(refund, result)
	if err != nil {
		stored, _ = s.aiWallet.ResolveRefund(refund.RefundNo, "unknown", result.ProviderRefundID, "{}", s.orders)
		if stored.RefundNo == "" {
			stored = refund
		}
		writeAIRefundResponse(w, http.StatusAccepted, stored, "退款结果还在核实，金额已预留，请先不要重复提交。")
		return
	}
	message := "退款已提交，该笔充值剩余的赠送已收回，到账速度由微信和银行处理进度决定。"
	if stored.Status == "closed" || stored.Status == "failed" {
		message = "这次退款没有完成，预留本金和赠送已恢复。"
	}
	if stored.Status == "action_required" {
		message = "微信退款需要进一步处理，金额已预留，请联系管理员核实。"
	}
	writeAIRefundResponse(w, http.StatusOK, stored, message)
}

// refundInput 始终使用原支付实付金额和原退款参数，赠送不传给支付渠道。
func refundInput(refund PaymentRefund, order PaymentOrder) PaymentProviderRefundInput {
	return PaymentProviderRefundInput{OrderNo: order.OrderNo, RefundNo: refund.RefundNo,
		AmountCents: refund.AmountCents, TotalCents: order.AmountCents, Reason: refundReason(refund.RefundType)}
}

// writeAIRefundResponse 分别返回实际退款本金与收回赠送金额。
func writeAIRefundResponse(w http.ResponseWriter, code int, refund PaymentRefund, message string) {
	writeJSON(w, code, map[string]any{"ok": true, "message": message, "refund": map[string]any{
		"refund_no": refund.RefundNo, "refund_type": refund.RefundType,
		"amount_cents": refund.AmountCents, "amount": centsToYuanString(refund.AmountCents),
		"revoked_bonus_units": refund.RevokedBonusUnits, "revoked_bonus": aiUnitsToYuanString(refund.RevokedBonusUnits),
		"status": refund.Status,
	}})
}

// applyAIRefundResult 校验原单、退款号和金额后，按已核实的状态完成或恢复预留。
func (s *PaymentService) applyAIRefundResult(refund PaymentRefund, result PaymentProviderRefundResult) (PaymentRefund, error) {
	order, err := s.orders.ByOrderNo(refund.OrderNo)
	if err != nil {
		return PaymentRefund{}, err
	}
	if result.Provider != order.PaymentProvider || result.OrderNo != refund.OrderNo || result.RefundNo != refund.RefundNo ||
		result.ProviderRefundID == "" || result.AmountCents != refund.AmountCents || result.TotalCents != order.AmountCents {
		return PaymentRefund{}, errors.New("退款通知的订单或金额不一致")
	}
	status := "unknown"
	switch strings.ToUpper(strings.TrimSpace(result.Status)) {
	case "SUCCESS":
		status = "success"
	case "PROCESSING":
		status = "processing"
	case "CLOSED":
		status = "closed"
	case "ABNORMAL":
		status = "action_required"
	default:
		return PaymentRefund{}, errors.New("退款状态暂时无法确认")
	}
	raw, _ := json.Marshal(result.Raw)
	if !refund.RightsReserved && openRefundStatus(refund.Status) {
		// 旧退款没有可靠预扣流水，不能自动宣告资金处理完成或凭空返还。
		return s.orders.UpdateRefund(refund.RefundNo, "action_required", result.ProviderRefundID, string(raw))
	}
	return s.aiWallet.ResolveRefund(refund.RefundNo, status, result.ProviderRefundID, string(raw), s.orders)
}

// WechatRefundNotify 验签解密退款通知，重复通知由钱包事务保持幂等。
func (s *PaymentService) WechatRefundNotify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"code": "FAIL", "message": "请求方式不支持"})
		return
	}
	provider, ok := s.providers[wechatPayProviderName].(PaymentRefundProvider)
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "FAIL", "message": "退款通知暂未配置"})
		return
	}
	result, err := provider.ParseRefundNotify(r.Context(), r)
	if err == nil {
		var refund PaymentRefund
		refund, err = s.orders.ByRefundNo(result.RefundNo)
		if err == nil {
			if refund.RefundType != refundTypeAIBalance {
				err = errors.New("此通知不是 AI 退款")
			} else {
				_, err = s.applyAIRefundResult(refund, result)
			}
		}
	}
	if err != nil {
		log.Printf("[退款] 微信退款通知核实失败 err=%v", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "FAIL", "message": "退款通知暂未核实"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"code": "SUCCESS", "message": "成功"})
}

// StartRefundReconciliation 定时核实持久化的 AI 退款，重启后继续使用原退款号。
func (s *PaymentService) StartRefundReconciliation(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(aiRefundReconcileInterval)
		defer ticker.Stop()
		for {
			s.reconcileAIRefunds(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// refundReconcileInterval 退款超过五分钟仍未结束时逐渐降低查询频率。
func refundReconcileInterval(refund PaymentRefund) time.Duration {
	return refundReconcileIntervalAt(refund, time.Now())
}

// refundReconcileIntervalAt 使用指定时间选择查单间隔，保证同批次内存筛选不会跨年龄边界漂移。
func refundReconcileIntervalAt(refund PaymentRefund, now time.Time) time.Duration {
	age := now.Sub(refund.CreatedAt)
	for _, rule := range aiRefundReconcileRules {
		if age > rule.OlderThan {
			return rule.Interval
		}
	}
	return aiRefundReconcileInterval
}

// refundReconcileDue 判断原退款是否已经到下一次核实时间。
func refundReconcileDue(refund PaymentRefund, now time.Time) bool {
	return !refund.UpdatedAt.After(now.Add(-refundReconcileIntervalAt(refund, now)))
}

// reconcileAIRefunds 在有限时间内查单，不把查询异常当作退款失败。
func (s *PaymentService) reconcileAIRefunds(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	refunds, err := s.orders.ListOpenRefunds("")
	if err != nil {
		log.Printf("[退款] 待核实记录读取失败 err=%v", err)
		return
	}
	for _, refund := range refunds {
		if ctx.Err() != nil {
			return
		}
		if time.Since(refund.UpdatedAt) < refundReconcileInterval(refund) {
			continue
		}
		if err := s.reconcileAIRefund(ctx, refund); err != nil {
			log.Printf("[退款] 原单核实未完成 refund=%s err=%v", refund.RefundNo, err)
		}
	}
}

// reconcileAIRefund 查询原退款单；确认不存在时仅用相同退款号和金额重试。
func (s *PaymentService) reconcileAIRefund(ctx context.Context, refund PaymentRefund) error {
	order, err := s.orders.ByOrderNo(refund.OrderNo)
	if err != nil {
		return err
	}
	provider, exists := s.providers[order.PaymentProvider]
	queryProvider, ok := provider.(PaymentRefundProvider)
	if !exists || !ok {
		return errors.New("支付提供方没有退款查单能力")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	result, err := queryProvider.QueryRefund(ctx, refund.RefundNo)
	if errors.Is(err, ErrRefundNotFound) && refund.RightsReserved && (refund.Status == "pending" || refund.Status == "unknown") {
		result, err = provider.Refund(ctx, refundInput(refund, order))
		if errors.Is(err, ErrRefundRejected) {
			_, resolveErr := s.aiWallet.ResolveRefund(refund.RefundNo, "failed", "", "{}", s.orders)
			return resolveErr
		}
	}
	if err != nil {
		// 更新最后核实时间，避免一条异常记录持续占据批次。
		_, _ = s.aiWallet.ResolveRefund(refund.RefundNo, refund.Status, refund.ProviderRefundID, refund.ResponseData, s.orders)
		return fmt.Errorf("退款状态暂未查明")
	}
	_, err = s.applyAIRefundResult(refund, result)
	return err
}
