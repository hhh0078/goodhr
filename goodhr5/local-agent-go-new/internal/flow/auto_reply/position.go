// Package auto_reply 本文件负责把页面可见沟通岗位唯一匹配到云端自动回复岗位。
package auto_reply

import (
	"fmt"
	"strings"
	"unicode"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// resolvePosition 使用完整或省略号前缀文本唯一匹配岗位，无法唯一确认时拒绝回复。
func resolvePosition(items []cloud.AutoReplyPositionSnapshot, visibleName string) (cloud.AutoReplyPositionSnapshot, error) {
	visible := normalizePositionName(visibleName)
	if visible == "" {
		return cloud.AutoReplyPositionSnapshot{}, fmt.Errorf("当前会话没有读到沟通岗位")
	}
	prefix, truncated := trimPositionEllipsis(visible)
	matches := make([]cloud.AutoReplyPositionSnapshot, 0, 1)
	for _, item := range items {
		name := normalizePositionName(item.Position.Name)
		if name == visible || (truncated && prefix != "" && strings.HasPrefix(name, prefix)) {
			matches = append(matches, item)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return cloud.AutoReplyPositionSnapshot{}, fmt.Errorf("沟通岗位“%s”没有匹配到已开启自动回复的岗位", visibleName)
	}
	return cloud.AutoReplyPositionSnapshot{}, fmt.Errorf("沟通岗位“%s”匹配到%d个岗位，暂时不能唯一确认", visibleName, len(matches))
}

// normalizePositionName 去除岗位文字中的空白差异并统一英文字母大小写。
func normalizePositionName(value string) string {
	return strings.ToLower(strings.Map(func(char rune) rune {
		if unicode.IsSpace(char) {
			return -1
		}
		return char
	}, strings.TrimSpace(value)))
}

// trimPositionEllipsis 返回平台截断岗位文字的可比较前缀。
func trimPositionEllipsis(value string) (string, bool) {
	for _, suffix := range []string{"...", "…"} {
		if strings.HasSuffix(value, suffix) {
			return strings.TrimSuffix(value, suffix), true
		}
	}
	return value, false
}
