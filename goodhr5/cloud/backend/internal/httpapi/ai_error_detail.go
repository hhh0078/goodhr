// 本文件提取上游明确的错误说明并隐藏凭据及请求回显，供逐次重试和最终失败共用；不记录模型成功正文。
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxAIErrorDetailBytes = 4096

var aiErrorHeaderLine = regexp.MustCompile(`(?im)(?:proxy-authorization|authorization|set-cookie|cookie|x-api-key)\s*[:=]\s*[^\r\n]+`)
var aiErrorURLUser = regexp.MustCompile(`(?i)(https?://)[^\s/<>"']+@`)
var aiErrorToken = regexp.MustCompile(`\b(?:sk-[A-Za-z0-9_-]{8,}|ghai_[A-Za-z0-9_-]{8,})\b`)
var aiErrorDataURL = regexp.MustCompile(`(?i)data:[^\s;,"']+;base64,[a-z0-9+/=_-]+`)
var aiErrorHTMLHeading = regexp.MustCompile(`(?is)<(?:title|h1)(?:\s[^>]*)?>(.*?)</(?:title|h1)>`)
var aiErrorHTMLTag = regexp.MustCompile(`(?s)<[^>]*>`)
var aiPlainErrorSignal = regexp.MustCompile(`(?i)error|fail|unavailable|overload|capacity|upstream|timeout|timed out|rate.limit|too many|no.available|invalid|denied|forbidden|unauthori[sz]ed|not.found|exhaust|\b[45][0-9]{2}\b|错误|失败|不可用|不可达|繁忙|超时|限流|额度|无可用|未找到|不支持|拒绝|通道|渠道`)
var aiPrivateRequestField = regexp.MustCompile(`(?i)^(name|text|content|message_text|input_text)$`)

// hideAIPrivateEchoPrefix 隐藏被服务商截短的请求行回显，保留其前面的实际服务错误说明。
func hideAIPrivateEchoPrefix(value, private string) string {
	count, end := 0, len(private)
	for index := range private {
		if count == 12 {
			end = index
			break
		}
		count++
	}
	if count < 12 {
		return value
	}
	if start := strings.Index(value, private[:end]); start >= 0 {
		if next := strings.IndexByte(value[start:], '\n'); next >= 0 {
			return value[:start] + "[请求内容已隐藏]" + value[start+next:]
		}
		return value[:start] + "[请求内容已隐藏]"
	}
	return value
}

// hideAIRequestEcho 先删除原始请求回显，再做其他脱敏，避免手机号等局部改写后无法匹配整段简历。
func hideAIRequestEcho(value string, payload []byte) string {
	var request struct {
		Messages []struct {
			Content any `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(payload, &request) != nil {
		return value
	}
	var hide func(any, bool, int)
	hide = func(item any, private bool, depth int) {
		if depth > 12 {
			return
		}
		switch item := item.(type) {
		case string:
			if len(item) >= 8 || private && len(item) >= 2 {
				value = strings.ReplaceAll(value, item, "[请求内容已隐藏]")
			}
			value = hideAIPrivateEchoPrefix(value, item)
			for _, line := range strings.Split(item, "\n") {
				if line = strings.TrimSpace(line); len(line) >= 8 {
					value = strings.ReplaceAll(value, line, "[请求内容已隐藏]")
					value = hideAIPrivateEchoPrefix(value, line)
				}
			}
			var nested any
			if json.Unmarshal([]byte(item), &nested) == nil {
				hide(nested, false, depth+1)
			}
		case map[string]any:
			for key, child := range item {
				hide(child, private || diagnosticSensitiveKey.MatchString(key) || aiPrivateRequestField.MatchString(key), depth+1)
			}
		case []any:
			for _, child := range item {
				hide(child, private, depth+1)
			}
		}
	}
	for _, message := range request.Messages {
		hide(message.Content, false, 0)
	}
	return value
}

// clipAIErrorText 按UTF-8边界限制实际字节，避免中文被截成非法字符串；返回是否有截断。
func clipAIErrorText(value string, limit int) (string, bool) {
	if len(value) <= limit {
		return value, false
	}
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit], true
}

// redactAIErrorText 仅处理错误文字，完整隐藏认证行、已知密钥、联系方式和URL凭据，再删除本次请求内容的回显。
func redactAIErrorText(value string, payload []byte, credentials ...string) string {
	value = strings.ToValidUTF8(value, "�")
	value = hideAIRequestEcho(value, payload)
	for _, secret := range credentials {
		if secret = strings.TrimSpace(secret); secret != "" {
			value = strings.ReplaceAll(value, secret, "[密钥已隐藏]")
		}
	}
	value = aiErrorHeaderLine.ReplaceAllString(value, "[认证信息已隐藏]")
	value = aiErrorURLUser.ReplaceAllString(value, "${1}[凭据已隐藏]@")
	value = diagnosticCredentialJSONText.ReplaceAllString(value, "${1}[已隐藏]${2}")
	value = diagnosticSecretText.ReplaceAllString(value, "${1}[已隐藏]")
	value = aiErrorToken.ReplaceAllString(value, "[密钥已隐藏]")
	value = aiErrorDataURL.ReplaceAllString(value, "[图片或文件内容已隐藏]")
	value = diagnosticURLQuery.ReplaceAllString(value, "${1}?[已隐藏]")
	value = diagnosticPhoneText.ReplaceAllString(value, "[手机号已隐藏]")
	value = diagnosticEmailText.ReplaceAllString(value, "[邮箱已隐藏]")
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, value))
}

// aiErrorScalar 仅读取错误对象中的标量，不序列化任意嵌套内容或模型正文。
func aiErrorScalar(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return string(v)
	}
	return ""
}

// explicitAIError 从标准错误对象提取供应商实际message、code和有限details，忽略choices/messages/content等正文。
func explicitAIError(raw []byte, topLevel bool) (string, string) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object map[string]any
	if decoder.Decode(&object) != nil || object == nil {
		return "", ""
	}
	root := object
	if child, ok := object["error"]; ok && child != nil {
		if text, ok := child.(string); ok {
			return text, aiErrorScalar(object["code"])
		}
		if nested, ok := child.(map[string]any); ok {
			root = nested
		} else {
			return "", ""
		}
	} else if !topLevel {
		return "", ""
	}
	var parts []string
	for _, key := range []string{"message", "error_description", "detail", "msg", "reason"} {
		if text := aiErrorScalar(root[key]); text != "" {
			parts = append(parts, text)
		}
	}
	for _, key := range []string{"type", "param"} {
		if text := aiErrorScalar(root[key]); text != "" {
			parts = append(parts, key+"="+text)
		}
	}
	if details, ok := root["details"].(map[string]any); ok {
		for _, key := range []string{"message", "reason", "code", "type"} {
			if text := aiErrorScalar(details[key]); text != "" {
				parts = append(parts, key+"="+text)
			}
		}
	}
	return strings.Join(parts, "; "), aiErrorScalar(root["code"])
}

// aiJSONFailureExplanation 保留解析器的实际原因和字节位置，不附带可能包含简历的失败片段。
func aiJSONFailureExplanation(err error) string {
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		return fmt.Sprintf("%s（字节%d）", syntax.Error(), syntax.Offset)
	}
	return err.Error()
}

// aiFailureExplanation 保留具体失败阶段及JSON语法偏移，不回显无法解析的模型文字。
func aiFailureExplanation(reason, mode string, raw []byte, cause error) string {
	labels := map[string]string{
		"timeout":                "等待上游响应超时",
		"cancelled":              "AI 请求已取消",
		"dns_error":              "上游域名解析失败",
		"connection_refused":     "上游连接被拒绝",
		"connection_reset":       "上游连接被重置",
		"connection_closed":      "上游连接提前关闭",
		"tls_error":              "上游TLS连接失败",
		"response_empty":         "上游没有返回可用的正文或工具参数",
		"response_too_large":     "上游响应超过8MiB读取上限",
		"response_json_invalid":  "上游接口响应不是有效JSON",
		"model_json_invalid":     "模型输出不符合要求的JSON格式",
		"tool_json_invalid":      "模型返回的工具参数不是有效JSON",
		"tool_index_invalid":     "上游返回了无效的工具调用序号",
		"stream_invalid":         "上游事件流格式无效",
		"upstream_error":         "上游返回了错误事件",
		"request_not_replayable": "请求正文无法重新读取，未发起重试",
		"balance_unavailable":    "重试前没能确认AI余额",
		"balance_insufficient":   "余额不足，已停止后续请求",
		"billing_failed":         "本次用量结算失败，已停止后续请求",
		"billing_total_invalid":  "累计用量金额校验失败，已停止后续请求",
	}
	label := labels[reason]
	if label == "" {
		label = "上游请求失败"
	}
	var parsed any
	if reason == "response_json_invalid" {
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return label + "：" + aiJSONFailureExplanation(err)
		}
	}
	if reason == "model_json_invalid" || reason == "tool_json_invalid" {
		wire, _ := readAIWire(raw)
		contents := []string{strings.TrimSpace(wire.Content)}
		if reason == "tool_json_invalid" {
			contents = wire.Arguments
		} else if mode == "json_lines" {
			contents = strings.Split(wire.Content, "\n")
			label += "（要求每行一个JSON对象）"
		}
		for index, text := range contents {
			if strings.TrimSpace(text) != "" {
				if err := json.Unmarshal([]byte(text), &parsed); err != nil {
					return fmt.Sprintf("%s，第%d段：%s", label, index+1, aiJSONFailureExplanation(err))
				}
			}
		}
	}
	if reason == "stream_invalid" && bytes.Contains(raw, []byte("data:")) && !bytes.Contains(raw, []byte("[DONE]")) {
		return "上游事件流已结束，但缺少结束标记 [DONE]"
	}
	if cause != nil && (reason == "transport_error" || reason == "read_body" || reason == "read_stream" || reason == "timeout" || reason == "cancelled" || reason == "dns_error" || reason == "connection_refused" || reason == "connection_reset" || reason == "connection_closed" || reason == "tls_error") {
		var external *url.Error
		if errors.As(cause, &external) {
			cause = external.Err // URL可能带认证或查询串，错误详情只使用内部网络错误。
		}
		if reason == "transport_error" || reason == "read_body" || reason == "read_stream" {
			label = "连接或读取上游失败"
		}
		return label + "：" + cause.Error()
	}
	return label
}

// fillAIErrorDetail 为一次已失败请求保留实际错误文字；成功响应、简历和聊天正文永远不作为错误片段。
func fillAIErrorDetail(event *serverAIAttemptEvent, response *http.Response, raw, payload []byte, reason, mode string, cause error, credentials ...string) {
	if reason == "success" {
		return
	}
	detail, code := "", ""
	if response != nil {
		event.ResponseBytes = int64(len(raw))
		httpError := response.StatusCode < 200 || response.StatusCode >= 300
		detail, code = explicitAIError(raw, httpError)
		if detail == "" && strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
			for _, line := range strings.Split(string(raw), "\n") {
				if strings.HasPrefix(line, "data:") {
					if text, value := explicitAIError([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), false); text != "" {
						detail, code = text, value
						break
					}
				}
			}
		}
		if detail == "" && httpError {
			text := strings.TrimSpace(string(raw))
			if matches := aiErrorHTMLHeading.FindAllStringSubmatch(text, 2); len(matches) > 0 {
				var titles []string
				for _, match := range matches {
					title := html.UnescapeString(aiErrorHTMLTag.ReplaceAllString(match[1], " "))
					if aiPlainErrorSignal.MatchString(title) {
						titles = append(titles, strings.Join(strings.Fields(title), " "))
					}
				}
				detail = strings.Join(titles, "; ")
			} else if !strings.HasPrefix(text, "{") && !strings.HasPrefix(text, "[") && !strings.Contains(text, "<") && aiPlainErrorSignal.MatchString(text) {
				// 非标准响应只保留明确错误行，避免将后面夹带的请求或模型内容整体记入日志。
				for _, line := range strings.Split(text, "\n") {
					if aiPlainErrorSignal.MatchString(line) {
						detail = line
						break
					}
				}
			}
		}
		if httpError {
			prefix := fmt.Sprintf("上游 HTTP %d %s", response.StatusCode, http.StatusText(response.StatusCode))
			if detail == "" {
				detail = prefix + "；未提供可识别的具体错误说明"
			} else {
				detail = prefix + "：" + detail
			}
		}
	}
	if detail == "" {
		detail = aiFailureExplanation(reason, mode, raw, cause)
	}
	detail = redactAIErrorText(detail, payload, credentials...)
	event.ErrorDetail, event.ErrorDetailTruncated = clipAIErrorText(detail, maxAIErrorDetailBytes)
	code = redactAIErrorText(code, payload, credentials...)
	if len(code) <= 128 && !strings.Contains(code, "已隐藏") {
		event.ErrorCode = safeAITraceID(code)
	}
}

// aiFinalFailureBody 给新旧客户端返回具体终止原因及逐次详情；保留原错误码并由调用方返回不可重试的4xx。
func aiFinalFailureBody(err error, attempts []serverAIAttemptEvent) map[string]any {
	detail, stop, upstreamStatus := "", "", 0
	var failure *serverAIRequestError
	if errors.As(err, &failure) {
		stop = failure.Reason
		if failure.Reason == "balance_unavailable" || failure.Reason == "balance_insufficient" || failure.Reason == "billing_failed" || failure.Reason == "billing_total_invalid" || failure.Reason == "request_not_replayable" {
			detail = aiFailureExplanation(failure.Reason, "", nil, nil)
		}
	}
	if errors.Is(err, context.Canceled) {
		detail, stop = "AI 请求已取消，后续尝试已停止", "cancelled"
	} else if errors.Is(err, context.DeadlineExceeded) {
		detail, stop = "AI 请求时间已到，后续尝试已停止", "timeout"
	}
	if len(attempts) > 0 {
		last := attempts[len(attempts)-1]
		upstreamStatus = last.HTTPStatus
		if detail == "" {
			detail = last.ErrorDetail
		}
	}
	if detail == "" && failure != nil {
		detail = failure.Detail
	}
	if detail == "" {
		detail = "未收到可用的AI响应；上游没有提供更具体的错误说明"
	}
	summary, clipped := clipAIErrorText(detail, 512)
	if clipped {
		summary += "…（完整原因见错误详情）"
	}
	message := "AI 请求失败：" + summary
	body := map[string]any{"error": message, "code": "AI_RETRY_EXHAUSTED", "attempts": attempts}
	if len(attempts) > 0 {
		body["attempt_count"] = len(attempts)
		body["error"] = fmt.Sprintf("AI 请求失败（共请求%d次）：%s", len(attempts), summary)
	} else if failure != nil {
		body["attempt_count"] = failure.Attempts
	}
	if upstreamStatus > 0 {
		body["upstream_status"] = upstreamStatus
	}
	if stop != "" {
		body["stop_reason"] = stop
	}
	return body
}
