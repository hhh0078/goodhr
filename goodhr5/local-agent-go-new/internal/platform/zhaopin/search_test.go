// Package zhaopin_test 文件作用：验证智联搜索页分页接入及原推荐页滚动规则保持可用。
package zhaopin_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/platform/zhaopin"
)

type searchListBrowser struct {
	model.Browser
	paging      bool
	nextPage    bool
	probeErr    error
	clicked     bool
	operations  []string
	scroll      contract.ScrollRequest
	markerCount int
}

type manualSearchBrowser struct {
	model.Browser
	pageURL string
	ready   bool
	probes  []contract.ElementFindAllRequest
}

// ListPages 返回合成活动页和另一张推荐页。
func (b *manualSearchBrowser) ListPages(context.Context) (contract.PageListResult, error) {
	return contract.PageListResult{Pages: []contract.PageInfo{
		{PageID: "recommend", URL: "https://rd6.zhaopin.com/app/recommend"},
		{PageID: "active", URL: b.pageURL, Current: true},
	}}, nil
}

// FindAll 模拟首张搜索结果卡片并记录探测参数。
func (b *manualSearchBrowser) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	b.probes = append(b.probes, request)
	if b.ready {
		return []contract.FindAllItem{{Index: 0}}, nil
	}
	return nil, nil
}

// TestManualSearchKeepsConditions 验证搜索页只探测首卡，并跳过选岗和配置筛选。
func TestManualSearchKeepsConditions(t *testing.T) {
	cfg, err := platform.LoadConfig("zhaopin")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ManualSearchURL != "https://rd6.zhaopin.com/app/search" {
		t.Fatalf("手动搜索地址不正确：%s", cfg.ManualSearchURL)
	}
	cfg.FilterActions = []model.ConfiguredAction{{Type: "input", SelectorKey: "position.input", Value: "新的筛选条件"}}
	cfg.GreetingInitActions = cfg.FilterActions
	browser := &manualSearchBrowser{pageURL: cfg.ManualSearchURL + "/?keyword=manual#filters", ready: true}
	runtime := zhaopin.NewRuntime()
	ctx := context.Background()
	position := model.Position{Name: "其他岗位", Keyword: "任务关键词"}
	if err = runtime.InitializeGreetingPage(ctx, browser, cfg); err != nil {
		t.Fatal(err)
	}
	if err = runtime.SelectPosition(ctx, browser, cfg, position); err != nil {
		t.Fatal(err)
	}
	if err = runtime.ApplyBasicFilters(ctx, browser, cfg, position); err != nil {
		t.Fatal(err)
	}
	if len(browser.probes) != 1 || !browser.probes[0].ProbeOnly || browser.probes[0].MaxItems != 1 || len(browser.probes[0].Fields) != 0 {
		t.Fatalf("搜索准备只需轻量确认一张搜索卡片：%+v", browser.probes)
	}
}

// TestManualSearchRequiresResults 验证搜索页没有已知结果时给出提示，登录跳转时不碰网页。
func TestManualSearchRequiresResults(t *testing.T) {
	cfg, err := platform.LoadConfig("zhaopin")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	runtime := zhaopin.NewRuntime()
	browser := &manualSearchBrowser{pageURL: cfg.ManualSearchURL}
	if err = runtime.InitializeGreetingPage(ctx, browser, cfg); err == nil || !strings.Contains(err.Error(), "请先在平台里筛选") {
		t.Fatalf("智联未知空白应提示先准备搜索结果：%v", err)
	}
	browser = &manualSearchBrowser{pageURL: "https://passport.zhaopin.com"}
	if err = runtime.SelectPosition(ctx, browser, cfg, model.Position{Name: "其他岗位"}); err == nil || len(browser.probes) != 0 {
		t.Fatalf("登录跳转不应读取候选人或选岗：%v", err)
	}
}

// FindAll 模拟搜索分页栏、下一页禁用状态以及翻页或追加后的新名单。
func (b *searchListBrowser) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	switch request.Selector.Description {
	case "智联搜索页分页栏":
		b.markerCount++
		if b.probeErr != nil {
			return nil, b.probeErr
		}
		if b.paging {
			return []contract.FindAllItem{{Index: 0}}, nil
		}
		return nil, nil
	case "智联搜索页下一页":
		if b.nextPage {
			return []contract.FindAllItem{{Index: 0}}, nil
		}
		return nil, nil
	default:
		if b.clicked || !b.paging && request.StartIndex == 1 {
			return []contract.FindAllItem{{Index: request.StartIndex, Text: "测试候选人乙 28岁", Fields: map[string]string{"name": "测试候选人乙", "basic_info": "28岁"}}}, nil
		}
		if !b.paging {
			return []contract.FindAllItem{{Index: 0, Text: "测试候选人甲 27岁", Fields: map[string]string{"name": "测试候选人甲", "basic_info": "27岁"}}}, nil
		}
		return nil, nil
	}
}

// Scroll 记录翻页按钮点击前是否使用了已有真实滚轮封装。
func (b *searchListBrowser) Scroll(_ context.Context, request contract.ScrollRequest) (contract.ScrollResult, error) {
	b.operations = append(b.operations, "scroll")
	b.scroll = request
	return contract.ScrollResult{}, nil
}

// Click 记录下一页点击，并切换模拟名单以供公共更新验证读取。
func (b *searchListBrowser) Click(context.Context, contract.ElementClickRequest) (contract.ClickResult, error) {
	b.operations = append(b.operations, "click")
	b.clicked = true
	return contract.ClickResult{Clicked: true}, nil
}

// TestSearchConfigPreservesRecommendSelectors 验证旧推荐入口与岗位规则保留，并完整增加搜索页路径。
func TestSearchConfigPreservesRecommendSelectors(t *testing.T) {
	cfg, err := platform.LoadConfig("zhaopin")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EntryURL != "https://rd6.zhaopin.com/app/recommend" || len(cfg.EntryURLs) != 2 || cfg.EntryURLs[1] != "https://rd6.zhaopin.com/app/search" {
		t.Fatalf("搜索页不应替换原推荐入口：%v %v", cfg.EntryURL, cfg.EntryURLs)
	}
	if !cfg.Behavior.DirectPositionSelection || !cfg.Behavior.SelectFirstPositionResult || cfg.Behavior.CandidateListMode != "infinite_scroll" || cfg.Behavior.SupportsPaging {
		t.Fatalf("原推荐页岗位和滚动规则改变：%+v", cfg.Behavior)
	}
	for key, oldCSS := range map[string]string{
		"position.open": "a[zp-stat-id='talent_more_jobs']", "position.input": ".job-side-selector__filter input",
		"position.item": ".job-side-selector__item", "position.item_text": ".job-side-selector__title",
		"position.click_target": ".job-side-selector__title", "position.panel": ".job-side-selector",
		"candidate.item": ".recommend-item__inner.recommend-resume-item__inner",
	} {
		variants := cfg.Selectors[key].Variants()
		if len(variants) != 2 || variants[0].Target.Selectors[0].Value != oldCSS {
			t.Fatalf("%s 没有保留原主路径和搜索页备用路径：%+v", key, variants)
		}
	}
	list := cfg.Selectors["candidate.list"].Variants()
	if len(list) != 2 || list[0].Target.Selectors[0].Value != ".search-result" || list[1].Target.Selectors[len(list[1].Target.Selectors)-1].Value != "body" {
		t.Fatalf("搜索页滚轮区域须包含名单和同级分页栏，推荐页保留原 body 兜底：%+v", list)
	}
	searchItem := cfg.Selectors["position.item"].Variants()[1].Target.Selectors[0].Value
	if !strings.Contains(searchItem, ":visible") || !strings.Contains(searchItem, ":not(:has(.app-job-selector__job:text-is('不限')))") {
		t.Fatalf("搜索页不能把不限或隐藏岗位当作第一条结果：%s", searchItem)
	}
	if _, configured := cfg.Selectors["candidate.detail_close"]; configured {
		t.Fatal("新增搜索页不应改变原推荐页的 Escape 关闭规则")
	}
}

// TestSearchSmallGreetKeepsExactIdentityScope 验证搜索页小屏备用按钮仍精确匹配文字并绑定原候选人。
func TestSearchSmallGreetKeepsExactIdentityScope(t *testing.T) {
	cfg, err := platform.LoadConfig("zhaopin")
	if err != nil {
		t.Fatal(err)
	}
	variants := cfg.Selectors["candidate.greet"].Variants()
	if len(variants) != 2 || variants[0].Target.Selectors[0].Value != ".large-screen-btn:visible" || variants[0].Target.Selectors[1].Value != ".small-screen-btn.is-mr-16:visible" {
		t.Fatalf("原推荐页按钮应完整保留在第一条路径：%+v", variants)
	}
	search := variants[1]
	if search.Target.Selectors[0].Value != ".search-resume-item__action .small-screen-btn:visible" || search.Target.Text != "打招呼" || search.Target.ExactText == nil || !*search.Target.ExactText {
		t.Fatalf("搜索页小屏按钮不能混入打电话：%+v", search)
	}
	candidate := model.Candidate{Name: "测试候选人", IdentityTexts: []string{"测试候选人", "28岁"}, IdentityOccurrence: 1}
	scoped, err := common.CandidateActionSelector(cfg, "candidate.greet", candidate)
	if err != nil {
		t.Fatal(err)
	}
	searchPaths := 0
	for _, variant := range scoped.Variants() {
		if variant.Target.Selectors[0].Value != search.Target.Selectors[0].Value {
			continue
		}
		searchPaths++
		bound := false
		for _, parent := range variant.Parents {
			if strings.Join(parent.Texts, "|") == strings.Join(candidate.IdentityTexts, "|") && parent.Index != nil && *parent.Index == 1 {
				bound = true
			}
		}
		if !bound {
			t.Fatalf("搜索备用按钮脱离了候选人身份作用域：%+v", variant)
		}
	}
	if searchPaths != 2 {
		t.Fatalf("搜索按钮应保留两种候选人卡片完整作用域，实际=%d", searchPaths)
	}
	continued := cfg.Selectors["candidate.continue"].Variants()
	if len(continued) != 2 || continued[0].Target.Selectors[0].Value != ".large-screen-btn:visible" || continued[0].Target.Text != "继续沟通" {
		t.Fatalf("原宽屏继续沟通规则应保留：%+v", continued)
	}
	searchContinue := continued[1]
	if searchContinue.Target.Selectors[0].Value != search.Target.Selectors[0].Value || searchContinue.Target.Text != "继续沟通" || searchContinue.Target.ExactText == nil || !*searchContinue.Target.ExactText {
		t.Fatalf("搜索页小屏继续沟通应使用同一区域和精确文字：%+v", searchContinue)
	}
}

// TestSearchAdvanceUsesPagingOnlyWhenPresent 验证搜索页滚轮后翻页，推荐页仍由原追加列表能力推进。
func TestSearchAdvanceUsesPagingOnlyWhenPresent(t *testing.T) {
	cfg, err := platform.LoadConfig("zhaopin")
	if err != nil {
		t.Fatal(err)
	}
	before := []model.Candidate{{Index: 0, Fingerprint: "zhaopin_测试候选人甲_27", Name: "测试候选人甲", Summary: "27岁"}}
	for _, test := range []struct {
		name    string
		browser searchListBrowser
		want    bool
		wantErr bool
	}{
		{name: "搜索页下一页", browser: searchListBrowser{paging: true, nextPage: true}, want: true},
		{name: "搜索页最后一页", browser: searchListBrowser{paging: true}},
		{name: "推荐页原滚动追加", browser: searchListBrowser{}, want: true},
		{name: "探测失败不可误当推荐页滚动", browser: searchListBrowser{probeErr: errors.New("浏览器断开")}, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			advanced, err := zhaopin.NewRuntime().AdvanceCandidateList(context.Background(), &test.browser, cfg, before, model.CandidateListOptions{})
			if advanced != test.want || (err != nil) != test.wantErr {
				t.Fatalf("推进结果不正确：advanced=%t err=%v", advanced, err)
			}
			if test.browser.markerCount != 1 {
				t.Fatalf("分页探测次数不正确：%d", test.browser.markerCount)
			}
			if test.browser.paging && test.browser.nextPage {
				if strings.Join(test.browser.operations, ",") != "scroll,click" || test.browser.scroll.Target == nil || test.browser.scroll.WheelAnchor == nil {
					t.Fatalf("翻页必须先真实滚轮定位，再点击：%+v", test.browser)
				}
			} else if len(test.browser.operations) != 0 {
				t.Fatalf("不应点击不存在或不属于当前页面的分页按钮：%v", test.browser.operations)
			}
		})
	}
}
