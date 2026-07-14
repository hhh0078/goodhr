// Package hliepin 文件作用：承载 entry.go 对应的平台职责实现。
package hliepin

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
	"strings"
)

// OpenEntryPage 打开猎聘猎头端入口页面。
func (r *Runtime) OpenEntryPage(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, entryURL string) error {
	if strings.TrimSpace(entryURL) == "" {
		return fmt.Errorf("云端平台配置缺少入口页面地址")
	}
	exec.Log("info", "入口页面打开成功："+entryURL)
	_, err := exec.Post(ctx, "/api/v1/page/open", map[string]any{"url": entryURL})
	return err
}

// PrepareEntryPage 处理猎聘猎头端入口页初始化动作。
func (r *Runtime) PrepareEntryPage(ctx context.Context, exec platformcore.Executor, _ cloudapi.PlatformConfig) error {
	// 搜索会刷新候选人条件；岗位选择和隐藏筛选统一在搜索完成后执行。
	return nil
}

// IsTaskEntryPage 判断当前页面是否仍是猎聘猎头端任务入口页。
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
