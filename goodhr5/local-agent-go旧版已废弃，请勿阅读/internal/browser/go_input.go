// Package browser 文件作用：按职责承载实验性 Go 浏览器控制库的拆分实现。
package browser

import (
	"context"
	"fmt"
	goruntime "runtime"
	"strings"
)

// ClickElement 点击元素。
func (c *GoController) ClickElement(ctx context.Context, selector ElementSelector) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.clickElementLocked(ctx, selector)
}

// clickElementLocked 使用 CDP 鼠标事件点击当前视口内的元素。
func (c *GoController) clickElementLocked(ctx context.Context, selector ElementSelector) error {
	view, err := c.elementViewLocked(ctx, selector)
	if err != nil {
		return err
	}
	if !view.Visible || !view.InViewport {
		return fmt.Errorf("元素当前不在可点击区域，请先调用 EnsureElementVisible")
	}
	x := view.X + view.Width/2
	y := view.Y + view.Height/2
	page, err := c.ensurePageLocked(ctx)
	if err != nil {
		return err
	}
	if _, err = page.client.Call(ctx, "Input.dispatchMouseEvent", map[string]any{"type": "mouseMoved", "x": x, "y": y}); err != nil {
		return err
	}
	if _, err = page.client.Call(ctx, "Input.dispatchMouseEvent", map[string]any{"type": "mousePressed", "x": x, "y": y, "button": "left", "buttons": 1, "clickCount": 1}); err != nil {
		return err
	}
	_, err = page.client.Call(ctx, "Input.dispatchMouseEvent", map[string]any{"type": "mouseReleased", "x": x, "y": y, "button": "left", "buttons": 0, "clickCount": 1})
	return err
}

// FillElement 输入文本。
func (c *GoController) FillElement(ctx context.Context, selector ElementSelector, text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.clickElementLocked(ctx, selector); err != nil {
		return err
	}
	selectAll := "Control+A"
	if goruntime.GOOS == "darwin" {
		selectAll = "Meta+A"
	}
	if err := c.dispatchKeyStrokeLocked(ctx, parseKeyStroke(selectAll)); err != nil {
		return err
	}
	if err := c.dispatchKeyStrokeLocked(ctx, parseKeyStroke("Backspace")); err != nil {
		return err
	}
	page, err := c.ensurePageLocked(ctx)
	if err != nil {
		return err
	}
	if text != "" {
		if _, err = page.client.Call(ctx, "Input.insertText", map[string]any{"text": text}); err != nil {
			return err
		}
	}
	expr, err := c.elementExprLocked(selector, `return "value" in el ? String(el.value || "") : String(el.textContent || "");`)
	if err != nil {
		return err
	}
	value, err := c.evalLocked(ctx, expr)
	if err != nil {
		return err
	}
	if fmt.Sprint(value) != text {
		return fmt.Errorf("输入内容校验失败")
	}
	return nil
}

// PressKey 按下键盘按键。
func (c *GoController) PressKey(ctx context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dispatchKeyStrokeLocked(ctx, parseKeyStroke(key))
}

// dispatchKeyStrokeLocked 使用 CDP 派发一次真实键盘按下和松开事件。
func (c *GoController) dispatchKeyStrokeLocked(ctx context.Context, stroke keyStroke) error {
	page, err := c.ensurePageLocked(ctx)
	if err != nil {
		return err
	}
	_, err = page.client.Call(ctx, "Input.dispatchKeyEvent", map[string]any{
		"type":                  "keyDown",
		"key":                   stroke.Key,
		"code":                  stroke.Code,
		"windowsVirtualKeyCode": stroke.CodeValue,
		"modifiers":             stroke.Modifiers,
	})
	if err != nil {
		return err
	}
	_, err = page.client.Call(ctx, "Input.dispatchKeyEvent", map[string]any{
		"type":                  "keyUp",
		"key":                   stroke.Key,
		"code":                  stroke.Code,
		"windowsVirtualKeyCode": stroke.CodeValue,
		"modifiers":             stroke.Modifiers,
	})
	return err
}

type keyStroke struct {
	Key       string
	Code      string
	CodeValue int
	Modifiers int
}

// parseKeyStroke 解析 ControlOrMeta+A 等跨平台组合键。
func parseKeyStroke(value string) keyStroke {
	parts := strings.Split(strings.TrimSpace(value), "+")
	key := strings.TrimSpace(parts[len(parts)-1])
	modifiers := 0
	for _, part := range parts[:len(parts)-1] {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "alt":
			modifiers |= 1
		case "control", "ctrl":
			modifiers |= 2
		case "meta", "command", "cmd":
			modifiers |= 4
		case "controlormeta":
			if goruntime.GOOS == "darwin" {
				modifiers |= 4
			} else {
				modifiers |= 2
			}
		case "shift":
			modifiers |= 8
		}
	}
	stroke := keyInfo(key)
	stroke.Modifiers = modifiers
	return stroke
}

// keyInfo 返回 CDP 键盘事件需要的按键信息。
func keyInfo(key string) keyStroke {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "enter":
		return keyStroke{Key: "Enter", Code: "Enter", CodeValue: 13}
	case "escape", "esc":
		return keyStroke{Key: "Escape", Code: "Escape", CodeValue: 27}
	case "tab":
		return keyStroke{Key: "Tab", Code: "Tab", CodeValue: 9}
	case "backspace":
		return keyStroke{Key: "Backspace", Code: "Backspace", CodeValue: 8}
	default:
		if key == "" {
			key = "Escape"
		}
		upper := strings.ToUpper(key)
		if len([]rune(upper)) == 1 {
			return keyStroke{Key: strings.ToLower(upper), Code: "Key" + upper, CodeValue: int([]rune(upper)[0])}
		}
		return keyStroke{Key: key, Code: key}
	}
}
