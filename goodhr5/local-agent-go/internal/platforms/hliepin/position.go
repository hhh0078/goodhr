// Package hliepin 文件作用：承载 position.go 对应的平台职责实现。
package hliepin

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
)

// ShouldSkipPositionSelection 返回猎聘猎头端无需处理页面岗位。
// 候选人范围已由岗位配置中的搜索关键词限定，不再点击“正在发布的职位”或三个隐藏筛选。
func (r *Runtime) ShouldSkipPositionSelection() bool { return true }

// CurrentPositionName 读取当前页面岗位名称。
func (r *Runtime) CurrentPositionName(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig) (string, error) {
	if name := normalizePositionName(r.currentPosition); name != "" {
		return name, nil
	}
	current := platformElement(cfg, "position", "current")
	if current == nil {
		return "", fmt.Errorf("平台配置中无当前岗位选择器")
	}
	result, err := exec.Post(ctx, "/api/v1/page/extract-text", map[string]any{"element": current, "timeout": 2500})
	if err != nil {
		return "", err
	}
	data := workerDataMap(result)
	name := normalizePositionName(firstNonEmpty(stringFromMap(data, "text"), firstStringFromAny(data["texts"])))
	if name == "" {
		return "", fmt.Errorf("页面当前岗位为空")
	}
	return name, nil
}

// SelectPosition 保留统一平台接口；猎聘猎头端由主流程跳过岗位切换。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置，positionName 为岗位运行岗位名称。
func (r *Runtime) SelectPosition(context.Context, platformcore.Executor, cloudapi.PlatformConfig, string) error {
	return nil
}
