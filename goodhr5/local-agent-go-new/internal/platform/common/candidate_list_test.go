// Package common 文件作用：验证候选人数字页码定位和翻页公共规则。
package common

import (
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestNumberedPageSelectorUsesExactPageText 验证数字翻页始终使用精确页码且不继承列表序号。
func TestNumberedPageSelectorUsesExactPageText(t *testing.T) {
	index := 4
	cfg := model.Config{
		ID: "test", Name: "测试平台",
		Selectors: map[string]contract.SelectorSpec{
			"candidate.page_number": {
				Target: contract.SelectorGroup{
					Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".page"}},
					Index:     &index,
				},
				Description: "数字页码",
			},
		},
	}
	selector, err := numberedPageSelector(cfg, "candidate.page_number", 3)
	if err != nil {
		t.Fatalf("numberedPageSelector() error = %v", err)
	}
	if selector.Target.Text != "3" || selector.Target.ExactText == nil || !*selector.Target.ExactText {
		t.Fatalf("selector = %+v", selector)
	}
	if selector.Target.Index != nil {
		t.Fatalf("数字页码不应该保留列表序号：%+v", selector.Target.Index)
	}
}
