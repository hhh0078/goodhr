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

type detailReadBrowser struct {
	model.Browser
	readCount int
}

type candidateChatBrowser struct {
	model.Browser
	opened        bool
	chatName      string
	nextChatName  string
	clicked       []string
	clickErrors   map[string]error
	readNames     []string
	readIndex     int
	closeVerified bool
	pressCount    int
}

type candidateScrollBrowser struct {
	model.Browser
	request contract.ScrollRequest
}

type detailCloseBrowser struct {
	model.Browser
	pressCount int
}

type detailVerifiedCloseBrowser struct {
	model.Browser
	verified bool
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

// Read 模拟候选人详情经过两次空内容后完成异步加载。
func (b *detailReadBrowser) Read(context.Context, contract.ElementReadRequest) (contract.ReadResult, error) {
	b.readCount++
	if b.readCount < 3 {
		return contract.ReadResult{}, nil
	}
	return contract.ReadResult{Value: "候选人详情已经加载"}, nil
}

// Click 记录聊天框流程点击顺序，并在点击继续沟通后模拟聊天框打开。
func (b *candidateChatBrowser) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	description := request.Selector.Description
	b.clicked = append(b.clicked, description)
	if description == "继续沟通" {
		b.opened = true
		if b.nextChatName != "" {
			b.chatName = b.nextChatName
		}
	}
	if description == "关闭聊天框" {
		b.closeVerified = request.Verify != nil && request.Verify.TargetHidden != nil
		b.opened = false
	}
	if err := b.clickErrors[description]; err != nil {
		return contract.ClickResult{}, err
	}
	return contract.ClickResult{Clicked: true}, nil
}

// PressKey 模拟点击关闭失败后的 Escape 兜底。
func (b *candidateChatBrowser) PressKey(context.Context, contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	b.pressCount++
	b.opened = false
	return contract.KeyboardPressResult{Pressed: true}, nil
}

// Read 模拟读取当前聊天框头部的候选人姓名。
func (b *candidateChatBrowser) Read(_ context.Context, request contract.ElementReadRequest) (contract.ReadResult, error) {
	if len(b.readNames) > 0 {
		index := min(b.readIndex, len(b.readNames)-1)
		b.chatName = b.readNames[index]
		b.readIndex++
	}
	if request.Selector.Description == "聊天姓名" && b.opened && strings.TrimSpace(b.chatName) != "" {
		return contract.ReadResult{Value: b.chatName}, nil
	}
	return contract.ReadResult{}, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
}

// FindAll 模拟候选人聊天框是否已经打开。
func (b *candidateChatBrowser) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if (request.Selector.Description == "聊天框" || request.Selector.Description == "关闭聊天框") && b.opened {
		return []contract.FindAllItem{{Index: 0}}, nil
	}
	if request.Selector.Description == "继续沟通" {
		return []contract.FindAllItem{{Index: 0}}, nil
	}
	return nil, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
}

// Scroll 记录公共候选人滚动传给 Worker 的视口要求。
func (b *candidateScrollBrowser) Scroll(_ context.Context, request contract.ScrollRequest) (contract.ScrollResult, error) {
	b.request = request
	return contract.ScrollResult{}, nil
}

// PressKey 记录关闭详情使用的 Escape 按键次数。
func (b *detailCloseBrowser) PressKey(context.Context, contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	b.pressCount++
	return contract.KeyboardPressResult{}, nil
}

// Click 记录详情关闭按钮是否等待详情正文隐藏。
func (b *detailVerifiedCloseBrowser) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	b.verified = request.Verify != nil && request.Verify.TargetHidden != nil
	return contract.ClickResult{Clicked: true, Verified: b.verified}, nil
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

// TestScrollToCandidateAcceptsPartiallyVisibleCard 验证高卡片只需进入安全区域，不强求整张完整显示。
func TestScrollToCandidateAcceptsPartiallyVisibleCard(t *testing.T) {
	browser := &candidateScrollBrowser{}
	cfg := model.Config{
		ID: "test",
		Selectors: map[string]contract.SelectorSpec{
			"candidate.item": selector("候选人卡片"),
			"candidate.list": selector("候选人列表"),
		},
	}
	err := ScrollToCandidate(context.Background(), browser, cfg, model.Candidate{Index: 0})
	if err != nil {
		t.Fatalf("公共候选人滚动失败：%v", err)
	}
	if browser.request.RequireFull == nil || *browser.request.RequireFull {
		t.Fatalf("候选人卡片不应要求完整显示：%+v", browser.request)
	}
	if browser.request.ViewportMargin != 48 {
		t.Fatalf("候选人滚动应保留 48 像素安全边距：%+v", browser.request)
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

// TestGreetCandidateProbesOptionalConfirmQuickly 验证打招呼成功弹框不存在时只做短探测。
func TestGreetCandidateProbesOptionalConfirmQuickly(t *testing.T) {
	browser := &greetBrowser{}
	cfg := greetTestConfig()
	confirm := selector("打招呼成功弹框关闭按钮")
	confirm.TimeoutMS = 5000
	cfg.Selectors["candidate.greet_confirm"] = confirm
	if err := GreetCandidate(
		context.Background(),
		browser,
		cfg,
		model.Candidate{Index: 0},
		model.GreetRequest{},
	); err != nil {
		t.Fatalf("可选弹框不存在时不应失败：%v", err)
	}
	probed := false
	for _, request := range browser.findRequests {
		if request.Selector.Description != "打招呼成功弹框关闭按钮" {
			continue
		}
		probed = true
		if request.Selector.TimeoutMS != 600 {
			t.Fatalf("可选弹框探测不应等待完整超时：%+v", request.Selector)
		}
	}
	if !probed {
		t.Fatal("没有探测打招呼成功弹框")
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

// TestExtractCandidateDetailWaitsForAsyncContent 验证详情正文异步加载时会定时重读。
func TestExtractCandidateDetailWaitsForAsyncContent(t *testing.T) {
	browser := &detailReadBrowser{}
	cfg := model.Config{
		ID: "test",
		Selectors: map[string]contract.SelectorSpec{
			"candidate.detail": {
				Target: contract.SelectorGroup{
					Selectors: []contract.SelectorCandidate{{Type: "css", Value: "body"}},
				},
				Description: "候选人详情",
			},
		},
	}
	detail, err := ExtractCandidateDetail(context.Background(), browser, cfg)
	if err != nil {
		t.Fatalf("异步详情读取失败：%v", err)
	}
	if detail.Text != "候选人详情已经加载" || browser.readCount != 3 {
		t.Fatalf("详情读取结果不正确：detail=%+v count=%d", detail, browser.readCount)
	}
}

// TestCloseCandidateDetailSkipsDestroyedFrameCheck 验证 Escape 关闭详情后不再查询正在销毁的 iframe。
func TestCloseCandidateDetailSkipsDestroyedFrameCheck(t *testing.T) {
	browser := &detailCloseBrowser{}
	cfg := model.Config{
		ID: "boss",
		Selectors: map[string]contract.SelectorSpec{
			"candidate.detail": selector("详情正文"),
		},
	}
	if err := CloseCandidateDetail(context.Background(), browser, cfg); err != nil {
		t.Fatalf("关闭详情失败：%v", err)
	}
	if browser.pressCount != 1 {
		t.Fatalf("关闭详情应只按一次 Escape：press=%d", browser.pressCount)
	}
}

// TestCloseCandidateDetailWaitsUntilHidden 验证详情关闭动画完成后才把点击记为成功。
func TestCloseCandidateDetailWaitsUntilHidden(t *testing.T) {
	browser := &detailVerifiedCloseBrowser{}
	cfg := model.Config{
		ID: "liepin",
		Selectors: map[string]contract.SelectorSpec{
			"candidate.detail":       selector("详情正文"),
			"candidate.detail_close": selector("详情关闭按钮"),
		},
	}
	if err := CloseCandidateDetail(context.Background(), browser, cfg); err != nil {
		t.Fatalf("关闭详情失败：%v", err)
	}
	if !browser.verified {
		t.Fatal("详情关闭按钮必须验证详情正文已经隐藏")
	}
}

// TestRequestCandidateInfoInChatReusesOpenedChat 验证已有聊天框时不会重复点击候选人继续沟通。
func TestRequestCandidateInfoInChatReusesOpenedChat(t *testing.T) {
	browser := &candidateChatBrowser{opened: true}
	err := RequestCandidateInfoInChat(
		context.Background(),
		browser,
		candidateChatTestConfig(),
		model.Candidate{Index: 0},
		model.CandidateInfoRequest{RequestPhone: true},
	)
	if err != nil {
		t.Fatalf("复用聊天框索要手机号失败：%v", err)
	}
	if actual := strings.Join(browser.clicked, ","); actual != "索要手机号,关闭聊天框" {
		t.Fatalf("已有聊天框时点击顺序不正确：%s", actual)
	}
	if !browser.closeVerified {
		t.Fatal("关闭聊天框时必须验证聊天框已经消失")
	}
}

// TestRequestCandidateInfoInChatOpensMissingChat 验证聊天框不存在时只打开一次再执行索要。
func TestRequestCandidateInfoInChatOpensMissingChat(t *testing.T) {
	browser := &candidateChatBrowser{}
	err := RequestCandidateInfoInChat(
		context.Background(),
		browser,
		candidateChatTestConfig(),
		model.Candidate{Index: 0},
		model.CandidateInfoRequest{RequestPhone: true},
	)
	if err != nil {
		t.Fatalf("打开聊天框索要手机号失败：%v", err)
	}
	if actual := strings.Join(browser.clicked, ","); actual != "继续沟通,索要手机号,关闭聊天框" {
		t.Fatalf("聊天框打开流程顺序不正确：%s", actual)
	}
}

// TestRequestCandidateInfoInChatReopensMismatchedChat 验证已有聊天框属于其他候选人时会先关闭再打开当前候选人。
func TestRequestCandidateInfoInChatReopensMismatchedChat(t *testing.T) {
	browser := &candidateChatBrowser{opened: true, chatName: "李四", nextChatName: "张三"}
	err := RequestCandidateInfoInChat(
		context.Background(),
		browser,
		candidateChatTestConfig(),
		model.Candidate{Index: 0, Name: "张三"},
		model.CandidateInfoRequest{RequestPhone: true},
	)
	if err != nil {
		t.Fatalf("切换到当前候选人聊天框失败：%v", err)
	}
	if actual := strings.Join(browser.clicked, ","); actual != "关闭聊天框,继续沟通,索要手机号,关闭聊天框" {
		t.Fatalf("候选人聊天框切换顺序不正确：%s", actual)
	}
}

// TestRequestCandidateInfoInChatWaitsForChatCandidateSwitch 验证聊天框切换中的旧姓名不会被误判为串台。
func TestRequestCandidateInfoInChatWaitsForChatCandidateSwitch(t *testing.T) {
	browser := &candidateChatBrowser{
		opened: true, readNames: []string{"李四", "张三"},
	}
	err := RequestCandidateInfoInChat(
		context.Background(),
		browser,
		candidateChatTestConfig(),
		model.Candidate{Index: 0, Name: "张三"},
		model.CandidateInfoRequest{RequestPhone: true},
	)
	if err != nil {
		t.Fatalf("等待聊天框切换候选人失败：%v", err)
	}
	if actual := strings.Join(browser.clicked, ","); actual != "索要手机号,关闭聊天框" {
		t.Fatalf("聊天框姓名刷新后不应重新打开：%s", actual)
	}
}

// TestCloseCandidateChatFallsBackToEscape 验证关闭按钮失败时会按 Escape 并再次确认弹框状态。
func TestCloseCandidateChatFallsBackToEscape(t *testing.T) {
	browser := &candidateChatBrowser{
		opened: true,
		clickErrors: map[string]error{
			"关闭聊天框": errors.New("关闭按钮没有生效"),
		},
	}
	if err := CloseCandidateChat(context.Background(), browser, candidateChatTestConfig()); err != nil {
		t.Fatalf("Escape 兜底后聊天框应关闭：%v", err)
	}
	if browser.pressCount != 1 || browser.opened {
		t.Fatalf("聊天框关闭兜底不正确：press=%d opened=%v", browser.pressCount, browser.opened)
	}
}

// TestRequestCandidateInfoContinuesAfterEarlierFailure 验证前一种索要失败时仍会继续尝试后两种。
func TestRequestCandidateInfoContinuesAfterEarlierFailure(t *testing.T) {
	browser := &candidateChatBrowser{
		clickErrors: map[string]error{"索要手机号": errors.New("手机号按钮暂不可用")},
	}
	err := RequestCandidateInfo(
		context.Background(),
		browser,
		candidateChatTestConfig(),
		model.CandidateInfoRequest{RequestPhone: true, RequestWechat: true, RequestResume: true},
	)
	if err == nil || !strings.Contains(err.Error(), "索要手机号失败") {
		t.Fatalf("应返回汇总后的手机号索要错误：%v", err)
	}
	if actual := strings.Join(browser.clicked, ","); actual != "索要手机号,索要微信,索要简历" {
		t.Fatalf("前一步失败后仍应继续尝试：%s", actual)
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

// candidateChatTestConfig 返回聊天框复用测试所需的最小平台配置。
func candidateChatTestConfig() model.Config {
	selector := func(description string) contract.SelectorSpec {
		return contract.SelectorSpec{
			Target: contract.SelectorGroup{
				Selectors: []contract.SelectorCandidate{{Type: "css", Value: "." + description}},
			},
			Description: description,
		}
	}
	return model.Config{
		ID: "test", Name: "测试平台",
		Selectors: map[string]contract.SelectorSpec{
			"candidate.item":           selector("候选人卡片"),
			"candidate.continue":       selector("继续沟通"),
			"candidate.chat_modal":     selector("聊天框"),
			"candidate.chat_name":      selector("聊天姓名"),
			"candidate.chat_close":     selector("关闭聊天框"),
			"candidate.request_phone":  selector("索要手机号"),
			"candidate.request_wechat": selector("索要微信"),
			"candidate.request_resume": selector("索要简历"),
		},
	}
}

// selector 返回公共平台测试使用的最小选择器。
func selector(description string) contract.SelectorSpec {
	return contract.SelectorSpec{
		Target: contract.SelectorGroup{
			Selectors: []contract.SelectorCandidate{{Type: "css", Value: "." + description}},
		},
		Description: description,
	}
}
