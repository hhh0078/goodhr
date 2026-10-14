// Package common 文件作用：探测唯一的页面操作状态，遇到同名元素时拒绝猜测。
package common

import (
	"context"
	"fmt"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// ProbeUniqueSelectorExists 短暂探测必需配置的状态元素，最多读取两个匹配以识别歧义。
func ProbeUniqueSelectorExists(ctx context.Context, browser model.Browser, cfg model.Config, key string) (bool, error) {
	selector, err := RequiredSelector(cfg, key)
	if err != nil {
		return false, err
	}
	selector.Target.Index = nil
	selector.TimeoutMS = 600
	items, err := browser.FindAll(ctx, contract.ElementFindAllRequest{
		Selector: selector, MaxItems: 2, ExpectedMissing: true, ProbeOnly: true,
	})
	if IsElementMissing(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(items) > 1 {
		return false, fmt.Errorf("%s匹配到多个元素，无法唯一确认，已停止点击", selector.Description)
	}
	return len(items) == 1, nil
}
