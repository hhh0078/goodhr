// Package hliepin 文件作用：承载 greet.go 对应的平台职责实现。
package hliepin

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
	"strings"
)

// GreetCandidate 执行猎聘猎头端候选人打招呼。
func (r *Runtime) GreetCandidate(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate) error {
	item := candidateItemElement(candidate, cfg)
	greetBtn := platformElement(cfg, "actions", "greetBtn")
	if greetBtn == nil {
		greetBtn = map[string]any{"selector": "button"}
	} else {
		greetBtn["selectors"] = []any{"button"}
	}
	if _, err := exec.Post(ctx, "/api/v1/page/list-click-by-index", map[string]any{"index": intFromMap(candidate, "card_index"), "item": item, "clickTarget": greetBtn, "timeout": 10000}); err != nil {
		return err
	}
	positionName := strings.TrimSpace(r.currentPosition)
	if positionName == "" {
		return fmt.Errorf("猎聘打招呼时任务岗位名称为空，无法选择开聊职位")
	}
	if err := exec.Delay(ctx, "等待猎聘开聊职位弹框", 0.4); err != nil {
		return err
	}
	if _, err := exec.Post(ctx, "/api/v1/page/click-by-text", map[string]any{
		"text": "请选择开聊的职位", "exact": true,
		"element": map[string]any{"selector": ".ant-modal .ant-select-selector"}, "timeout": 5000,
	}); err != nil {
		return fmt.Errorf("打开猎聘开聊职位下拉框失败：%w", err)
	}
	if _, err := exec.Post(ctx, "/api/v1/page/click-by-text", map[string]any{
		"text": positionName, "exact": false,
		"element":         map[string]any{"selector": ".ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option"},
		"resolve_tooltip": true, "tooltip_wait_ms": 300, "timeout": 5000,
	}); err != nil {
		return fmt.Errorf("猎聘开聊弹框中未找到任务岗位“%s”：%w", positionName, err)
	}
	if err := exec.Delay(ctx, "等待猎聘开聊职位选择生效", 0.2); err != nil {
		return err
	}
	if _, err := exec.Post(ctx, "/api/v1/page/click-by-text", map[string]any{
		"text": "立即开聊", "exact": true,
		"element": map[string]any{"selector": ".ant-modal button"}, "timeout": 5000,
	}); err != nil {
		return fmt.Errorf("点击猎聘“立即开聊”失败：%w", err)
	}
	if err := exec.Delay(ctx, "等待猎聘开聊后提示弹框", 1); err != nil {
		return err
	}
	if _, err := exec.Post(ctx, "/api/v1/page/press-key", map[string]any{"key": "Escape"}); err != nil {
		return fmt.Errorf("猎聘立即开聊后按 Esc 失败：%w", err)
	}
	return nil
}
