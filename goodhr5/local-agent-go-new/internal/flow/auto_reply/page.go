// Package auto_reply 本文件负责按当前页面域名确认自动回复使用的唯一招聘标签页。
package auto_reply

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// selectCurrentPlatformPage 优先使用当前招聘页，仅在唯一匹配时切回其他标签页。
func (f *Flow) selectCurrentPlatformPage(ctx context.Context, config model.Config) error {
	pages, err := f.Browser.ListPages(ctx)
	if err != nil {
		return fmt.Errorf("读取浏览器标签页失败：%w", err)
	}
	hosts := configuredPlatformHosts(config)
	if len(hosts) == 0 {
		return fmt.Errorf("%s没有可用于识别页面的域名", config.Name)
	}
	matches := make([]contract.PageInfo, 0)
	for _, page := range pages.Pages {
		if !pageMatchesPlatform(page.URL, hosts) {
			continue
		}
		if page.Current {
			return nil
		}
		matches = append(matches, page)
	}
	switch len(matches) {
	case 0:
		return fmt.Errorf("当前没有打开%s页面，请先在已有浏览器里打开后再试", config.Name)
	case 1:
		_, err = f.Browser.UsePage(ctx, contract.PageUseRequest{PageID: matches[0].PageID})
		if err != nil {
			return fmt.Errorf("切回%s页面失败：%w", config.Name, err)
		}
		return nil
	default:
		return fmt.Errorf("浏览器里同时打开了%d个%s页面，我不敢猜要用哪一个，请只保留一个再试", len(matches), config.Name)
	}
}

// configuredPlatformHosts 返回平台登录页、推荐页和可选消息页的去重域名。
func configuredPlatformHosts(config model.Config) map[string]struct{} {
	hosts := make(map[string]struct{})
	for _, rawURL := range []string{config.LoginURL, config.EntryURL, config.MessagesURL} {
		parsed, err := url.Parse(strings.TrimSpace(rawURL))
		if err == nil && strings.TrimSpace(parsed.Hostname()) != "" {
			hosts[strings.ToLower(parsed.Hostname())] = struct{}{}
		}
	}
	return hosts
}

// pageMatchesPlatform 判断页面域名是否属于当前岗位招聘平台。
func pageMatchesPlatform(rawURL string, hosts map[string]struct{}) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	_, exists := hosts[strings.ToLower(parsed.Hostname())]
	return exists
}
