// Package hliepin 文件作用：验证猎聘猎头端候选人稳定文本、岗位匹配和聊天姓名保护规则。
package hliepin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// shortcutClickBrowser 记录猎聘快捷搜索点击参数，其余浏览器能力不会在当前测试中调用。
type shortcutClickBrowser struct {
	model.Browser
	request     contract.ElementClickRequest
	pressedKeys []string
	currentPage string
}

type greetingJobBrowser struct {
	model.Browser
	items        []contract.FindAllItem
	findRequest  contract.ElementFindAllRequest
	clicks       []contract.ElementClickRequest
	scrolls      []contract.ScrollRequest
	selectedName string
	itemClickErr error
}

// promotionBrowser 模拟猎聘开聊后推广弹框及 Escape 兜底关闭行为。
type promotionBrowser struct {
	model.Browser
	opened            bool
	pressCount        int
	scrollCount       int
	scrollWhileOpened bool
}

// Click 记录猎聘快捷搜索点击参数并返回成功。
func (b *shortcutClickBrowser) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	b.request = request
	return contract.ClickResult{Clicked: true}, nil
}

// PressKey 记录猎聘选择快捷搜索前用于回到页面顶部的真实按键。
func (b *shortcutClickBrowser) PressKey(_ context.Context, request contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	b.pressedKeys = append(b.pressedKeys, request.Key)
	return contract.KeyboardPressResult{Pressed: true}, nil
}

// Read 返回猎聘当前激活的候选人页码。
func (b *shortcutClickBrowser) Read(_ context.Context, request contract.ElementReadRequest) (contract.ReadResult, error) {
	if request.Selector.Description == "当前页码" && strings.TrimSpace(b.currentPage) != "" {
		return contract.ReadResult{Value: b.currentPage}, nil
	}
	return contract.ReadResult{}, &contract.WorkerError{
		Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"},
	}
}

// Click 记录猎聘开聊职位下拉和职位选项的点击参数。
func (b *greetingJobBrowser) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	b.clicks = append(b.clicks, request)
	if request.Selector.Description == "开聊职位列表项" && len(b.items) > 0 {
		b.selectedName = b.items[0].Fields["position_name"]
		if b.itemClickErr != nil {
			return contract.ClickResult{}, b.itemClickErr
		}
	}
	return contract.ClickResult{Clicked: true}, nil
}

// Read 返回猎聘开聊弹框当前已经选中的岗位名称。
func (b *greetingJobBrowser) Read(_ context.Context, request contract.ElementReadRequest) (contract.ReadResult, error) {
	if request.Selector.Description == "当前已选开聊职位" && strings.TrimSpace(b.selectedName) != "" {
		return contract.ReadResult{Value: b.selectedName}, nil
	}
	return contract.ReadResult{}, &contract.WorkerError{
		Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"},
	}
}

// FindAll 返回猎聘开聊职位选项并记录字段读取参数。
func (b *greetingJobBrowser) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	b.findRequest = request
	return b.items, nil
}

// Scroll 记录猎聘开聊职位选项的真实滚轮定位参数。
func (b *greetingJobBrowser) Scroll(_ context.Context, request contract.ScrollRequest) (contract.ScrollResult, error) {
	b.scrolls = append(b.scrolls, request)
	return contract.ScrollResult{}, nil
}

// FindAll 返回猎聘推广弹框的当前可见状态。
func (b *promotionBrowser) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if request.Selector.Description == "开聊后推广弹框" && b.opened {
		return []contract.FindAllItem{{Index: 0}}, nil
	}
	return nil, &contract.WorkerError{
		Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"},
	}
}

// Click 模拟推广弹框右上角关闭按钮暂时没有响应。
func (b *promotionBrowser) Click(_ context.Context, _ contract.ElementClickRequest) (contract.ClickResult, error) {
	return contract.ClickResult{}, errors.New("关闭按钮暂时没有响应")
}

// PressKey 模拟公共弹框能力按下 Escape 后关闭推广弹框。
func (b *promotionBrowser) PressKey(_ context.Context, request contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	if request.Key == "Escape" {
		b.opened = false
		b.pressCount++
	}
	return contract.KeyboardPressResult{}, nil
}

// Scroll 记录候选人滚动时推广弹框是否仍然存在。
func (b *promotionBrowser) Scroll(_ context.Context, _ contract.ScrollRequest) (contract.ScrollResult, error) {
	b.scrollCount++
	b.scrollWhileOpened = b.opened
	return contract.ScrollResult{Scrolled: true}, nil
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
	if !common.CandidateNamesMatch("张三", "张三先生") {
		t.Fatalf("完整姓名应该匹配")
	}
	if !common.CandidateNamesMatch("张*三", "张三") {
		t.Fatalf("脱敏姓名首字相同应该匹配")
	}
	if common.CandidateNamesMatch("张*三", "李四") {
		t.Fatalf("不同姓氏不应该匹配")
	}
}

// TestSelectPositionUsesExactShortcutName 验证快捷搜索按完整名称点击并等待候选人出现。
func TestSelectPositionUsesExactShortcutName(t *testing.T) {
	browser := &shortcutClickBrowser{currentPage: "2"}
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
			"candidate.current_page": selectorForGreetingTest("当前页码"),
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
	if strings.Join(browser.pressedKeys, ",") != "Home" {
		t.Fatalf("选择快捷搜索前没有用真实按键回到页面顶部：%v", browser.pressedKeys)
	}
	if runtime.positionName != "AI应用开发工程师初级可以实习" {
		t.Fatalf("后端完整岗位名称没有保存：%q", runtime.positionName)
	}
	if runtime.nextCandidatePage != 3 {
		t.Fatalf("复用第 2 页时下一页应从 3 开始，实际为 %d", runtime.nextCandidatePage)
	}
}

// TestSelectPositionKeepsManualSearch 验证配置要求沿用手动筛选时不会自动点击快捷搜索。
func TestSelectPositionKeepsManualSearch(t *testing.T) {
	browser := &shortcutClickBrowser{}
	cfg := model.Config{
		ID: "hliepin",
		Behavior: model.Behavior{
			SkipPositionSelection: true,
		},
	}
	runtime := NewRuntime()

	err := runtime.SelectPosition(
		context.Background(),
		browser,
		cfg,
		model.Position{
			Name:                      "AI应用开发工程师",
			HLiepinShortcutSearchName: "AI应用开发工程师初",
		},
	)

	if err != nil {
		t.Fatalf("沿用手动筛选不应失败：%v", err)
	}
	if browser.request.Selector.Description != "" {
		t.Fatalf("沿用手动筛选时不应点击快捷搜索：%+v", browser.request)
	}
	if runtime.positionName != "AI应用开发工程师" {
		t.Fatalf("沿用手动筛选时也应保存后端岗位名称：%q", runtime.positionName)
	}
}

// TestMatchingGreetingJobUsesTruncatedTitle 验证岗位匹配只读取标题并兼容页面末尾省略号。
func TestMatchingGreetingJobUsesTruncatedTitle(t *testing.T) {
	items := []contract.FindAllItem{
		{
			Index:  0,
			Text:   "AI应用开发工程师初... 某深圳人工智能公司 | 经验不限 | 7-8k | 成都",
			Fields: map[string]string{"position_name": "AI应用开发工程师初..."},
		},
		{
			Index:  1,
			Text:   "Java开发工程师高... 某公司 | 1-3年 | 成都",
			Fields: map[string]string{"position_name": "Java开发工程师高..."},
		},
	}
	if index := matchingGreetingJob(items, "AI应用开发工程师初级可以实习"); index != 0 {
		t.Fatalf("省略岗位匹配下标 = %d，期望 0", index)
	}
}

// TestMatchingGreetingJobRejectsAmbiguousPrefix 验证相同长度的省略前缀无法唯一确认时不会乱点。
func TestMatchingGreetingJobRejectsAmbiguousPrefix(t *testing.T) {
	items := []contract.FindAllItem{
		{Index: 0, Fields: map[string]string{"position_name": "AI应用开发..."}},
		{Index: 1, Fields: map[string]string{"position_name": "AI应用开发..."}},
	}
	if index := matchingGreetingJob(items, "AI应用开发工程师初级"); index != -1 {
		t.Fatalf("相同省略前缀不应自动选择：%d", index)
	}
}

// TestSelectGreetingJobReadsTitleAndScrolls 验证选择职位前读取独立标题并用真实滚轮定位。
func TestSelectGreetingJobReadsTitleAndScrolls(t *testing.T) {
	browser := &greetingJobBrowser{items: []contract.FindAllItem{
		{Index: 0, Fields: map[string]string{"position_name": "AI应用开发工程师初..."}},
	}}
	cfg := model.Config{
		ID: "hliepin",
		Selectors: map[string]contract.SelectorSpec{
			"candidate.greet_job_open":          selectorForGreetingTest("开聊职位下拉"),
			"candidate.greet_job_item":          selectorForGreetingTest("开聊职位列表项"),
			"candidate.greet_job_list":          selectorForGreetingTest("开聊职位列表"),
			"candidate.greet_job_name":          selectorForGreetingTest("开聊职位名称"),
			"candidate.greet_job_selected_name": selectorForGreetingTest("当前已选开聊职位"),
		},
	}
	selected, err := selectGreetingJob(
		context.Background(),
		browser,
		cfg,
		"AI应用开发工程师初级可以实习",
	)
	if err != nil || !selected {
		t.Fatalf("选择省略岗位失败：selected=%v err=%v", selected, err)
	}
	if _, ok := browser.findRequest.Fields["position_name"]; !ok {
		t.Fatal("没有单独读取职位标题")
	}
	if len(browser.scrolls) != 1 || browser.scrolls[0].Target == nil ||
		browser.scrolls[0].Target.Target.Index == nil ||
		*browser.scrolls[0].Target.Target.Index != 0 {
		t.Fatalf("选择岗位前没有滚动定位：%+v", browser.scrolls)
	}
	if len(browser.clicks) != 2 || browser.clicks[1].Verify != nil {
		t.Fatalf("职位点击不应再依赖下拉框关闭状态判断结果：%+v", browser.clicks)
	}
}

// TestSelectGreetingJobAcceptsVerifiedSelectionAfterClickError 验证点击返回异常但页面已经选中时继续开聊。
func TestSelectGreetingJobAcceptsVerifiedSelectionAfterClickError(t *testing.T) {
	browser := &greetingJobBrowser{
		items: []contract.FindAllItem{{
			Index: 0, Fields: map[string]string{"position_name": "AI应用开发工程师初..."},
		}},
		itemClickErr: errors.New("点击结果偶发误判"),
	}
	cfg := model.Config{Selectors: map[string]contract.SelectorSpec{
		"candidate.greet_job_open":          selectorForGreetingTest("开聊职位下拉"),
		"candidate.greet_job_item":          selectorForGreetingTest("开聊职位列表项"),
		"candidate.greet_job_list":          selectorForGreetingTest("开聊职位列表"),
		"candidate.greet_job_name":          selectorForGreetingTest("开聊职位名称"),
		"candidate.greet_job_selected_name": selectorForGreetingTest("当前已选开聊职位"),
	}}
	selected, err := selectGreetingJob(context.Background(), browser, cfg, "AI应用开发工程师初级可以实习")
	if err != nil || !selected {
		t.Fatalf("页面已经选中时不应被点击误判打断：selected=%v err=%v", selected, err)
	}
}

// TestClosePostGreetPromotionFallsBackToEscape 验证猎聘推广弹框关闭按钮失效时会按 Escape 并确认关闭。
func TestClosePostGreetPromotionFallsBackToEscape(t *testing.T) {
	browser := &promotionBrowser{opened: true}
	cfg := model.Config{
		ID:   "hliepin",
		Name: "猎聘猎头端",
		Selectors: map[string]contract.SelectorSpec{
			"candidate.greet_promotion_modal": selectorForGreetingTest("开聊后推广弹框"),
			"candidate.greet_promotion_close": selectorForGreetingTest("关闭开聊后推广弹框"),
		},
	}
	if err := closePostGreetPromotion(context.Background(), browser, cfg, 1); err != nil {
		t.Fatalf("关闭猎聘开聊后推广弹框失败：%v", err)
	}
	if browser.opened || browser.pressCount != 1 {
		t.Fatalf("Escape 兜底没有关闭推广弹框：opened=%v press=%d", browser.opened, browser.pressCount)
	}
}

// TestInitializeGreetingPageClosesStalePromotion 验证新任务选择岗位前会先关闭上次遗留的开聊推广弹框。
func TestInitializeGreetingPageClosesStalePromotion(t *testing.T) {
	browser := &promotionBrowser{opened: true}
	cfg := model.Config{
		ID:   "hliepin",
		Name: "猎聘猎头端",
		Selectors: map[string]contract.SelectorSpec{
			"candidate.greet_promotion_modal": selectorForGreetingTest("开聊后推广弹框"),
			"candidate.greet_promotion_close": selectorForGreetingTest("关闭开聊后推广弹框"),
		},
	}
	if err := NewRuntime().InitializeGreetingPage(context.Background(), browser, cfg); err != nil {
		t.Fatalf("初始化猎聘找人页没有清理遗留推广弹框：%v", err)
	}
	if browser.opened || browser.pressCount != 1 {
		t.Fatalf("遗留推广弹框没有在选择岗位前关闭：opened=%v press=%d", browser.opened, browser.pressCount)
	}
}

// TestScrollToCandidateClosesLatePromotion 验证晚出现的猎聘推广弹框会在滚动下一位候选人前关闭。
func TestScrollToCandidateClosesLatePromotion(t *testing.T) {
	browser := &promotionBrowser{opened: true}
	cfg := model.Config{
		ID:   "hliepin",
		Name: "猎聘猎头端",
		Selectors: map[string]contract.SelectorSpec{
			"candidate.greet_promotion_modal": selectorForGreetingTest("开聊后推广弹框"),
			"candidate.greet_promotion_close": selectorForGreetingTest("关闭开聊后推广弹框"),
			"candidate.item":                  selectorForGreetingTest("候选人行"),
		},
	}
	if err := NewRuntime().ScrollToCandidate(
		context.Background(),
		browser,
		cfg,
		model.Candidate{Index: 0},
	); err != nil {
		t.Fatalf("清理推广弹框后滚动候选人失败：%v", err)
	}
	if browser.opened || browser.pressCount != 1 {
		t.Fatalf("滚动前没有关闭推广弹框：opened=%v press=%d", browser.opened, browser.pressCount)
	}
	if browser.scrollCount != 1 || browser.scrollWhileOpened {
		t.Fatalf("候选人滚动顺序不正确：count=%d while_opened=%v", browser.scrollCount, browser.scrollWhileOpened)
	}
}

// TestFinishGreetingConversationDoesNotWaitAfterSelectingJob 验证匹配岗位开聊后不会误等平台本来就不弹出的聊天框。
func TestFinishGreetingConversationDoesNotWaitAfterSelectingJob(t *testing.T) {
	browser := &promotionBrowser{}
	err := finishGreetingConversation(
		context.Background(),
		browser,
		model.Config{ID: "hliepin", Name: "猎聘猎头端"},
		model.Candidate{Name: "张**"},
		true,
		true,
	)
	if err != nil {
		t.Fatalf("匹配岗位开聊后不应等待自动聊天框：%v", err)
	}
	if browser.pressCount != 0 {
		t.Fatalf("匹配岗位开聊后不应多按按键：%d", browser.pressCount)
	}
}

// selectorForGreetingTest 返回猎聘开聊岗位测试使用的最小选择器。
func selectorForGreetingTest(description string) contract.SelectorSpec {
	return contract.SelectorSpec{
		Target: contract.SelectorGroup{
			Selectors: []contract.SelectorCandidate{{Type: "css", Value: "." + description}},
		},
		Description: description,
	}
}
