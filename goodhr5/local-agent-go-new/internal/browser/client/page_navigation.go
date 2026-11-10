// Package client 文件作用：复用已打开的支持页面，兼容旧的单 URL 打开请求。
package client

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
)

// PageNavigator 定义页面复用所需的三个既有 Worker 封装能力。
type PageNavigator interface {
	OpenPage(context.Context, contract.PageOpenRequest) (contract.PageInfo, error)
	ListPages(context.Context) (contract.PageListResult, error)
	UsePage(context.Context, contract.PageUseRequest) (contract.PageInfo, error)
}

// StartBrowserWithSupportedPage 先启动或复用 Profile 再选支持的页面，防止默认地址提前覆盖用户页面。
func (c *Client) StartBrowserWithSupportedPage(ctx context.Context, request contract.BrowserStartRequest, supportedURLs []string) (contract.BrowserStatus, error) {
	if len(supportedURLs) == 0 || (request.NewTab != nil && *request.NewTab) {
		return c.StartBrowser(ctx, request)
	}
	pageRequest := contract.PageOpenRequest{
		URL: request.URL, WaitUntil: request.WaitUntil, TimeoutMS: request.TimeoutMS, NewTab: request.NewTab,
	}
	request.URL = ""
	status, err := c.StartBrowser(ctx, request)
	if err != nil {
		return contract.BrowserStatus{}, err
	}
	page, err := OpenSupportedPage(ctx, c, pageRequest, supportedURLs)
	if err != nil {
		return contract.BrowserStatus{}, err
	}
	status.CurrentURL = page.URL
	return status, nil
}

// OpenSupportedPage 仅在支持 URL 数组非空时复用当前或已打开的页面，新标签请求和旧单地址行为不变。
func OpenSupportedPage(ctx context.Context, browser PageNavigator, request contract.PageOpenRequest, supportedURLs []string) (contract.PageInfo, error) {
	if len(supportedURLs) == 0 || (request.NewTab != nil && *request.NewTab) {
		return browser.OpenPage(ctx, request)
	}
	pages, err := browser.ListPages(ctx)
	if err != nil {
		return contract.PageInfo{}, fmt.Errorf("读取已打开页面失败：%w", err)
	}
	selectedIndex := -1
	for index, page := range pages.Pages {
		if page.Current && PageMatchesSupportedURLs(page.URL, request.URL, supportedURLs) {
			selectedIndex = index
			break
		}
	}
	if selectedIndex < 0 {
		for index, page := range pages.Pages {
			if PageMatchesSupportedURLs(page.URL, request.URL, supportedURLs) {
				selectedIndex = index
				break
			}
		}
	}
	if selectedIndex >= 0 {
		// Worker 当前指针不代表原生窗口已经在前台，复用当前标签也必须激活，不能跳过 bringToFront。
		selected, err := browser.UsePage(ctx, contract.PageUseRequest{PageID: pages.Pages[selectedIndex].PageID})
		if err != nil {
			return contract.PageInfo{}, fmt.Errorf("激活已打开页面失败：%w", err)
		}
		return selected, nil
	}
	return browser.OpenPage(ctx, request)
}

// PageMatchesSupportedURLs 检查默认地址和支持数组，地址匹配不跨域名或相似路径。
func PageMatchesSupportedURLs(pageURL string, fallbackURL string, supportedURLs []string) bool {
	if PageURLMatches(pageURL, fallbackURL) {
		return true
	}
	for _, candidate := range supportedURLs {
		if PageURLMatches(pageURL, candidate) {
			return true
		}
	}
	return false
}

// PageURLMatches 沿用平台原有的同协议、同主机、完整路径边界和可选查询条件匹配规则。
func PageURLMatches(pageURL string, targetURL string) bool {
	page, pageErr := url.Parse(strings.TrimSpace(pageURL))
	target, targetErr := url.Parse(strings.TrimSpace(targetURL))
	if pageErr != nil || targetErr != nil || page.Scheme == "" || target.Scheme == "" {
		return false
	}
	pagePath := strings.TrimRight(page.EscapedPath(), "/")
	targetPath := strings.TrimRight(target.EscapedPath(), "/")
	pathMatches := pagePath == targetPath ||
		(targetPath != "" && strings.HasPrefix(pagePath, targetPath+"/"))
	if !strings.EqualFold(page.Scheme, target.Scheme) ||
		!strings.EqualFold(page.Host, target.Host) ||
		!pathMatches {
		return false
	}
	return target.RawQuery == "" || strings.Contains(page.RawQuery, target.RawQuery)
}
