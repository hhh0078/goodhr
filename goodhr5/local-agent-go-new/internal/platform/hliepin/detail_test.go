// Package hliepin 文件作用：验证猎聘猎头端关闭候选人详情后按原地址唯一返回标签页。
package hliepin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// detailPageBrowser 模拟关闭详情前后的标签页，并记录最终切换目标。
type detailPageBrowser struct {
	model.Browser
	pages         contract.PageListResult
	closed        bool
	useRequest    *contract.PageUseRequest
	currentURL    string
	closeCalls    int
	closeErr      error
	useErr        error
	detailOpenURL string
}

// requestInfoPageBrowser 模拟索要简历后平台意外打开新的简历详情标签页。
type requestInfoPageBrowser struct {
	model.Browser
	listURL    string
	detailOpen bool
	closed     bool
	useRequest *contract.PageUseRequest
}

// ListPages 返回当前模拟的全部标签页。
func (b *detailPageBrowser) ListPages(context.Context) (contract.PageListResult, error) {
	if !b.closed {
		currentURL := b.currentURL
		if currentURL == "" {
			currentURL = "https://h.liepin.com/resume/showresumedetail/?id=test"
		}
		pages := append([]contract.PageInfo(nil), b.pages.Pages...)
		found := false
		for index := range pages {
			pages[index].Current = pages[index].URL == currentURL
			found = found || pages[index].Current
		}
		if !found {
			pages = append(pages, contract.PageInfo{PageID: "detail-page", URL: currentURL, Current: true})
		}
		return contract.PageListResult{Pages: pages}, nil
	}
	return b.pages, nil
}

// ClosePage 记录候选人详情页已经关闭。
func (b *detailPageBrowser) ClosePage(context.Context) error {
	b.closeCalls++
	if b.closeErr != nil {
		return b.closeErr
	}
	b.closed = true
	for index := range b.pages.Pages {
		b.pages.Pages[index].Current = index == 0
	}
	return nil
}

// UsePage 记录按地址匹配后选择的标签页。
func (b *detailPageBrowser) UsePage(_ context.Context, request contract.PageUseRequest) (contract.PageInfo, error) {
	b.useRequest = &request
	if b.useErr != nil {
		return contract.PageInfo{}, b.useErr
	}
	for index := range b.pages.Pages {
		b.pages.Pages[index].Current = b.pages.Pages[index].PageID == request.PageID
	}
	return contract.PageInfo{PageID: request.PageID}, nil
}

// Click 模拟打开候选人新详情页并返回 Worker 实际捕获的完整地址。
func (b *detailPageBrowser) Click(_ context.Context, _ contract.ElementClickRequest) (contract.ClickResult, error) {
	b.currentURL = b.detailOpenURL
	b.pages.Pages = append(b.pages.Pages, contract.PageInfo{PageID: "new-detail", URL: b.detailOpenURL})
	return contract.ClickResult{Clicked: true, NewPageOpened: true, NewPageURL: b.detailOpenURL}, nil
}

// TestCloseCandidateDetailRejectsHiddenOpenedPage 验证活动页变化但本次详情仍存在时明确失败，详情消失后安全回到列表。
func TestCloseCandidateDetailRejectsHiddenOpenedPage(t *testing.T) {
	const returnURL = "https://h.liepin.com/search/getConditionItem"
	const detailURL = "https://h.liepin.com/resume/showresumedetail/?id=test"
	browser := &detailPageBrowser{
		currentURL: returnURL, detailOpenURL: detailURL,
		pages: contract.PageListResult{Pages: []contract.PageInfo{{PageID: "list", URL: returnURL}}},
	}
	cfg := model.Config{Selectors: map[string]contract.SelectorSpec{
		"candidate.item":        {Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".candidate-card"}}}},
		"candidate.open_target": {Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".candidate-detail"}}}},
	}}
	runtime := NewRuntime()
	if err := runtime.OpenCandidateDetail(context.Background(), browser, cfg, model.Candidate{}); err != nil {
		t.Fatal(err)
	}
	if runtime.detailURL != detailURL {
		t.Fatal("没有保存本次实际打开的详情地址")
	}
	browser.currentURL = returnURL
	err := runtime.CloseCandidateDetail(context.Background(), browser, cfg, model.Candidate{})
	if err == nil || strings.Contains(err.Error(), detailURL) || browser.closeCalls != 0 || runtime.detailReturnURL != returnURL || runtime.detailURL != detailURL {
		t.Fatalf("隐藏详情被误认已关闭或丢失补关状态：err=%v closes=%d", err, browser.closeCalls)
	}
	browser.pages.Pages = browser.pages.Pages[:1]
	if err := runtime.CloseCandidateDetail(context.Background(), browser, cfg, model.Candidate{}); err != nil || browser.closeCalls != 0 || runtime.detailReturnURL != "" || runtime.detailURL != "" {
		t.Fatalf("详情消失后未能安全返回列表：err=%v closes=%d", err, browser.closeCalls)
	}
}

// TestCloseCandidateDetailDoesNotCloseOtherCandidate 验证本次详情消失但另一个候选人详情活动时不会误关或丢失补关状态。
func TestCloseCandidateDetailDoesNotCloseOtherCandidate(t *testing.T) {
	const returnURL = "https://h.liepin.com/search/getConditionItem"
	const openedURL = "https://h.liepin.com/resume/showresumedetail/?id=opened"
	const otherURL = "https://h.liepin.com/resume/showresumedetail/?id=other"
	browser := &detailPageBrowser{currentURL: otherURL, pages: contract.PageListResult{Pages: []contract.PageInfo{{PageID: "list", URL: returnURL}}}}
	runtime := NewRuntime()
	runtime.detailReturnURL, runtime.detailURL = returnURL, openedURL
	err := runtime.CloseCandidateDetail(context.Background(), browser, model.Config{}, model.Candidate{})
	if err == nil || strings.Contains(err.Error(), otherURL) || browser.closeCalls != 0 || browser.useRequest != nil || runtime.detailReturnURL != returnURL || runtime.detailURL != openedURL {
		t.Fatalf("误关另一个候选人详情或丢失补关状态：err=%v closes=%d use=%+v", err, browser.closeCalls, browser.useRequest)
	}
}

// TestCloseCandidateDetailIsIdempotent 验证成功返回列表后重复补关不会关闭列表标签页。
func TestCloseCandidateDetailIsIdempotent(t *testing.T) {
	const returnURL = "https://h.liepin.com/search/getConditionItem#session"
	browser := &detailPageBrowser{pages: contract.PageListResult{Pages: []contract.PageInfo{{PageID: "list-page", URL: returnURL}}}}
	runtime := NewRuntime()
	runtime.detailReturnURL = returnURL
	for attempt := 0; attempt < 2; attempt++ {
		if err := runtime.CloseCandidateDetail(context.Background(), browser, model.Config{}, model.Candidate{}); err != nil {
			t.Fatal(err)
		}
	}
	if browser.closeCalls != 1 || runtime.detailReturnURL != "" {
		t.Fatalf("重复补关误关列表或未清理地址：calls=%d return=%s", browser.closeCalls, runtime.detailReturnURL)
	}
}

// TestCloseCandidateDetailPreservesURLOnFailure 验证关闭或切换失败时仍记住原地址，补关不会重复关闭已关详情。
func TestCloseCandidateDetailPreservesURLOnFailure(t *testing.T) {
	for _, stage := range []string{"close", "use"} {
		t.Run(stage, func(t *testing.T) {
			const returnURL = "https://h.liepin.com/search/getConditionItem#session"
			failure := errors.New("标签页操作失败")
			browser := &detailPageBrowser{pages: contract.PageListResult{Pages: []contract.PageInfo{
				{PageID: "other", URL: "https://example.test/other"}, {PageID: "list", URL: returnURL},
			}}}
			if stage == "close" {
				browser.closeErr = failure
			} else {
				browser.useErr = failure
			}
			runtime := NewRuntime()
			runtime.detailReturnURL = returnURL
			runtime.detailURL = "https://h.liepin.com/resume/showresumedetail/?id=test"
			if err := runtime.CloseCandidateDetail(context.Background(), browser, model.Config{}, model.Candidate{}); !errors.Is(err, failure) || runtime.detailReturnURL != returnURL {
				t.Fatalf("失败后丢失原列表地址：err=%v return=%s", err, runtime.detailReturnURL)
			}
			browser.closeErr, browser.useErr = nil, nil
			if err := runtime.CloseCandidateDetail(context.Background(), browser, model.Config{}, model.Candidate{}); err != nil {
				t.Fatal(err)
			}
			wantCalls := 1
			if stage == "close" {
				wantCalls = 2
			}
			if browser.closeCalls != wantCalls || browser.useRequest == nil || browser.useRequest.PageID != "list" || runtime.detailReturnURL != "" {
				t.Fatalf("补关重复关闭列表或未返回：calls=%d use=%+v return=%s", browser.closeCalls, browser.useRequest, runtime.detailReturnURL)
			}
		})
	}
}

// TestCloseCandidateDetailOnlyClosesRealDetailURL 验证当前已在列表、其他标签或仅查询参数包含详情地址时不关闭页面。
func TestCloseCandidateDetailOnlyClosesRealDetailURL(t *testing.T) {
	const returnURL = "https://h.liepin.com/search/getConditionItem?next=/resume/showresumedetail/"
	for _, currentURL := range []string{returnURL, "https://example.test/other", "https://example.test/resume/showresumedetail/?id=other"} {
		browser := &detailPageBrowser{currentURL: currentURL, pages: contract.PageListResult{Pages: []contract.PageInfo{{PageID: "list", URL: returnURL}}}}
		runtime := NewRuntime()
		runtime.detailReturnURL = returnURL
		if err := runtime.CloseCandidateDetail(context.Background(), browser, model.Config{}, model.Candidate{}); err != nil || browser.closeCalls != 0 {
			t.Fatalf("误关非猎聘详情页面 %s：err=%v calls=%d", currentURL, err, browser.closeCalls)
		}
	}
}

// TestCloseCandidateDetailWithoutReturnURLIsSafe 验证没有记住列表地址时仅对未关闭详情返回错误，绝不关闭未知页面。
func TestCloseCandidateDetailWithoutReturnURLIsSafe(t *testing.T) {
	for _, item := range []struct {
		currentURL string
		wantErr    bool
	}{
		{currentURL: "https://h.liepin.com/search/getConditionItem"},
		{currentURL: "https://h.liepin.com/resume/showresumedetail/?id=test", wantErr: true},
	} {
		browser := &detailPageBrowser{currentURL: item.currentURL}
		err := NewRuntime().CloseCandidateDetail(context.Background(), browser, model.Config{}, model.Candidate{})
		if (err != nil) != item.wantErr || browser.closeCalls != 0 {
			t.Fatalf("缺失地址时不安全：url=%s err=%v closes=%d", item.currentURL, err, browser.closeCalls)
		}
	}
}

// ListPages 返回索要简历前后的动态标签页状态。
func (b *requestInfoPageBrowser) ListPages(context.Context) (contract.PageListResult, error) {
	if b.detailOpen && !b.closed {
		return contract.PageListResult{Pages: []contract.PageInfo{
			{PageID: "list-page", URL: b.listURL},
			{PageID: "detail-page", URL: "https://h.liepin.com/resume/showresumedetail/?id=test", Current: true},
		}}, nil
	}
	return contract.PageListResult{Pages: []contract.PageInfo{{
		PageID: "list-page", URL: b.listURL, Current: true,
	}}}, nil
}

// Click 模拟点击索要简历后新开详情页。
func (b *requestInfoPageBrowser) Click(_ context.Context, _ contract.ElementClickRequest) (contract.ClickResult, error) {
	b.detailOpen = true
	return contract.ClickResult{Clicked: true}, nil
}

// ClosePage 记录意外打开的简历详情页已关闭。
func (b *requestInfoPageBrowser) ClosePage(context.Context) error {
	b.closed = true
	return nil
}

// UsePage 记录程序按原地址切回的候选人列表页。
func (b *requestInfoPageBrowser) UsePage(_ context.Context, request contract.PageUseRequest) (contract.PageInfo, error) {
	b.useRequest = &request
	return contract.PageInfo{PageID: request.PageID}, nil
}

// TestCloseCandidateDetailReturnsByUniqueURL 验证唯一匹配原地址时会切回关闭后的最新标签页编号。
func TestCloseCandidateDetailReturnsByUniqueURL(t *testing.T) {
	const returnURL = "https://h.liepin.com/candidate/recommend?job=123"
	browser := &detailPageBrowser{pages: contract.PageListResult{Pages: []contract.PageInfo{
		{PageID: "page-1", URL: "https://www.zhipin.com/web/geek/recommend"},
		{PageID: "page-2", URL: returnURL},
	}}}
	runtime := NewRuntime()
	runtime.detailReturnURL = returnURL

	err := runtime.CloseCandidateDetail(context.Background(), browser, model.Config{}, model.Candidate{})
	if err != nil {
		t.Fatalf("唯一地址应该可以正常返回：%v", err)
	}
	if !browser.closed {
		t.Fatalf("候选人详情页没有关闭")
	}
	if browser.useRequest == nil || browser.useRequest.PageID != "page-2" {
		t.Fatalf("没有切回原地址对应的标签页：%+v", browser.useRequest)
	}
}

// TestCloseCandidateDetailRejectsDuplicateURL 验证存在两个相同地址时直接报错且不猜测标签页。
func TestCloseCandidateDetailRejectsDuplicateURL(t *testing.T) {
	const returnURL = "https://h.liepin.com/candidate/recommend?job=123"
	browser := &detailPageBrowser{pages: contract.PageListResult{Pages: []contract.PageInfo{
		{PageID: "page-1", URL: returnURL},
		{PageID: "page-2", URL: returnURL},
	}}}
	runtime := NewRuntime()
	runtime.detailReturnURL = returnURL

	err := runtime.CloseCandidateDetail(context.Background(), browser, model.Config{}, model.Candidate{})
	if err == nil || !strings.Contains(err.Error(), "2 个地址相同") {
		t.Fatalf("重复地址应该返回明确错误：%v", err)
	}
	if browser.useRequest != nil {
		t.Fatalf("重复地址时不应该猜测并切换标签页：%+v", browser.useRequest)
	}
}

// TestCloseCandidateDetailRejectsMissingURL 验证原地址已经不存在时不会切换到其他平台页面。
func TestCloseCandidateDetailRejectsMissingURL(t *testing.T) {
	const returnURL = "https://h.liepin.com/candidate/recommend?job=123"
	browser := &detailPageBrowser{pages: contract.PageListResult{Pages: []contract.PageInfo{
		{PageID: "page-1", URL: "https://www.zhipin.com/web/geek/recommend"},
	}}}
	runtime := NewRuntime()
	runtime.detailReturnURL = returnURL

	err := runtime.CloseCandidateDetail(context.Background(), browser, model.Config{}, model.Candidate{})
	if err == nil || !strings.Contains(err.Error(), "已经不在了") {
		t.Fatalf("原地址缺失应该返回明确错误：%v", err)
	}
	if browser.useRequest != nil {
		t.Fatalf("原地址缺失时不应该切换到其他标签页：%+v", browser.useRequest)
	}
}

// TestRequestCandidateInfoClosesUnexpectedResumeDetail 验证索要简历意外打开详情页后会关闭并返回原列表页。
func TestRequestCandidateInfoClosesUnexpectedResumeDetail(t *testing.T) {
	const returnURL = "https://h.liepin.com/search/getConditionItem#session"
	browser := &requestInfoPageBrowser{listURL: returnURL}
	cfg := model.Config{
		ID: "hliepin", Name: "猎聘猎头端",
		Selectors: map[string]contract.SelectorSpec{
			"candidate.request_resume": {
				Target: contract.SelectorGroup{
					Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".action-resume"}},
				},
				Description: "猎聘猎头端索要简历",
			},
		},
	}
	err := NewRuntime().RequestCandidateInfo(
		context.Background(),
		browser,
		cfg,
		model.Candidate{},
		model.CandidateInfoRequest{RequestResume: true},
	)
	if err != nil {
		t.Fatalf("索要简历后的意外详情页没有清理成功：%v", err)
	}
	if !browser.closed {
		t.Fatalf("索要简历意外打开的详情页没有关闭")
	}
	if browser.useRequest == nil || browser.useRequest.PageID != "list-page" {
		t.Fatalf("没有按原地址切回猎聘候选人列表页：%+v", browser.useRequest)
	}
}
