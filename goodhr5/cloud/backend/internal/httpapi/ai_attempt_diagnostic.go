// Package httpapi 文件作用：为内置 AI 每次真实上游请求记录安全的关联、耗时与重试原因，并在重试时完整切换备用接口配置。
package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"unicode"
)

const aiAttemptDetailsHeader = "X-GoodHR-AI-Attempt-Details"
const aiClientRequestIDHeader = "X-GoodHR-Client-Request-ID"
const aiAnalysisGroupIDHeader = "X-GoodHR-Analysis-Group-ID"

var aiProviderRequestIDHeaders = []string{"X-Request-ID", "Request-ID", "X-Amzn-RequestId", "Apim-Request-Id"}

// serverAIAttemptEvent 只保存实际请求的固定枚举与数字，不保存模型输入、响应正文、地址或凭据。
type serverAIAttemptEvent struct {
	Layer                string `json:"layer"`
	Operation            string `json:"operation"`
	Event                string `json:"event,omitempty"`
	ClientRequestID      string `json:"client_request_id,omitempty"`
	AnalysisGroupID      string `json:"analysis_group_id,omitempty"`
	ServerAttempt        int    `json:"server_attempt"`
	MaxAttempts          int    `json:"max_attempts"`
	Provider             string `json:"provider"`
	Model                string `json:"model,omitempty"`
	StartedAt            string `json:"started_at"`
	FinishedAt           string `json:"finished_at,omitempty"`
	ElapsedMS            int64  `json:"elapsed_ms"`
	HTTPStatus           int    `json:"http_status"`
	Result               string `json:"result,omitempty"`
	Reason               string `json:"reason,omitempty"`
	StopReason           string `json:"stop_reason,omitempty"`
	WillRetry            bool   `json:"will_retry"`
	BackoffMS            int64  `json:"backoff_ms"`
	ProviderRequestID    string `json:"provider_request_id,omitempty"`
	ErrorDetail          string `json:"error_detail,omitempty"`
	ErrorCode            string `json:"error_code,omitempty"`
	ErrorDetailTruncated bool   `json:"error_detail_truncated,omitempty"`
	ResponseBytes        int64  `json:"response_bytes,omitempty"`
}

// safeAITraceID 仅接受短 ASCII 标识，拒绝正文、控制符和凭据格式；不截取可能有歧义的标识。
func safeAITraceID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > 128 {
		return ""
	}
	for _, c := range value {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == ':' {
			continue
		}
		return ""
	}
	return value
}

// safeAIModelName 允许常见模型路径分隔符，只记录配置中的短模型名。
func safeAIModelName(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 200 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return ""
	}
	return value
}

// aiProviderRequestID 只读取上游明确的请求标识响应头，未知头和不安全值均不记录。
func aiProviderRequestID(header http.Header, credentials ...string) string {
	for _, key := range aiProviderRequestIDHeaders {
		if id := safeAITraceID(header.Get(key)); id != "" {
			if !containsAITraceCredential(id, credentials...) {
				return id
			}
		}
	}
	return ""
}

// containsAITraceCredential 拒绝服务商将真实凭据回显为请求ID，包括带前后缀的回显。
func containsAITraceCredential(value string, credentials ...string) bool {
	for _, credential := range credentials {
		if credential = strings.TrimSpace(credential); credential != "" && strings.Contains(value, credential) {
			return true
		}
	}
	return false
}

// omitCredentialRequestIDHeaders 阻止已知密钥从普通上游请求ID响应头绕过安全元数据协议返回。
func omitCredentialRequestIDHeaders(header http.Header, credentials ...string) {
	for _, key := range aiProviderRequestIDHeaders {
		if containsAITraceCredential(header.Get(key), credentials...) {
			header.Del(key)
		}
	}
}

// observeServerAIAttempt 记录真实请求开始和结束；回调只传安全结构，不改变原请求结果。
func observeServerAIAttempt(option serverAIRequestOptions, event serverAIAttemptEvent) {
	if encoded, err := json.Marshal(event); err == nil {
		log.Printf("[AI请求记录] %s", encoded)
	}
	if option.Observe != nil {
		option.Observe(event)
	}
}

// usableAIFallback 只启用完整、有效的备用接口；历史缺项配置继续使用原有主用重试路径。
func usableAIFallback(config *BuiltinAIFallbackConfig) bool {
	if config == nil || !config.Enabled {
		return false
	}
	copy := *config
	copy.BaseURL, copy.APIKey, copy.Model = strings.TrimSpace(copy.BaseURL), strings.TrimSpace(copy.APIKey), strings.TrimSpace(copy.Model)
	return validateAIFallbackConfig(copy) == nil && !isCurrentSiteAIURL(copy.BaseURL)
}

// prepareAIFallbackRequest 对本次请求副本同时替换地址、凭据和模型，保留流、JSON 及消息合同。
func prepareAIFallbackRequest(request *http.Request, original []byte, config *BuiltinAIFallbackConfig) (*http.Request, error) {
	endpoint, err := url.Parse(normalizeAIChatCompletionsURL(config.BaseURL))
	if err != nil {
		return nil, err
	}
	body := rewriteAIModel(original, strings.TrimSpace(config.Model))
	if request.Body != nil {
		request.Body.Close()
	}
	request.URL, request.Host = endpoint, ""
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(config.APIKey))
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	request.ContentLength, request.TransferEncoding = int64(len(body)), nil
	return request, nil
}

// aiAttemptDetailsWriter 收集一轮内最多四次结束记录，并复用诊断协商头返回给本地岗位日志。
type aiAttemptDetailsWriter struct {
	writer   http.ResponseWriter
	enabled  bool
	attempts []serverAIAttemptEvent
}

// newAIAttemptDetailsWriter 只为明确协商诊断协议的客户端生成扩展回执。
func newAIAttemptDetailsWriter(w http.ResponseWriter, request *http.Request) *aiAttemptDetailsWriter {
	return &aiAttemptDetailsWriter{writer: w, enabled: request.Header.Get(aiDiagnosticHeader) == "1"}
}

// observe 将结束记录附到响应头；后续成功也保留之前失败，原响应正文和收费回执不变。
func (w *aiAttemptDetailsWriter) observe(event serverAIAttemptEvent) {
	if event.Event != "end" || len(w.attempts) >= serverAIMaxAttempts {
		return
	}
	w.attempts = append(w.attempts, event)
	w.writeHeader()
}

// writeHeader 在原8KiB预算内保留全部尝试；压缩详情时标明截断，不让整批记录因响应头过大被丢弃。
func (w *aiAttemptDetailsWriter) writeHeader() {
	if !w.enabled {
		return
	}
	compact := append([]serverAIAttemptEvent(nil), w.attempts...)
	for limit := 1024; limit >= 0; limit /= 2 {
		for index := range compact {
			text, clipped := clipAIErrorText(compact[index].ErrorDetail, limit)
			compact[index].ErrorDetail = text
			compact[index].ErrorDetailTruncated = compact[index].ErrorDetailTruncated || clipped
		}
		var output bytes.Buffer
		encoder := json.NewEncoder(&output)
		encoder.SetEscapeHTML(false) // 响应头不是HTML；避免错误中的<>&无谓膨胀并挤掉其他尝试。
		if err := encoder.Encode(compact); err == nil && output.Len()-1 <= 8<<10 {
			w.writer.Header().Set(aiAttemptDetailsHeader, strings.TrimSuffix(output.String(), "\n"))
			return
		}
		if limit == 0 {
			break
		}
	}
}

// stop 标记退避或下一次开始前停止，避免把计划重试写成已经发起了下一次请求。
func (w *aiAttemptDetailsWriter) stop(err error) {
	if err == nil || len(w.attempts) == 0 {
		return
	}
	last := &w.attempts[len(w.attempts)-1]
	if !last.WillRetry {
		return
	}
	last.WillRetry, last.StopReason = false, aiProxyReason(err, "retry_stopped")
	w.writeHeader()
}
