// Package browser 文件作用：按职责承载实验性 Go 浏览器控制库的拆分实现。
package browser

import (
	"context"
	"fmt"
	"time"
)

// EnsureElementVisible 使用真实鼠标滚轮把元素移动到当前视口。
func (c *GoController) EnsureElementVisible(ctx context.Context, selector ElementSelector, distance int, maxAttempts int) (map[string]any, error) {
	if selector.Selector == "" && selector.Ref == "" {
		return map[string]any{"visible": false}, fmt.Errorf("滚动目标不能为空")
	}
	if distance == 0 {
		distance = 120
	}
	if maxAttempts <= 0 {
		maxAttempts = 12
	}
	attempts := make([]map[string]any, 0, maxAttempts)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		view, err := c.ElementView(ctx, selector)
		if err != nil {
			return map[string]any{"visible": false, "attempts": attempts}, err
		}
		attempts = append(attempts, map[string]any{"attempt": attempt, "view": view})
		if view.InViewport {
			return map[string]any{"visible": true, "attempts": attempts, "final_view": view}, nil
		}
		wheelDistance := distance
		if wheelDistance < 0 {
			wheelDistance = -wheelDistance
		}
		if view.Y < 0 {
			wheelDistance = -wheelDistance
		}
		if err := c.ScrollElement(ctx, selector, wheelDistance); err != nil {
			return map[string]any{"visible": false, "attempts": attempts, "final_view": view}, err
		}
		select {
		case <-ctx.Done():
			return map[string]any{"visible": false, "attempts": attempts, "final_view": view}, ctx.Err()
		case <-time.After(180 * time.Millisecond):
		}
	}
	view, err := c.ElementView(ctx, selector)
	result := map[string]any{"visible": view.InViewport, "attempts": attempts, "final_view": view}
	if err != nil {
		return result, err
	}
	if !view.InViewport {
		return result, fmt.Errorf("滚动后元素仍不在当前视口")
	}
	return result, nil
}

// ClickListItemByIndex 是通用组合动作，不建议长期放这里。
// 建议迁到 internal/browser/actions.go，由 FindAll、ScrollElement、ClickElement 组合。
func (c *GoController) ClickListItemByIndex(ctx context.Context, payload map[string]any) error {
	selector := firstSelectorFromAny(payload["item"])
	if selector == "" {
		selector = firstSelectorFromAny(payload["element"])
	}
	if selector == "" {
		selector = firstSelectorFromAny(payload)
	}
	if selector == "" {
		return fmt.Errorf("Go 浏览器模式列表点击缺少选择器")
	}
	index := goIntFromAny(payload["index"])
	ref := ElementSelector{Selector: selector, Index: index}
	if clickTarget := firstSelectorFromAny(payload["click_target"]); clickTarget != "" {
		ref = ElementSelector{Selector: selector + " " + clickTarget, Index: index}
	}
	if clickTarget := firstSelectorFromAny(payload["clickTarget"]); clickTarget != "" {
		ref = ElementSelector{Selector: selector + " " + clickTarget, Index: index}
	}
	return c.ClickElement(ctx, ref)
}

// MarkOverlay 是通用可视化组合动作，不建议长期放这里。
// 建议迁到 internal/browser/actions.go，由基础 DOM 操作组合。
func (c *GoController) MarkOverlay(ctx context.Context, payload map[string]any) (map[string]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	label := stringFromAny(payload["label"])
	if label == "" {
		label = "GoodHR"
	}
	_, err := c.evalLocked(ctx, fmt.Sprintf(`(() => {
const badge = document.createElement("div");
badge.textContent = %s;
badge.style.cssText = "position:fixed;right:16px;bottom:16px;z-index:2147483647;background:#16794c;color:#fff;padding:8px 10px;border-radius:8px;font-size:12px;box-shadow:0 6px 16px rgba(0,0,0,.18)";
document.body.appendChild(badge);
setTimeout(() => badge.remove(), 2200);
return true;
})()`, jsString(label)))
	return map[string]any{"applied": err == nil, "worker": WorkerModeGo}, err
}

// ExtractPlatformCandidates 是平台个性化动作，不建议放这里。
// 建议迁到 internal/platforms/{platform}/runtime.go；这里仅按传入选择器做兼容复刻。
func (c *GoController) ExtractPlatformCandidates(ctx context.Context, payload map[string]any) (map[string]any, error) {
	selector := selectorFromPayload(payload)
	if selector.Selector == "" {
		selector.Selector = firstSelectorFromAny(mapValue(payload["rules"])["cards"])
	}
	items, err := c.FindAll(ctx, selector, payload["fields"], goIntFromAny(payload["max_items"]))
	if err != nil {
		return nil, err
	}
	candidates := make([]map[string]any, 0, len(items))
	for _, item := range items {
		candidates = append(candidates, map[string]any{
			"card_index":  item.Index,
			"element_ref": item.ElementRef,
			"text":        item.Text,
			"fields":      item.Fields,
		})
	}
	return map[string]any{"candidates": candidates, "items": candidates, "count": len(candidates), "worker": WorkerModeGo}, nil
}

// ScrollPlatformCandidateList 是平台个性化动作，不建议放这里。
// 建议迁到 internal/platforms/{platform}/runtime.go；这里仅按传入选择器做兼容复刻。
func (c *GoController) ScrollPlatformCandidateList(ctx context.Context, payload map[string]any) error {
	selector := selectorFromPayload(payload)
	if selector.Selector == "" {
		selector.Selector = firstSelectorFromAny(mapValue(payload["rules"])["list"])
	}
	if selector.Selector != "" {
		return c.ScrollElement(ctx, selector, goIntFromAny(payload["distance"]))
	}
	return c.ScrollPage(ctx, goIntFromAny(payload["distance"]))
}

// GreetPlatformCandidate 是平台个性化动作，不建议放这里。
// 建议迁到 internal/platforms/{platform}/runtime.go；这里仅按传入按钮选择器做兼容复刻。
func (c *GoController) GreetPlatformCandidate(ctx context.Context, payload map[string]any) error {
	if ref := stringFromAny(payload["element_ref"]); ref != "" {
		if greetBtn := firstSelectorFromAny(payload["greet_button"]); greetBtn != "" {
			item, err := c.GetElementByRef(ref)
			if err == nil && item.Selector != "" {
				return c.ClickElement(ctx, ElementSelector{Selector: item.Selector + " " + greetBtn, Index: item.Index})
			}
		}
		return c.ClickElement(ctx, ElementSelector{Ref: ref})
	}
	return c.ClickListItemByIndex(ctx, payload)
}

// ExtractPlatformCandidateDetail 是平台个性化动作，不建议放这里。
// 建议迁到 internal/platforms/{platform}/runtime.go；这里仅按详情容器选择器提取文本和截图。
func (c *GoController) ExtractPlatformCandidateDetail(ctx context.Context, payload map[string]any) (map[string]any, error) {
	selector := selectorFromPayload(payload)
	if selector.Selector == "" {
		selector.Selector = firstSelectorFromAny(mapValue(payload["rules"])["detail_containers"])
	}
	text, textErr := c.ElementText(ctx, selector)
	screen, shotErr := c.screenshotFromPayload(ctx, payload)
	if textErr != nil && shotErr != nil {
		return nil, textErr
	}
	return map[string]any{
		"detail_text": text,
		"text":        text,
		"screenshot":  map[string]any{"path": screen.Path, "file": screen.File},
		"source":      "go-compatible",
		"worker":      WorkerModeGo,
	}, nil
}

// ClosePlatformCandidateDetail 是平台个性化动作，不建议放这里。
// 建议迁到 internal/platforms/{platform}/runtime.go；这里仅默认按 Escape 兼容关闭。
func (c *GoController) ClosePlatformCandidateDetail(ctx context.Context, payload map[string]any) error {
	key := stringFromAny(payload["key"])
	if key == "" {
		key = "Escape"
	}
	return c.PressKey(ctx, key)
}
