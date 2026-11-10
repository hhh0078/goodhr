// Package common 文件作用：验证多 URL 页面复用及旧单 URL 入口不会退化。
package common

import (
	"context"
	"errors"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestCurrentPageUsesManualSearch 验证手动搜索仅匹配配置的活动页地址，兼容旧配置并拒绝不明确的页状态。
func TestCurrentPageUsesManualSearch(t *testing.T) {
	const recommend = "https://example.com/recommend"
	const search = "https://example.com/talent/search"
	activePage := func(rawURL string) contract.PageInfo {
		return contract.PageInfo{URL: rawURL, Current: true}
	}
	cfg := model.Config{EntryURL: recommend, EntryURLs: []string{recommend, search, search + "/other"}, ManualSearchURL: search}
	for _, test := range []struct {
		name      string
		pages     []contract.PageInfo
		manual    bool
		wantError bool
	}{
		{name: "搜索页带手动筛选参数", pages: []contract.PageInfo{activePage(search + "/?keyword=manual#filters")}, manual: true},
		{name: "搜索标签页未激活", pages: []contract.PageInfo{{URL: search}, activePage(recommend)}},
		{name: "支持数组内的其他地址", pages: []contract.PageInfo{activePage(search + "/other")}, wantError: true},
		{name: "搜索路径相似", pages: []contract.PageInfo{activePage(search + "-history")}, wantError: true},
		{name: "搜索地址不同域", pages: []contract.PageInfo{activePage("https://other.example.com/talent/search")}, wantError: true},
		{name: "登录重定向", pages: []contract.PageInfo{activePage("https://example.com/login")}, wantError: true},
		{name: "无活动页", pages: []contract.PageInfo{{URL: search}}, wantError: true},
		{name: "活动页地址为空", pages: []contract.PageInfo{activePage("")}, wantError: true},
		{name: "两个活动页", pages: []contract.PageInfo{activePage(search), activePage(recommend)}, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			browser := &configuredPageBrowser{pages: test.pages}
			manual, err := CurrentPageUsesManualSearch(context.Background(), browser, cfg)
			if manual != test.manual || (err != nil) != test.wantError {
				t.Fatalf("手动搜索判断 = %v, %v", manual, err)
			}
		})
	}
	manual, err := CurrentPageUsesManualSearch(context.Background(), nil, model.Config{EntryURL: recommend})
	if err != nil || manual {
		t.Fatalf("旧配置不应探测活动页：%v, %v", manual, err)
	}
}

type configuredPageBrowser struct {
	model.Browser
	pages     []contract.PageInfo
	listErr   error
	useErr    error
	openErr   error
	openedURL string
	usedID    string
	listed    bool
	finalURL  string
}

// ListPages 返回模拟标签页，并记录旧单 URL 流程是否多调用了页面枚举。
func (b *configuredPageBrowser) ListPages(context.Context) (contract.PageListResult, error) {
	b.listed = true
	return contract.PageListResult{Pages: b.pages, Count: len(b.pages)}, b.listErr
}

// UsePage 记录切换目标，并返回标签页切换后的地址。
func (b *configuredPageBrowser) UsePage(_ context.Context, request contract.PageUseRequest) (contract.PageInfo, error) {
	b.usedID = request.PageID
	if b.useErr != nil {
		return contract.PageInfo{}, b.useErr
	}
	for _, page := range b.pages {
		if page.PageID == request.PageID {
			page.Current = true
			if b.finalURL != "" {
				page.URL = b.finalURL
			}
			return page, nil
		}
	}
	return contract.PageInfo{}, errors.New("标签页已关闭")
}

// OpenPage 记录默认页面跳转，模拟登录重定向等最终地址变化。
func (b *configuredPageBrowser) OpenPage(_ context.Context, request contract.PageOpenRequest) (contract.PageInfo, error) {
	b.openedURL = request.URL
	finalURL := request.URL
	if b.finalURL != "" {
		finalURL = b.finalURL
	}
	return contract.PageInfo{PageID: "new", URL: finalURL, Current: true}, b.openErr
}

// TestOpenVerifiedPagePreservesSingleURLBehavior 验证旧单地址不枚举、不切换其他页面并保留重定向检查。
func TestOpenVerifiedPagePreservesSingleURLBehavior(t *testing.T) {
	const target = "https://example.com/recommend"
	browser := &configuredPageBrowser{pages: []contract.PageInfo{{PageID: "search", URL: "https://example.com/search", Current: true}}}
	if err := OpenVerifiedPage(context.Background(), browser, "test", target, "推荐页"); err != nil {
		t.Fatal(err)
	}
	if browser.listed || browser.usedID != "" || browser.openedURL != target {
		t.Fatalf("旧入口行为改变：%+v", browser)
	}
	browser.finalURL = "https://example.com/login"
	if err := OpenVerifiedPage(context.Background(), browser, "test", target, "推荐页"); err == nil {
		t.Fatal("旧入口仍应拒绝登录重定向")
	}
}

// TestOpenVerifiedPageReusesSupportedPages 验证数组入口优先当前匹配页，再选其他已打开页，否则回退默认地址。
func TestOpenVerifiedPageReusesSupportedPages(t *testing.T) {
	const recommend = "https://example.com/recommend"
	const search = "https://example.com/search"
	for _, test := range []struct {
		name      string
		pages     []contract.PageInfo
		usedID    string
		openedURL string
	}{
		{name: "当前搜索页优先于较早打开的推荐页", pages: []contract.PageInfo{{PageID: "recommend", URL: recommend}, {PageID: "search", URL: search + "?keyword=teacher", Current: true}}, usedID: "search"},
		{name: "当前无关页时复用已打开的搜索页", pages: []contract.PageInfo{{PageID: "other", URL: "https://other.example.com", Current: true}, {PageID: "search", URL: search + "/"}}, usedID: "search"},
		{name: "没有支持页时回退默认地址", pages: []contract.PageInfo{{PageID: "other", URL: "https://example.com/searching", Current: true}}, openedURL: recommend},
		{name: "没有标签页时回退默认地址", openedURL: recommend},
		{name: "相似域名不能匹配", pages: []contract.PageInfo{{PageID: "other", URL: "https://example.com.evil.test/search", Current: true}}, openedURL: recommend},
	} {
		t.Run(test.name, func(t *testing.T) {
			browser := &configuredPageBrowser{pages: test.pages}
			if err := OpenVerifiedPage(context.Background(), browser, "test", recommend, "打招呼页", recommend, search); err != nil {
				t.Fatal(err)
			}
			if !browser.listed || browser.usedID != test.usedID || browser.openedURL != test.openedURL {
				t.Fatalf("多页复用顺序不正确：%+v", browser)
			}
		})
	}
}

// TestOpenVerifiedPagePropagatesMultiPageFailures 验证枚举、切页、默认跳转和登录重定向失败不会被当作成功。
func TestOpenVerifiedPagePropagatesMultiPageFailures(t *testing.T) {
	const recommend = "https://example.com/recommend"
	const search = "https://example.com/search"
	failure := errors.New("浏览器暂时不可用")
	for _, test := range []struct {
		name    string
		browser configuredPageBrowser
		message string
	}{
		{name: "列表失败", browser: configuredPageBrowser{listErr: failure}, message: failure.Error()},
		{name: "切页失败", browser: configuredPageBrowser{pages: []contract.PageInfo{{PageID: "search", URL: search}}, useErr: failure}, message: failure.Error()},
		{name: "当前支持页激活失败", browser: configuredPageBrowser{pages: []contract.PageInfo{{PageID: "search", URL: search, Current: true}}, useErr: failure}, message: failure.Error()},
		{name: "默认跳转失败", browser: configuredPageBrowser{openErr: failure}, message: failure.Error()},
		{name: "切页后已变成登录页", browser: configuredPageBrowser{pages: []contract.PageInfo{{PageID: "search", URL: search}}, finalURL: "https://example.com/login"}, message: "没有停在"},
		{name: "默认页重定向到登录", browser: configuredPageBrowser{finalURL: "https://example.com/login"}, message: "没有停在"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := OpenVerifiedPage(context.Background(), &test.browser, "test", recommend, "打招呼页", recommend, search)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("失败未明确返回：%v", err)
			}
			if test.browser.listErr != nil || test.browser.useErr != nil {
				if test.browser.openedURL != "" {
					t.Fatalf("页面定位失败后不应擅自跳转：%+v", test.browser)
				}
			}
		})
	}
}
