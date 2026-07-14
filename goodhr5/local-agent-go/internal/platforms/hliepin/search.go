package hliepin

import (
	"context"
	"fmt"
	"strings"

	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
)

// PreparePositionSearch 在猎聘猎头端先按岗位配置搜索候选人，再由主流程选择正在发布的职位。
func (r *Runtime) PreparePositionSearch(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, positionSnapshot map[string]any) error {
	commonConfig := mapFromAny(positionSnapshot["common_config"])
	keyword := strings.TrimSpace(stringFromMap(commonConfig, "hliepin_search_keyword"))
	if keyword == "" {
		exec.Log("warning", "猎聘候选人搜索：岗位未配置猎聘搜索关键词，继续使用当前搜索条件")
		return nil
	}
	exec.Log("info", "猎聘候选人搜索：准备输入关键词="+keyword)
	if _, err := exec.Post(ctx, "/api/v1/page/type", map[string]any{
		// 提示文字由 Ant Design 组件绘制，不是 input.placeholder；通过顶部搜索容器定位真正可编辑的输入框。
		"element": map[string]any{"selector": ".search-auto-complete-box .auto-input-wrap-v3 input"},
		"text":    keyword, "timeout": 10000,
	}); err != nil {
		return fmt.Errorf("输入猎聘搜索关键词失败：%w", err)
	}
	if _, err := exec.Post(ctx, "/api/v1/page/click", map[string]any{
		"element": map[string]any{"selector": ".search-auto-complete-box button.search-btn"}, "timeout": 8000,
	}); err != nil {
		return fmt.Errorf("点击猎聘搜索按钮失败：%w", err)
	}
	if err := exec.Delay(ctx, "等待猎聘关键词搜索结果刷新", 1.2); err != nil {
		return err
	}
	return nil
}
