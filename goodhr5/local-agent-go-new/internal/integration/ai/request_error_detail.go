// Package ai 文件作用：从失败错误提取有限、脱敏的服务说明，逐次日志不复制模型正文，完整响应仍沿原诊断链保存。
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"goodhr5/local-agent-go-new/internal/system/diagnostics"
)

const maxRequestErrorDetailBytes = 4096
const maxHeaderRequestErrorDetailBytes = 1024
const maxRequestErrorSummaryBytes = 512

var requestErrorHeaderLine = regexp.MustCompile(`(?im)(?:proxy-authorization|authorization|set-cookie|cookie|x-api-key)["']?\s*[:=]\s*[^\r\n]+`)
var requestErrorCredential = regexp.MustCompile(`(?i)(["']?(?:api[_-]?key|authorization|token|password|secret|cookie)["']?\s*[:=]\s*["']?)[^\s"',;}\]]+`)
var requestErrorURLCredential = regexp.MustCompile(`(?i)(https?://)[^\s/@"<>]+@`)

// shortRequestErrorReason 只裁剪用户摘要，完整4KiB内具体错误由typed详情保留，不伪造详情截断标记。
func shortRequestErrorReason(value string) string {
	if len(value) <= maxRequestErrorSummaryBytes {
		return value
	}
	const suffix = "…详见错误详情"
	boundary := maxRequestErrorSummaryBytes - len(suffix)
	for !utf8.ValidString(value[:boundary]) {
		boundary--
	}
	return value[:boundary] + suffix
}

// SafeRequestErrorSummary 只读取真实请求已脱敏的错误小结；任意外部错误或取消仍由原流程保护处理。
func SafeRequestErrorSummary(err error) string {
	var service *ServiceError
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &service) {
		return ""
	}
	return service.requestErrorSummary
}

// NormalizeRequestAttempt 仅归一化可选错误字段；旧事件无需新字段，开始和成功事件不携带错误正文。
func NormalizeRequestAttempt(event RequestAttempt) RequestAttempt {
	return normalizeRequestAttempt(event, maxRequestErrorDetailBytes)
}

// normalizeRequestAttempt 按来源限制安全错误说明，普通持久日志保留4KiB，协商响应头仍限制单条1KiB。
func normalizeRequestAttempt(event RequestAttempt, maxDetailBytes int) RequestAttempt {
	if event.Phase == "start" {
		event.ResponseBytes = 0
	}
	if event.Result != "failed" || event.Phase == "start" {
		event.ErrorDetail, event.ErrorCode, event.ErrorDetailTruncated = "", "", false
		return event
	}
	event.ErrorCode = safeRequestLabel(event.ErrorCode)
	detail := strings.ToValidUTF8(event.ErrorDetail, "�")
	// Basic认证及Cookie可包含空格或多个键，必须整行隐藏，不能只移除第一个词。
	detail = redactRequestErrorCredentials(detail, "")
	encoded, _ := json.Marshal(detail)
	_ = json.Unmarshal(diagnostics.Redact(encoded), &detail)
	detail = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, detail)
	detail = strings.Join(strings.Fields(detail), " ")
	if len(detail) > maxDetailBytes {
		detail = detail[:maxDetailBytes]
		for !utf8.ValidString(detail) {
			detail = detail[:len(detail)-1]
		}
		event.ErrorDetailTruncated = true
	}
	event.ErrorDetail = detail
	return event
}

// requestErrorDetail 只从错误说明及HTTP错误响应的固定message/code字段取值，不读取成功模型输出或请求数据。
func requestErrorDetail(err error) (string, string) {
	if err == nil {
		return "", ""
	}
	var mismatch *json.UnmarshalTypeError
	if errors.As(err, &mismatch) {
		return fmt.Sprintf("响应字段%s类型不匹配，应为%s（字节%d）", mismatch.Field, mismatch.Type, mismatch.Offset), ""
	}
	var service *ServiceError
	if errors.As(err, &service) && service.StatusCode >= 400 {
		if service.responseErrorDetail != "" {
			return service.responseErrorDetail, service.responseErrorCode
		}
		body := ""
		if diagnostic, ok := ResponseDiagnosticFromError(err); ok && diagnostic.ResponseKind == "http_body" {
			body = diagnostic.Response
		}
		if body == "" {
			if _, after, ok := strings.Cut(service.Message, "，响应："); ok {
				body = after
			}
		}
		if strings.TrimSpace(body) != "" {
			return providerErrorDetail(body)
		}
		return service.Message, ""
	}
	// JSON语法或类型错误的Error只包含解析原因和字段位置，完整实际返回由ResponseDiagnostic独立保留。
	return err.Error(), ""
}

// providerErrorDetail 对JSON只接收明确错误字段，忽略choices、提示词和任意附加对象；纯文本错误保留原服务说明。
func providerErrorDetail(body string) (string, string) {
	body = strings.TrimSpace(body)
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(body), &fields) == nil && fields != nil {
		var nested map[string]json.RawMessage
		if json.Unmarshal(fields["error"], &nested) == nil && nested != nil {
			return errorMessageField(nested), errorCodeField(nested["code"])
		}
		var message string
		_ = json.Unmarshal(fields["error"], &message)
		if message == "" {
			message = errorMessageField(fields)
		}
		return message, errorCodeField(fields["code"])
	}
	// 截断或无固定错误字段的JSON不得作为普通文本暴露，原文仍可在响应诊断中查看。
	if strings.HasPrefix(body, "{") || strings.HasPrefix(body, "[") || strings.HasPrefix(body, "<") {
		return "", ""
	}
	return body, ""
}

// errorMessageField 兼容常用服务错误字段，不递归读取外部对象或数组。
func errorMessageField(fields map[string]json.RawMessage) string {
	for _, key := range []string{"message", "detail", "error_description"} {
		var value string
		if json.Unmarshal(fields[key], &value) == nil && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// errorCodeField 保留有限字符串或数字错误码，复杂错误对象不能进入日志标签。
func errorCodeField(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return safeRequestLabel(value)
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return safeRequestLabel(string(number))
	}
	return ""
}
