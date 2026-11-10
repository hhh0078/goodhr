// Package zhaopin_test 文件作用：用模拟页面验证智联手动搜索的发招呼选岗和发送确认。
package zhaopin_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/platform/zhaopin"
)

const greetingJobName = "小学语文老师（双语）"

type greetingPageBrowser struct {
	model.Browser
	pageURL             string
	names               []string
	selected            string
	selectedReadback    string
	opensJobDialog      bool
	wantsPhrase         bool
	canConfirmGreeting  bool
	confirmationDelay   int
	contacted           bool
	jobDialog           bool
	dropdown            bool
	phraseDialog        bool
	operations          []string
	jobDialogProbes     int
	contactedStateReads int
}

// ListPages 返回合成的活动搜索页或推荐页。
func (b *greetingPageBrowser) ListPages(context.Context) (contract.PageListResult, error) {
	return contract.PageListResult{Pages: []contract.PageInfo{{PageID: "fixture", URL: b.pageURL, Current: true}}}, nil
}

// FindAll 返回弹框、岗位列表和同一卡片的模拟状态。
func (b *greetingPageBrowser) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	selector := request.Selector
	visible := false
	switch selector.Description {
	case "candidate.continue":
		if len(selector.Parents) == 0 || !slices.Contains(selector.Parents[len(selector.Parents)-1].Texts, "合成候选人") {
			return nil, fmt.Errorf("继续沟通状态必须绑定到指定候选人")
		}
		visible = b.contacted
		if visible {
			if b.confirmationDelay > 0 {
				b.confirmationDelay--
				return nil, nil
			}
			b.contactedStateReads++
		}
	case "candidate.greet_job_dialog":
		b.jobDialogProbes++
		visible = b.jobDialog
	case "candidate.greet_job_open", "candidate.greet_job_confirm":
		visible = b.jobDialog
	case "candidate.greet_job_input":
		visible = b.dropdown
	case "candidate.greet_job_selected":
		visible = b.jobDialog && b.selected != "" && (selector.Target.Text == "" || selector.Target.Text == b.selected)
	case "candidate.greet_job_name":
		if selector.Target.ExactText == nil || !*selector.Target.ExactText || !request.ProbeOnly || request.MaxItems != 2 {
			return nil, fmt.Errorf("岗位必须精确匹配并确认没有重名")
		}
		var items []contract.FindAllItem
		if b.dropdown {
			for i, name := range b.names {
				if selector.Target.Text == name {
					items = append(items, contract.FindAllItem{Index: i})
				}
			}
		}
		return items, nil
	case "candidate.greet_dialog", "candidate.greet_send":
		visible = b.phraseDialog
	default:
		return nil, fmt.Errorf("意外探测选择器 %s", selector.Description)
	}
	if visible {
		return []contract.FindAllItem{{Index: 0, Text: b.selected}}, nil
	}
	return nil, nil
}

// Read 读取弹框显示的已选岗位，模拟未选时没有目标文字。
func (b *greetingPageBrowser) Read(_ context.Context, request contract.ElementReadRequest) (contract.ReadResult, error) {
	if request.Selector.Description != "candidate.greet_job_selected" {
		return contract.ReadResult{}, fmt.Errorf("意外读取选择器 %s", request.Selector.Description)
	}
	if b.selectedReadback != "" {
		return contract.ReadResult{Value: b.selectedReadback}, nil
	}
	if b.selected == "" {
		return contract.ReadResult{}, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
	}
	return contract.ReadResult{Value: b.selected}, nil
}

// Input 只允许在岗位弹框的下拉框内输入岗位搜索词。
func (b *greetingPageBrowser) Input(_ context.Context, request contract.ElementInputRequest) (contract.InputResult, error) {
	b.operations = append(b.operations, "输入:"+request.Text)
	if request.Selector.Description != "candidate.greet_job_input" || !b.dropdown {
		return contract.InputResult{}, fmt.Errorf("不能修改人才搜索页的筛选条件")
	}
	return contract.InputResult{Typed: true, Verified: true}, nil
}

// Click 推进模拟页面状态，记录每次选岗和发送动作。
func (b *greetingPageBrowser) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	selector := request.Selector
	b.operations = append(b.operations, selector.Description)
	switch selector.Description {
	case "candidate.greet":
		b.jobDialog = b.opensJobDialog
		if !b.jobDialog {
			b.phraseDialog = b.wantsPhrase
			b.contacted = !b.phraseDialog && b.canConfirmGreeting
		}
	case "candidate.greet_job_open":
		b.dropdown = b.jobDialog
	case "candidate.greet_job_name":
		if !b.dropdown {
			return contract.ClickResult{}, fmt.Errorf("岗位下拉框尚未打开")
		}
		if selector.Target.ExactText == nil || !*selector.Target.ExactText || !slices.Contains(b.names, selector.Target.Text) {
			return contract.ClickResult{}, fmt.Errorf("选择器未锁定列表中的岗位")
		}
		b.selected = selector.Target.Text
		b.dropdown = false
	case "candidate.greet_job_confirm":
		if b.selected != greetingJobName || !b.jobDialog {
			return contract.ClickResult{}, fmt.Errorf("不能用未核实的岗位确认招呼")
		}
		b.jobDialog = false
		b.phraseDialog = b.wantsPhrase
		b.contacted = !b.phraseDialog && b.canConfirmGreeting
	case "candidate.greet_send":
		if !b.phraseDialog {
			return contract.ClickResult{}, fmt.Errorf("没有招呼语弹框")
		}
		b.phraseDialog = false
		b.contacted = b.canConfirmGreeting
	default:
		return contract.ClickResult{}, fmt.Errorf("意外点击选择器 %s", selector.Description)
	}
	return contract.ClickResult{Clicked: true, Verified: true}, nil
}

// loadGreetingFixture 使用实际页面地址和简化的模拟选择器测试平台步骤。
func loadGreetingFixture(t *testing.T) model.Config {
	t.Helper()
	cfg, err := platform.LoadConfig("zhaopin")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Selectors = make(map[string]contract.SelectorSpec)
	for _, key := range []string{
		"candidate.item", "candidate.greet", "candidate.continue",
		"candidate.greet_job_dialog", "candidate.greet_job_open", "candidate.greet_job_input",
		"candidate.greet_job_name", "candidate.greet_job_selected",
		"candidate.greet_job_confirm", "candidate.greet_dialog", "candidate.greet_send",
	} {
		cfg.Selectors[key] = contract.SelectorSpec{
			Description: key,
			Target:      contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".fixture"}}},
		}
	}
	return cfg
}

// TestSearchGreetingSelectsAndConfirmsJob 验证搜索页只在招呼弹框内选岗，发送后必须看到同一张卡片变为继续沟通。
func TestSearchGreetingSelectsAndConfirmsJob(t *testing.T) {
	cfg := loadGreetingFixture(t)
	candidate := model.Candidate{Index: 3, Name: "合成候选人", IdentityTexts: []string{"合成候选人"}}
	for _, test := range []struct {
		name       string
		browser    greetingPageBrowser
		operations []string
		sentinel   error
		timeout    time.Duration
	}{
		{
			name: "跳过相似首项并精确选岗后发送招呼语",
			browser: greetingPageBrowser{
				names:          []string{"小学语文老师（普通班）", greetingJobName},
				opensJobDialog: true, wantsPhrase: true, canConfirmGreeting: true, confirmationDelay: 2,
			},
			operations: []string{"candidate.greet", "candidate.greet_job_open", "输入:小学语文老师", "candidate.greet_job_name", "candidate.greet_job_confirm", "candidate.greet_send"},
		},
		{
			name:       "平台无需选岗时只点击一次招呼",
			browser:    greetingPageBrowser{canConfirmGreeting: true},
			operations: []string{"candidate.greet"},
		},
		{
			name:     "已有遗留岗位弹框时停止",
			browser:  greetingPageBrowser{jobDialog: true},
			sentinel: model.ErrGreetingUnconfirmed,
		},
		{
			name:       "只有相似岗位时停止",
			browser:    greetingPageBrowser{opensJobDialog: true, names: []string{"小学语文老师（普通班）"}},
			operations: []string{"candidate.greet", "candidate.greet_job_open", "输入:小学语文老师"},
			sentinel:   model.ErrGreetingUnconfirmed,
		},
		{
			name:       "有两个完全同名岗位时停止",
			browser:    greetingPageBrowser{opensJobDialog: true, names: []string{greetingJobName, greetingJobName}},
			operations: []string{"candidate.greet", "candidate.greet_job_open", "输入:小学语文老师"},
			sentinel:   model.ErrGreetingUnconfirmed,
		},
		{
			name:       "选岗后回显不同岗位时停止",
			browser:    greetingPageBrowser{opensJobDialog: true, names: []string{greetingJobName}, selectedReadback: "其他岗位"},
			operations: []string{"candidate.greet", "candidate.greet_job_open", "输入:小学语文老师", "candidate.greet_job_name"},
			sentinel:   model.ErrGreetingUnconfirmed,
		},
		{
			name:     "已沟通候选人不再点击",
			browser:  greetingPageBrowser{contacted: true},
			sentinel: model.ErrCandidateAlreadyContacted,
		},
		{
			name:       "发送点击成功但卡片状态未知时停止",
			browser:    greetingPageBrowser{wantsPhrase: true},
			operations: []string{"candidate.greet", "candidate.greet_send"},
			sentinel:   model.ErrGreetingUnconfirmed,
			timeout:    650 * time.Millisecond,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			browser := &test.browser
			browser.pageURL = cfg.ManualSearchURL + "?keyword=用户手动筛选"
			timeout := test.timeout
			if timeout == 0 {
				timeout = 3 * time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			runtime := zhaopin.NewRuntime()
			if err := runtime.SelectPosition(ctx, browser, cfg, model.Position{Name: greetingJobName, Keyword: "任务筛选词"}); err != nil || len(browser.operations) != 0 {
				t.Fatalf("任务启动时应保留用户筛选：%v %v", browser.operations, err)
			}
			err := runtime.GreetCandidate(ctx, browser, cfg, candidate, model.GreetRequest{})
			if !errors.Is(err, test.sentinel) {
				t.Fatalf("发送结果不符合预期：%v", err)
			}
			if !slices.Equal(browser.operations, test.operations) {
				t.Fatalf("浏览器动作 = %v，期望 %v", browser.operations, test.operations)
			}
			if err == nil && browser.contactedStateReads == 0 {
				t.Fatal("未读到同一候选人的继续沟通状态，不能记为发送成功")
			}
		})
	}
}

// TestRecommendGreetingKeepsExistingFlow 验证推荐页仍处理原招呼语弹框，无需进入搜索页选岗和新增状态检查。
func TestRecommendGreetingKeepsExistingFlow(t *testing.T) {
	cfg := loadGreetingFixture(t)
	browser := &greetingPageBrowser{pageURL: cfg.EntryURL, wantsPhrase: true}
	candidate := model.Candidate{Name: "合成候选人", IdentityTexts: []string{"合成候选人"}}
	if err := zhaopin.NewRuntime().GreetCandidate(context.Background(), browser, cfg, candidate, model.GreetRequest{}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(browser.operations, []string{"candidate.greet", "candidate.greet_send"}) || browser.jobDialogProbes != 0 {
		t.Fatalf("推荐页原招呼流程改变：%v", browser.operations)
	}
}
