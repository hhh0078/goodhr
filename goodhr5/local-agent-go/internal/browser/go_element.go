// Package browser 文件作用：按职责承载实验性 Go 浏览器控制库的拆分实现。
package browser

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// FindOne 查找一个元素。
func (c *GoController) FindOne(ctx context.Context, selector ElementSelector) (ElementRef, error) {
	items, err := c.FindAll(ctx, selector, nil, 1)
	if err != nil {
		return ElementRef{}, err
	}
	if len(items) == 0 {
		return ElementRef{}, fmt.Errorf("Go 浏览器模式未找到元素：%s", selector.Selector)
	}
	return c.GetElementByRef(items[0].Ref)
}

// FindAll 查找多个元素。
func (c *GoController) FindAll(ctx context.Context, selector ElementSelector, fields any, maxItems int) ([]ElementInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, err := c.ensurePageLocked(ctx); err != nil {
		return nil, err
	}
	css := strings.TrimSpace(selector.Selector)
	if css == "" && selector.Ref != "" {
		if ref, ok := c.refs[selector.Ref]; ok {
			css = ref.Selector
		}
	}
	if css == "" {
		return nil, fmt.Errorf("Go 浏览器模式查找元素时选择器不能为空")
	}
	expr := fmt.Sprintf(`(() => {
const selector = %s;
const maxItems = %d;
const fields = %s;
function visible(el) {
  const r = el.getBoundingClientRect();
  const s = getComputedStyle(el);
  return r.width > 0 && r.height > 0 && s.display !== "none" && s.visibility !== "hidden";
}
function firstSelector(v) {
  if (!v) return "";
  if (typeof v === "string") return v;
  if (Array.isArray(v)) {
    for (const item of v) { const s = firstSelector(item); if (s) return s; }
  }
  if (typeof v === "object") {
    for (const k of ["selector", "css", "path", "selectors", "element"]) {
      const s = firstSelector(v[k]); if (s) return s;
    }
  }
  return "";
}
function readFields(root) {
  const out = {};
  if (!Array.isArray(fields)) return out;
  for (const group of fields) {
    if (!group || typeof group !== "object") continue;
    for (const [name, cfg] of Object.entries(group)) {
      const s = firstSelector(cfg);
      const el = s ? root.querySelector(s) : null;
      out[name] = el ? (el.innerText || el.textContent || "").trim() : "";
    }
  }
  return out;
}
return Array.from(document.querySelectorAll(selector)).map((el, domIndex) => ({ el, domIndex }))
  .filter((item) => %t ? visible(item.el) : true)
  .slice(0, maxItems > 0 ? maxItems : undefined)
  .map((item, index) => ({
    index,
    dom_index: item.domIndex,
    text: (item.el.innerText || item.el.textContent || "").trim(),
    fields: readFields(item.el)
  }));
})()`, jsString(css), maxItems, jsJSON(fields), selector.Visible)
	value, err := c.evalLocked(ctx, expr)
	if err != nil {
		return nil, err
	}
	rawItems, _ := value.([]any)
	items := make([]ElementInfo, 0, len(rawItems))
	for _, raw := range rawItems {
		item, _ := raw.(map[string]any)
		domIndex := goIntFromAny(item["dom_index"])
		ref := c.RememberElementLocked(css, domIndex)
		fieldsMap, _ := item["fields"].(map[string]any)
		items = append(items, ElementInfo{
			Index:      goIntFromAny(item["index"]),
			Ref:        ref.ID,
			ElementRef: ref.ID,
			Text:       strings.TrimSpace(stringFromAny(item["text"])),
			Fields:     fieldsMap,
		})
	}
	return items, nil
}

// RememberElement 保存元素引用。
func (c *GoController) RememberElement(selector string) ElementRef {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.RememberElementLocked(selector, 0)
}

// RememberElementLocked 保存元素引用。
func (c *GoController) RememberElementLocked(selector string, index int) ElementRef {
	c.refSeq++
	ref := ElementRef{ID: fmt.Sprintf("go-el-%d", c.refSeq), Created: time.Now(), Selector: selector, Index: index}
	c.refs[ref.ID] = ref
	return ref
}

// GetElementByRef 根据引用读取元素。
func (c *GoController) GetElementByRef(ref string) (ElementRef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	item, ok := c.refs[ref]
	if !ok {
		return ElementRef{}, fmt.Errorf("Go 浏览器模式找不到元素引用：%s", ref)
	}
	return item, nil
}

// ClearElementRefs 清空元素引用。
func (c *GoController) ClearElementRefs() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refs = make(map[string]ElementRef)
}

// ElementText 读取元素文本。
func (c *GoController) ElementText(ctx context.Context, selector ElementSelector) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if selector.Selector == "" && selector.Ref == "" {
		value, err := c.evalLocked(ctx, `(document.body && (document.body.innerText || document.body.textContent) || "").trim()`)
		return stringFromAny(value), err
	}
	expr, err := c.elementExprLocked(selector, "return (el.innerText || el.textContent || '').trim();")
	if err != nil {
		return "", err
	}
	value, err := c.evalLocked(ctx, expr)
	return stringFromAny(value), err
}

// ElementAttribute 读取元素属性。
func (c *GoController) ElementAttribute(ctx context.Context, selector ElementSelector, name string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	expr, err := c.elementExprLocked(selector, fmt.Sprintf("const name = %s; return name in el ? String(el[name] ?? '') : String(el.getAttribute(name) ?? '');", jsString(name)))
	if err != nil {
		return "", err
	}
	value, err := c.evalLocked(ctx, expr)
	return stringFromAny(value), err
}

// ElementHTML 读取元素 HTML。
func (c *GoController) ElementHTML(ctx context.Context, selector ElementSelector) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	expr, err := c.elementExprLocked(selector, "return el.outerHTML || '';")
	if err != nil {
		return "", err
	}
	value, err := c.evalLocked(ctx, expr)
	return stringFromAny(value), err
}

// ElementView 读取元素位置；这里只读取页面状态，不通过脚本推动页面滚动。
func (c *GoController) ElementView(ctx context.Context, selector ElementSelector) (ElementView, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.elementViewLocked(ctx, selector)
}

// elementViewLocked 在控制器锁内读取元素位置和视口信息。
func (c *GoController) elementViewLocked(ctx context.Context, selector ElementSelector) (ElementView, error) {
	expr, err := c.elementExprLocked(selector, `const r = el.getBoundingClientRect();
const style = getComputedStyle(el);
const visible = r.width > 0 && r.height > 0 && style.display !== "none" && style.visibility !== "hidden";
const inViewport = visible && r.bottom > 0 && r.right > 0 && r.top < innerHeight && r.left < innerWidth;
return {x:r.left,y:r.top,width:r.width,height:r.height,viewport_width:innerWidth,viewport_height:innerHeight,visible,in_viewport:inViewport};`)
	if err != nil {
		return ElementView{}, err
	}
	value, err := c.evalLocked(ctx, expr)
	if err != nil {
		return ElementView{}, err
	}
	data := mapValue(value)
	return ElementView{
		X:          floatFromAny(data["x"]),
		Y:          floatFromAny(data["y"]),
		Width:      floatFromAny(data["width"]),
		Height:     floatFromAny(data["height"]),
		ViewportW:  floatFromAny(data["viewport_width"]),
		ViewportH:  floatFromAny(data["viewport_height"]),
		Visible:    goBoolFromAny(data["visible"]),
		InViewport: goBoolFromAny(data["in_viewport"]),
	}, nil
}

func (c *GoController) elementExprLocked(selector ElementSelector, body string) (string, error) {
	css := strings.TrimSpace(selector.Selector)
	index := selector.Index
	if selector.Ref != "" {
		ref, ok := c.refs[selector.Ref]
		if !ok {
			return "", fmt.Errorf("Go 浏览器模式找不到元素引用：%s", selector.Ref)
		}
		css = ref.Selector
		index = ref.Index
	}
	if css == "" {
		return "", fmt.Errorf("Go 浏览器模式缺少选择器")
	}
	return fmt.Sprintf(`(() => {
const nodes = Array.from(document.querySelectorAll(%s));
const el = nodes[%d] || nodes[0];
if (!el) throw new Error("元素不存在：%s");
%s
})()`, jsString(css), maxInt(0, index), escapeJSMessage(css), body), nil
}

func (c *GoController) elementClipLocked(ctx context.Context, selector ElementSelector) (map[string]any, error) {
	expr, err := c.elementExprLocked(selector, `const r = el.getBoundingClientRect();
return {x: Math.max(0, r.left), y: Math.max(0, r.top), width: Math.max(1, r.width), height: Math.max(1, r.height), scale: 1};`)
	if err != nil {
		return nil, err
	}
	value, err := c.evalLocked(ctx, expr)
	if err != nil {
		return nil, err
	}
	clip, _ := value.(map[string]any)
	if clip == nil {
		return nil, fmt.Errorf("Go 浏览器模式获取元素截图区域失败")
	}
	return clip, nil
}

func selectorFromPayload(payload map[string]any) ElementSelector {
	element := any(payload)
	if payload["element"] != nil {
		element = payload["element"]
	}
	selector := ElementSelector{
		Selector: combinedSelectorFromAny(element),
		Ref:      stringFromAny(firstNonEmpty(payload["element_ref"], payload["ref"])),
		Visible:  payload["visible_only"] != false,
		Index:    goIntFromAny(payload["index"]),
	}
	return selector
}

// combinedSelectorFromAny 把云端的父级和目标元素配置合并成 CSS 选择器。
func combinedSelectorFromAny(value any) string {
	config, ok := value.(map[string]any)
	if !ok {
		return firstSelectorFromAny(value)
	}
	target := firstSelectorFromAny(config)
	parent := firstSelectorFromAny(config["parent_classes"])
	if parent != "" && target != "" {
		return parent + " " + target
	}
	return target
}

func firstSelectorFromAny(value any) string {
	switch v := value.(type) {
	case string:
		return normalizeGoSelector(v)
	case []string:
		for _, item := range v {
			if text := normalizeGoSelector(item); text != "" {
				return text
			}
		}
	case []any:
		for _, item := range v {
			if text := firstSelectorFromAny(item); text != "" {
				return text
			}
		}
	case map[string]any:
		for _, key := range []string{"selector", "css", "path", "selectors", "target_classes", "classes"} {
			if text := firstSelectorFromAny(v[key]); text != "" {
				return text
			}
		}
		if text := firstSelectorFromAny(v["element"]); text != "" {
			return text
		}
	}
	return ""
}

// normalizeGoSelector 把纯 class 名称转换成 CSS 选择器，完整 CSS 保持不变。
func normalizeGoSelector(value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return ""
	}
	if strings.ContainsAny(text, ".#[:>~+*= ") {
		return text
	}
	return "." + strings.Map(func(char rune) rune {
		if char == '_' || char == '-' || char >= '0' && char <= '9' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' {
			return char
		}
		return -1
	}, text)
}
