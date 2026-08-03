// Package browser 文件作用：按职责承载实验性 Go 浏览器控制库的拆分实现。
package browser

import (
	"context"
)

// ScrollPage 滚动页面。
func (c *GoController) ScrollPage(ctx context.Context, distance int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if distance == 0 {
		distance = 700
	}
	viewport, err := c.viewportSizeLocked(ctx)
	if err != nil {
		return err
	}
	return c.dispatchWheelLocked(ctx, viewport[0]/2, viewport[1]/2, distance)
}

// ScrollElement 滚动元素。
func (c *GoController) ScrollElement(ctx context.Context, selector ElementSelector, distance int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if distance == 0 {
		distance = 700
	}
	view, err := c.elementViewLocked(ctx, selector)
	if err != nil {
		return err
	}
	x := clampFloat(view.X+view.Width/2, 1, view.ViewportW-1)
	y := clampFloat(view.Y+view.Height/2, 1, view.ViewportH-1)
	return c.dispatchWheelLocked(ctx, x, y, distance)
}

// dispatchWheelLocked 使用 CDP 鼠标滚轮事件滚动，不注入页面滚动脚本。
func (c *GoController) dispatchWheelLocked(ctx context.Context, x float64, y float64, distance int) error {
	page, err := c.ensurePageLocked(ctx)
	if err != nil {
		return err
	}
	if _, err = page.client.Call(ctx, "Input.dispatchMouseEvent", map[string]any{"type": "mouseMoved", "x": x, "y": y}); err != nil {
		return err
	}
	_, err = page.client.Call(ctx, "Input.dispatchMouseEvent", map[string]any{
		"type": "mouseWheel", "x": x, "y": y, "deltaX": 0, "deltaY": distance,
	})
	return err
}

// viewportSizeLocked 读取当前视口尺寸，只用于确定真实滚轮落点。
func (c *GoController) viewportSizeLocked(ctx context.Context) ([2]float64, error) {
	value, err := c.evalLocked(ctx, `({width:innerWidth,height:innerHeight})`)
	if err != nil {
		return [2]float64{}, err
	}
	data := mapValue(value)
	return [2]float64{floatFromAny(data["width"]), floatFromAny(data["height"])}, nil
}

func (c *GoController) scrollFromPayload(ctx context.Context, payload map[string]any) error {
	selector := selectorFromPayload(payload)
	if selector.Selector != "" || selector.Ref != "" {
		return c.ScrollElement(ctx, selector, goIntFromAny(payload["distance"]))
	}
	return c.ScrollPage(ctx, goIntFromAny(payload["distance"]))
}

// clampFloat 把鼠标坐标限制在视口范围内。
func clampFloat(value float64, minValue float64, maxValue float64) float64 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
