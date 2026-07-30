// Package common 文件作用：验证候选人数字页码定位和翻页公共规则。
package common

import (
	"context"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// numberPageBrowser 记录数字翻页的查找、真实滚轮和点击顺序。
type numberPageBrowser struct {
	model.Browser
	events        []string
	scrollRequest contract.ScrollRequest
}

// FindAll 返回数字页码或翻页后的候选人。
func (b *numberPageBrowser) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if strings.Contains(request.Selector.Description, "第 2 页") {
		b.events = append(b.events, "查找页码")
		return []contract.FindAllItem{{Index: 0, Text: "2"}}, nil
	}
	b.events = append(b.events, "读取候选人")
	return []contract.FindAllItem{{
		Index: 0,
		Text:  "张三 25岁",
		Fields: map[string]string{
			"name": "张三",
			"age":  "25",
		},
	}}, nil
}

// Scroll 记录数字页码点击前的真实滚轮参数。
func (b *numberPageBrowser) Scroll(_ context.Context, request contract.ScrollRequest) (contract.ScrollResult, error) {
	b.events = append(b.events, "滚动页码")
	b.scrollRequest = request
	return contract.ScrollResult{}, nil
}

// Click 记录数字页码物理点击。
func (b *numberPageBrowser) Click(_ context.Context, _ contract.ElementClickRequest) (contract.ClickResult, error) {
	b.events = append(b.events, "点击页码")
	return contract.ClickResult{Clicked: true}, nil
}

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

// TestAdvanceCandidateNumberPageScrollsBeforeClick 验证数字页码必须先用真实滚轮进入视口再点击。
func TestAdvanceCandidateNumberPageScrollsBeforeClick(t *testing.T) {
	browser := &numberPageBrowser{}
	cfg := model.Config{
		ID: "test", Name: "测试平台",
		Selectors: map[string]contract.SelectorSpec{
			"candidate.page_number": {
				Target: contract.SelectorGroup{
					Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".page"}},
				},
				Description: "数字页码",
			},
			"candidate.item": {
				Target: contract.SelectorGroup{
					Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".candidate"}},
				},
				Description: "候选人",
			},
			"candidate.list": {
				Target: contract.SelectorGroup{
					Selectors: []contract.SelectorCandidate{{Type: "css", Value: "body"}},
				},
				Description: "候选人列表",
			},
		},
	}
	advanced, err := AdvanceCandidateNumberPage(
		context.Background(),
		browser,
		cfg,
		"test",
		"candidate.page_number",
		2,
		nil,
	)
	if err != nil || !advanced {
		t.Fatalf("数字翻页失败：advanced=%v err=%v", advanced, err)
	}
	if actual := strings.Join(browser.events, ","); actual != "查找页码,滚动页码,点击页码,读取候选人" {
		t.Fatalf("数字翻页顺序不正确：%s", actual)
	}
	if browser.scrollRequest.Target == nil || browser.scrollRequest.WheelAnchor == nil ||
		browser.scrollRequest.MaxAttempts < 20 {
		t.Fatalf("数字页码没有使用足够的真实滚轮定位：%+v", browser.scrollRequest)
	}
}
