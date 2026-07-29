// Package hliepin 文件作用：验证猎聘猎头端候选人稳定文本和聊天姓名保护规则。
package hliepin

import (
	"context"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// shortcutClickBrowser 记录猎聘快捷搜索点击参数，其余浏览器能力不会在当前测试中调用。
type shortcutClickBrowser struct {
	model.Browser
	request contract.ElementClickRequest
}

// Click 记录猎聘快捷搜索点击参数并返回成功。
func (b *shortcutClickBrowser) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	b.request = request
	return contract.ClickResult{Clicked: true}, nil
}

// TestStableCandidateText 验证动态状态不会进入候选人稳定指纹。
func TestStableCandidateText(t *testing.T) {
	result := stableCandidateText("张三\n28岁\n在线\n立即沟通\n今天活跃")
	if strings.Contains(result, "在线") || strings.Contains(result, "立即沟通") || strings.Contains(result, "活跃") {
		t.Fatalf("稳定文本仍包含动态状态：%q", result)
	}
	if !strings.Contains(result, "张三") || !strings.Contains(result, "28岁") {
		t.Fatalf("稳定文本丢失候选人基础信息：%q", result)
	}
}

// TestStableCandidateName 验证姓名区域中的浏览标记不会参与候选人重新定位。
func TestStableCandidateName(t *testing.T) {
	for input, expected := range map[string]string{
		"冯先生\n阅":    "冯先生",
		"温**\n名片简历": "温**",
	} {
		if actual := stableCandidateName(input); actual != expected {
			t.Fatalf("稳定姓名不正确：input=%q actual=%q expected=%q", input, actual, expected)
		}
	}
}

// TestCandidateNamesMatch 验证完整姓名和脱敏姓名不会明显串台。
func TestCandidateNamesMatch(t *testing.T) {
	if !candidateNamesMatch("张三", "张三先生") {
		t.Fatalf("完整姓名应该匹配")
	}
	if !candidateNamesMatch("张*三", "张三") {
		t.Fatalf("脱敏姓名首字相同应该匹配")
	}
	if candidateNamesMatch("张*三", "李四") {
		t.Fatalf("不同姓氏不应该匹配")
	}
}

// TestSelectPositionUsesExactShortcutName 验证快捷搜索按完整名称点击并等待候选人出现。
func TestSelectPositionUsesExactShortcutName(t *testing.T) {
	browser := &shortcutClickBrowser{}
	cfg := model.Config{
		ID: "hliepin",
		Selectors: map[string]contract.SelectorSpec{
			"position.shortcut_item": {
				Target: contract.SelectorGroup{
					Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".shortcut"}},
				},
				Description: "快捷搜索",
			},
			"candidate.item": {
				Target: contract.SelectorGroup{
					Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".candidate"}},
				},
				Description: "候选人",
			},
		},
	}
	runtime := NewRuntime()

	err := runtime.SelectPosition(
		context.Background(),
		browser,
		cfg,
		model.Position{
			Name:                      "AI应用开发工程师初级可以实习",
			HLiepinShortcutSearchName: "AI应用开发工程师初",
		},
	)

	if err != nil {
		t.Fatalf("选择快捷搜索失败：%v", err)
	}
	if browser.request.Selector.Target.Text != "AI应用开发工程师初" {
		t.Fatalf("快捷搜索名称 = %q", browser.request.Selector.Target.Text)
	}
	if browser.request.Selector.Target.ExactText == nil || !*browser.request.Selector.Target.ExactText {
		t.Fatalf("快捷搜索没有使用完整文本匹配")
	}
	if browser.request.Verify == nil || browser.request.Verify.TargetVisible == nil {
		t.Fatalf("快捷搜索点击后没有等待候选人列表")
	}
	if runtime.selectJobWhenGreeting {
		t.Fatalf("快捷搜索模式不应该在开聊弹框中再次选择岗位")
	}
}
