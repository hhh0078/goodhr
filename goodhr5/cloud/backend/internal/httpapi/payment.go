// 本文件负责提供会员订阅支付的 HTTP API 和统一支付业务逻辑。
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
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

// subscriptionPaymentQuote 表示创建订阅订单前计算出的实付金额和升级抵扣。
type subscriptionPaymentQuote struct {
	AmountCents        int
	UpgradeFromType    string
	UpgradeCreditCents int
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
	current, err := s.subscriptions.UserSubscription(session.Email)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "会员状态暂时没查清楚，这次我先不乱下单")
		return
	}
	plans, err := loadSubscriptionPlans(s.systemConfigs)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "会员套餐配置暂时没读明白，请稍后再试")
		return
	}
	quote, err := buildSubscriptionPaymentQuote(plans, current, plan, time.Now())
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if quote.AmountCents < 0 {
		writeError(w, http.StatusBadRequest, "subscription plan amount is invalid")
		return
	}

	orderNo := generatePaymentOrderNo()
	expiredAt := time.Now().Add(30 * time.Minute)
	paymentProvider := defaultPaymentProvider
	if quote.AmountCents == 0 {
		paymentProvider = "upgrade_credit"
	}
	order, err := s.orders.Create(PaymentOrder{
		OrderNo:             orderNo,
		UserEmail:           session.Email,
		PlanID:              plan.ID,
		PlanName:            plan.Name,
		MemberType:          defaultString(plan.MemberType, defaultMemberType),
		DurationDays:        plan.DurationDays,
		OriginalAmountCents: priceToCents(plan.OriginalPrice),
		DiscountAmountCents: priceToCents(plan.DiscountAmount),
		UpgradeFromType:     quote.UpgradeFromType,
		UpgradeCreditCents:  quote.UpgradeCreditCents,
		AmountCents:         quote.AmountCents,
		PaymentProvider:     paymentProvider,
		Status:              "pending",
		ExpiredAt:           &expiredAt,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create payment order")
		return
	}

	if quote.AmountCents == 0 {
		paidOrder, changed, markErr := s.orders.MarkPaid(order.OrderNo, "upgrade-credit", "{}")
		if markErr != nil {
			writeError(w, http.StatusInternalServerError, "升级订单暂时没记好，请稍后再试")
			return
		}
		if changed {
			if applyErr := s.applyPaidSubscriptionOrder(paidOrder); applyErr != nil {
				writeError(w, http.StatusInternalServerError, applyErr.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":                true,
			"payment_completed": true,
			"order":             publicPaymentOrder(paidOrder),
		})
		return
	}

	provider, ok := s.providers[defaultPaymentProvider]
	if !ok {
		writeError(w, http.StatusInternalServerError, "payment provider is not configured")
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
	paidOrder, _, err := s.orders.MarkPaid(order.OrderNo, result.TradeNo, string(raw))
	if err != nil {
		return err
	}
	if paidOrder.Status != "paid" {
		return fmt.Errorf("payment order is not pending")
	}
	if paidOrder.OrderType == "ai_balance" {
		if s.aiWallet == nil {
			return fmt.Errorf("ai wallet not configured")
		}
		_, err = s.aiWallet.AdjustBalance(AIWalletRecord{
			UserEmail:      paidOrder.UserEmail,
			ChangeUnits:    centsToAIUnits(paidOrder.AmountCents),
			Category:       "recharge",
			Reason:         "AI余额充值成功",
			RelatedOrderNo: paidOrder.OrderNo,
		})
		return err
	}
	return s.applyPaidSubscriptionOrder(paidOrder)
}

// applyPaidSubscriptionOrder 根据普通续费或 Plus 升级订单更新会员并发送提醒。
func (s *PaymentService) applyPaidSubscriptionOrder(order PaymentOrder) error {
	subscription, changed, err := s.subscriptions.ApplyGrant(SubscriptionGrant{
		OrderNo:    order.OrderNo,
		GrantType:  "buyer",
		Email:      order.UserEmail,
		MemberType: order.MemberType,
		Days:       order.DurationDays,
		ReplaceFromNow: normalizeMemberType(order.UpgradeFromType) == memberTypePlus &&
			normalizeMemberType(order.MemberType) == memberTypeMax,
	})
	if err != nil {
		return err
	}
	reason := "充值会员成功"
	if order.UpgradeFromType != "" {
		reason = "Plus 升级 Max 成功"
	}
	if changed {
		if noticeErr := sendSubscriptionRewardNotice(s.mailer, s.systemConfigs, order.UserEmail, SubscriptionRewardNotice{
			Reason:     reason,
			Days:       order.DurationDays,
			MemberType: subscription.MemberType,
			ExpiresAt:  subscription.ExpiresAt,
		}); noticeErr != nil {
			log.Printf("[支付] 会员已到账，但通知邮件发送失败 order=%s user=%s err=%v", order.OrderNo, order.UserEmail, noticeErr)
		}
	}
	return s.applyInvitePaymentReward(order)
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
	subscription, changed, err := s.subscriptions.ApplyGrant(SubscriptionGrant{
		OrderNo:    order.OrderNo,
		GrantType:  "inviter",
		Email:      inviterEmail,
		MemberType: "",
		Days:       rewardDays,
	})
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	if noticeErr := sendSubscriptionRewardNotice(s.mailer, s.systemConfigs, inviterEmail, SubscriptionRewardNotice{
		Reason:       "邀请好友充值成功奖励",
		Days:         rewardDays,
		MemberType:   subscription.MemberType,
		ExpiresAt:    subscription.ExpiresAt,
		RelatedEmail: order.UserEmail,
	}); noticeErr != nil {
		log.Printf("[支付] 邀请奖励已到账，但通知邮件发送失败 order=%s user=%s err=%v", order.OrderNo, inviterEmail, noticeErr)
	}
	return nil
}

// sendSubscriptionRewardNotice 发送会员天数变动提醒邮件。
func sendSubscriptionRewardNotice(mailer Mailer, systemConfigs SystemConfigStore, email string, notice SubscriptionRewardNotice) error {
	if mailer == nil || notice.Days == 0 {
		return nil
	}
	access, err := subscriptionAccess(systemConfigs, Subscription{
		MemberType: notice.MemberType,
		ExpiresAt:  notice.ExpiresAt,
	}, time.Now())
	if err != nil {
		return err
	}
	notice.MemberName = access.MemberName
	notice.RemainingDays = access.RemainingDays
	notice.AllowAutoReply = access.AllowAutoReply
	notice.Features = access.Features
	return mailer.SendSubscriptionReward(email, notice)
}

// subscriptionPlanByID 从系统配置中读取指定订阅套餐。
func (s *PaymentService) subscriptionPlanByID(planID string) (subscriptionPlan, error) {
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return subscriptionPlan{}, ErrNotFound
	}
	plans, err := loadSubscriptionPlans(s.systemConfigs)
	if err != nil {
		return subscriptionPlan{}, err
	}
	for _, plan := range plans {
		if plan.ID == planID && plan.DurationDays > 0 {
			return plan, nil
		}
	}
	return subscriptionPlan{}, ErrNotFound
}

// buildSubscriptionPaymentQuote 计算普通购买或 Plus 升级 Max 的实际支付金额。
func buildSubscriptionPaymentQuote(plans []subscriptionPlan, current Subscription, target subscriptionPlan, now time.Time) (subscriptionPaymentQuote, error) {
	targetAmount := priceToCents(target.OriginalPrice) - priceToCents(target.DiscountAmount)
	if targetAmount < 0 {
		return subscriptionPaymentQuote{}, fmt.Errorf("套餐价格配置不正确")
	}
	currentAccess := subscriptionAccessFromPlans(plans, current, now)
	if currentAccess.Active &&
		currentAccess.MemberType == memberTypeMax &&
		normalizeMemberType(target.MemberType) == memberTypePlus {
		return subscriptionPaymentQuote{}, fmt.Errorf("Max 全能版还在有效期内，暂时不能降为 Plus 基础版")
	}
	quote := subscriptionPaymentQuote{AmountCents: targetAmount}
	if !currentAccess.Active ||
		currentAccess.MemberType != memberTypePlus ||
		normalizeMemberType(target.MemberType) != memberTypeMax {
		return quote, nil
	}
	plusPlan, ok := subscriptionPlanByMemberType(plans, memberTypePlus)
	if !ok {
		return subscriptionPaymentQuote{}, fmt.Errorf("Plus 基础版套餐配置缺失")
	}
	plusAmount := priceToCents(plusPlan.OriginalPrice) - priceToCents(plusPlan.DiscountAmount)
	if plusAmount <= 0 || plusPlan.DurationDays <= 0 {
		return subscriptionPaymentQuote{}, fmt.Errorf("Plus 基础版价格配置不正确")
	}
	remainingSeconds := math.Max(0, current.ExpiresAt.Sub(now).Seconds())
	periodSeconds := time.Duration(plusPlan.DurationDays) * 24 * time.Hour
	credit := int(math.Floor(float64(plusAmount) * remainingSeconds / periodSeconds.Seconds()))
	if credit > targetAmount {
		credit = targetAmount
	}
	quote.UpgradeFromType = memberTypePlus
	quote.UpgradeCreditCents = credit
	quote.AmountCents = targetAmount - credit
	return quote, nil
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
		"id":                       order.ID,
		"order_no":                 order.OrderNo,
		"order_type":               defaultString(order.OrderType, "subscription"),
		"user_email":               order.UserEmail,
		"plan_id":                  order.PlanID,
		"plan_name":                order.PlanName,
		"member_type":              order.MemberType,
		"duration_days":            order.DurationDays,
		"original_amount_cents":    order.OriginalAmountCents,
		"discount_amount_cents":    order.DiscountAmountCents,
		"upgrade_from_member_type": order.UpgradeFromType,
		"upgrade_credit_cents":     order.UpgradeCreditCents,
		"amount_cents":             order.AmountCents,
		"amount":                   centsToYuanString(order.AmountCents),
		"payment_provider":         order.PaymentProvider,
		"trade_no":                 order.TradeNo,
		"status":                   order.Status,
		"paid_at":                  order.PaidAt,
		"expired_at":               order.ExpiredAt,
		"created_at":               order.CreatedAt,
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
