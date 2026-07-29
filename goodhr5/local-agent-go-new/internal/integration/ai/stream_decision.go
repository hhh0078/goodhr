// Package ai 文件作用：从尚未结束的 AI 流式文本中安全提取已完整返回的评分对象。
package ai

import (
	"encoding/json"
	"strconv"
	"strings"
)

// tryExtractStreamDecision 从累计流式正文中提取第一组完整的分数和原因。
func tryExtractStreamDecision(content string, threshold float64) (Decision, bool) {
	for _, object := range completeJSONObjects(content) {
		var value any
		if err := json.Unmarshal([]byte(object), &value); err != nil {
			continue
		}
		score, reason, ok := findScoreReason(value)
		if !ok {
			continue
		}
		return normalizedDecision(score, reason, threshold), true
	}
	return Decision{}, false
}

// completeJSONObjects 返回文本中所有已经闭合的 JSON 对象，并忽略字符串内的大括号。
func completeJSONObjects(content string) []string {
	result := make([]string, 0)
	runes := []rune(content)
	for start, char := range runes {
		if char != '{' {
			continue
		}
		depth := 0
		inString := false
		escaped := false
		for index := start; index < len(runes); index++ {
			current := runes[index]
			if inString {
				if escaped {
					escaped = false
					continue
				}
				if current == '\\' {
					escaped = true
					continue
				}
				if current == '"' {
					inString = false
				}
				continue
			}
			switch current {
			case '"':
				inString = true
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					result = append(result, string(runes[start:index+1]))
					index = len(runes)
				}
			}
		}
	}
	return result
}

// findScoreReason 递归查找同一个 JSON 对象中的 score 和 reason。
func findScoreReason(value any) (float64, string, bool) {
	switch item := value.(type) {
	case map[string]any:
		score, hasScore := numberValue(item["score"])
		reason, _ := item["reason"].(string)
		if hasScore && strings.TrimSpace(reason) != "" {
			return score, reason, true
		}
		for _, child := range item {
			if score, reason, ok := findScoreReason(child); ok {
				return score, reason, true
			}
		}
	case []any:
		for _, child := range item {
			if score, reason, ok := findScoreReason(child); ok {
				return score, reason, true
			}
		}
	}
	return 0, "", false
}

// numberValue 把 AI JSON 中常见的数字表示转换为浮点数。
func numberValue(value any) (float64, bool) {
	switch item := value.(type) {
	case float64:
		return item, true
	case json.Number:
		parsed, err := item.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(item), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

// normalizedDecision 统一限制评分范围、原因长度并计算是否通过。
func normalizedDecision(score float64, reason string, threshold float64) Decision {
	score = min(100, max(0, score))
	reason = truncateRunes(strings.TrimSpace(reason), 30)
	if reason == "" {
		reason = "AI 没有给出原因"
	}
	return Decision{Accepted: score >= threshold, Score: score, Reason: reason}
}
