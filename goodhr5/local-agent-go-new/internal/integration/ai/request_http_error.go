// Package ai 文件作用：只在HTTP失败边界清理认证回显，完整错误响应保留其它内容和字节布局，不处理成功模型输出。
package ai

import (
	"encoding/json"
	"regexp"
	"strings"
)

var requestErrorBearer = regexp.MustCompile(`(?i)Bearer\s+[^\s",;]+`)
var requestErrorURLQuery = regexp.MustCompile(`(https?://[^\s?"<>]+)\?[^\s"<>]*`)

// redactRequestErrorCredentials 只隐藏认证形式和已知请求密钥，保留普通错误文字、换行和非认证数据。
func redactRequestErrorCredentials(value, credential string) string {
	if credential != "" {
		value = strings.ReplaceAll(value, credential, "[已隐藏]")
	}
	value = requestErrorHeaderLine.ReplaceAllString(value, "[认证信息已隐藏]")
	value = requestErrorCredential.ReplaceAllString(value, "${1}[已隐藏]")
	value = requestErrorURLCredential.ReplaceAllString(value, "${1}[已隐藏]@")
	value = requestErrorBearer.ReplaceAllString(value, "Bearer [已隐藏]")
	return requestErrorURLQuery.ReplaceAllString(value, "${1}?[已隐藏]")
}

// redactRequestHTTPErrorBody 逐个清理JSON字符串，不解析业务字段或重排整个响应；非JSON错误按认证行清理。
func redactRequestHTTPErrorBody(body, credential string) string {
	if !json.Valid([]byte(body)) {
		return redactRequestErrorCredentials(body, credential)
	}
	var output strings.Builder
	copyFrom := 0
	for index := 0; index < len(body); {
		if body[index] != '"' {
			index++
			continue
		}
		start := index
		index++
		for index < len(body) {
			if body[index] == '\\' {
				index += 2
				continue
			}
			index++
			if body[index-1] == '"' {
				break
			}
		}
		var original string
		if json.Unmarshal([]byte(body[start:index]), &original) != nil {
			return redactRequestErrorCredentials(body, credential)
		}
		safe := redactRequestErrorCredentials(original, credential)
		if safe != original {
			output.WriteString(body[copyFrom:start])
			encoded, _ := json.Marshal(safe)
			output.Write(encoded)
			copyFrom = index
		}
		next := requestHTTPValueStart(body, index)
		if next < len(body) && body[next] == ':' && requestHTTPSecretKey(original) {
			next = requestHTTPValueStart(body, next+1)
			decoder := json.NewDecoder(strings.NewReader(body[next:]))
			var value json.RawMessage
			if decoder.Decode(&value) != nil {
				return redactRequestErrorCredentials(body, credential)
			}
			// 认证字段也可能是数组、对象或数字，整块替换其原始span，避免子项绕过字符串清理。
			end := next + int(decoder.InputOffset())
			output.WriteString(body[copyFrom:next])
			output.WriteString(`"[认证信息已隐藏]"`)
			copyFrom, index = end, end
		}
	}
	if copyFrom == 0 {
		return body
	}
	output.WriteString(body[copyFrom:])
	return output.String()
}

// requestHTTPValueStart 跳过JSON允许的空白，定位当前键后的原始值，不读取任何响应业务对象。
func requestHTTPValueStart(body string, start int) int {
	for start < len(body) && strings.ContainsRune(" \r\n\t", rune(body[start])) {
		start++
	}
	return start
}

// requestHTTPSecretKey 仅识别常见认证字段，普通错误和模型结果字段保持原样。
func requestHTTPSecretKey(key string) bool {
	switch strings.ToLower(key) {
	case "authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "api_key", "api-key", "apikey", "token", "access_token", "refresh_token", "password", "secret":
		return true
	}
	return false
}
