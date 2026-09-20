// Package common 文件作用：验证候选人姓名短暂缺失时重新读取，禁止把简历正文用作身份或日志标题。
package common

import (
	"bytes"
	"context"
	"log"
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
	lastRequest   contract.ElementFindAllRequest
}

// FindAll 返回内容稳定但姓名字段可能尚未加载的候选人卡片。
func (b *incompleteIdentityBrowser) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	b.calls++
	b.lastRequest = request
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
	items, err := FindCandidates(context.Background(), browser, cfg, "test", model.CandidateListOptions{ReadConcurrency: 10})
	if err != nil || browser.calls != 2 || len(items) != 1 {
		t.Fatalf("重读结果异常：calls=%d items=%d err=%v", browser.calls, len(items), err)
	}
	if browser.lastRequest.ReadConcurrency != 10 {
		t.Fatalf("候选人读取并发数没有传给 Worker：%d", browser.lastRequest.ReadConcurrency)
	}
	selector, err := CandidateSelector(cfg, items[0])
	if err != nil || selector.Description != "测试候选人" || strings.Contains(strings.Join(selector.Target.Texts, "|"), "简历正文") {
		t.Fatalf("身份不应包含简历正文：%+v err=%v", selector, err)
	}
}

// TestFindCandidatesRejectsMissingName 验证持续缺失姓名时有界停止，不泄露正文或按旧序号误点。
func TestFindCandidatesRejectsMissingName(t *testing.T) {
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previous)
	browser := &incompleteIdentityBrowser{alwaysMissing: true}
	cfg := model.Config{
		Selectors:       map[string]contract.SelectorSpec{"candidate.item": {Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".card"}}}}},
		CandidateFields: map[string]contract.SelectorSpec{"name": {}},
	}
	items, err := FindCandidates(context.Background(), browser, cfg, "test", model.CandidateListOptions{})
	if err == nil || browser.calls != 2 || len(items) != 0 || strings.Contains(err.Error(), "简历正文") {
		t.Fatalf("缺少身份必须安全停止：calls=%d items=%d err=%v", browser.calls, len(items), err)
	}
	if !strings.Contains(logs.String(), "attempt=1 returned_count=1 missing_count=1 missing_indices=[0]") || !strings.Contains(logs.String(), "attempt=2 returned_count=1 missing_count=1 missing_indices=[0]") || strings.Contains(logs.String(), "测试候选人") {
		t.Fatalf("诊断信息不完整或包含个人资料：%s", logs.String())
	}
	if browser.lastRequest.ReadConcurrency != 100 {
		t.Fatalf("未配置时应恢复默认并发数 100，实际为 %d", browser.lastRequest.ReadConcurrency)
	}
}
