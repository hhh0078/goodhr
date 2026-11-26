// Package ai 文件作用：记录每次真实AI请求与重试决策的安全元数据，将固定岗位归属传给现有日志链，不记录请求正文。
package ai

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"goodhr5/local-agent-go-new/internal/system/diagnostics"
)

// RequestAttempt 只包含请求关联、计时与有限结果分类，不能添加提示词、候选人或聊天正文。
type RequestAttempt struct {
	Layer             string `json:"layer"`
	Phase             string `json:"phase"`
	ClientRequestID   string `json:"client_request_id"`
	AnalysisGroupID   string `json:"analysis_group_id"`
	ClientAttempt     int64  `json:"client_attempt"`
	ServerAttempt     int64  `json:"server_attempt,omitempty"`
	MaxAttempts       int64  `json:"max_attempts"`
	Operation         string `json:"operation"`
	Model             string `json:"model"`
	Provider          string `json:"provider"`
	StartedAt         string `json:"started_at"`
	FinishedAt        string `json:"finished_at,omitempty"`
	ElapsedMS         int64  `json:"elapsed_ms"`
	HTTPStatus        int    `json:"http_status"`
	Result            string `json:"result"`
	Reason            string `json:"reason"`
	RetryReason       string `json:"retry_reason,omitempty"`
	WillRetry         bool   `json:"will_retry"`
	BackoffMS         int64  `json:"backoff_ms"`
	StopReason        string `json:"stop_reason,omitempty"`
	ProviderRequestID string `json:"provider_request_id,omitempty"`
}

// RequestLogLine 使用与其他任务日志相同的结构，stdout和岗位回调共用这一条已归一化日志。
type RequestLogLine struct {
	Timestamp    string `json:"timestamp"`
	TaskID       string `json:"task_id"`
	TraceID      string `json:"trace_id"`
	PositionID   string `json:"position_id"`
	Action       string `json:"action"`
	Step         string `json:"step"`
	Status       string `json:"status"`
	Message      string `json:"message"`
	Category     string `json:"category"`
	IsError      bool   `json:"is_error"`
	ErrorMessage string `json:"error_message"`
	DurationMS   int64  `json:"duration_ms"`
	Output       struct {
		Attempt RequestAttempt `json:"ai_request_attempt"`
	} `json:"output"`
}

type requestLogScope struct {
	taskID, positionID string
	report             func([]byte, error)
}
type requestLogScopeKey struct{}
type requestLogGroup struct {
	id          string
	maxAttempts int64
}
type requestLogGroupKey struct{}
type requestLogAttemptKey struct{}
type requestLogAttempt struct {
	ctx     context.Context
	event   RequestAttempt
	started time.Time
}

var requestLogSequence atomic.Uint64

// WithRequestLogReporter 固定本次分析的任务和岗位归属，异步尾部不得猜测当前正在运行的岗位。
func WithRequestLogReporter(ctx context.Context, taskID, positionID string, report func([]byte, error)) context.Context {
	return context.WithValue(ctx, requestLogScopeKey{}, requestLogScope{taskID: strings.TrimSpace(taskID), positionID: strings.TrimSpace(positionID), report: report})
}

// newAIRequestID 为逻辑分析和真实HTTP请求生成独立标识，不包含用户或候选人信息。
func newAIRequestID(prefix string) string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err == nil {
		return prefix + hex.EncodeToString(value[:])
	}
	return fmt.Sprintf("%s%x%x", prefix, time.Now().UnixNano(), requestLogSequence.Add(1))
}

// withRequestLogGroup 让同一逻辑调用的重试共享分析编号，单次解析仍只允许一次请求。
func withRequestLogGroup(ctx context.Context, maxAttempts int64) context.Context {
	if _, ok := ctx.Value(requestLogGroupKey{}).(requestLogGroup); ok {
		return ctx
	}
	return context.WithValue(ctx, requestLogGroupKey{}, requestLogGroup{id: newAIRequestID("aigr_"), maxAttempts: max(1, maxAttempts)})
}

// withRequestLogAttempt 保存本次请求记录，供退避取消时说明为什么没有真正发起下一次。
func withRequestLogAttempt(ctx context.Context) (context.Context, *requestLogAttempt) {
	value := &requestLogAttempt{}
	return context.WithValue(ctx, requestLogAttemptKey{}, value), value
}

// safeRequestLabel 只接收有限长度的请求标识，非法值保持未知而不输出外部正文。
func safeRequestLabel(value string) string {
	if value == "" || len(value) > 128 {
		return ""
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' || r == ':') {
			return ""
		}
	}
	return value
}

// safeModelLabel 沿用后台模型名的200字节限制，保留合法中文和路径，只拒绝控制字符或越界内容。
func safeModelLabel(value string) string {
	if len(value) > 200 {
		return ""
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return value
}

// startRequestLog 在真正调用HTTP客户端前记录开始，只提取payload中的模型名。
func startRequestLog(ctx context.Context, req *http.Request, payload []byte, operation string, attempt int64, builtin bool) *requestLogAttempt {
	ctx = withRequestLogGroup(ctx, 1)
	group := ctx.Value(requestLogGroupKey{}).(requestLogGroup)
	record, _ := ctx.Value(requestLogAttemptKey{}).(*requestLogAttempt)
	if record == nil {
		record = &requestLogAttempt{}
	}
	var model struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(payload, &model)
	if operation == "" {
		operation = "AI 请求"
	}
	provider := "external"
	if builtin {
		provider = "builtin"
	}
	record.ctx, record.started = ctx, time.Now()
	record.event = RequestAttempt{Layer: "client", Phase: "start", ClientRequestID: newAIRequestID("airq_"), AnalysisGroupID: group.id, ClientAttempt: max(1, attempt), MaxAttempts: group.maxAttempts, Operation: operation, Model: safeModelLabel(model.Model), Provider: provider, StartedAt: record.started.UTC().Format(time.RFC3339Nano), Result: "started", Reason: "request_started"}
	if builtin {
		req.Header.Set("X-GoodHR-Client-Request-ID", record.event.ClientRequestID)
		req.Header.Set("X-GoodHR-Analysis-Group-ID", record.event.AnalysisGroupID)
	}
	record.emit(record.event, nil)
	return record
}

// requestRetryDecision 复用原有三次/RetryAfter退避规则，已可用评分的请求不允许重新发起。
func requestRetryDecision(err error, attempt, maxAttempts int64, early bool) (bool, time.Duration) {
	var service *ServiceError
	if err == nil || early || attempt >= maxAttempts || !errors.As(err, &service) || !service.Retryable {
		return false, 0
	}
	return true, max(service.RetryAfter, time.Duration(attempt)*500*time.Millisecond)
}

// requestFailureReason 使用错误类型和状态码分类，不能把URL、返回正文或密钥作为重试原因。
func requestFailureReason(err error, status int) string {
	if err == nil {
		return "success"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return "timeout"
	}
	if status >= 400 {
		return "http_status"
	}
	var syntax *json.SyntaxError
	var typ *json.UnmarshalTypeError
	if errors.As(err, &syntax) || errors.As(err, &typ) {
		return "response_json_invalid"
	}
	if status == 0 {
		return "transport_error"
	}
	return "response_invalid"
}

// finish 在原诊断包装后记录结束，will_retry只表示准备重试，是否发出由下一条真实start证明。
func (r *requestLogAttempt) finish(observation *aiAttempt, err error) {
	if r == nil || r.ctx == nil {
		return
	}
	event := r.event
	event.Phase = "end"
	event.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	event.ElapsedMS = time.Since(r.started).Milliseconds()
	if observation != nil {
		event.HTTPStatus = observation.detail.HTTPStatus
	}
	event.Result = "success"
	if err != nil {
		event.Result = "failed"
	}
	event.Reason = requestFailureReason(err, event.HTTPStatus)
	if err != nil && event.Reason == "response_invalid" && observation != nil && observation.readErr != nil {
		event.Reason = "read_body"
		if observation.stream {
			event.Reason = "read_stream"
		}
	}
	early := observation != nil && observation.early
	willRetry, delay := requestRetryDecision(err, event.ClientAttempt, event.MaxAttempts, early)
	event.WillRetry, event.BackoffMS = willRetry, delay.Milliseconds()
	if willRetry {
		event.RetryReason = event.Reason
	}
	if err != nil && !willRetry {
		event.StopReason = "not_retryable"
		if early {
			event.StopReason = "early_score_available"
		} else if event.ClientAttempt >= event.MaxAttempts {
			event.StopReason = "attempts_exhausted"
		}
	}
	r.event = event
	r.emit(event, err)
}

// retryStopped 保留退避被取消的事实，不把计划重试伪装成已执行的新请求。
func (r *requestLogAttempt) retryStopped(err error, started time.Time, delay time.Duration) {
	if r == nil || r.ctx == nil {
		return
	}
	event := r.event
	event.Phase = "retry_wait_stopped"
	event.Result = "failed"
	event.WillRetry = false
	event.BackoffMS = delay.Milliseconds()
	event.StopReason = requestFailureReason(err, 0)
	event.StartedAt = started.UTC().Format(time.RFC3339Nano)
	event.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	event.ElapsedMS = time.Since(started).Milliseconds()
	r.emit(event, err)
}

// RequestAttemptMessage 统一生成不含姓名和正文的岗位摘要，成功仅表示请求完成而非评分已通过。
func RequestAttemptMessage(event RequestAttempt) string {
	layer, attempt := "本机", event.ClientAttempt
	if event.Layer == "server" {
		layer, attempt = "上游", event.ServerAttempt
	}
	message := fmt.Sprintf("%s，%s第%d次请求", event.Operation, layer, attempt)
	if event.Phase == "start" {
		return message + "开始"
	}
	if event.Phase == "retry_wait_stopped" {
		reason := "已停止"
		if event.StopReason == "cancelled" {
			reason = "已取消"
		} else if event.StopReason == "timeout" {
			reason = "已超时"
		}
		return message + "的重试等待" + reason + "，没有发起下一次请求"
	}
	if event.Result == "success" {
		return fmt.Sprintf("%s完成，耗时%d毫秒", message, event.ElapsedMS)
	}
	message += fmt.Sprintf("失败（%s），耗时%d毫秒", RequestAttemptReason(event), event.ElapsedMS)
	if event.WillRetry {
		message += fmt.Sprintf("，准备重试，等待%d毫秒", event.BackoffMS)
	}
	return message
}

// RequestAttemptReason 将有限分类翻成中文，原始完整错误仍由既有诊断详情保存。
func RequestAttemptReason(event RequestAttempt) string {
	if event.Phase == "retry_wait_stopped" {
		if event.StopReason == "cancelled" {
			return "重试等待已取消"
		}
		if event.StopReason == "timeout" {
			return "重试等待超时"
		}
	}
	if event.HTTPStatus >= 400 {
		return fmt.Sprintf("HTTP %d", event.HTTPStatus)
	}
	labels := map[string]string{"cancelled": "请求已取消", "timeout": "等待超时", "transport_error": "连接失败", "dns_error": "域名解析失败", "connection_refused": "服务拒绝连接", "connection_reset": "连接被重置", "connection_closed": "连接提前关闭", "tls_error": "安全连接握手失败", "response_json_invalid": "响应格式无法解析", "response_invalid": "响应内容无法处理", "read_body": "读取响应体失败", "read_stream": "事件流未正常完成", "http_status": "服务返回错误状态", "response_too_large": "响应超过读取上限", "tool_json_invalid": "工具参数无法解析", "tool_index_invalid": "工具序号无效", "model_json_invalid": "模型输出无法解析", "stream_invalid": "事件流格式无效", "response_empty": "服务没有返回内容", "billing_failed": "计费回执未完成", "billing_total_invalid": "计费汇总无效", "retry_stopped": "后续重试已停止", "upstream_error": "模型服务返回错误"}
	if label := labels[event.Reason]; label != "" {
		return label
	}
	return "请求未完成"
}

// emit 将同一条结构化日志送入标准输出和固定岗位回调，回调不再依赖当前任务指针。
func (r *requestLogAttempt) emit(event RequestAttempt, cause error) {
	scope, _ := r.ctx.Value(requestLogScopeKey{}).(requestLogScope)
	line := RequestLogLine{Timestamp: event.StartedAt, TaskID: scope.taskID, TraceID: scope.taskID, PositionID: scope.positionID, Action: "ai.request", Step: "ai_request_attempt", Status: event.Result, Message: RequestAttemptMessage(event), Category: "运行", DurationMS: event.ElapsedMS}
	if event.Phase != "start" {
		line.Timestamp = event.FinishedAt
	}
	line.IsError = event.Result == "failed"
	if line.IsError {
		line.Category = "错误"
		// 页面优先显示error_message，因此这里保留本次请求与准备重试的上下文，避免只剩一个HTTP状态码。
		line.ErrorMessage = line.Message
	}
	line.Output.Attempt = event
	encoded, err := json.Marshal(line)
	if err != nil {
		return
	}
	safe := bytes.TrimSpace(diagnostics.Normalize(encoded))
	log.Print(string(safe))
	if scope.report != nil {
		scope.report(safe, cause)
	}
}

// serverAttempts 仅接收协商过的可信代理元数据，返回的历史失败也独立进入岗位日志。
func (r *requestLogAttempt) serverAttempts(header string, trusted bool) {
	if r == nil || !trusted || header == "" || len(header) > 8<<10 {
		return
	}
	var entries []RequestAttempt
	if json.Unmarshal([]byte(header), &entries) != nil || len(entries) == 0 || len(entries) > 4 {
		return
	}
	previous := int64(0)
	allowed := map[string]bool{}
	for _, v := range strings.Fields("success http_status transport_error dns_error connection_refused connection_reset connection_closed tls_error timeout cancelled read_body read_stream response_too_large response_json_invalid tool_json_invalid tool_index_invalid model_json_invalid stream_invalid response_empty billing_failed billing_total_invalid retry_stopped upstream_error") {
		allowed[v] = true
	}
	allowedStop := map[string]bool{"": true, "billing_failed": true, "billing_total_invalid": true, "retry_stopped": true, "cancelled": true, "timeout": true}
	for _, event := range entries {
		start, e1 := time.Parse(time.RFC3339Nano, event.StartedAt)
		finish, e2 := time.Parse(time.RFC3339Nano, event.FinishedAt)
		if event.Layer != "server" || event.ClientRequestID != r.event.ClientRequestID || event.AnalysisGroupID != "" && event.AnalysisGroupID != r.event.AnalysisGroupID || event.ClientAttempt != 0 && event.ClientAttempt != r.event.ClientAttempt || event.ServerAttempt <= previous || event.ServerAttempt < 1 || event.MaxAttempts > 4 || event.MaxAttempts < event.ServerAttempt || event.ElapsedMS < 0 || event.BackoffMS < 0 || event.HTTPStatus != 0 && (event.HTTPStatus < 100 || event.HTTPStatus > 599) || !allowed[event.Reason] || !allowedStop[event.StopReason] || event.Result != "success" && event.Result != "failed" || event.Provider != "primary" && event.Provider != "fallback" || e1 != nil || e2 != nil || finish.Before(start) {
			return
		}
		previous = event.ServerAttempt
	}
	for _, event := range entries {
		event.ClientAttempt = r.event.ClientAttempt
		event.AnalysisGroupID = r.event.AnalysisGroupID
		event.Model = safeModelLabel(event.Model)
		event.ProviderRequestID = safeRequestLabel(event.ProviderRequestID)
		// 业务操作来自本机调用现场，代理通用chat_completions标签不能覆盖它。
		event.Operation = r.event.Operation
		event.RetryReason = ""
		if event.WillRetry {
			event.RetryReason = event.Reason
		}
		started := event
		started.Phase = "start"
		started.Result = "started"
		started.HTTPStatus = 0
		started.ElapsedMS = 0
		started.FinishedAt = ""
		started.WillRetry = false
		started.BackoffMS = 0
		started.Reason = "request_started"
		started.RetryReason, started.StopReason, started.ProviderRequestID = "", "", ""
		r.emit(started, nil)
		event.Phase = "end"
		r.emit(event, nil)
	}
}
