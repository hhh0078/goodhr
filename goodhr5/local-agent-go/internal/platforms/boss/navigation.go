// Package boss 提供 Boss 直聘平台的页面和岗位辅助逻辑。
package boss

import "strings"

// platformEntryPage 返回平台入口页配置。
// cfg 为平台配置。
func platformEntryPage(cfg map[string]any) map[string]any {
	auth := platformSection(cfg, "auth")
	pages, ok := auth["pages"].([]any)
	if !ok || len(pages) == 0 {
		pages, ok = cfg["pages"].([]any)
	}
	if !ok || len(pages) == 0 {
		if url := stringFromMap(cfg, "url"); url != "" {
			return map[string]any{"url": url}
		}
		return map[string]any{}
	}
	for _, page := range pages {
		item := mapFromAny(page)
		if item["entry"] == true && stringFromMap(item, "url") != "" {
			return item
		}
	}
	for _, page := range pages {
		item := mapFromAny(page)
		if stringFromMap(item, "url") != "" {
			return item
		}
	}
	return map[string]any{}
}

// currentDefaultPage 返回默认页面。
// pages 为 Worker 返回的页面列表。
func currentDefaultPage(pages []map[string]any) map[string]any {
	for _, page := range pages {
		if value, ok := page["is_default"].(bool); ok && value {
			return page
		}
	}
	if len(pages) > 0 {
		return pages[0]
	}
	return map[string]any{}
}

// pageMatchesEntry 判断页面 URL 是否匹配入口配置。
// rawURL 为当前 URL，entry 为入口配置。
func pageMatchesEntry(rawURL string, entry map[string]any) bool {
	pageURL := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	target := strings.TrimRight(stringFromMap(entry, "url"), "/")
	if pageURL == "" || target == "" {
		return false
	}
	switch strings.ToLower(stringFromMap(entry, "match")) {
	case "prefix":
		return strings.HasPrefix(pageURL, target)
	case "contains", "":
		return strings.Contains(pageURL, target)
	default:
		return pageURL == target
	}
}

// normalizePositionName 规范化岗位名称。
// value 为原始岗位名称。
func normalizePositionName(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), "")
}

// positionListItemElement 合并岗位列表容器和岗位项配置。
// list 为列表容器配置，item 为列表项配置。
func positionListItemElement(list map[string]any, item map[string]any) map[string]any {
	if item == nil {
		return nil
	}
	merged := map[string]any{}
	for key, value := range item {
		merged[key] = value
	}
	if list == nil {
		return merged
	}
	parents := []any{}
	if existing, ok := merged["parent_classes"].([]any); ok {
		parents = append(parents, existing...)
	}
	if listParents, ok := list["parent_classes"].([]any); ok {
		parents = append(parents, listParents...)
	}
	if listTargets, ok := list["target_classes"].([]any); ok {
		parents = append(parents, listTargets...)
	}
	if len(parents) > 0 {
		merged["parent_classes"] = parents
	}
	return merged
}

// firstStringFromAny 从任意列表中读取第一个字符串。
// value 为原始值。
func firstStringFromAny(value any) string {
	items, ok := value.([]any)
	if !ok {
		return ""
	}
	for _, item := range items {
		if text := strings.TrimSpace(toString(item)); text != "" {
			return text
		}
	}
	return ""
}

// toString 将任意值转成字符串。
// value 为原始值。
func toString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

// detailModeLabel 返回详情模式中文名。
// mode 为详情模式。
func detailModeLabel(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "dom":
		return "结构"
	case "ocr":
		return "OCR"
	case "ai":
		return "AI"
	default:
		return "未知"
	}
}
