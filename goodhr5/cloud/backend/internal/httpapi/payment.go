// 本文件负责提供会员订阅支付的 HTTP API 和统一支付业务逻辑。
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const defaultPaymentProvider = "haoshoumi"

type createPaymentOrderRequest struct {
	PlanID string `json:"plan_id"`
}

type createAIBalanceOrderRequest struct {
	AmountCents int    `json:"amount_cents"`
	AmountYuan  string `json:"amount_yuan"`
}

type subscriptionPlan struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	MemberType     string   `json:"member_type"`
	DurationDays   int      `json:"duration_days"`
	OriginalPrice  float64  `json:"original_price"`
	DiscountAmount float64  `json:"discount_amount"`
	Features       []string `json:"features"`
	Description    string   `json:"description"`
	CreatedAt      string   `json:"created_at"`
}

// PaymentService 处理会员订阅支付、回调和支付记录查询。
type PaymentService struct {
	auth          *AuthService
	orders        PaymentStore
	subscriptions SubscriptionStore
	systemConfigs SystemConfigStore
	invitations   InvitationStore
	mailer        Mailer
	aiWallet      AIWalletStore
	providers     map[string]PaymentProvider
}

// NewPaymentService 创建支付服务。
func NewPaymentService(auth *AuthService, orders PaymentStore, subscriptions SubscriptionStore, systemConfigs SystemConfigStore, invitations InvitationStore, mailer Mailer, aiWallet AIWalletStore, providers ...PaymentProvider) *PaymentService {
	providerMap := map[string]PaymentProvider{}
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		providerMap[provider.Name()] = provider
	}
	return &PaymentService{
		auth:          auth,
		orders:        orders,
		subscriptions: subscriptions,
		systemConfigs: systemConfigs,
		invitations:   invitations,
		mailer:        mailer,
		aiWallet:      aiWallet,
		providers:     providerMap,
	}
}

// Orders 按请求方法处理用户支付记录列表和创建订单。
func (s *PaymentService) Orders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.ListMyOrders(w, r)
	case http.MethodPost:
		s.CreateOrder(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// CreateOrder 为当前用户创建订阅支付订单。
func (s *PaymentService) CreateOrder(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}

	var req createPaymentOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	plan, err := s.subscriptionPlanByID(req.PlanID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "subscription plan not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load subscription plan")
		return
	}
	amountCents := priceToCents(plan.OriginalPrice) - priceToCents(plan.DiscountAmount)
	if amountCents <= 0 {
		writeError(w, http.StatusBadRequest, "subscription plan amount is invalid")
		return
	}

	provider, ok := s.providers[defaultPaymentProvider]
	if !ok {
		writeError(w, http.StatusInternalServerError, "payment provider is not configured")
		return
	}

	orderNo := generatePaymentOrderNo()
	expiredAt := time.Now().Add(30 * time.Minute)
	order, err := s.orders.Create(PaymentOrder{
		OrderNo:             orderNo,
		UserEmail:           session.Email,
		PlanID:              plan.ID,
		PlanName:            plan.Name,
		MemberType:          defaultString(plan.MemberType, defaultMemberType),
		DurationDays:        plan.DurationDays,
		OriginalAmountCents: priceToCents(plan.OriginalPrice),
		DiscountAmountCents: priceToCents(plan.DiscountAmount),
		AmountCents:         amountCents,
		PaymentProvider:     defaultPaymentProvider,
		Status:              "pending",
		ExpiredAt:           &expiredAt,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create payment order")
		return
	}

	payResult, err := provider.CreateOrder(PaymentProviderOrderInput{
		OrderNo:     order.OrderNo,
		Title:       "GoodHR " + order.PlanName,
		AmountCents: order.AmountCents,
		Remark:      "user:" + session.Email + ",plan:" + order.PlanID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"order":   publicPaymentOrder(order),
		"payment": payResult,
	})
}

// AIBalanceOrder 为当前用户创建内置 AI 余额充值订单。
func (s *PaymentService) AIBalanceOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	var req createAIBalanceOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	amountCents := req.AmountCents
	if amountCents <= 0 && strings.TrimSpace(req.AmountYuan) != "" {
		amountCents, err = yuanTextToCents(req.AmountYuan)
		if err != nil {
			writeError(w, http.StatusBadRequest, "充值金额不太对，我没敢收。")
			return
		}
	}
	if amountCents <= 0 {
		amountCents = defaultAIRechargeAmountCents
	}
	if amountCents < 100 || amountCents > 100000 {
		writeError(w, http.StatusBadRequest, "充值金额建议在 1 元到 1000 元之间。")
		return
	}
	provider, ok := s.providers[defaultPaymentProvider]
	if !ok {
		writeError(w, http.StatusInternalServerError, "payment provider not configured")
		return
	}
	orderNo := generatePaymentOrderNo()
	expiredAt := time.Now().Add(30 * time.Minute)
	order, err := s.orders.Create(PaymentOrder{
		OrderNo:             orderNo,
		OrderType:           "ai_balance",
		UserEmail:           session.Email,
		PlanID:              "ai_balance",
		PlanName:            "AI余额充值",
		MemberType:          "",
		DurationDays:        0,
		OriginalAmountCents: amountCents,
		DiscountAmountCents: 0,
		AmountCents:         amountCents,
		PaymentProvider:     defaultPaymentProvider,
		Status:              "pending",
		ExpiredAt:           &expiredAt,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed create payment order")
		return
	}
	payResult, err := provider.CreateOrder(PaymentProviderOrderInput{
		OrderNo:     order.OrderNo,
		Title:       "GoodHR AI余额充值",
		AmountCents: order.AmountCents,
		Remark:      "user:" + session.Email + ",type:ai_balance",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "order": publicPaymentOrder(order), "payment": payResult})
}

// ListMyOrders 返回当前用户自己的支付记录。
func (s *PaymentService) ListMyOrders(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	orders, err := s.orders.ListByUser(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list payment orders")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "orders": publicPaymentOrders(orders)})
}

// ListAdminOrders 返回全部支付记录，只有超级管理员可访问。
func (s *PaymentService) ListAdminOrders(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	if !s.auth.IsSuperAdmin(session.Email) {
		writeError(w, http.StatusForbidden, "super admin access required")
		return
	}
	orders, err := s.orders.ListAll()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list payment orders")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "orders": publicPaymentOrders(orders)})
}

// OrderDetail 返回当前用户可见的单条支付记录。
func (s *PaymentService) OrderDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	orderNo := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/payment/orders/"), "/")
	if orderNo == "" {
		writeError(w, http.StatusBadRequest, "order no is required")
		return
	}
	order, err := s.orders.ByOrderNo(orderNo)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "payment order not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load payment order")
		return
	}
	if order.UserEmail != session.Email && !s.auth.IsSuperAdmin(session.Email) {
		writeError(w, http.StatusForbidden, "permission denied")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "order": publicPaymentOrder(order)})
}

// HaoshoumiNotify 处理好收米支付回调。
func (s *PaymentService) HaoshoumiNotify(w http.ResponseWriter, r *http.Request) {
	values, err := readNotifyValues(r)
	if err != nil {
		http.Error(w, "fail", http.StatusBadRequest)
		return
	}
	if err := s.HandleNotify(defaultPaymentProvider, values); err != nil {
		http.Error(w, "fail", http.StatusBadRequest)
		return
	}
	_, _ = w.Write([]byte("success"))
}

// HandleNotify 统一处理第三方支付回调和会员续期。
func (s *PaymentService) HandleNotify(providerName string, values map[string]string) error {
	provider, ok := s.providers[providerName]
	if !ok {
		return fmt.Errorf("payment provider not found")
	}
	result, err := provider.VerifyNotify(values)
	if err != nil {
		return err
	}
	order, err := s.orders.ByOrderNo(result.OrderNo)
	if err != nil {
		return err
	}
	if order.AmountCents != result.AmountCents {
		return fmt.Errorf("payment amount mismatch")
	}
	raw, _ := json.Marshal(result.Raw)
	paidOrder, changed, err := s.orders.MarkPaid(order.OrderNo, result.TradeNo, string(raw))
	if err != nil {
		return err
	}
	if changed {
		if paidOrder.OrderType == "ai_balance" {
			if s.aiWallet == nil {
				return fmt.Errorf("ai wallet not configured")
			}
			_, err = s.aiWallet.AdjustBalance(AIWalletRecord{
				UserEmail:      paidOrder.UserEmail,
				ChangeCents:    paidOrder.AmountCents,
				Category:       "recharge",
				Reason:         "AI余额充值成功",
				RelatedOrderNo: paidOrder.OrderNo,
			})
			return err
		}
		subscription, err := s.subscriptions.ExtendSubscription(paidOrder.UserEmail, paidOrder.MemberType, paidOrder.DurationDays)
		if err != nil {
			return err
		}
		if err := sendSubscriptionRewardNotice(s.mailer, paidOrder.UserEmail, SubscriptionRewardNotice{
			Reason:     "充值会员成功",
			Days:       paidOrder.DurationDays,
			MemberType: subscription.MemberType,
			ExpiresAt:  subscription.ExpiresAt,
		}); err != nil {
			return err
		}
		err = s.applyInvitePaymentReward(paidOrder)
	}
	return err
}

// applyInvitePaymentReward 在被邀请用户支付成功后给邀请人发放奖励。
func (s *PaymentService) applyInvitePaymentReward(order PaymentOrder) error {
	if s.invitations == nil || s.subscriptions == nil {
		return nil
	}
	inviterEmail, err := s.invitations.InviterEmailByInvitee(order.UserEmail)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	config := loadInviteConfig(s.systemConfigs)
	if config.PaidMonthRewardDays <= 0 {
		return nil
	}
	months := order.DurationDays / 30
	if months <= 0 {
		months = 1
	}
	rewardDays := config.PaidMonthRewardDays * months
	subscription, err := s.subscriptions.ExtendSubscription(inviterEmail, defaultMemberType, rewardDays)
	if err != nil {
		return err
	}
	return sendSubscriptionRewardNotice(s.mailer, inviterEmail, SubscriptionRewardNotice{
		Reason:       "邀请好友充值成功奖励",
		Days:         rewardDays,
		MemberType:   subscription.MemberType,
		ExpiresAt:    subscription.ExpiresAt,
		RelatedEmail: order.UserEmail,
	})
}

// sendSubscriptionRewardNotice 发送会员天数变动提醒邮件。
func sendSubscriptionRewardNotice(mailer Mailer, email string, notice SubscriptionRewardNotice) error {
	if mailer == nil || notice.Days == 0 {
		return nil
	}
	return mailer.SendSubscriptionReward(email, notice)
}

// subscriptionPlanByID 从系统配置中读取指定订阅套餐。
func (s *PaymentService) subscriptionPlanByID(planID string) (subscriptionPlan, error) {
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return subscriptionPlan{}, ErrNotFound
	}
	cfg, err := s.systemConfigs.Get("system.subscription_plans")
	if err != nil {
		return subscriptionPlan{}, err
	}
	var plans []subscriptionPlan
	if err := json.Unmarshal([]byte(cfg.ConfigValue), &plans); err != nil {
		return subscriptionPlan{}, err
	}
	for _, plan := range plans {
		if plan.ID == planID && plan.DurationDays > 0 {
			return plan, nil
		}
	}
	return subscriptionPlan{}, ErrNotFound
}

// publicPaymentOrders 转换支付记录列表为前端响应。
func publicPaymentOrders(orders []PaymentOrder) []map[string]any {
	result := make([]map[string]any, 0, len(orders))
	for _, order := range orders {
		result = append(result, publicPaymentOrder(order))
	}
	return result
}

// publicPaymentOrder 转换单条支付记录为前端响应。
func publicPaymentOrder(order PaymentOrder) map[string]any {
	return map[string]any{
		"id":                    order.ID,
		"order_no":              order.OrderNo,
		"order_type":            defaultString(order.OrderType, "subscription"),
		"user_email":            order.UserEmail,
		"plan_id":               order.PlanID,
		"plan_name":             order.PlanName,
		"member_type":           order.MemberType,
		"duration_days":         order.DurationDays,
		"original_amount_cents": order.OriginalAmountCents,
		"discount_amount_cents": order.DiscountAmountCents,
		"amount_cents":          order.AmountCents,
		"amount":                centsToYuanString(order.AmountCents),
		"payment_provider":      order.PaymentProvider,
		"trade_no":              order.TradeNo,
		"status":                order.Status,
		"paid_at":               order.PaidAt,
		"expired_at":            order.ExpiredAt,
		"created_at":            order.CreatedAt,
	}
}

// readNotifyValues 读取支付平台 GET、表单或 JSON 回调参数。
func readNotifyValues(r *http.Request) (map[string]string, error) {
	values := map[string]string{}
	for key, vals := range r.URL.Query() {
		if len(vals) > 0 {
			values[key] = vals[0]
		}
	}
	if len(values) > 0 {
		return values, nil
	}
	if err := r.ParseForm(); err == nil {
		for key, vals := range r.PostForm {
			if len(vals) > 0 {
				values[key] = vals[0]
			}
		}
	}
	if len(values) > 0 {
		return values, nil
	}
	if err := json.NewDecoder(r.Body).Decode(&values); err != nil {
		return nil, err
	}
	return values, nil
}

// generatePaymentOrderNo 生成会员订阅订单号。
func generatePaymentOrderNo() string {
	return fmt.Sprintf("S%d", time.Now().UnixNano())
}

// priceToCents 将元价格转换成分。
func priceToCents(value float64) int {
	return int(math.Round(value * 100))
}

// yuanTextToCents 将元字符串转换成分。
func yuanTextToCents(value string) (int, error) {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0, err
	}
	return priceToCents(parsed), nil
}

// defaultString 返回非空字符串或默认值。
func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
