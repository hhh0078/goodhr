// Package common 文件作用：按多页面配置复用当前或已经打开的页面，旧单地址流程保持原样。
package common

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// CurrentPageUsesManualSearch 根据 Worker 活动页的准确地址识别手动搜索页，不触发切换或重新加载。
func CurrentPageUsesManualSearch(ctx context.Context, browser model.Browser, cfg model.Config) (bool, error) {
	target := strings.TrimSpace(cfg.ManualSearchURL)
	if target == "" {
		return false, nil
	}
	search, err := url.Parse(target)
	if err != nil || search.Scheme == "" || search.Host == "" {
		return false, fmt.Errorf("平台 %s 的手动搜索页地址配置不正确", cfg.ID)
	}
	pages, err := browser.ListPages(ctx)
	if err != nil {
		return false, fmt.Errorf("读取%s当前标签页失败：%w", cfg.Name, err)
	}
	var current string
	currentCount := 0
	for _, page := range pages.Pages {
		if page.Current {
			current = strings.TrimSpace(page.URL)
			currentCount++
		}
	}
	if currentCount != 1 || current == "" {
		return false, fmt.Errorf("没确认%s当前停在哪个页面，请切到人才搜索或推荐页后再试", cfg.Name)
	}
	active, err := url.Parse(current)
	// 搜索筛选可以保存在查询参数或片段里，判断页面时只比较协议、主机和完整路径。
	if err == nil && strings.EqualFold(active.Scheme, search.Scheme) && strings.EqualFold(active.Host, search.Host) &&
		strings.TrimRight(active.EscapedPath(), "/") == strings.TrimRight(search.EscapedPath(), "/") {
		return true, nil
	}
	if PageURLMatches(current, cfg.EntryURL) {
		return false, nil
	}
	return false, fmt.Errorf("%s当前没有停在人才搜索或推荐页，请确认页面和登录状态后再试", cfg.Name)
}

// openConfiguredPage 仅在数组配置启用时优先复用支持的页面，其余情况沿用旧入口。
func openConfiguredPage(ctx context.Context, browser model.Browser, platformID string, rawURL string, label string, supportedURLs []string) (contract.PageInfo, error) {
	if len(supportedURLs) == 0 {
		return openPage(ctx, browser, platformID, rawURL, label)
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return contract.PageInfo{}, fmt.Errorf("平台 %s 没有配置%s地址", platformID, label)
	}
	return client.OpenSupportedPage(ctx, browser, contract.PageOpenRequest{
		URL: rawURL, WaitUntil: "domcontentloaded", TimeoutMS: 30000,
	}, supportedURLs)
}
