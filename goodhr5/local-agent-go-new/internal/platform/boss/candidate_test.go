// Package boss 文件作用：验证热搜推荐卡片只在确认无打招呼入口时正常跳过，并保持后续普通卡片的稳定定位。
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

// recommendationBrowser 模拟列表读取和推荐卡片内的只读入口检查。
type recommendationBrowser struct {
	model.Browser
	items    []contract.FindAllItem
	buttons  []contract.FindAllItem
	probeErr error
	probes   []contract.ElementFindAllRequest
}

// FindAll 返回原列表或记录推荐卡片入口检查，不允许任何点击动作。
func (b *recommendationBrowser) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if request.ProbeOnly {
		b.probes = append(b.probes, request)
		return b.buttons, b.probeErr
	}
	return b.items, nil
}

// recommendationConfig 读取随程序发布的真实 Boss 配置，验证推荐标签及入口选择器一并生效。
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
	marker, ok := cfg.CandidateFields["recommendation_marker"]
	if !ok || marker.Target.ExactText == nil || !*marker.Target.ExactText {
		t.Fatal("推荐标签必须读取独立标签，并精确匹配文字")
	}
	return cfg
}

// TestRecommendationWithoutGreetingKeepsOriginalList 验证推荐卡片跳过后普通卡片的原始序号和定位文本仍正确。
func TestRecommendationWithoutGreetingKeepsOriginalList(t *testing.T) {
	for _, probeErr := range []error{nil, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}} {
		browser := &recommendationBrowser{
			probeErr: probeErr,
			items: []contract.FindAllItem{
				{Index: 10, Text: "文先生 32岁", Fields: map[string]string{"name": "文先生"}},
				{Index: 11, Text: "尚先生 31岁 热搜 首页推荐", Fields: map[string]string{"name": "尚先生", "recommendation_marker": "热搜"}},
				{Index: 12, Text: "王先生 34岁 打招呼", Fields: map[string]string{"name": "王先生"}},
			},
		}
		candidates, err := NewRuntime().FindCandidates(context.Background(), browser, recommendationConfig(t), model.CandidateListOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(candidates) != 3 || candidates[0].SkipReason != "" || candidates[1].SkipReason == "" || candidates[2].SkipReason != "" {
			t.Fatalf("只应标记无入口的推荐卡片：%+v", candidates)
		}
		for index, candidate := range candidates {
			if candidate.Index != index+10 || candidate.IdentityTexts[0] != candidate.Name {
				t.Fatalf("列表原序号或稳定定位文字被改变：%+v", candidate)
			}
		}
		if len(browser.probes) != 1 {
			t.Fatalf("只能检查推荐卡片的入口：%+v", browser.probes)
		}
		probe := browser.probes[0]
		if !probe.ExpectedMissing || !probe.Diagnostic || probe.Selector.State != "attached" || len(probe.Selector.Parents) == 0 || probe.Selector.Parents[0].Texts[0] != "尚先生" {
			t.Fatalf("入口检查必须只读、记录诊断且绑定推荐卡片：%+v", probe)
		}
	}
}

// TestRecommendationWithGreetingStillProcessed 验证有打招呼入口的推荐候选人不会被误跳过。
func TestRecommendationWithGreetingStillProcessed(t *testing.T) {
	browser := &recommendationBrowser{
		items:   []contract.FindAllItem{{Index: 7, Text: "推荐候选人 热搜 打招呼", Fields: map[string]string{"name": "推荐候选人", "recommendation_marker": "热搜"}}},
		buttons: []contract.FindAllItem{{Index: 0}},
	}
	candidates, err := NewRuntime().FindCandidates(context.Background(), browser, recommendationConfig(t), model.CandidateListOptions{})
	if err != nil || len(candidates) != 1 || candidates[0].SkipReason != "" {
		t.Fatalf("可以打招呼的推荐候选人应继续处理：候选人=%+v 错误=%v", candidates, err)
	}
}

// TestRecommendationProbeFailureIsNotNormalSkip 验证取消、遮挡和浏览器故障不会被伪装成推荐卡片正常跳过。
func TestRecommendationProbeFailureIsNotNormalSkip(t *testing.T) {
	for _, probeErr := range []error{context.Canceled, errors.New("浏览器连接断开"), &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_VISIBLE"}}} {
		browser := &recommendationBrowser{
			items:    []contract.FindAllItem{{Index: 0, Text: "尚先生 首页推荐", Fields: map[string]string{"name": "尚先生", "recommendation_marker": "首页推荐"}}},
			probeErr: probeErr,
		}
		if _, err := NewRuntime().FindCandidates(context.Background(), browser, recommendationConfig(t), model.CandidateListOptions{}); !errors.Is(err, probeErr) {
			t.Fatalf("入口检查的真实故障必须原样保留：期望=%v 实际=%v", probeErr, err)
		}
	}
}
