// Package hliepin 文件作用：承载 position.go 对应的平台职责实现。
package hliepin

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
	"strings"
)

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

// SelectPosition 在猎聘猎头端页面切换岗位。
func (r *Runtime) SelectPosition(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, positionName string) error {
	target := strings.TrimSpace(positionName)
	if target == "" {
		return fmt.Errorf("任务岗位名称为空")
	}
	// 猎聘猎头端的岗位来自“正在发布的职位”，不使用输入框搜索。
	_, _ = exec.Post(ctx, "/api/v1/page/click-by-text", map[string]any{
		"text": "展开更多职位", "exact": true, "timeout": 2500,
	})
	if err := exec.Delay(ctx, "等待正在发布的职位展开", 0.4); err != nil {
		return err
	}
	_, err := exec.Post(ctx, "/api/v1/page/click-by-text", map[string]any{
		"text": target, "exact": true, "element": map[string]any{"selector": "li"},
		"resolve_tooltip": true, "tooltip_wait_ms": 300, "timeout": 8000,
	})
	if err != nil {
		return fmt.Errorf("正在发布的职位中未找到岗位：%s：%w", positionName, err)
	}
	r.currentPosition = target
	if err := exec.Delay(ctx, "等待猎聘候选人结果刷新", 1.2); err != nil {
		return err
	}
	if err := r.ensureHiddenCandidateFilters(ctx, exec, true); err != nil {
		return err
	}
	return nil
}

// ensureHiddenCandidateFilters 确保猎聘结果页的三个隐藏筛选已选中。
func (r *Runtime) ensureHiddenCandidateFilters(ctx context.Context, exec platformcore.Executor, required bool) error {
	timeout := 3500
	if !required {
		timeout = 300
	}
	for _, label := range []string{"隐藏已查看", "隐藏已沟通", "隐藏已获取联系方式"} {
		// 猎聘每次勾选后都会自动滚到候选人列表，下一次必须先用真实滚轮回到顶部再重新查找。
		if _, err := exec.Post(ctx, "/api/v1/page/scroll", map[string]any{"distance": -10000}); err != nil {
			return fmt.Errorf("设置猎聘筛选前向上滚动失败：%w", err)
		}
		if err := exec.Delay(ctx, "等待猎聘隐藏筛选重新可见", 0.45); err != nil {
			return err
		}
		if _, err := exec.Post(ctx, "/api/v1/page/ensure-checked-by-text", map[string]any{
			"text": label, "required": required, "timeout": timeout, "viewport_margin": 20,
		}); err != nil {
			return fmt.Errorf("设置猎聘筛选“%s”失败：%w", label, err)
		}
	}
	return nil
}
