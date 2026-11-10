// Package diagnostics 文件作用：移除日志正文与凭据，保留云端定位故障所需的错误、步骤、耗时及调用栈。
package diagnostics

import (
	"encoding/json"
	"regexp"
	"strings"
)

var uploadContentKey = regexp.MustCompile(`(?i)^(text|value|content|html|body|inner_text|text_content|candidate|messages|prompt|response|resume|conversation)$`)

// UploadSafeDiagnostic 递归清理运行日志，不因读取或输入动作就清空整条错误。
func UploadSafeDiagnostic(item map[string]any) map[string]any {
	data, _ := json.Marshal(item)
	var safe map[string]any
	_ = json.Unmarshal(Redact(data), &safe)
	if safe == nil {
		return map[string]any{}
	}
	redactUploadContent(safe)
	action, _ := safe["source_action"].(string)
	if action == "" {
		action, _ = safe["action"].(string)
	}
	step, _ := safe["step"].(string)
	if strings.Contains(action, "auto_reply") || strings.Contains(step, "auto_reply") {
		safe["message"] = "自动回复步骤已记录，正文已隐藏"
		// AI 原文和上下文不通过通用运行日志上传，错误码与规范化错误原因仍保留。
		delete(safe, "ai_response_diagnostic")
	}
	return safe
}

// redactUploadContent 只移除正文数据，保留定位字段和明确的异常信息。
func redactUploadContent(item map[string]any) {
	for key, value := range item {
		if uploadContentKey.MatchString(key) || key == "ai_response_diagnostic" {
			delete(item, key)
			continue
		}
		switch v := value.(type) {
		case map[string]any:
			redactUploadContent(v)
		case []any:
			for _, entry := range v {
				if child, ok := entry.(map[string]any); ok {
					redactUploadContent(child)
				}
			}
		}
	}
}
