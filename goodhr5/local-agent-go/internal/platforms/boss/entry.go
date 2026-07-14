// Package boss 文件作用：承载 entry.go 对应的平台职责实现。
package boss

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
	"strings"
)

// OpenEntryPage 打开 Boss 入口页面。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置，entryURL 为入口地址。
func (r *Runtime) OpenEntryPage(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, entryURL string) error {
	if strings.TrimSpace(entryURL) == "" {
		return fmt.Errorf("云端平台配置缺少入口页面地址")
	}
	exec.Log("info", "入口页面打开成功："+entryURL)
	_, err := exec.Post(ctx, "/api/v1/page/open", map[string]any{"url": entryURL})
	return err
}

// PrepareEntryPage 处理 Boss 入口页可能出现的确认弹框。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置。
func (r *Runtime) PrepareEntryPage(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig) error {
	dialog := map[string]any{"selector": ".boss-dialog__body"}
	result, err := exec.Post(ctx, "/api/v1/page/extract-text", map[string]any{
		"element": dialog,
		"timeout": 1500,
	})
	if err != nil {
		return err
	}
	data := workerDataMap(result)
	if data["found"] != true && intFromMap(data, "count") <= 0 {
		exec.Log("info", "Boss 入口页未发现需要确认的弹框")
		return nil
	}
	exec.Log("info", "Boss 入口页发现确认弹框，准备点击确认")
	_, err = exec.Post(ctx, "/api/v1/page/click", map[string]any{
		"element": map[string]any{
			"selector":         ".confirm-btn",
			"parent_classes":   []any{[]any{".boss-dialog__body"}},
			"find_attempts":    2,
			"find_interval_ms": 300,
		},
		"timeout": 3000,
	})
	if err != nil {
		return err
	}
	return exec.Delay(ctx, "等待 Boss 弹框关闭", 0.3)
}

// IsTaskEntryPage 判断当前页面是否仍是 Boss 任务入口页面。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置。
func (r *Runtime) IsTaskEntryPage(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig) (bool, error) {
	entry := platformEntryPage(cfg)
	if strings.TrimSpace(stringFromMap(entry, "url")) == "" {
		return false, fmt.Errorf("云端平台配置缺少入口页面地址")
	}
	result, err := exec.Post(ctx, "/api/v1/page/list", map[string]any{})
	if err != nil {
		return false, err
	}
	pages := mapList(workerData(result, "pages"))
	if len(pages) == 0 {
		return false, nil
	}
	current := currentDefaultPage(pages)
	return pageMatchesEntry(stringFromMap(current, "url"), entry), nil
}
