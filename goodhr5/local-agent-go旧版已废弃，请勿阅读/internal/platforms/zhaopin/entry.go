// Package zhaopin 文件作用：承载 entry.go 对应的平台职责实现。
package zhaopin

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
	"strings"
)

// OpenEntryPage 打开智联招聘入口页面。
func (r *Runtime) OpenEntryPage(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, entryURL string) error {
	if strings.TrimSpace(entryURL) == "" {
		return fmt.Errorf("云端平台配置缺少入口页面地址")
	}
	exec.Log("info", "入口页面打开成功："+entryURL)
	_, err := exec.Post(ctx, "/api/v1/page/open", map[string]any{"url": entryURL})
	return err
}

// PrepareEntryPage 处理智联招聘入口页初始化动作。
func (r *Runtime) PrepareEntryPage(context.Context, platformcore.Executor, cloudapi.PlatformConfig) error {
	return nil
}

// IsPositionEntryPage 判断当前页面是否仍是智联招聘岗位运行入口页。
func (r *Runtime) IsPositionEntryPage(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig) (bool, error) {
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
