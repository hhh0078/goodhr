// Package common 文件作用：验证备用定位路径仍绑定相同候选人身份、列表序号和操作父级。
package common

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// variantReadBrowser 记录配置驱动的读取请求，避免真实页面操作。
type variantReadBrowser struct {
	model.Browser
	request contract.ElementReadRequest
}

// Read 记录读取参数并返回固定测试值。
func (b *variantReadBrowser) Read(_ context.Context, request contract.ElementReadRequest) (contract.ReadResult, error) {
	b.request = request
	return contract.ReadResult{Value: "测试结果"}, nil
}

// variantTestConfig 创建父级不同的候选人卡片和动作备用路径，用于检查完整作用域组合。
func variantTestConfig(t *testing.T) model.Config {
	t.Helper()
	var cfg model.Config
	content := `{"id":"test","selectors":{"candidate.item":[{"parents":[{"selectors":[{"type":"css","value":".list-a"}]}],"target":{"selectors":[{"type":"css","value":".card-a"}]}},{"parents":[{"selectors":[{"type":"css","value":".list-b"}]},{"selectors":[{"type":"css","value":".group-b"}]}],"target":{"selectors":[{"type":"css","value":".card-b"}]}}],"candidate.greet":[{"target":{"selectors":[{"type":"css","value":".greet-a"}]}},{"parents":[{"selectors":[{"type":"css","value":".actions-b"}]}],"target":{"selectors":[{"type":"css","value":".greet-b"}]}}],"candidate.page_number":[{"target":{"selectors":[{"type":"css","value":".page-a"}]}},{"target":{"selectors":[{"type":"css","value":".page-b"}]}}]}}`
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestCandidateActionVariantsKeepIdentity 验证每个备用动作都在同一候选人卡片内，且不会污染原配置。
func TestCandidateActionVariantsKeepIdentity(t *testing.T) {
	cfg := variantTestConfig(t)
	candidate := model.Candidate{Index: 8, Name: "测试候选人", IdentityTexts: []string{"测试候选人", "28岁"}, IdentityOccurrence: 1}
	selector, err := CandidateActionSelector(cfg, "candidate.greet", candidate)
	if err != nil {
		t.Fatal(err)
	}
	variants := selector.Variants()
	if len(variants) != 4 {
		t.Fatalf("父级和动作的完整路径组合数量错误：%d", len(variants))
	}
	for _, variant := range variants {
		foundCard := false
		for _, parent := range variant.Parents {
			if parent.Selectors[0].Value != ".card-a" && parent.Selectors[0].Value != ".card-b" {
				continue
			}
			foundCard = true
			if parent.Index == nil || *parent.Index != 1 || !reflect.DeepEqual(parent.Texts, candidate.IdentityTexts) {
				t.Fatalf("备用路径丢失候选人身份：%+v", parent)
			}
		}
		if !foundCard {
			t.Fatalf("动作脱离候选人卡片：%+v", variant)
		}
	}
	for _, original := range cfg.Selectors["candidate.item"].Variants() {
		if original.Target.Index != nil || len(original.Target.Texts) != 0 {
			t.Fatalf("候选人定位污染了复用配置：%+v", original)
		}
	}
}

// TestIndexedAndScopedVariantsKeepIndex 验证无身份文本时所有备用路径仍使用指定序号。
func TestIndexedAndScopedVariantsKeepIndex(t *testing.T) {
	cfg := variantTestConfig(t)
	indexed, err := IndexedSelector(cfg, "candidate.item", 6)
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range indexed.Variants() {
		if variant.Target.Index == nil || *variant.Target.Index != 6 {
			t.Fatalf("备用路径丢失列表序号：%+v", variant)
		}
	}
	scoped, err := CandidateScopedSelector(cfg, "candidate.greet", 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range scoped.Variants() {
		for _, parent := range variant.Parents {
			if parent.Selectors[0].Value == ".card-a" || parent.Selectors[0].Value == ".card-b" {
				if parent.Index == nil || *parent.Index != 4 {
					t.Fatalf("备用动作丢失父级序号：%+v", parent)
				}
			}
		}
	}
	page, err := numberedPageSelector(cfg, "candidate.page_number", 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range page.Variants() {
		if variant.Target.Text != "3" || variant.Target.ExactText == nil || !*variant.Target.ExactText || variant.Target.Index != nil {
			t.Fatalf("备用分页路径没有绑定精确页码：%+v", variant)
		}
	}
}

// TestLegacyCandidateActionScopeUnchanged 验证没有数组的旧对象仍沿用原有父级拼接顺序。
func TestLegacyCandidateActionScopeUnchanged(t *testing.T) {
	group := func(value string) contract.SelectorGroup {
		return contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: value}}}
	}
	cfg := model.Config{Selectors: map[string]contract.SelectorSpec{
		"candidate.item":  {Parents: []contract.SelectorGroup{group(".list")}, Target: group(".card")},
		"candidate.greet": {Parents: []contract.SelectorGroup{group(".actions")}, Target: group(".greet")},
	}}
	actual, err := CandidateActionSelector(cfg, "candidate.greet", model.Candidate{Index: 2})
	if err != nil {
		t.Fatal(err)
	}
	index := 2
	card := group(".card")
	card.Index = &index
	expected := contract.SelectorSpec{Parents: []contract.SelectorGroup{group(".list"), card, group(".actions")}, Target: group(".greet"), Description: "candidate.greet"}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("旧对象的作用域发生变化：得到%+v，期望%+v", actual, expected)
	}
}

// TestOptionalReadOnlyOptsIntoVariantMetadata 验证只有新备用路径读取才启用命中方案的读取方式。
func TestOptionalReadOnlyOptsIntoVariantMetadata(t *testing.T) {
	selector := contract.SelectorSpec{Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".name"}}}, ReadProperty: "text"}
	browser := &variantReadBrowser{}
	cfg := model.Config{Selectors: map[string]contract.SelectorSpec{"name": selector}}
	if _, found, err := ReadOptional(context.Background(), browser, cfg, "name"); err != nil || !found || browser.request.UseSelectorRead || browser.request.Property != "text" {
		t.Fatalf("旧对象读取请求发生变化：%+v，错误：%v", browser.request, err)
	}
	selector.Alternatives = []contract.SelectorSpec{{Target: selector.Target, ReadAttribute: "title"}}
	cfg.Selectors["name"] = selector
	if _, found, err := ReadOptional(context.Background(), browser, cfg, "name"); err != nil || !found || !browser.request.UseSelectorRead {
		t.Fatalf("备用读取没有启用命中路径设置：%+v，错误：%v", browser.request, err)
	}
}
