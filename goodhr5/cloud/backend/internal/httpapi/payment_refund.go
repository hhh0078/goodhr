// 本文件负责计算会员和 AI 余额可退金额，并自动提交微信原路退款。
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"
)

const (
	refundTypeMembership = "membership"
	refundTypeAIBalance  = "ai_balance"
)

// createPaymentRefundRequest 表示用户确认提交的退款请求。
type createPaymentRefundRequest struct {
	RefundType  string `json:"refund_type"`
	AmountCents int    `json:"amount_cents"`
	AmountYuan  string `json:"amount_yuan"`
}

// paymentRefundEligibility 表示一笔可承接指定退款金额的成功支付订单。
type paymentRefundEligibility struct {
	Order          PaymentOrder
	AvailableCents int
	DeductedDays   int
}

// Refund 按请求方法返回退款额度或自动提交退款。
func (s *PaymentService) Refund(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.refundQuote(w, r)
	case http.MethodPost:
		s.createRefund(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// refundQuote 返回当前账号两种退款类型的最高可退金额。
func (s *PaymentService) refundQuote(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	s.refundMu.Lock()
	defer s.refundMu.Unlock()

	membership, membershipErr := s.findRefundEligibility(session.Email, refundTypeMembership, 0, time.Now())
	aiBalance, aiErr := s.findRefundEligibility(session.Email, refundTypeAIBalance, 0, time.Now())
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"membership": refundQuoteItem(membership, membershipErr),
		"ai_balance": refundQuoteItem(aiBalance, aiErr),
	})
}

// refundQuoteItem 将退款资格转换为前端展示数据。
func refundQuoteItem(eligibility paymentRefundEligibility, err error) map[string]any {
	item := map[string]any{
		"available":        err == nil && eligibility.AvailableCents > 0,
		"max_amount_cents": max(0, eligibility.AvailableCents),
		"max_amount":       centsToYuanString(max(0, eligibility.AvailableCents)),
	}
	if err != nil {
		item["message"] = err.Error()
	}
	return item
}

// createRefund 重新校验订单和权益后提交微信退款并扣减对应权益。
func (s *PaymentService) createRefund(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	var req createPaymentRefundRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "退款信息没读明白，请重新填写。")
		return
	}
	amountCents := req.AmountCents
	if amountCents <= 0 && strings.TrimSpace(req.AmountYuan) != "" {
		amountCents, err = yuanTextToCents(req.AmountYuan)
	}
	if err != nil || amountCents <= 0 {
		writeError(w, http.StatusBadRequest, "退款金额得大于 0，我先小声拦一下。")
		return
	}
	refundType := strings.TrimSpace(req.RefundType)
	if refundType != refundTypeMembership && refundType != refundTypeAIBalance {
		writeError(w, http.StatusBadRequest, "请选择会员退款或 AI 余额退款。")
		return
	}

	s.refundMu.Lock()
	defer s.refundMu.Unlock()
	eligibility, err := s.findRefundEligibility(session.Email, refundType, amountCents, time.Now())
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	provider, ok := s.providers[eligibility.Order.PaymentProvider]
	if !ok || eligibility.Order.PaymentProvider != wechatPayProviderName {
		writeError(w, http.StatusConflict, "这笔订单不是微信支付，暂时不能自动原路退款。")
		return
	}
	refundNo := generatePaymentRefundNo()
	refund, err := s.orders.CreateRefund(PaymentRefund{
		RefundNo:     refundNo,
		OrderNo:      eligibility.Order.OrderNo,
		UserEmail:    session.Email,
		RefundType:   refundType,
		AmountCents:  amountCents,
		DeductedDays: eligibility.DeductedDays,
		Status:       "pending",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "退款记录没保存好，这次没有扣钱，请稍后再试。")
		return
	}
	result, err := provider.Refund(r.Context(), PaymentProviderRefundInput{
		OrderNo:     eligibility.Order.OrderNo,
		RefundNo:    refund.RefundNo,
		AmountCents: amountCents,
		TotalCents:  eligibility.Order.AmountCents,
		Reason:      refundReason(refundType),
	})
	if err != nil {
		_, _ = s.orders.UpdateRefund(refund.RefundNo, "failed", "", "{}")
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	status := strings.ToUpper(strings.TrimSpace(result.Status))
	if status != "SUCCESS" && status != "PROCESSING" {
		raw, _ := json.Marshal(result.Raw)
		_, _ = s.orders.UpdateRefund(refund.RefundNo, "failed", result.ProviderRefundID, string(raw))
		writeError(w, http.StatusConflict, "微信没有受理这次退款，请稍后再试。")
		return
	}
	if err := s.deductRefundRights(refund); err != nil {
		raw, _ := json.Marshal(result.Raw)
		_, _ = s.orders.UpdateRefund(refund.RefundNo, "action_required", result.ProviderRefundID, string(raw))
		writeError(w, http.StatusInternalServerError, "微信已受理退款，但权益扣减没完成，记录已保留，请联系管理员处理。")
		return
	}
	storedStatus := "processing"
	if status == "SUCCESS" {
		storedStatus = "success"
	}
	raw, _ := json.Marshal(result.Raw)
	refund, err = s.orders.UpdateRefund(refund.RefundNo, storedStatus, result.ProviderRefundID, string(raw))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "退款已受理，但结果记录更新失败，请联系管理员。")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "退款已提交，到账速度由微信和银行处理进度决定。",
		"refund": map[string]any{
			"refund_no":    refund.RefundNo,
			"refund_type":  refund.RefundType,
			"amount_cents": refund.AmountCents,
			"amount":       centsToYuanString(refund.AmountCents),
			"status":       refund.Status,
		},
	})
}

// findRefundEligibility 查找一笔金额充足的成功支付订单并计算其可退额度。
func (s *PaymentService) findRefundEligibility(email string, refundType string, requestedCents int, now time.Time) (paymentRefundEligibility, error) {
	orders, err := s.orders.ListByUser(email)
	if err != nil {
		return paymentRefundEligibility{}, fmt.Errorf("支付记录暂时没查清楚，请稍后再试。")
	}
	currentRemainingDays := 0
	if refundType == refundTypeMembership {
		subscription, subErr := s.subscriptions.UserSubscription(email)
		if subErr != nil {
			return paymentRefundEligibility{}, fmt.Errorf("会员剩余时间暂时没查清楚，请稍后再试。")
		}
		currentRemainingDays = int(math.Ceil(subscription.ExpiresAt.Sub(now).Hours() / 24))
		if currentRemainingDays <= 0 {
			return paymentRefundEligibility{}, fmt.Errorf("当前没有可退的会员剩余时间。")
		}
	}
	walletCents := int64(0)
	if refundType == refundTypeAIBalance {
		if s.aiWallet == nil {
			return paymentRefundEligibility{}, fmt.Errorf("AI 余额暂时没准备好，请稍后再试。")
		}
		units, balanceErr := s.aiWallet.BalanceUnits(email)
		if balanceErr != nil {
			return paymentRefundEligibility{}, fmt.Errorf("AI 余额暂时没查清楚，请稍后再试。")
		}
		walletCents = aiUnitsToCents(units)
		if walletCents <= 0 {
			return paymentRefundEligibility{}, fmt.Errorf("当前 AI 余额不足，没有可退金额。")
		}
	}

	best := paymentRefundEligibility{}
	for _, order := range orders {
		if order.Status != "paid" || order.PaymentProvider != wechatPayProviderName || order.AmountCents <= 0 {
			continue
		}
		orderType := defaultString(order.OrderType, "subscription")
		expectedOrderType := refundType
		if refundType == refundTypeMembership {
			expectedOrderType = "subscription"
		}
		if orderType != expectedOrderType {
			continue
		}
		refunded, refundErr := s.orders.RefundedCentsByOrder(order.OrderNo)
		if refundErr != nil {
			return paymentRefundEligibility{}, fmt.Errorf("历史退款暂时没查清楚，请稍后再试。")
		}
		available := order.AmountCents - refunded
		deductedDays := 0
		if refundType == refundTypeMembership {
			if order.PaidAt == nil || order.DurationDays <= 0 {
				continue
			}
			usedDays := int(math.Ceil(now.Sub(*order.PaidAt).Hours() / 24))
			if usedDays < 0 {
				usedDays = 0
			}
			remainingDays := order.DurationDays - usedDays
			if remainingDays > currentRemainingDays {
				remainingDays = currentRemainingDays
			}
			if remainingDays <= 0 {
				continue
			}
			prorated := order.AmountCents * remainingDays / order.DurationDays
			if available > prorated {
				available = prorated
			}
			if requestedCents > 0 {
				deductedDays = int(math.Ceil(float64(requestedCents*order.DurationDays) / float64(order.AmountCents)))
				if deductedDays > remainingDays {
					continue
				}
			}
		} else if int64(available) > walletCents {
			available = int(walletCents)
		}
		if available <= 0 || (requestedCents > 0 && available < requestedCents) {
			continue
		}
		if best.Order.OrderNo == "" || available > best.AvailableCents {
			best = paymentRefundEligibility{Order: order, AvailableCents: available, DeductedDays: deductedDays}
		}
	}
	if best.Order.OrderNo == "" {
		if requestedCents > 0 {
			return paymentRefundEligibility{}, fmt.Errorf("没有单笔成功支付订单能覆盖这次退款金额，我先替你拦下了。")
		}
		return paymentRefundEligibility{}, fmt.Errorf("没有找到可退款的成功支付订单。")
	}
	return best, nil
}

// deductRefundRights 在微信受理退款后扣减对应会员天数或 AI 余额。
func (s *PaymentService) deductRefundRights(refund PaymentRefund) error {
	if refund.RefundType == refundTypeAIBalance {
		_, err := s.aiWallet.AdjustBalance(AIWalletRecord{
			UserEmail: refund.UserEmail, ChangeUnits: -centsToAIUnits(refund.AmountCents),
			Category: "refund", Reason: "AI余额退款", RelatedOrderNo: refund.RefundNo,
		})
		return err
	}
	if refund.DeductedDays <= 0 {
		return errors.New("会员退款扣减天数不正确")
	}
	_, err := s.subscriptions.AdjustSubscriptionDays(refund.UserEmail, "", -refund.DeductedDays)
	return err
}

// refundReason 返回微信账单里显示的退款原因。
func refundReason(refundType string) string {
	if refundType == refundTypeAIBalance {
		return "GoodHR AI余额退款"
	}
	return "GoodHR 会员退款"
}

// generatePaymentRefundNo 生成唯一商户退款单号。
func generatePaymentRefundNo() string {
	return fmt.Sprintf("R%d", time.Now().UnixNano())
}
