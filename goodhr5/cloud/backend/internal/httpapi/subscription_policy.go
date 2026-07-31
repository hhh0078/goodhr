// 本文件负责统一解析会员套餐、计算会员权限和校验后台套餐配置。
package httpapi

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	memberTypeFree = "free"
	memberTypePlus = "plus"
	memberTypeMax  = "max"
)

// subscriptionPlan 表示后台系统配置中的一个会员套餐。
type subscriptionPlan struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	MemberType     string   `json:"member_type"`
	DurationDays   int      `json:"duration_days"`
	OriginalPrice  float64  `json:"original_price"`
	DiscountAmount float64  `json:"discount_amount"`
	AllowAutoReply *bool    `json:"allow_auto_reply"`
	Features       []string `json:"features"`
	Description    string   `json:"description"`
	CreatedAt      string   `json:"created_at"`
}

// SubscriptionAccess 表示根据会员类型、到期时间和套餐配置计算出的统一权限。
type SubscriptionAccess struct {
	Active         bool
	MemberType     string
	MemberName     string
	ExpiresAt      time.Time
	RemainingDays  int
	RemainingSecs  int64
	AllowAI        bool
	AllowAutoReply bool
	Features       []string
}

// loadSubscriptionPlans 从系统配置读取并校验全部会员套餐。
func loadSubscriptionPlans(store SystemConfigStore) ([]subscriptionPlan, error) {
	if store == nil {
		return nil, fmt.Errorf("会员套餐配置暂时不可用")
	}
	cfg, err := store.Get("system.subscription_plans")
	if err != nil {
		return nil, err
	}
	return parseSubscriptionPlans(cfg.ConfigValue)
}

// parseSubscriptionPlans 解析并校验会员套餐 JSON。
func parseSubscriptionPlans(raw string) ([]subscriptionPlan, error) {
	var plans []subscriptionPlan
	if err := json.Unmarshal([]byte(raw), &plans); err != nil {
		return nil, fmt.Errorf("会员套餐配置不是有效 JSON：%w", err)
	}
	if len(plans) == 0 {
		return nil, fmt.Errorf("会员套餐不能为空")
	}
	seenIDs := make(map[string]struct{}, len(plans))
	seenMemberTypes := make(map[string]struct{}, len(plans))
	for index := range plans {
		plan := &plans[index]
		plan.ID = strings.TrimSpace(plan.ID)
		plan.Name = strings.TrimSpace(plan.Name)
		plan.MemberType = normalizeMemberType(plan.MemberType)
		plan.Features = trimStringList(plan.Features)
		if plan.ID == "" || plan.Name == "" {
			return nil, fmt.Errorf("第 %d 个套餐缺少 id 或 name", index+1)
		}
		if _, exists := seenIDs[plan.ID]; exists {
			return nil, fmt.Errorf("套餐 id %s 重复了", plan.ID)
		}
		seenIDs[plan.ID] = struct{}{}
		if !supportedMemberType(plan.MemberType) {
			return nil, fmt.Errorf("套餐 %s 的 member_type 只能是 free、plus 或 max", plan.Name)
		}
		if _, exists := seenMemberTypes[plan.MemberType]; exists {
			return nil, fmt.Errorf("会员类型 %s 只能配置一个套餐", plan.MemberType)
		}
		seenMemberTypes[plan.MemberType] = struct{}{}
		if plan.AllowAutoReply == nil {
			return nil, fmt.Errorf("套餐 %s 缺少 allow_auto_reply", plan.Name)
		}
		if plan.OriginalPrice < 0 || plan.DiscountAmount < 0 {
			return nil, fmt.Errorf("套餐 %s 的价格和优惠金额不能小于 0", plan.Name)
		}
		if plan.MemberType != memberTypeFree {
			if plan.DurationDays <= 0 {
				return nil, fmt.Errorf("套餐 %s 的 duration_days 必须大于 0", plan.Name)
			}
			if priceToCents(plan.OriginalPrice)-priceToCents(plan.DiscountAmount) < 0 {
				return nil, fmt.Errorf("套餐 %s 的优惠金额不能超过原价", plan.Name)
			}
		}
	}
	for _, required := range []string{memberTypeFree, memberTypePlus, memberTypeMax} {
		if _, exists := seenMemberTypes[required]; !exists {
			return nil, fmt.Errorf("会员套餐缺少 %s 类型", required)
		}
	}
	return plans, nil
}

// subscriptionAccess 根据会员记录和套餐配置计算当前统一权限。
func subscriptionAccess(store SystemConfigStore, subscription Subscription, now time.Time) (SubscriptionAccess, error) {
	plans, err := loadSubscriptionPlans(store)
	if err != nil {
		return SubscriptionAccess{}, err
	}
	return subscriptionAccessFromPlans(plans, subscription, now), nil
}

// subscriptionAccessFromPlans 使用已加载的套餐计算会员权限。
func subscriptionAccessFromPlans(plans []subscriptionPlan, subscription Subscription, now time.Time) SubscriptionAccess {
	memberType := normalizeMemberType(subscription.MemberType)
	remaining := subscription.ExpiresAt.Sub(now)
	active := (memberType == memberTypePlus || memberType == memberTypeMax) && remaining > 0
	access := SubscriptionAccess{
		Active:        active,
		MemberType:    memberType,
		MemberName:    memberTypeName(memberType),
		ExpiresAt:     subscription.ExpiresAt,
		RemainingDays: remainingDays(remaining),
		Features:      []string{},
		AllowAI:       active,
	}
	if remaining > 0 {
		access.RemainingSecs = int64(math.Ceil(remaining.Seconds()))
	}
	if plan, ok := subscriptionPlanByMemberType(plans, memberType); ok {
		access.Features = append([]string(nil), plan.Features...)
		access.AllowAutoReply = active && planAllowsAutoReply(plan)
	}
	return access
}

// publicSubscriptionAccess 转换统一会员权限为 HTTP 响应。
func publicSubscriptionAccess(access SubscriptionAccess) map[string]any {
	return map[string]any{
		"member_type":       access.MemberType,
		"member_name":       access.MemberName,
		"expires_at":        access.ExpiresAt,
		"active":            access.Active,
		"remaining_days":    access.RemainingDays,
		"remaining_seconds": access.RemainingSecs,
		"allow_ai":          access.AllowAI,
		"allow_auto_reply":  access.AllowAutoReply,
		"features":          access.Features,
	}
}

// subscriptionPlanByMemberType 按会员类型查找对应套餐。
func subscriptionPlanByMemberType(plans []subscriptionPlan, memberType string) (subscriptionPlan, bool) {
	target := normalizeMemberType(memberType)
	for _, plan := range plans {
		if normalizeMemberType(plan.MemberType) == target {
			return plan, true
		}
	}
	return subscriptionPlan{}, false
}

// planAllowsAutoReply 返回套餐是否允许使用自动回复。
func planAllowsAutoReply(plan subscriptionPlan) bool {
	return plan.AllowAutoReply != nil && *plan.AllowAutoReply
}

// memberTypeName 返回会员类型对应的中文名称。
func memberTypeName(memberType string) string {
	switch normalizeMemberType(memberType) {
	case memberTypeMax:
		return "Max 全能版"
	case memberTypePlus:
		return "Plus 基础版"
	default:
		return "免费版"
	}
}

// normalizeMemberType 标准化会员类型。
func normalizeMemberType(memberType string) string {
	return strings.ToLower(strings.TrimSpace(memberType))
}

// supportedMemberType 判断会员类型是否受当前系统支持。
func supportedMemberType(memberType string) bool {
	switch normalizeMemberType(memberType) {
	case memberTypeFree, memberTypePlus, memberTypeMax:
		return true
	default:
		return false
	}
}

// remainingDays 把剩余时长向上取整为用户可读天数。
func remainingDays(remaining time.Duration) int {
	if remaining <= 0 {
		return 0
	}
	return int(math.Ceil(remaining.Hours() / 24))
}
