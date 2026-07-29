// Package common 文件作用：验证公共岗位选择、名称清理和搜索词规则不会随平台改动退化。
package common

import (
	"context"
	"errors"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

type clickFailureBrowser struct {
	model.Browser
}

type greetBrowser struct {
	model.Browser
	dialogOpened bool
	clicked      []string
	findRequests []contract.ElementFindAllRequest
}

// Click 模拟岗位入口元素找不到。
func (clickFailureBrowser) Click(context.Context, contract.ElementClickRequest) (contract.ClickResult, error) {
	return contract.ClickResult{}, errors.New("ELEMENT_NOT_FOUND")
}

// Click 记录公共打招呼能力实际点击的目标。
func (b *greetBrowser) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	b.clicked = append(b.clicked, request.Selector.Description)
	return contract.ClickResult{Clicked: true}, nil
}

// FindAll 模拟招呼语弹框存在或不存在。
func (b *greetBrowser) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	b.findRequests = append(b.findRequests, request)
	if b.dialogOpened {
		return []contract.FindAllItem{{Index: 0, Text: "选择招呼语"}}, nil
	}
	return nil, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
}

// TestSelectPositionRequiresConfiguredOpenSelector 验证已配置的岗位入口找不到时会立即报错。
func TestSelectPositionRequiresConfiguredOpenSelector(t *testing.T) {
	cfg := model.Config{
		ID: "zhaopin",
		Behavior: model.Behavior{
			DirectPositionSelection: true,
		},
		Selectors: map[string]contract.SelectorSpec{
			"position.open": {
				Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{
					{Type: "css", Value: "a[zp-stat-id='talent_more_jobs']"},
				}},
			},
		},
	}
	err := SelectPosition(context.Background(), clickFailureBrowser{}, cfg, model.Position{Name: "Java开发"})
	if err == nil || !strings.Contains(err.Error(), "打开岗位列表失败") {
		t.Fatalf("岗位入口失败没有立即返回：%v", err)
	}
}

// TestPositionSearchQuery 验证岗位配置后缀和中英文括号备注会被清理。
func TestPositionSearchQuery(t *testing.T) {
	cases := map[string]string{
		"Java开发 _ 上海":    "Java开发",
		"Java开发（初级）":     "Java开发",
		"Java开发(可接受应届生)": "Java开发",
		" Java 开发 ":      "Java 开发",
	}
	for input, expected := range cases {
		if actual := PositionSearchQuery(input); actual != expected {
			t.Fatalf("岗位搜索词不正确：input=%q actual=%q expected=%q", input, actual, expected)
		}
	}
}

// TestPageURLMatches 验证入口地址允许保留用户筛选参数，但会拒绝登录页跳转。
func TestPageURLMatches(t *testing.T) {
	if !PageURLMatches(
		"https://www.zhipin.com/web/chat/recommend?city=101280100",
		"https://www.zhipin.com/web/chat/recommend",
	) {
		t.Fatal("带筛选参数的入口页应当匹配")
	}
	if PageURLMatches(
		"https://login.zhipin.com/?redirect=%2Fweb%2Fchat%2Frecommend",
		"https://www.zhipin.com/web/chat/recommend",
	) {
		t.Fatal("登录页跳转不应被当作入口页")
	}
	if PageURLMatches(
		"https://login.zhipin.com/?redirect=https://www.zhipin.com/web/chat/recommend",
		"https://www.zhipin.com/web/chat/recommend",
	) {
		t.Fatal("查询参数包含目标地址时也不应误判")
	}
}

// TestCandidateFingerprint 验证普通平台候选人仍只按姓名和年龄生成稳定编号。
func TestCandidateFingerprint(t *testing.T) {
	first := CandidateFingerprint("boss", "范召", nil, "范召 29岁 本科 5年 带货主播")
	second := CandidateFingerprint("boss", "范召", nil, "范召 29岁 大专 8年 直播运营")
	if first != "boss_范召_29" || second != first {
		t.Fatalf("同名同年龄候选人编号不稳定：first=%q second=%q", first, second)
	}
	if value := CandidateFingerprint("boss", "范召", nil, "范召 本科 5年"); value != "" {
		t.Fatalf("缺少年龄时不应生成稳定编号：%q", value)
	}
}

// TestGreetCandidateSendsGreetingDialog 验证招呼语弹框出现后才会记录最终发送成功。
func TestGreetCandidateSendsGreetingDialog(t *testing.T) {
	browser := &greetBrowser{dialogOpened: true}
	err := GreetCandidate(
		context.Background(),
		browser,
		greetTestConfig(),
		model.Candidate{Index: 0},
		model.GreetRequest{},
	)
	if err != nil {
		t.Fatalf("打招呼不应失败：%v", err)
	}
	if actual := strings.Join(browser.clicked, ","); actual != "候选人打招呼按钮,招呼语发送按钮" {
		t.Fatalf("点击顺序不正确：%s", actual)
	}
	if len(browser.findRequests) != 1 || !browser.findRequests[0].ExpectedMissing {
		t.Fatalf("弹框检查必须允许元素不存在：%+v", browser.findRequests)
	}
}

// TestGreetCandidateSkipsMissingGreetingDialog 验证用户关闭招呼语弹框后可以直接继续处理候选人。
func TestGreetCandidateSkipsMissingGreetingDialog(t *testing.T) {
	browser := &greetBrowser{}
	err := GreetCandidate(
		context.Background(),
		browser,
		greetTestConfig(),
		model.Candidate{Index: 0},
		model.GreetRequest{},
	)
	if err != nil {
		t.Fatalf("弹框不存在时不应失败：%v", err)
	}
	if actual := strings.Join(browser.clicked, ","); actual != "候选人打招呼按钮" {
		t.Fatalf("弹框不存在时不应点击发送：%s", actual)
	}
}

// greetTestConfig 返回公共打招呼弹框测试使用的最小平台配置。
func greetTestConfig() model.Config {
	selector := func(description string) contract.SelectorSpec {
		return contract.SelectorSpec{
			Target: contract.SelectorGroup{
				Selectors: []contract.SelectorCandidate{{Type: "css", Value: "." + description}},
			},
			Description: description,
		}
	}
	return model.Config{
		ID: "test",
		Selectors: map[string]contract.SelectorSpec{
			"candidate.item":         selector("候选人卡片"),
			"candidate.greet":        selector("候选人打招呼按钮"),
			"candidate.greet_dialog": selector("招呼语弹框"),
			"candidate.greet_send":   selector("招呼语发送按钮"),
		},
	}
}
