// Package client 文件作用：验证控制台打开平台时先保留已打开页面，旧请求与新增标签页仍保持原行为。
package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
)

// TestStartBrowserWithSupportedPage 验证数组复用不会在浏览器启动时先覆盖搜索页，并保留旧请求参数。
func TestStartBrowserWithSupportedPage(t *testing.T) {
	const recommend = "https://example.com/recommend"
	const search = "https://example.com/search?keyword=teacher"
	newTab := true
	for _, test := range []struct {
		name      string
		urls      []string
		newTab    *bool
		pages     []contract.PageInfo
		startURL  string
		usedID    string
		openedURL string
		finalURL  string
	}{
		{name: "旧请求", startURL: recommend, finalURL: recommend},
		{name: "显式新增标签", urls: []string{recommend, "https://example.com/search"}, newTab: &newTab, startURL: recommend, finalURL: recommend},
		{name: "复用并激活当前搜索页", urls: []string{recommend, "https://example.com/search"}, pages: []contract.PageInfo{{PageID: "0", URL: recommend}, {PageID: "1", URL: search, Current: true}}, usedID: "1", finalURL: search},
		{name: "切到已打开搜索页", urls: []string{recommend, "https://example.com/search"}, pages: []contract.PageInfo{{PageID: "0", URL: "https://example.com/home", Current: true}, {PageID: "1", URL: search}}, usedID: "1", finalURL: search},
		{name: "首次打开默认推荐页", urls: []string{recommend, "https://example.com/search"}, openedURL: recommend, finalURL: recommend},
	} {
		t.Run(test.name, func(t *testing.T) {
			starts := make(chan contract.BrowserStartRequest, 1)
			opens := make(chan contract.PageOpenRequest, 1)
			uses := make(chan contract.PageUseRequest, 1)
			lists := make(chan bool, 1)
			worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/browser/start":
					var request contract.BrowserStartRequest
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
					}
					starts <- request
					_ = json.NewEncoder(w).Encode(struct {
						OK   bool                   `json:"ok"`
						Data contract.BrowserStatus `json:"data"`
					}{true, contract.BrowserStatus{Running: true, Reused: true, CurrentURL: request.URL}})
				case "/api/v1/page/list":
					lists <- true
					_ = json.NewEncoder(w).Encode(struct {
						OK   bool                    `json:"ok"`
						Data contract.PageListResult `json:"data"`
					}{true, contract.PageListResult{Pages: test.pages, Count: len(test.pages)}})
				case "/api/v1/page/use":
					var request contract.PageUseRequest
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
					}
					uses <- request
					_ = json.NewEncoder(w).Encode(struct {
						OK   bool              `json:"ok"`
						Data contract.PageInfo `json:"data"`
					}{true, contract.PageInfo{PageID: request.PageID, URL: search, Current: true}})
				case "/api/v1/page/open":
					var request contract.PageOpenRequest
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
					}
					opens <- request
					_ = json.NewEncoder(w).Encode(struct {
						OK   bool              `json:"ok"`
						Data contract.PageInfo `json:"data"`
					}{true, contract.PageInfo{PageID: "new", URL: request.URL, Current: true}})
				default:
					t.Errorf("多余 Worker 请求：%s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer worker.Close()
			result, err := New(worker.URL).StartBrowserWithSupportedPage(context.Background(), contract.BrowserStartRequest{
				URL: recommend, UserDataDir: "/test/profile", DownloadsPath: "/test/downloads", WaitUntil: "load", TimeoutMS: 12000, NewTab: test.newTab,
			}, test.urls)
			if err != nil {
				t.Fatal(err)
			}
			start := <-starts
			if start.URL != test.startURL || start.UserDataDir != "/test/profile" || start.DownloadsPath != "/test/downloads" || start.WaitUntil != "load" || start.TimeoutMS != 12000 {
				t.Fatalf("启动请求不正确或丢失参数：%+v", start)
			}
			if result.CurrentURL != test.finalURL || !result.Running || !result.Reused {
				t.Fatalf("返回的浏览器状态不正确：%+v", result)
			}
			if test.openedURL != "" {
				opened := <-opens
				if opened.URL != test.openedURL || opened.WaitUntil != "load" || opened.TimeoutMS != 12000 {
					t.Fatalf("默认页面参数不正确：%+v", opened)
				}
			} else if len(opens) != 0 {
				t.Fatal("复用页面或旧启动请求不应多发打开页面请求")
			}
			if test.usedID != "" {
				if used := <-uses; used.PageID != test.usedID {
					t.Fatalf("切换了错误标签：%+v", used)
				}
			} else if len(uses) != 0 {
				t.Fatal("不应切换其他标签")
			}
			if test.startURL != "" && len(lists) != 0 {
				t.Fatal("旧请求或新增标签不应枚举页面")
			}
		})
	}
}
