// 本文件负责校验 AI 充值活动，并按最高达标档位计算订单赠送快照。
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// aiRechargePromotion 表示一档充值门槛和赠送金额，金额单位均为分。
type aiRechargePromotion struct {
	AmountCents int `json:"amount_cents"`
	BonusCents  int `json:"bonus_cents"`
}

// parseAIRechargePromotions 校验其它系统配置中的活动数组，保留其它配置不参与解析。
func parseAIRechargePromotions(raw string) ([]aiRechargePromotion, error) {
	var config map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &config); err != nil || config == nil {
		return nil, errors.New("其它配置需要是一个 JSON 对象")
	}
	result := []aiRechargePromotion{}
	value, exists := config["ai_recharge_promotions"]
	if !exists {
		return result, nil
	}
	if string(value) == "null" {
		return nil, errors.New("充值活动请填写数组，没有活动时填 []")
	}
	if err := json.Unmarshal(value, &result); err != nil {
		return nil, errors.New("充值活动金额需要填写整数分，例如 100 元填写 10000")
	}
	if len(result) > 50 {
		return nil, errors.New("充值活动最多添加 50 档")
	}
	seen := map[int]bool{}
	for _, item := range result {
		if item.AmountCents < 100 || item.AmountCents > 100000 {
			return nil, errors.New("充值门槛需要在 1 元到 1000 元之间")
		}
		if item.BonusCents <= 0 || item.BonusCents > 100000 {
			return nil, errors.New("赠送金额需要大于 0，且不超过 1000 元")
		}
		if seen[item.AmountCents] {
			return nil, fmt.Errorf("充值门槛 %s 元重复了，请保留一档", centsToYuanString(item.AmountCents))
		}
		seen[item.AmountCents] = true
	}
	sort.Slice(result, func(i, j int) bool { return result[i].AmountCents < result[j].AmountCents })
	return result, nil
}

// loadAIRechargePromotions 读取充值活动；首次未配置时返回空数组，读取异常时停止猜测。
func loadAIRechargePromotions(store SystemConfigStore) ([]aiRechargePromotion, error) {
	if store == nil {
		return nil, errors.New("充值活动暂时没查清楚，请稍后再试")
	}
	config, err := store.Get("system.app_config")
	if errors.Is(err, ErrConfigNotFound) {
		return []aiRechargePromotion{}, nil
	}
	if err != nil {
		return nil, errors.New("充值活动暂时没查清楚，请稍后再试")
	}
	return parseAIRechargePromotions(config.ConfigValue)
}

// rechargeBonusCents 只取达到门槛的最高档活动，不叠加多个赠送。
func rechargeBonusCents(promotions []aiRechargePromotion, amountCents int) int {
	threshold, bonus := 0, 0
	for _, item := range promotions {
		if item.AmountCents <= amountCents && item.AmountCents > threshold {
			threshold, bonus = item.AmountCents, item.BonusCents
		}
	}
	return bonus
}
