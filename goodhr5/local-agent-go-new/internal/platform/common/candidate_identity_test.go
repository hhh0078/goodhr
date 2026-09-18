// Package common 文件作用：验证候选人姓名短暂缺失时重新读取，禁止把简历正文用作身份或日志标题。
package common

import (
	"context"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// incompleteIdentityBrowser 模拟第一次读取姓名为空，以及字段恢复或持续缺失。
type incompleteIdentityBrowser struct {
	model.Browser
	calls         int
	alwaysMissing bool
}

// FindAll 返回内容稳定但姓名字段可能尚未加载的候选人卡片。
func (b *incompleteIdentityBrowser) FindAll(context.Context, contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	b.calls++
	name := ""
	if b.calls > 1 && !b.alwaysMissing {
		name = "测试候选人"
	}
	return []contract.FindAllItem{{Index: 0, Text: "测试候选人 25岁 本科 刚刚活跃 简历正文 打招呼", Fields: map[string]string{"name": name}}}, nil
}

// TestFindCandidatesRetriesMissingName 验证字段恢复后只使用姓名和年龄定位，不包含变化的卡片正文。
func TestFindCandidatesRetriesMissingName(t *testing.T) {
	browser := &incompleteIdentityBrowser{}
	cfg := model.Config{
		Selectors:       map[string]contract.SelectorSpec{"candidate.item": {Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".card"}}}}},
		CandidateFields: map[string]contract.SelectorSpec{"name": {}},
	}
	items, err := FindCandidates(context.Background(), browser, cfg, "test")
	if err != nil || browser.calls != 2 || len(items) != 1 {
		t.Fatalf("重读结果异常：calls=%d items=%d err=%v", browser.calls, len(items), err)
	}
	selector, err := CandidateSelector(cfg, items[0])
	if err != nil || selector.Description != "测试候选人" || strings.Contains(strings.Join(selector.Target.Texts, "|"), "简历正文") {
		t.Fatalf("身份不应包含简历正文：%+v err=%v", selector, err)
	}
}

// TestFindCandidatesRejectsMissingName 验证持续缺失姓名时有界停止，不泄露正文或按旧序号误点。
func TestFindCandidatesRejectsMissingName(t *testing.T) {
	browser := &incompleteIdentityBrowser{alwaysMissing: true}
	cfg := model.Config{
		Selectors:       map[string]contract.SelectorSpec{"candidate.item": {Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".card"}}}}},
		CandidateFields: map[string]contract.SelectorSpec{"name": {}},
	}
	items, err := FindCandidates(context.Background(), browser, cfg, "test")
	if err == nil || browser.calls != 2 || len(items) != 0 || strings.Contains(err.Error(), "简历正文") {
		t.Fatalf("缺少身份必须安全停止：calls=%d items=%d err=%v", browser.calls, len(items), err)
	}
}
