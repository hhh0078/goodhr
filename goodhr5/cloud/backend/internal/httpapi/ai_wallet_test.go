// 本文件负责测试内置 AI 钱包扣费精度和 token 用量计费。
package httpapi

import "testing"

// TestAIUsageCostUnitsUsesFourDecimalPrecision 验证 AI 扣费按 0.0001 元精度向上取整。
func TestAIUsageCostUnitsUsesFourDecimalPrecision(t *testing.T) {
	model := builtinAIModel{
		ID:                    "qwen3.5-plus",
		InputPricePer1MCents:  80,
		OutputPricePer1MCents: 480,
	}

	cost := aiUsageCostUnits(model, 181, 4361)
	if cost != 211 {
		t.Fatalf("cost units = %d, want 211", cost)
	}
	if got := aiUnitsToYuanString(cost); got != "0.0211" {
		t.Fatalf("cost yuan = %q, want 0.0211", got)
	}
}
