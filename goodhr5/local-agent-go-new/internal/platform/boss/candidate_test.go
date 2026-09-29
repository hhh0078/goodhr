// Package boss 文件作用：验证完整卡片含热搜牛人时无条件跳过，且不检查按钮、不改变相邻候选人的定位。
package boss

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// recommendationBrowser 只提供整批卡片读取，任何额外按钮检查都会返回错误。
type recommendationBrowser struct {
	model.Browser
	items    []contract.FindAllItem
	listErr  error
	requests []contract.ElementFindAllRequest
}

// FindAll 记录读取次数并禁止按钮探测，其他页面操作因未实现而无法执行。
func (b *recommendationBrowser) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	b.requests = append(b.requests, request)
	if request.ProbeOnly || len(request.Fields) == 0 {
		return nil, errors.New("不得额外检查热搜牛人卡片的按钮")
	}
	return b.items, b.listErr
}

// recommendationConfig 读取实际发布配置，确保完整卡片文字随列表一并读取。
func recommendationConfig(t *testing.T) model.Config {
	t.Helper()
	content, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg model.Config
	if err := json.Unmarshal(content, &cfg); err != nil {
		t.Fatal(err)
	}
	if field, ok := cfg.CandidateFields["card_text"]; !ok || len(field.Target.Selectors) == 0 {
		t.Fatal("候选人列表必须读取完整卡片文字")
	}
	return cfg
}

// TestHotSearchCardKeepsOriginalList 验证外层标题命中时直接跳过，后续同名普通卡片保留原序号和出现次数。
func TestHotSearchCardKeepsOriginalList(t *testing.T) {
	browser := &recommendationBrowser{items: []contract.FindAllItem{
		{Index: 10, Text: "文先生 32岁", Fields: map[string]string{"name": "文先生", "card_text": "文先生 32岁 打招呼"}},
		{Index: 11, Text: "王先生 34岁 热搜 首页推荐 打招呼", Fields: map[string]string{"name": "王先生", "card_text": "热搜牛人推荐 查看更多 王先生 34岁 热搜 首页推荐 打招呼"}},
		{Index: 12, Text: "王先生 34岁 打招呼", Fields: map[string]string{"name": "王先生", "card_text": "王先生 34岁 打招呼"}},
	}}
	candidates, err := NewRuntime().FindCandidates(context.Background(), browser, recommendationConfig(t), model.CandidateListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 3 || candidates[0].SkipReason != "" || candidates[1].SkipReason == "" || candidates[2].SkipReason != "" {
		t.Fatalf("只应跳过完整卡片含热搜牛人的候选人：%+v", candidates)
	}
	for index, candidate := range candidates {
		if candidate.Index != index+10 || candidate.IdentityTexts[0] != candidate.Name {
			t.Fatalf("原序号或稳定定位文字被改变：%+v", candidate)
		}
	}
	if candidates[1].IdentityOccurrence != 0 || candidates[2].IdentityOccurrence != 1 {
		t.Fatalf("跳过后仍须保留同名候选人的出现次数：%+v", candidates)
	}
	if len(browser.requests) != 1 {
		t.Fatalf("只允许整批文字读取，不允许额外检查按钮：%+v", browser.requests)
	}
}

// TestHotSearchTextAlwaysSkips 验证四个字出现在卡片任意位置或被空白分开时均跳过，不取决于评分或按钮。
func TestHotSearchTextAlwaysSkips(t *testing.T) {
	for _, test := range []struct {
		name, summary, cardText string
	}{
		{"外层标题且有按钮", "尚先生 31岁 打招呼", "热搜牛人推荐 尚先生 31岁 打招呼"},
		{"卡片正文", "尚先生 31岁 热搜牛人 打招呼", "尚先生 31岁 热搜牛人 打招呼"},
		{"不同标签间换行", "尚先生 31岁", "热搜\n牛人推荐 尚先生 31岁"},
		{"标记位于结尾", "尚先生 31岁", "尚先生 31岁 工作经历 热搜牛人"},
		{"正文已命中但外层读取为空", "尚先生 热搜牛人推荐", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			browser := &recommendationBrowser{items: []contract.FindAllItem{{Index: 7, Text: test.summary, Fields: map[string]string{"name": "尚先生", "card_text": test.cardText}}}}
			candidates, err := NewRuntime().FindCandidates(context.Background(), browser, recommendationConfig(t), model.CandidateListOptions{})
			if err != nil || len(candidates) != 1 || candidates[0].SkipReason == "" || len(browser.requests) != 1 {
				t.Fatalf("热搜牛人必须直接跳过且不得检查按钮：候选人=%+v 请求=%d 错误=%v", candidates, len(browser.requests), err)
			}
		})
	}
}

// TestOrdinaryCardsRemainEligible 验证单独热搜标签、首页推荐标签及普通卡片不被这四个字的规则误伤。
func TestOrdinaryCardsRemainEligible(t *testing.T) {
	for _, text := range []string{"王先生 34岁 打招呼", "王先生 34岁 热搜 打招呼", "王先生 34岁 首页推荐 打招呼"} {
		browser := &recommendationBrowser{items: []contract.FindAllItem{{Text: text, Fields: map[string]string{"name": "王先生", "card_text": text}}}}
		candidates, err := NewRuntime().FindCandidates(context.Background(), browser, recommendationConfig(t), model.CandidateListOptions{})
		if err != nil || len(candidates) != 1 || candidates[0].SkipReason != "" || len(browser.requests) != 1 {
			t.Fatalf("未包含热搜牛人的卡片应继续正常处理：候选人=%+v 错误=%v", candidates, err)
		}
	}
}

// TestUnreadableWholeCardDoesNotAllowActions 验证完整文字读取失败时暂不操作，避免漏掉外层热搜标题后误点。
func TestUnreadableWholeCardDoesNotAllowActions(t *testing.T) {
	browser := &recommendationBrowser{items: []contract.FindAllItem{{Text: "尚先生 31岁", Fields: map[string]string{"name": "尚先生"}}}}
	if candidates, err := NewRuntime().FindCandidates(context.Background(), browser, recommendationConfig(t), model.CandidateListOptions{}); err == nil || len(candidates) != 0 {
		t.Fatalf("完整卡片未读到时不得继续点击：候选人=%+v 错误=%v", candidates, err)
	}
}

// TestCandidateListFailureIsPreserved 验证取消及浏览器故障仍按真实错误返回，不伪装成正常跳过。
func TestCandidateListFailureIsPreserved(t *testing.T) {
	for _, listErr := range []error{context.Canceled, errors.New("浏览器连接断开")} {
		browser := &recommendationBrowser{listErr: listErr}
		if _, err := NewRuntime().FindCandidates(context.Background(), browser, recommendationConfig(t), model.CandidateListOptions{}); !errors.Is(err, listErr) {
			t.Fatalf("列表真实故障必须保留：期望=%v 实际=%v", listErr, err)
		}
	}
}
