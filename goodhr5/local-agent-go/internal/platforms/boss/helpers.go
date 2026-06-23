// Package boss 提供 Boss 直聘平台的本地运行时实现。
package boss

import (
	"fmt"
	"strings"
)

// stringFromMap 从 map 中读取字符串。
// item 为原始 map，key 为字段名。
func stringFromMap(item map[string]any, key string) string {
	if item == nil {
		return ""
	}
	if value, ok := item[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

// intFromMap 从 map 中读取整数。
// item 为原始 map，key 为字段名。
func intFromMap(item map[string]any, key string) int {
	if item == nil {
		return 0
	}
	switch value := item[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	case int64:
		return int(value)
	}
	return 0
}

// formatElapsedMS 将毫秒耗时格式化成适合任务日志展示的文本。
// value 为毫秒数，小于等于零时返回 0ms。
func formatElapsedMS(value int) string {
	if value <= 0 {
		return "0ms"
	}
	if value < 1000 {
		return fmt.Sprintf("%dms", value)
	}
	return fmt.Sprintf("%.1fs", float64(value)/1000)
}

// mapFromAny 将任意值转成 map。
// value 为原始值。
func mapFromAny(value any) map[string]any {
	if item, ok := value.(map[string]any); ok {
		return item
	}
	return map[string]any{}
}

// mapList 将任意值转成 map 列表。
// value 为原始值。
func mapList(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return []map[string]any{}
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if mapped, ok := item.(map[string]any); ok {
			result = append(result, mapped)
		}
	}
	return result
}

// workerData 从 Worker 响应中读取 data 字段。
// result 为 Worker 返回体，key 为 data 内字段名。
func workerData(result map[string]any, key string) any {
	if result == nil {
		return nil
	}
	data := mapFromAny(result["data"])
	if len(data) == 0 {
		return result[key]
	}
	return data[key]
}

// workerDataMap 从 Worker 响应中读取 data map。
// result 为 Worker 返回体。
func workerDataMap(result map[string]any) map[string]any {
	if result == nil {
		return map[string]any{}
	}
	data := mapFromAny(result["data"])
	if len(data) > 0 {
		return data
	}
	return result
}

// firstNonEmpty 返回第一个非空字符串。
// values 为候选字符串列表。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if text := strings.TrimSpace(value); text != "" {
			return text
		}
	}
	return ""
}

// platformSection 读取平台配置分区。
// cfg 为平台配置，name 为分区名。
func platformSection(cfg map[string]any, name string) map[string]any {
	return mapFromAny(cfg[name])
}

// platformElement 读取平台配置中的元素定位。
// cfg 为平台配置，section 为分区名，key 为元素名。
func platformElement(cfg map[string]any, section string, key string) map[string]any {
	group := platformSection(cfg, section)
	if len(group) == 0 {
		return nil
	}
	value := group[key]
	if locator := elementPayload(value); len(locator) > 0 {
		return locator
	}
	return nil
}

// elementPayload 将云端定位配置转成 Worker 统一元素协议。
// value 为云端配置对象。
func elementPayload(value any) map[string]any {
	item := mapFromAny(value)
	if len(item) == 0 {
		return nil
	}
	if targets, ok := item["target_classes"]; ok {
		payload := map[string]any{"target_classes": targets}
		if parents, ok := item["parent_classes"]; ok {
			payload["parent_classes"] = parents
		}
		if attempts, ok := item["find_attempts"]; ok {
			payload["find_attempts"] = attempts
		}
		if interval, ok := item["find_interval_ms"]; ok {
			payload["find_interval_ms"] = interval
		}
		return payload
	}
	if targets, ok := item["targetClasses"]; ok {
		payload := map[string]any{"target_classes": targets}
		if parents, ok := item["parentClasses"]; ok {
			payload["parent_classes"] = parents
		}
		return payload
	}
	return item
}

// cardFieldRequests 读取候选人卡片字段定位。
// cfg 为平台配置。
func cardFieldRequests(cfg map[string]any) []map[string]any {
	card := platformSection(cfg, "card")
	raw, ok := card["fields"].([]any)
	if !ok {
		return []map[string]any{}
	}
	result := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		fieldMap := mapFromAny(item)
		for name, locator := range fieldMap {
			if payload := elementPayload(locator); len(payload) > 0 {
				result = append(result, map[string]any{name: payload})
			}
		}
	}
	return result
}

// candidateName 返回候选人展示名。
// candidate 为候选人 map。
func candidateName(candidate map[string]any) string {
	return firstNonEmpty(stringFromMap(candidate, "name"), stringFromMap(candidate, "candidate_name"), "候选人")
}

// normalizeText 规范化文本用于比较或指纹。
// value 为原始文本。
func normalizeText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

// previewFieldValue 生成字段日志摘要。
// value 为字段值，limit 为最大长度。
func previewFieldValue(value any, limit int) string {
	text := normalizeText(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return "空"
	}
	runes := []rune(text)
	if limit > 0 && len(runes) > limit {
		return string(runes[:limit]) + "..."
	}
	return text
}
