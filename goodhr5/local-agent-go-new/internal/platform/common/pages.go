// Package common 文件作用：按多页面配置复用当前或已经打开的页面，旧单地址流程保持原样。
package common

import (
	"context"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

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
