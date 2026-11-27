// Package httpapi 本文件定义时间套餐、独立使用额度和请求发起时的资金归属，保护按量充值本金。
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

const aiTimePlansKey = "system.ai_time_plans"

// AITimePlan 表示按后台价格购买的使用期限，余额单位为分，不作为可退款本金。
type AITimePlan struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	DurationDays int    `json:"duration_days"`
	PriceCents   int64  `json:"price_cents"`
	BalanceCents int64  `json:"balance_cents"`
	Enabled      bool   `json:"enabled"`
}

// AITimeAccount 保存一轮时间套餐额度；同一有效期续费沿用编号，过期重购建立新账期。
type AITimeAccount struct {
	ID           string    `json:"id"`
	UserEmail    string    `json:"-"`
	PlanID       string    `json:"plan_id"`
	PlanName     string    `json:"plan_name"`
	ExpiresAt    time.Time `json:"expires_at"`
	BalanceUnits int64     `json:"balance_units"`
}

// active 判断当前账期是否仍有效，不把旧会员记录当作时间套餐。
func (a AITimeAccount) active(now time.Time) bool { return a.ID != "" && a.ExpiresAt.After(now) }

// aiTimeTerms 保存下单时的套餐额度快照，价格与购买天数由原支付订单保存。
type aiTimeTerms struct{ BalanceUnits int64 }

// AITimeStore 定义时间额度独立账本；所有资金修改和对应流水必须在同一锁或事务完成。
type AITimeStore interface {
	Current(email string, now time.Time) (AITimeAccount, error)
	Account(email, id string, now time.Time) (AITimeAccount, error)
	SaveTerms(order PaymentOrder, terms aiTimeTerms) error
	Grant(order PaymentOrder, now time.Time) (AITimeAccount, error)
	Adjust(record AIWalletRecord, accountID string, now time.Time) (int64, error)
	Expire(now time.Time) error
}

// timeStoreOf 复用原钱包具体类型，兼容旧接口及历史测试存根。
func timeStoreOf(wallet AIWalletStore) AITimeStore {
	switch value := wallet.(type) {
	case *MemoryAIWalletStore:
		return value.timePackages
	case *PostgresAIWalletStore:
		return value.timePackages
	default:
		return nil
	}
}

// aiUsageFunding 固定本次请求的资金归属，防止等待或重试期间到期后误扣按量本金。
type aiUsageFunding struct{ AccountID string }

// captureAIUsageFunding 在上游请求前获取服务端账期快照；客户端不能指定扣费账户。
func captureAIUsageFunding(wallet AIWalletStore, email string) (aiUsageFunding, error) {
	store := timeStoreOf(wallet)
	if store == nil {
		return aiUsageFunding{}, nil
	}
	account, err := store.Current(email, time.Now())
	if err != nil {
		return aiUsageFunding{}, err
	}
	if account.active(time.Now()) {
		return aiUsageFunding{AccountID: account.ID}, nil
	}
	return aiUsageFunding{}, nil
}

// bind 把只在服务端生成的资金归属交给原扣费入口。
func (f aiUsageFunding) bind(record *AIWalletRecord) {
	record.FundingAccountID, record.FundingBalance = f.AccountID, f.AccountID == ""
}

// timeBalanceSnapshot 返回有效套餐的可用额度，过期时自动回收但不改按量账本。
func timeBalanceSnapshot(store AITimeStore, email string) (int64, bool, error) {
	if store == nil {
		return 0, false, nil
	}
	now := time.Now()
	account, err := store.Current(email, now)
	return account.BalanceUnits, account.active(now), err
}

// routeTimeAdjustment 将 AI 扣费和有效期内的人工调额计入套餐；充值、赠送和退款仍走原账本。
func routeTimeAdjustment(store AITimeStore, record AIWalletRecord) (int64, bool, error) {
	if store == nil || record.FundingBalance {
		return 0, false, nil
	}
	now := time.Now()
	if record.FundingAccountID != "" {
		balance, err := store.Adjust(record, record.FundingAccountID, now)
		return balance, true, err
	}
	if record.Category != "ai_usage" && record.Category != "admin_adjust" {
		return 0, false, nil
	}
	account, err := store.Current(record.UserEmail, now)
	if err != nil {
		return 0, true, err
	}
	if !account.active(now) {
		return 0, false, nil
	}
	balance, err := store.Adjust(record, account.ID, now)
	return balance, true, err
}

// balanceForFunding 在重试前检查同一账期，不借用新套餐或按量本金继续旧请求。
func balanceForFunding(wallet AIWalletStore, email string, funding aiUsageFunding) (int64, error) {
	if funding.AccountID != "" {
		store := timeStoreOf(wallet)
		if store == nil {
			return 0, errors.New("套餐账本暂时不可用")
		}
		account, err := store.Account(email, funding.AccountID, time.Now())
		return account.BalanceUnits, err
	}
	switch value := wallet.(type) {
	case *MemoryAIWalletStore:
		return value.regularBalanceUnits(email)
	case *PostgresAIWalletStore:
		return value.regularBalanceUnits(email)
	default:
		return wallet.BalanceUnits(email)
	}
}

// publicTimeAccount 只向账户本人返回当前套餐摘要，过期后的页面显示按量余额。
func publicTimeAccount(wallet AIWalletStore, email string) (map[string]any, error) {
	store := timeStoreOf(wallet)
	if store == nil {
		return map[string]any{"active": false}, nil
	}
	now := time.Now()
	account, err := store.Current(email, now)
	if err != nil {
		return nil, err
	}
	return map[string]any{"active": account.active(now), "plan_id": account.PlanID, "plan_name": account.PlanName, "expires_at": account.ExpiresAt, "balance_units": account.BalanceUnits}, nil
}

// timeAwareBalanceMessage 为有效套餐不足提供补额提示，不引导用户充值到未使用的按量余额。
func timeAwareBalanceMessage(wallet AIWalletStore, email, fallback string) string {
	if store := timeStoreOf(wallet); store != nil {
		if account, err := store.Current(email, time.Now()); err == nil && account.active(time.Now()) {
			return "套餐可用额度不足，请联系作者补充额度，或续费后再试。"
		}
	}
	return fallback
}

// TimePlans 返回后台启用的时间套餐，不暴露服务商密钥或管理配置。
func (s *PaymentService) TimePlans(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	plans, err := loadAITimePlans(s.systemConfigs)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	enabled := []AITimePlan{}
	for _, plan := range plans {
		if plan.Enabled {
			enabled = append(enabled, plan)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"plans": enabled})
}

// timeAccountReason 标明独立额度来源，避免把虚拟额度当成充值本金。
func timeAccountReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "套餐额度调整"
	}
	return fmt.Sprintf("%s（时间套餐额度）", reason)
}

// loadAITimePlans 严格读取后台套餐，配置错误时不使用猜测价格创建支付订单。
func loadAITimePlans(store SystemConfigStore) ([]AITimePlan, error) {
	cfg, err := store.Get(aiTimePlansKey)
	if errors.Is(err, ErrConfigNotFound) {
		return []AITimePlan{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return []AITimePlan{}, nil
	}
	return parseAITimePlans(cfg.ConfigValue)
}

// parseAITimePlans 校验价格、天数和额度边界，避免空套餐或溢出进入支付和钱包。
func parseAITimePlans(raw string) ([]AITimePlan, error) {
	var plans []AITimePlan
	if json.Unmarshal([]byte(raw), &plans) != nil || plans == nil || len(plans) > 20 {
		return nil, errors.New("时间套餐需填写数组，最多20个")
	}
	seen := map[string]bool{}
	for _, p := range plans {
		if strings.TrimSpace(p.ID) == "" || len(p.ID) > 60 || seen[p.ID] || strings.TrimSpace(p.Name) == "" || len(p.Name) > 120 || p.DurationDays < 1 || p.DurationDays > 3660 || p.PriceCents < 1 || p.PriceCents > 10000000 || p.BalanceCents < 1 || p.BalanceCents > 100000000 {
			return nil, errors.New("套餐编号不可重复；天数1–3660，价格及额度须为有效分数值")
		}
		seen[p.ID] = true
	}
	return plans, nil
}

// CreateTimeOrder 使用后台套餐价格与额度快照创建微信支付，客户端不能自行指定权益。
func (s *PaymentService) CreateTimeOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, 401, "session invalid or expired")
		return
	}
	var req createPaymentOrderRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, 400, "套餐信息没读完整，请重新选择")
		return
	}
	plans, err := loadAITimePlans(s.systemConfigs)
	if err != nil {
		writeError(w, 503, err.Error())
		return
	}
	var selected AITimePlan
	for _, p := range plans {
		if p.ID == req.PlanID && p.Enabled {
			selected = p
			break
		}
	}
	if selected.ID == "" {
		writeError(w, 400, "这个套餐暂时不能购买，请重新选择")
		return
	}
	store := timeStoreOf(s.aiWallet)
	provider := s.providers[defaultPaymentProvider]
	if store == nil || provider == nil {
		writeError(w, 503, "套餐支付暂时没准备好，请稍后再试")
		return
	}
	expires := time.Now().Add(30 * time.Minute)
	order, err := s.orders.Create(PaymentOrder{OrderNo: generatePaymentOrderNo(), OrderType: "ai_time", UserEmail: session.Email, PlanID: selected.ID, PlanName: selected.Name, DurationDays: selected.DurationDays, MemberType: memberTypeMax, OriginalAmountCents: int(selected.PriceCents), AmountCents: int(selected.PriceCents), PaymentProvider: defaultPaymentProvider, Status: "pending", ExpiredAt: &expires})
	if err == nil {
		err = store.SaveTerms(order, aiTimeTerms{BalanceUnits: selected.BalanceCents * aiWalletUnitsPerCent})
	}
	if err != nil {
		writeError(w, 500, "套餐订单没保存成功，请稍后再试")
		return
	}
	payment, err := provider.CreateOrder(r.Context(), PaymentProviderOrderInput{OrderNo: order.OrderNo, Title: "GoodHR " + selected.Name, AmountCents: order.AmountCents, Remark: "user:" + session.Email + ",type:ai_time"})
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "order": publicPaymentOrder(order), "payment": payment})
}

// startAITimeExpiry 定期清理过期套餐，失败仅记服务日志并在下一轮继续，读写入口仍会即时核对期限。
func startAITimeExpiry(ctx context.Context, store AITimeStore) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if err := store.Expire(now); err != nil {
					log.Printf("回收过期套餐额度失败: %v", err)
				}
			}
		}
	}()
}

// regularWalletSnapshot 读取按量资金，不让套餐在页面请求期间到期或续费造成余额与期限串账。
func regularWalletSnapshot(wallet AIWalletStore, email string) (int64, int64, error) {
	switch value := wallet.(type) {
	case *MemoryAIWalletStore:
		return value.regularBalanceSnapshot(email)
	case *PostgresAIWalletStore:
		return value.regularBalanceSnapshot(email)
	default:
		return wallet.BalanceSnapshot(email)
	}
}

// timeTopUpUnits 计算只补足不减少的续费额度，并拒绝极端欠费导致的整数溢出。
func timeTopUpUnits(balance, target int64) (int64, error) {
	if balance >= target {
		return 0, nil
	}
	if balance == -1<<63 {
		return 0, errors.New("套餐额度超出可记账范围")
	}
	return checkedAIWalletAdd(target, -balance)
}
