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
	clickRequest  contract.ElementClickRequest
}

// changingCandidatePageBrowser 模拟旧列表、翻页短暂无列表和新列表依次出现。
type changingCandidatePageBrowser struct {
	model.Browser
	readCount int
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
func (b *numberPageBrowser) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	b.events = append(b.events, "点击页码")
	b.clickRequest = request
	return contract.ClickResult{Clicked: true}, nil
}

// FindAll 按顺序返回第一页旧候选人、短暂缺失和第二页新候选人。
func (b *changingCandidatePageBrowser) FindAll(_ context.Context, _ contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	b.readCount++
	switch b.readCount {
	case 1:
		return []contract.FindAllItem{{
			Index: 0,
			Text:  "旧候选人 25岁",
			Fields: map[string]string{
				"platform_candidate_id": "old-resume-id",
				"name":                  "旧候选人",
				"age":                   "25",
			},
		}}, nil
	case 2:
		return nil, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
	default:
		return []contract.FindAllItem{{
			Index: 0,
			Text:  "新候选人 26岁",
			Fields: map[string]string{
				"platform_candidate_id": "new-resume-id",
				"name":                  "新候选人",
				"age":                   "26",
			},
		}}, nil
	}
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
			"candidate.current_page": {
				Target: contract.SelectorGroup{
					Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".page.active"}},
				},
				Description: "当前页码",
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
	if browser.clickRequest.Verify == nil || browser.clickRequest.Verify.TargetVisible == nil ||
		browser.clickRequest.Verify.TargetVisible.Target.Text != "2" {
		t.Fatalf("数字翻页点击后没有确认当前页码：%+v", browser.clickRequest.Verify)
	}
}

// TestWaitForCandidateListChangeRetriesTemporaryMissing 验证翻页时旧列表不会被误判，短暂无列表后仍会等到新列表。
func TestWaitForCandidateListChangeRetriesTemporaryMissing(t *testing.T) {
	browser := &changingCandidatePageBrowser{}
	cfg := model.Config{
		ID: "hliepin",
		Selectors: map[string]contract.SelectorSpec{
			"candidate.item": {
				Target: contract.SelectorGroup{
					Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".candidate"}},
				},
				Description: "候选人",
			},
		},
		CandidateFields: map[string]contract.SelectorSpec{
			"platform_candidate_id": {
				Target: contract.SelectorGroup{
					Selectors: []contract.SelectorCandidate{{Type: "css", Value: "input[name='resume-id']"}},
				},
			},
		},
	}
	before := []model.Candidate{{
		Fingerprint: "hliepin_old-resume-id",
		Name:        "旧候选人",
		Summary:     "旧候选人 25岁",
		Fields: map[string]string{
			"platform_candidate_id": "old-resume-id",
		},
	}}

	changed, err := waitForCandidateListChange(context.Background(), browser, cfg, "hliepin", before)
	if err != nil || !changed {
		t.Fatalf("翻页列表没有等到新候选人：changed=%v err=%v", changed, err)
	}
	if browser.readCount != 3 {
		t.Fatalf("旧列表或短暂缺失被错误当成翻页完成：读取次数=%d", browser.readCount)
	}
}
