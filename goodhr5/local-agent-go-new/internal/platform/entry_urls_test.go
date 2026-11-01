// Package platform_test 文件作用：验证各平台三类入口完整透传可选 URL 数组，旧单地址和登录重定向规则保持不变。
package platform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// entryURLBrowser 记录入口打开和页面复用请求，不连接真实招聘页面。
type entryURLBrowser struct {
	model.Browser
	pages      []contract.PageInfo
	listed     bool
	usedID     string
	openedURL  string
	redirectTo string
}

// OpenPage 返回默认地址或模拟重定向，并记录是否保留旧单地址跳转。
func (b *entryURLBrowser) OpenPage(_ context.Context, request contract.PageOpenRequest) (contract.PageInfo, error) {
	b.openedURL = request.URL
	url := request.URL
	if b.redirectTo != "" {
		url = b.redirectTo
	}
	return contract.PageInfo{PageID: "new", URL: url, Current: true}, nil
}

// ListPages 返回已打开的第二个支持地址，记录是否启用了数组复用。
func (b *entryURLBrowser) ListPages(context.Context) (contract.PageListResult, error) {
	b.listed = true
	return contract.PageListResult{Pages: b.pages, Count: len(b.pages)}, nil
}

// UsePage 记录并返回被激活的页面，模拟激活前已跳转到登录页的情况。
func (b *entryURLBrowser) UsePage(_ context.Context, request contract.PageUseRequest) (contract.PageInfo, error) {
	b.usedID = request.PageID
	for _, page := range b.pages {
		if page.PageID == request.PageID {
			page.Current = true
			if b.redirectTo != "" {
				page.URL = b.redirectTo
			}
			return page, nil
		}
	}
	return contract.PageInfo{}, fmt.Errorf("模拟页面不存在")
}

// TestPlatformEntriesHonorURLFormats 验证五个平台的登录、打招呼、消息入口均透传数组，并保持旧字符串与单对象不枚举标签。
func TestPlatformEntriesHonorURLFormats(t *testing.T) {
	const first = "https://example.test/first"
	const second = "https://example.test/second"
	for _, platformID := range []string{"boss", "zhaopin", "liepin", "hliepin", "51job"} {
		runtime, err := platform.RuntimeFor(platformID)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range []struct {
			field string
			open  func(context.Context, model.Browser, model.Config) error
		}{
			{field: "login_url", open: runtime.OpenLoginPage},
			{field: "entry_url", open: runtime.OpenGreetingPage},
			{field: "messages_url", open: runtime.OpenMessagesPage},
		} {
			for _, format := range []struct {
				name  string
				value string
				array bool
			}{
				{name: "旧字符串", value: `"` + first + `"`},
				{name: "单对象", value: `{"url":"` + first + `"}`},
				{name: "混合数组", value: `["` + first + `",{"url":"` + second + `"}]`, array: true},
			} {
				t.Run(platformID+"/"+entry.field+"/"+format.name, func(t *testing.T) {
					var cfg model.Config
					if err := json.Unmarshal([]byte(`{"`+entry.field+`":`+format.value+`}`), &cfg); err != nil {
						t.Fatal(err)
					}
					browser := &entryURLBrowser{pages: []contract.PageInfo{{PageID: "second", URL: second, Current: true}}}
					if err := entry.open(context.Background(), browser, cfg); err != nil {
						t.Fatal(err)
					}
					if format.array {
						if !browser.listed || browser.usedID != "second" || browser.openedURL != "" {
							t.Fatalf("数组入口没有复用第二地址：%+v", browser)
						}
					} else if browser.listed || browser.usedID != "" || browser.openedURL != first {
						t.Fatalf("旧单地址入口发生变化：%+v", browser)
					}
					if format.array && entry.field != "login_url" {
						browser.redirectTo = "https://example.test/login"
						if err := entry.open(context.Background(), browser, cfg); err == nil {
							t.Fatal("数组入口激活后跳到登录页，仍必须拒绝继续任务")
						}
					}
				})
			}
		}
	}
}
