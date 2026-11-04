// Package job51 文件作用：验证前程无忧推荐页选岗、搜索页保留筛选及结果就绪。
package job51

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

type positionBrowserStub struct {
	model.Browser
	current             string
	pageURL             string
	search              bool
	jobs                []contract.FindAllItem
	cards               []contract.FindAllItem
	clicks              []contract.ElementClickRequest
	inputs              []contract.ElementInputRequest
	loadingReads        int
	empty               bool
	fullReads           int
	currentDelay        int
	positionReads       int
	detailOpened        bool
	detailCount         int
	readWhileDetailOpen bool
}

// ListPages 返回合成活动标签页，让地址判断独立于页面元素。
func (s *positionBrowserStub) ListPages(context.Context) (contract.PageListResult, error) {
	active := s.pageURL
	if active == "" {
		active = "https://mall.51job.com/Revision/online/talentRecommend"
	}
	return contract.PageListResult{Pages: []contract.PageInfo{{PageID: "test", URL: active, Current: true}}}, nil
}

// Read 返回合成当前岗位，不从浏览器读取真实资料。
func (s *positionBrowserStub) Read(context.Context, contract.ElementReadRequest) (contract.ReadResult, error) {
	s.positionReads++
	if s.detailOpened {
		s.readWhileDetailOpen = true
	}
	if s.currentDelay > 0 {
		s.currentDelay--
		return contract.ReadResult{}, nil
	}
	return contract.ReadResult{Value: s.current}, nil
}

// FindAll 按配置作用返回合成岗位列表、候选人或搜索入口。
func (s *positionBrowserStub) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	switch request.Selector.Description {
	case "前程无忧简历详情弹窗", "前程无忧简历详情关闭按钮":
		if s.detailOpened {
			count := s.detailCount
			if count == 0 {
				count = 1
			}
			items := make([]contract.FindAllItem, count)
			return items, nil
		}
		return nil, nil
	case "前程无忧候选列表加载层":
		if s.loadingReads > 0 {
			s.loadingReads--
			return []contract.FindAllItem{{Index: 0}}, nil
		}
		return nil, nil
	case "前程无忧搜索明确空结果":
		if s.empty {
			return []contract.FindAllItem{{Index: 0}}, nil
		}
		return nil, nil
	case "前程无忧搜索页岗位下拉入口", "前程无忧人才关键词输入框":
		if s.search {
			return []contract.FindAllItem{{Index: 0}}, nil
		}
		return nil, nil
	case "前程无忧岗位列表项":
		return s.jobs, nil
	case "前程无忧主候选人卡片":
		if len(request.Fields) > 0 {
			s.fullReads++
		}
		if len(s.cards) == 0 {
			return nil, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
		}
		return s.cards, nil
	}
	return nil, nil
}

// Click 记录完整点击请求，选岗后模拟新岗位对应的候选人列表。
func (s *positionBrowserStub) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	s.clicks = append(s.clicks, request)
	if request.Selector.Description == "前程无忧简历详情关闭按钮" {
		s.detailOpened = false
	}
	if request.Selector.Description == "position.click_target" {
		s.cards = []contract.FindAllItem{{Index: 0, Text: "合成人选乙 35岁", Fields: map[string]string{"name": "合成人选乙", "age": "35"}}}
	}
	return contract.ClickResult{}, nil
}

// TestInitializeClosesExistingDetailBeforeReadingPosition 验证用户留在上一份详情时，先确认关闭再读取岗位和候选列表。
func TestInitializeClosesExistingDetailBeforeReadingPosition(t *testing.T) {
	browser := &positionBrowserStub{current: "高级软件工程（半导体）", detailOpened: true, cards: []contract.FindAllItem{{Index: 0}}}
	if err := NewRuntime().InitializeGreetingPage(context.Background(), browser, loadPositionConfig(t)); err != nil {
		t.Fatal(err)
	}
	if browser.detailOpened || browser.readWhileDetailOpen || len(browser.clicks) != 1 || browser.clicks[0].Selector.Description != "前程无忧简历详情关闭按钮" {
		t.Fatal("不能把上一位详情当成新候选人页面准备完成")
	}
	duplicate := &positionBrowserStub{current: "高级软件工程（半导体）", detailOpened: true, detailCount: 2}
	if err := NewRuntime().InitializeGreetingPage(context.Background(), duplicate, loadPositionConfig(t)); err == nil || len(duplicate.clicks) != 0 {
		t.Fatal("多个详情不能猜测关闭，更不能继续任务")
	}
}

// Input 记录合成关键词，不向真实平台提交。
func (s *positionBrowserStub) Input(_ context.Context, request contract.ElementInputRequest) (contract.InputResult, error) {
	s.inputs = append(s.inputs, request)
	return contract.InputResult{}, nil
}

// loadPositionConfig 从被测平台目录读取正式配置，避免测试另造一套选择器。
func loadPositionConfig(t *testing.T) model.Config {
	t.Helper()
	content, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg model.Config
	if err = json.Unmarshal(content, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestSelectPositionKeepsCurrent 验证同岗与空名称均不会触发选岗点击或改写筛选。
func TestSelectPositionKeepsCurrent(t *testing.T) {
	cfg := loadPositionConfig(t)
	browser := &positionBrowserStub{current: "高级软件工程（半导体）", search: true}
	runtime := NewRuntime()
	if err := runtime.SelectPosition(context.Background(), browser, cfg, model.Position{Name: browser.current}); err != nil {
		t.Fatal(err)
	}
	if len(browser.clicks) != 0 || len(browser.inputs) != 0 {
		t.Fatal("同岗不应重复点击或改写搜索")
	}
	if err := runtime.SelectPosition(context.Background(), browser, cfg, model.Position{}); err == nil {
		t.Fatal("空岗位名称必须停止")
	}
}

// TestLegacySelectPositionSupportsInlineAndDropdown 验证未配置手动搜索地址时沿用旧的两页选岗方式。
func TestLegacySelectPositionSupportsInlineAndDropdown(t *testing.T) {
	for _, search := range []bool{false, true} {
		t.Run(map[bool]string{false: "推荐内联列表", true: "搜索岗位下拉"}[search], func(t *testing.T) {
			cfg := loadPositionConfig(t)
			cfg.ManualSearchURL = ""
			browser := &positionBrowserStub{current: "其他岗位", search: search,
				jobs:  []contract.FindAllItem{{Index: 0, Fields: map[string]string{"position_name": "其他岗位"}}, {Index: 2, Fields: map[string]string{"position_name": "高级软件工程（半导体）"}}},
				cards: []contract.FindAllItem{{Index: 0, Text: "合成人选甲 30岁", Fields: map[string]string{"name": "合成人选甲", "age": "30"}}},
			}
			if err := NewRuntime().SelectPosition(context.Background(), browser, cfg, model.Position{Name: "高级软件工程（半导体）"}); err != nil {
				t.Fatal(err)
			}
			wantClicks := 1
			if search {
				wantClicks = 2
			}
			if len(browser.clicks) != wantClicks || len(browser.inputs) != 0 {
				t.Fatalf("点击/输入数量不正确：%d/%d", len(browser.clicks), len(browser.inputs))
			}
			click := browser.clicks[len(browser.clicks)-1]
			if len(click.Selector.Parents) == 0 || click.Selector.Parents[len(click.Selector.Parents)-1].Index == nil || *click.Selector.Parents[len(click.Selector.Parents)-1].Index != 2 {
				t.Fatal("必须点击匹配岗位，不能直接选第一项")
			}
			if click.Verify == nil || click.Verify.TargetVisible == nil {
				t.Fatal("选岗后必须核对页面当前岗位")
			}
		})
	}
}

// TestSelectPositionMissingStops 验证目标岗位不存在时不会任意点击其他岗位。
func TestSelectPositionMissingStops(t *testing.T) {
	browser := &positionBrowserStub{current: "其他岗位", jobs: []contract.FindAllItem{{Index: 0, Fields: map[string]string{"position_name": "会计"}}}}
	err := NewRuntime().SelectPosition(context.Background(), browser, loadPositionConfig(t), model.Position{Name: "高级软件工程师"})
	if err == nil || !strings.Contains(err.Error(), "没找到") || len(browser.clicks) != 0 {
		t.Fatalf("缺失岗位没有安全停止：%v", err)
	}
}

// TestLegacyKeywordFilters 验证未配置手动搜索地址时，旧版仍只在搜索页提交明确提供的关键词。
func TestLegacyKeywordFilters(t *testing.T) {
	cfg := loadPositionConfig(t)
	cfg.ManualSearchURL = ""
	for _, item := range []struct {
		search  bool
		keyword string
		want    int
	}{{true, "", 0}, {false, "Java", 0}, {true, "Java C++", 1}} {
		browser := &positionBrowserStub{search: item.search, cards: []contract.FindAllItem{{Index: 0, Text: "合成人选 30岁", Fields: map[string]string{"name": "合成人选", "age": "30"}}}}
		if err := NewRuntime().ApplyBasicFilters(context.Background(), browser, cfg, model.Position{Keyword: item.keyword}); err != nil {
			t.Fatal(err)
		}
		if len(browser.inputs) != item.want || len(browser.clicks) != item.want {
			t.Fatalf("不应修改未指定的搜索条件：%+v", item)
		}
		if item.want > 0 && browser.inputs[0].Text != item.keyword {
			t.Fatal("提交了任务以外的关键词")
		}
	}
}

// TestManualSearchKeepsFilters 验证已筛选的搜索页沿用页面条件，即使任务中提供了其他岗位、关键词或平台动作。
func TestManualSearchKeepsFilters(t *testing.T) {
	cfg := loadPositionConfig(t)
	cfg.FilterActions = []model.ConfiguredAction{{Type: "input", SelectorKey: "search.input", Value: "平台动作关键词"}}
	cfg.GreetingInitActions = cfg.FilterActions
	position := model.Position{Name: "其他岗位", Keyword: "任务关键词"}
	for _, empty := range []bool{false, true} {
		browser := &positionBrowserStub{
			pageURL: cfg.ManualSearchURL + "/?keyword=manual#filters",
			search:  true, loadingReads: 1, empty: empty,
		}
		if !empty {
			browser.cards = []contract.FindAllItem{{Index: 0}}
		}
		ctx := context.Background()
		runtime := NewRuntime()
		if err := runtime.InitializeGreetingPage(ctx, browser, cfg); err != nil {
			t.Fatal(err)
		}
		if err := runtime.SelectPosition(ctx, browser, cfg, position); err != nil {
			t.Fatal(err)
		}
		if err := runtime.ApplyBasicFilters(ctx, browser, cfg, position); err != nil {
			t.Fatal(err)
		}
		if browser.positionReads != 0 || len(browser.clicks) != 0 || len(browser.inputs) != 0 || browser.fullReads != 0 || browser.loadingReads != 0 {
			t.Fatalf("搜索准备只能探测就绪，不能改筛选或读取全量名单：%+v", browser)
		}
	}
}

// TestManualSearchDoesNotTreatMissingCardsAsEmpty 验证没有首卡或明确空态时，提示结果尚未准备好。
func TestManualSearchDoesNotTreatMissingCardsAsEmpty(t *testing.T) {
	cfg := loadPositionConfig(t)
	browser := &positionBrowserStub{pageURL: cfg.ManualSearchURL, search: true}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := NewRuntime().InitializeGreetingPage(ctx, browser, cfg)
	if err == nil || !strings.Contains(err.Error(), "列表还没加载好") {
		t.Fatalf("未知结果不应视为空态：%v", err)
	}
}

// TestPositionNamesKeepParentheticalSpecialty 验证括号中的岗位方向不能被忽略，列表也不能误选相近名称。
func TestPositionNamesKeepParentheticalSpecialty(t *testing.T) {
	if samePositionName("高级软件工程（自动化）", "高级软件工程（半导体）") {
		t.Fatal("不同岗位方向不能认成同岗")
	}
	browser := &positionBrowserStub{current: "高级软件工程（自动化）", jobs: []contract.FindAllItem{{Index: 0, Fields: map[string]string{"position_name": "高级软件工程（自动化）"}}, {Index: 1, Fields: map[string]string{"position_name": "高级软件工程（半导体）"}}}}
	if err := NewRuntime().SelectPosition(context.Background(), browser, loadPositionConfig(t), model.Position{Name: "高级软件工程（半导体）"}); err != nil {
		t.Fatal(err)
	}
	click := browser.clicks[len(browser.clicks)-1]
	if *click.Selector.Parents[len(click.Selector.Parents)-1].Index != 1 || click.Verify.TargetVisible.Target.Text != "高级软件工程（半导体）" || click.Verify.TargetVisible.Target.ExactText == nil || !*click.Verify.TargetVisible.Target.ExactText {
		t.Fatal("选岗及复核必须使用完整岗位名")
	}
}

// TestPositionVerificationUsesActualName 验证名称匹配允许的空白和大小写差异不会导致点击后误报验证失败。
func TestPositionVerificationUsesActualName(t *testing.T) {
	browser := &positionBrowserStub{current: "其他岗位", jobs: []contract.FindAllItem{{Index: 0, Fields: map[string]string{"position_name": "JAVA开发"}}}}
	if err := NewRuntime().SelectPosition(context.Background(), browser, loadPositionConfig(t), model.Position{Name: "Java 开发"}); err != nil {
		t.Fatal(err)
	}
	if browser.clicks[0].Verify.TargetVisible.Target.Text != "JAVA开发" {
		t.Fatal("必须用实际匹配到的完整岗位名复核")
	}
}

// TestKeywordWaitOnlyProbesReadiness 验证搜索提交后等待加载结束，但不会重复读取整批五十人正文。
func TestKeywordWaitOnlyProbesReadiness(t *testing.T) {
	browser := &positionBrowserStub{search: true, loadingReads: 2, cards: []contract.FindAllItem{{Index: 0, Fields: map[string]string{"name": "合成人选", "age": "30"}}}}
	cfg := loadPositionConfig(t)
	cfg.ManualSearchURL = ""
	if err := NewRuntime().ApplyBasicFilters(context.Background(), browser, cfg, model.Position{Keyword: "Java"}); err != nil {
		t.Fatal(err)
	}
	if browser.loadingReads != 0 || browser.fullReads != 0 {
		t.Fatal("搜索准备只能轻量探测，名单由主流程读取一次")
	}
}

// TestInitializeWaitsForSPAPosition 验证导航后岗位标题尚未出现时等待真实页面就绪。
func TestInitializeWaitsForSPAPosition(t *testing.T) {
	browser := &positionBrowserStub{current: "高级软件工程（半导体）", currentDelay: 2, cards: []contract.FindAllItem{{Index: 0}}}
	if err := NewRuntime().InitializeGreetingPage(context.Background(), browser, loadPositionConfig(t)); err != nil {
		t.Fatal(err)
	}
	if browser.currentDelay != 0 || browser.fullReads != 0 {
		t.Fatal("初始化必须等SPA标题且不重复读取全文")
	}
}

// TestFindCandidatesRecognizesOnlyLoadedEmpty 验证加载层和空态同时出现时继续等待，明确空结果正常返回，未知缺卡仍报错。
func TestFindCandidatesRecognizesOnlyLoadedEmpty(t *testing.T) {
	cfg := loadPositionConfig(t)
	browser := &positionBrowserStub{loadingReads: 2, empty: true}
	candidates, err := NewRuntime().FindCandidates(context.Background(), browser, cfg, model.CandidateListOptions{})
	if err != nil || len(candidates) != 0 || browser.loadingReads != 0 {
		t.Fatalf("已加载的明确空态应正常返回：%v", err)
	}
	if _, err = NewRuntime().FindCandidates(context.Background(), &positionBrowserStub{}, cfg, model.CandidateListOptions{}); err == nil {
		t.Fatal("没有明确空态的选择器缺失不能假装没有候选人")
	}
}

// TestCandidateIDsSeparateSameNameAndAge 验证真实不同编号的人选即使同名同龄也分别保留，稳定编号不能被当成页面可见文字。
func TestCandidateIDsSeparateSameNameAndAge(t *testing.T) {
	browser := &positionBrowserStub{cards: []contract.FindAllItem{
		{Index: 0, Text: "合成人选 28岁", Fields: map[string]string{"name": "合成人选", "age": "28", "platform_candidate_id": "RESUME_CARD_1000010"}},
		{Index: 1, Text: "合成人选 28岁", Fields: map[string]string{"name": "合成人选", "age": "28", "platform_candidate_id": "RESUME_CARD_2000021"}},
	}}
	candidates, err := NewRuntime().FindCandidates(context.Background(), browser, loadPositionConfig(t), model.CandidateListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || candidates[0].Fingerprint == candidates[1].Fingerprint || candidates[0].Fingerprint != "51job_100001" || candidates[0].Fields["card_dom_id"] != "RESUME_CARD_1000010" {
		t.Fatal("同名同龄的不同编号不能去重合并")
	}
	for _, candidate := range candidates {
		for _, text := range candidate.IdentityTexts {
			if strings.Contains(text, "RESUME_CARD_") {
				t.Fatal("编号是属性，不能追加到可见文字定位")
			}
		}
	}
}

// TestCandidateMissingIDStopsBatch 验证编号缺失或异常时不回落到姓名年龄去重，也不返回可被操作的部分列表。
func TestCandidateMissingIDStopsBatch(t *testing.T) {
	for _, id := range []string{"", "RESUME_CARD_", "unverified-id", "RESUME_CARD_1000011", "RESUME_CARD_bad0", "RESUME_CARD_0"} {
		browser := &positionBrowserStub{cards: []contract.FindAllItem{{Index: 0, Text: "合成人选 28岁", Fields: map[string]string{"name": "合成人选", "age": "28", "platform_candidate_id": id}}}}
		candidates, err := NewRuntime().FindCandidates(context.Background(), browser, loadPositionConfig(t), model.CandidateListOptions{})
		if err == nil || len(candidates) != 0 {
			t.Fatal("没有稳定身份编号时必须先停下，不合并或误操作")
		}
	}
}
