// Package job51 文件作用：用独立的合成浏览器验证搜索页招呼只发送一次、弹框内精确选岗和同一候选人的完成证据，不依赖真实页面结构。
package job51

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

const searchGreetingPosition = "软件工程师（半导体）"

type searchGreetingBrowserStub struct {
	model.Browser
	pageURL                   string
	jobNames                  []string
	jobDialog                 bool
	opensJobDialog            bool
	jobDialogDelay            int
	dropdown                  bool
	nativePositionPicker      bool
	verifyNativeSelectors     bool
	selectedJob               string
	selectedReadback          string
	selectedReadbackDelay     int
	selectedReadbackReads     int
	selectedMatchedReads      int
	jobConfirmed              bool
	keepJobDialogAfterConfirm bool
	confirmsGreeting          bool
	optimisticContinue        bool
	contacted                 bool
	continueReads             int
	unconfirmedContinueReads  int
	greetClicks               int
	jobSelectClicks           int
	jobOpenClicks             int
	jobConfirmClicks          int
	jobDialogAfterProbes      int
	jobInputs                 []string
	pageInputs                int
	cancelOnGreet             context.CancelFunc
}

// ListPages 只返回合成的已筛选搜索页，不打开或修改任何真实标签页。
func (browser *searchGreetingBrowserStub) ListPages(context.Context) (contract.PageListResult, error) {
	return contract.PageListResult{Pages: []contract.PageInfo{{PageID: "search-fixture", URL: browser.pageURL, Current: true}}}, nil
}

// Read 按逻辑键返回公开卡片编号或已选岗位，绝不读取真实候选人资料。
func (browser *searchGreetingBrowserStub) Read(_ context.Context, request contract.ElementReadRequest) (contract.ReadResult, error) {
	switch request.Selector.Description {
	case "candidate.list":
		return contract.ReadResult{Value: `<li id="RESUME_CARD_1000010">合成人选 28岁</li>`}, nil
	case "candidate.greet_job_selected":
		if browser.verifyNativeSelectors {
			if err := verifyNativeSearchGreetingSelector(request.Selector); err != nil {
				return contract.ReadResult{}, err
			}
		}
		browser.selectedReadbackReads++
		if browser.selectedReadbackDelay > 0 {
			browser.selectedReadbackDelay--
			return contract.ReadResult{Value: "软件工程师（旧岗位）"}, nil
		}
		if browser.selectedReadback != "" {
			return contract.ReadResult{Value: browser.selectedReadback}, nil
		}
		if browser.selectedJob == searchGreetingPosition {
			browser.selectedMatchedReads++
		}
		return contract.ReadResult{Value: browser.selectedJob}, nil
	default:
		return contract.ReadResult{}, fmt.Errorf("未授权读取搜索条件或未知页面字段：%s", request.Selector.Description)
	}
}

// Scroll 返回合成的完整可见位置，保持产品既有卡片和按钮安全区检查可执行。
func (browser *searchGreetingBrowserStub) Scroll(context.Context, contract.ScrollRequest) (contract.ScrollResult, error) {
	return contract.ScrollResult{After: &contract.ElementView{
		Box:      contract.ElementBox{X: 300, Y: 300, Width: 80, Height: 32},
		Viewport: contract.ViewportSize{Width: 900, Height: 1040}, Visible: true, FullyInViewport: true,
	}}, nil
}

// FindAll 只推进模拟弹框和当前卡片状态，岗位结果必须经过完整名称和唯一性检查。
func (browser *searchGreetingBrowserStub) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	selector := request.Selector
	visible := false
	switch selector.Description {
	case "candidate.greet_blocker", "candidate.greet_dialog":
		return nil, nil
	case "candidate.continue":
		if err := verifySearchGreetingScope(selector); err != nil {
			return nil, err
		}
		visible = browser.contacted
		if visible {
			browser.continueReads++
			if browser.opensJobDialog && !browser.jobConfirmed {
				browser.unconfirmedContinueReads++
			}
		}
	case "candidate.greet_job_dialog":
		if browser.greetClicks > 0 && browser.opensJobDialog && !browser.jobConfirmed {
			browser.jobDialogAfterProbes++
			if browser.jobDialogDelay > 0 {
				browser.jobDialogDelay--
			} else {
				browser.jobDialog = true
			}
		}
		visible = browser.jobDialog
	case "candidate.greet_job_open", "candidate.greet_job_confirm":
		visible = browser.jobDialog
	case "candidate.greet_job_input":
		visible = browser.dropdown || browser.nativePositionPicker && browser.jobDialog
	case "candidate.greet_job_selected":
		visible = (browser.jobDialog || browser.nativePositionPicker) && browser.selectedJob != ""
	case "candidate.greet_job_name":
		if browser.verifyNativeSelectors {
			if err := verifyNativeSearchGreetingSelector(selector); err != nil {
				return nil, err
			}
		}
		if selector.Target.ExactText == nil || !*selector.Target.ExactText || request.MaxItems != 2 || !request.ProbeOnly {
			return nil, fmt.Errorf("选岗前必须精确匹配完整岗位名，并检查是否重名")
		}
		var matches []contract.FindAllItem
		if browser.dropdown || browser.nativePositionPicker && browser.jobDialog {
			for index, name := range browser.jobNames {
				if name == selector.Target.Text {
					matches = append(matches, contract.FindAllItem{Index: index, Text: name, Fields: map[string]string{"position_name": name}})
				}
			}
		}
		return matches, nil
	default:
		return nil, fmt.Errorf("未授权查询搜索条件或未知页面字段：%s", selector.Description)
	}
	if visible {
		return []contract.FindAllItem{{Index: 0}}, nil
	}
	return nil, nil
}

// Input 只允许沟通弹框中的岗位搜索，搜索页关键词和筛选输入一律作为测试失败。
func (browser *searchGreetingBrowserStub) Input(_ context.Context, request contract.ElementInputRequest) (contract.InputResult, error) {
	if request.Selector.Description != "candidate.greet_job_input" || !(browser.dropdown || browser.nativePositionPicker && browser.jobDialog) {
		browser.pageInputs++
		return contract.InputResult{}, fmt.Errorf("不能改写手动搜索页的筛选条件")
	}
	if browser.verifyNativeSelectors {
		if err := verifyNativeSearchGreetingSelector(request.Selector); err != nil {
			return contract.InputResult{}, err
		}
	}
	browser.jobInputs = append(browser.jobInputs, request.Text)
	return contract.InputResult{Typed: true, Verified: true}, nil
}

// Click 记录单次招呼和弹框确认，重复招呼或未核实岗位的确认立即失败。
func (browser *searchGreetingBrowserStub) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	selector := request.Selector
	switch selector.Description {
	case "candidate.greet":
		if err := verifySearchGreetingScope(selector); err != nil {
			return contract.ClickResult{}, err
		}
		browser.greetClicks++
		if browser.greetClicks > 1 || browser.jobDialog {
			return contract.ClickResult{}, fmt.Errorf("招呼不能重发，也不能跨遗留弹框发送")
		}
		if !browser.opensJobDialog {
			browser.contacted = browser.confirmsGreeting
		} else if browser.optimisticContinue {
			browser.contacted = true
		}
		if browser.cancelOnGreet != nil {
			browser.cancelOnGreet()
		}
	case "candidate.greet_job_open":
		browser.jobOpenClicks++
		if browser.nativePositionPicker {
			return contract.ClickResult{}, fmt.Errorf("前程原生职位层没有额外展开按钮")
		}
		if !browser.jobDialog {
			return contract.ClickResult{}, fmt.Errorf("沟通职位弹框尚未打开")
		}
		browser.dropdown = true
	case "candidate.greet_job_name":
		if !(browser.dropdown || browser.nativePositionPicker && browser.jobDialog) || selector.Target.ExactText == nil || !*selector.Target.ExactText {
			return contract.ClickResult{}, fmt.Errorf("只能精确选择已展开的沟通职位")
		}
		if browser.verifyNativeSelectors {
			if err := verifyNativeSearchGreetingSelector(selector); err != nil {
				return contract.ClickResult{}, err
			}
		}
		matches := 0
		for _, name := range browser.jobNames {
			if name == selector.Target.Text {
				matches++
			}
		}
		if matches != 1 {
			return contract.ClickResult{}, fmt.Errorf("同名岗位不唯一，不能猜测")
		}
		browser.selectedJob = selector.Target.Text
		browser.jobSelectClicks++
		browser.dropdown = false
		if browser.nativePositionPicker {
			browser.jobConfirmed = true
			browser.jobDialog = false
			browser.contacted = browser.confirmsGreeting
		}
	case "candidate.greet_job_confirm":
		if browser.nativePositionPicker {
			return contract.ClickResult{}, fmt.Errorf("前程原生职位层没有额外确认按钮")
		}
		if !browser.jobDialog || browser.selectedJob != searchGreetingPosition || browser.selectedMatchedReads == 0 || browser.selectedReadback != "" && browser.selectedReadback != searchGreetingPosition {
			return contract.ClickResult{}, fmt.Errorf("岗位未经完整回显核对，不能确认发送")
		}
		browser.jobConfirmClicks++
		if browser.jobConfirmClicks > 1 {
			return contract.ClickResult{}, fmt.Errorf("沟通职位确认不能重复提交")
		}
		browser.jobConfirmed = true
		browser.jobDialog = browser.keepJobDialogAfterConfirm
		browser.contacted = browser.confirmsGreeting
	default:
		return contract.ClickResult{}, fmt.Errorf("不能操作搜索页条件或未知按钮：%s", selector.Description)
	}
	return contract.ClickResult{Clicked: true, Verified: true}, nil
}

// verifyNativeSearchGreetingSelector 核对正式配置的输入和岗位始终限定原生职位层，回显限定当前聊天区域，不能引用人才搜索栏。
func verifyNativeSearchGreetingSelector(selector contract.SelectorSpec) error {
	parentValue := ".im-position-filter.position-filter-dialog:visible"
	targetValue := ""
	switch selector.Description {
	case "candidate.greet_job_input":
		targetValue = ".position-search input.el-input__inner"
	case "candidate.greet_job_name":
		targetValue = ".position-list-box .position-item .position-name"
	case "candidate.greet_job_selected":
		parentValue = ".chat-container:visible #PositionEmbedOperator:visible"
		targetValue = ".change-position-box .change-position"
	default:
		return fmt.Errorf("未知的原生职位控件：%s", selector.Description)
	}
	for _, variant := range selector.Variants() {
		parentMatched, targetMatched := false, false
		for _, parent := range variant.Parents {
			for _, candidate := range parent.Selectors {
				parentMatched = parentMatched || candidate.Value == parentValue
			}
		}
		for _, candidate := range variant.Target.Selectors {
			targetMatched = targetMatched || candidate.Value == targetValue
			if strings.Contains(candidate.Value, ".search-header") || strings.Contains(candidate.Value, ".talent-box") {
				return fmt.Errorf("沟通职位操作不能引用人才搜索栏")
			}
		}
		if !parentMatched || !targetMatched {
			return fmt.Errorf("正式沟通职位选择器没有限定已取证的原生区域：%s", selector.Description)
		}
	}
	return nil
}

// verifySearchGreetingScope 确认发送和成功探测均使用当前稳定编号，不以旧序号或同名卡片代替。
func verifySearchGreetingScope(selector contract.SelectorSpec) error {
	for _, parent := range selector.Parents {
		if parent.Attributes["id"] == "RESUME_CARD_1000010" && parent.Index != nil && *parent.Index == 0 {
			return nil
		}
	}
	return fmt.Errorf("招呼和继续聊聊必须定位到同一稳定编号卡片")
}

// searchGreetingConfig 使用逻辑键建立合成配置，不引入任何真实沟通弹框的DOM或选择器猜测。
func searchGreetingConfig() model.Config {
	cfg := model.Config{ID: "51job", Name: "前程无忧", ManualSearchURL: "https://mall.51job.com/Revision/online/talent/search", Selectors: make(map[string]contract.SelectorSpec)}
	for _, key := range []string{
		"candidate.list", "candidate.item", "candidate.greet", "candidate.continue", "candidate.greet_blocker",
		"candidate.greet_job_dialog", "candidate.greet_job_open", "candidate.greet_job_input", "candidate.greet_job_name",
		"candidate.greet_job_selected", "candidate.greet_job_confirm",
	} {
		cfg.Selectors[key] = contract.SelectorSpec{
			Description: key, Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".synthetic-fixture"}}},
		}
	}
	return cfg
}

// TestSearchGreetingNativePositionPicker 验证前程原生职位层直接输入、选中后自行收起及异步回显，无展开或确认按钮仍只招呼一次。
func TestSearchGreetingNativePositionPicker(t *testing.T) {
	for _, productionConfig := range []bool{false, true} {
		t.Run(fmt.Sprintf("使用正式配置=%t", productionConfig), func(t *testing.T) {
			cfg := searchGreetingConfig()
			delete(cfg.Selectors, "candidate.greet_job_open")
			delete(cfg.Selectors, "candidate.greet_job_confirm")
			if productionConfig {
				cfg = loadPositionConfig(t)
				if _, configured := cfg.Selectors["candidate.greet_job_open"]; configured {
					t.Fatal("前程正式配置不能增加不存在的展开按钮")
				}
				if _, configured := cfg.Selectors["candidate.greet_job_confirm"]; configured {
					t.Fatal("前程正式配置不能增加不存在的确认按钮")
				}
				for key, selector := range cfg.Selectors {
					cfg.Selectors[key] = selector.MapVariants(func(variant contract.SelectorSpec) contract.SelectorSpec {
						variant.Description = key
						return variant
					})
				}
			}
			browser := &searchGreetingBrowserStub{
				pageURL: cfg.ManualSearchURL + "?keyword=手动人才筛选", opensJobDialog: true,
				jobNames: []string{"软件工程师（自动化）", searchGreetingPosition}, confirmsGreeting: true,
				nativePositionPicker: true, verifyNativeSelectors: productionConfig, selectedReadbackDelay: 2,
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			runtime := NewRuntime()
			if err := runtime.SelectPosition(ctx, browser, cfg, model.Position{Name: searchGreetingPosition, Keyword: "不能覆盖的人才搜索词"}); err != nil {
				t.Fatal(err)
			}
			if err := runtime.GreetCandidate(ctx, browser, cfg, greetCandidateFixture(), model.GreetRequest{}); err != nil {
				t.Fatalf("原生职位层选岗失败：%v", err)
			}
			if browser.greetClicks != 1 || browser.jobSelectClicks != 1 || browser.jobOpenClicks != 0 || browser.jobConfirmClicks != 0 || browser.jobDialog || browser.pageInputs != 0 || browser.continueReads == 0 {
				t.Fatalf("原生选岗不能额外展开、确认或重发：招呼=%d，选岗=%d，展开=%d，确认=%d", browser.greetClicks, browser.jobSelectClicks, browser.jobOpenClicks, browser.jobConfirmClicks)
			}
			if browser.selectedReadbackReads != 3 || browser.selectedMatchedReads == 0 || len(browser.jobInputs) != 1 || browser.jobInputs[0] != "软件工程师" {
				t.Fatal("必须只搜索职位层，并等两次旧值更新为目标完整岗位后才成功")
			}
		})
	}
}

// TestSearchGreetingJobStateMachine 验证晚出现的职位弹框、完整名称唯一性及成功回执，所有场景均禁止重复招呼。
func TestSearchGreetingJobStateMachine(t *testing.T) {
	for _, scenario := range []struct {
		name             string
		browser          searchGreetingBrowserStub
		wantGreetClicks  int
		wantJobConfirms  int
		wantError        bool
		wantDelayedProbe bool
	}{
		{name: "遗留职位弹框拒绝发送", browser: searchGreetingBrowserStub{jobDialog: true}, wantError: true},
		{name: "不弹框直接确认继续聊聊", browser: searchGreetingBrowserStub{confirmsGreeting: true}, wantGreetClicks: 1},
		{name: "晚弹出选岗且跳过相似首项", browser: searchGreetingBrowserStub{opensJobDialog: true, jobDialogDelay: 2, jobNames: []string{"软件工程师（自动化）", searchGreetingPosition}, confirmsGreeting: true}, wantGreetClicks: 1, wantJobConfirms: 1, wantDelayedProbe: true},
		{name: "相似岗位不能代替完整岗位", browser: searchGreetingBrowserStub{opensJobDialog: true, jobNames: []string{"软件工程师（自动化）"}}, wantGreetClicks: 1, wantError: true},
		{name: "完全同名重复岗位停止", browser: searchGreetingBrowserStub{opensJobDialog: true, jobNames: []string{searchGreetingPosition, searchGreetingPosition}}, wantGreetClicks: 1, wantError: true},
		{name: "已选岗位回显不同不能确认", browser: searchGreetingBrowserStub{opensJobDialog: true, jobNames: []string{searchGreetingPosition}, selectedReadback: "软件工程师（自动化）"}, wantGreetClicks: 1, wantError: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			cfg := searchGreetingConfig()
			browser := &scenario.browser
			browser.pageURL = cfg.ManualSearchURL + "?keyword=手动关键词#用户筛选"
			runtime := NewRuntime()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := runtime.SelectPosition(ctx, browser, cfg, model.Position{Name: " " + searchGreetingPosition + " ", Keyword: "不能提交的任务关键词"}); err != nil {
				t.Fatal(err)
			}
			err := runtime.GreetCandidate(ctx, browser, cfg, greetCandidateFixture(), model.GreetRequest{})
			if scenario.wantError && !errors.Is(err, model.ErrGreetingUnconfirmed) || !scenario.wantError && err != nil {
				t.Fatalf("招呼结果错误：%v", err)
			}
			if browser.greetClicks != scenario.wantGreetClicks || browser.jobConfirmClicks != scenario.wantJobConfirms || browser.pageInputs != 0 {
				t.Fatalf("发送或选岗次数错误：招呼=%d，选岗确认=%d，页面输入=%d", browser.greetClicks, browser.jobConfirmClicks, browser.pageInputs)
			}
			if !scenario.wantError && browser.continueReads == 0 {
				t.Fatal("没有同一候选人的继续聊聊证据，不能算发送成功")
			}
			if scenario.wantDelayedProbe && browser.jobDialogAfterProbes < 3 {
				t.Fatal("未持续等待点击后晚出现的职位弹框")
			}
		})
	}
}

// TestSearchGreetingWaitsForDelayedPageState 验证旧岗位回显和乐观更新的卡片不能提前结束流程，最终选岗及发送均只提交一次。
func TestSearchGreetingWaitsForDelayedPageState(t *testing.T) {
	for _, scenario := range []struct {
		name               string
		readbackDelay      int
		optimisticContinue bool
	}{
		{name: "一次旧岗位回显后更新", readbackDelay: 1},
		{name: "两次旧岗位回显后更新", readbackDelay: 2},
		{name: "继续聊聊先更新而职位弹框稍后出现", optimisticContinue: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			cfg := searchGreetingConfig()
			browser := &searchGreetingBrowserStub{
				pageURL: cfg.ManualSearchURL, opensJobDialog: true,
				jobNames: []string{searchGreetingPosition}, confirmsGreeting: true,
				selectedReadbackDelay: scenario.readbackDelay, optimisticContinue: scenario.optimisticContinue,
			}
			if scenario.optimisticContinue {
				browser.jobDialogDelay = 1
			}
			runtime := NewRuntime()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := runtime.SelectPosition(ctx, browser, cfg, model.Position{Name: searchGreetingPosition}); err != nil {
				t.Fatal(err)
			}
			if err := runtime.GreetCandidate(ctx, browser, cfg, greetCandidateFixture(), model.GreetRequest{}); err != nil {
				t.Fatalf("页面延迟更新后应正常完成：%v", err)
			}
			if browser.greetClicks != 1 || browser.jobSelectClicks != 1 || browser.jobConfirmClicks != 1 || !browser.jobConfirmed || browser.jobDialog || browser.pageInputs != 0 {
				t.Fatalf("延迟更新不能导致提前成功或重复提交：招呼=%d，选岗=%d，确认=%d，已确认=%t", browser.greetClicks, browser.jobSelectClicks, browser.jobConfirmClicks, browser.jobConfirmed)
			}
			if browser.selectedReadbackReads < scenario.readbackDelay+1 || browser.selectedMatchedReads == 0 {
				t.Fatal("必须等到旧回显更新为完整目标岗位后才能确认")
			}
			if scenario.optimisticContinue && (browser.unconfirmedContinueReads == 0 || browser.jobDialogAfterProbes < 2) {
				t.Fatal("必须复核乐观更新的继续聊聊，处理随后出现的职位弹框")
			}
		})
	}
}

// TestSearchGreetingInvalidJobNeverResends 验证空任务岗位和确认后未关闭的弹框不能记为成功，也不能重复点击招呼或确认。
func TestSearchGreetingInvalidJobNeverResends(t *testing.T) {
	for _, scenario := range []struct {
		name            string
		positionName    string
		keepDialog      bool
		wantJobSelects  int
		wantJobConfirms int
	}{
		{name: "空岗位进入职位弹框", positionName: " \t"},
		{name: "确认后职位弹框仍未关闭", positionName: searchGreetingPosition, keepDialog: true, wantJobSelects: 1, wantJobConfirms: 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			cfg := searchGreetingConfig()
			browser := &searchGreetingBrowserStub{
				pageURL: cfg.ManualSearchURL, opensJobDialog: true,
				jobNames: []string{searchGreetingPosition}, confirmsGreeting: true,
				keepJobDialogAfterConfirm: scenario.keepDialog,
			}
			ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
			defer cancel()
			runtime := NewRuntime()
			if err := runtime.SelectPosition(ctx, browser, cfg, model.Position{Name: scenario.positionName}); err != nil {
				t.Fatal(err)
			}
			err := runtime.GreetCandidate(ctx, browser, cfg, greetCandidateFixture(), model.GreetRequest{})
			if !errors.Is(err, model.ErrGreetingUnconfirmed) || browser.greetClicks != 1 || browser.jobSelectClicks != scenario.wantJobSelects || browser.jobConfirmClicks != scenario.wantJobConfirms || browser.pageInputs != 0 {
				t.Fatalf("岗位或弹框未确认时不能成功或重发：err=%v，招呼=%d，选岗=%d，确认=%d", err, browser.greetClicks, browser.jobSelectClicks, browser.jobConfirmClicks)
			}
			if scenario.keepDialog && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("弹框保持打开时必须等到确认超时：%v", err)
			}
		})
	}
}

// TestSearchGreetingCancellationNeverResends 验证点击后的等待超时或用户取消只返回未确认，不重新点击招呼。
func TestSearchGreetingCancellationNeverResends(t *testing.T) {
	for _, cancelAfterClick := range []bool{false, true} {
		t.Run(fmt.Sprintf("点击后取消=%t", cancelAfterClick), func(t *testing.T) {
			cfg := searchGreetingConfig()
			browser := &searchGreetingBrowserStub{pageURL: cfg.ManualSearchURL}
			ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
			defer cancel()
			if cancelAfterClick {
				browser.cancelOnGreet = cancel
			}
			runtime := NewRuntime()
			if err := runtime.SelectPosition(ctx, browser, cfg, model.Position{Name: searchGreetingPosition}); err != nil {
				t.Fatal(err)
			}
			err := runtime.GreetCandidate(ctx, browser, cfg, greetCandidateFixture(), model.GreetRequest{})
			cause := context.DeadlineExceeded
			if cancelAfterClick {
				cause = context.Canceled
			}
			if !errors.Is(err, model.ErrGreetingUnconfirmed) || !errors.Is(err, cause) || browser.greetClicks != 1 || browser.jobConfirmClicks != 0 || browser.pageInputs != 0 {
				t.Fatalf("中断后不应重发或误记成功：err=%v，招呼=%d，选岗确认=%d", err, browser.greetClicks, browser.jobConfirmClicks)
			}
		})
	}
}
