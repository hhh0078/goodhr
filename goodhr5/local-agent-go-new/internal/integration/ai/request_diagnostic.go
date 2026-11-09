// Package ai 文件作用：记录单次 AI 请求从连接、响应头、首段内容到流结束的时限证据，只保存固定描述和数字。
package ai

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"goodhr5/local-agent-go-new/internal/config"
	"goodhr5/local-agent-go-new/internal/system/failurediag"
)

var errScoreBudget = errors.New("单次 AI 评分的等待时限已到")
var errScoreParentDeadline = errors.New("上层任务的处理时限已到")

// scoreParentDeadlineKey 保存上层截止时间，提前返回评分后不再用它限制尾部请求。
type scoreParentDeadlineKey struct{}

// ScoringContext 给评分设置独立预算；等待评分时跟随父任务，评分可用后可解除关联并保留尾部输出。
func ScoringContext(parent context.Context, budget time.Duration) (context.Context, context.CancelFunc, func() bool) {
	linked, cancelParent := context.WithCancelCause(context.WithoutCancel(parent))
	ctx, cancelBudget := context.WithTimeoutCause(linked, budget, errScoreBudget)
	detach := context.AfterFunc(parent, func() {
		if errors.Is(parent.Err(), context.DeadlineExceeded) {
			cancelParent(errScoreParentDeadline)
		} else {
			cancelParent(context.Canceled)
		}
	})
	if deadline, ok := parent.Deadline(); ok {
		ctx = context.WithValue(ctx, scoreParentDeadlineKey{}, deadline)
	}
	return ctx, func() { detach(); cancelBudget(); cancelParent(nil) }, detach
}

type aiAttempt struct {
	completion                                       *streamCompletion
	ctx                                              context.Context
	started                                          time.Time
	limit                                            time.Duration
	detail                                           failurediag.Detail
	transport                                        atomic.Int32
	builtin, trustedResponse, stream, content, early bool
	proxy, upstreamCompletion                        *failurediag.Detail
	readErr                                          error
}

// startAIAttempt 创建本次请求的独立计时器，区分本机配置的内置代理与外部接口。
func startAIAttempt(ctx context.Context, req *http.Request, apiKey, operation string, attempt int64, limit time.Duration) (*aiAttempt, *http.Request) {
	if operation == "" {
		operation = "AI 请求"
	}
	if attempt < 1 {
		attempt = 1
	}
	t := &aiAttempt{ctx: ctx, started: time.Now(), limit: limit, builtin: strings.HasPrefix(strings.TrimSpace(apiKey), "ghai_") && trustedAIProxy(req.URL)}
	t.detail = failurediag.Detail{Source: "AI 客户端（自定义接口）", Operation: operation, Attempt: attempt}
	if t.builtin {
		t.detail.Source = "AI 客户端（内置接口）"
		req.Header.Set("X-GoodHR-Diagnostics", "1")
	}
	trace := &httptrace.ClientTrace{
		DNSStart:          func(httptrace.DNSStartInfo) { t.transport.Store(1) },
		ConnectStart:      func(string, string) { t.transport.Store(2) },
		TLSHandshakeStart: func() { t.transport.Store(3) },
		GotConn:           func(httptrace.GotConnInfo) { t.transport.Store(4) },
		WroteRequest: func(w httptrace.WroteRequestInfo) {
			if w.Err == nil {
				t.transport.Store(5)
			}
		},
	}
	return t, req.WithContext(httptrace.WithClientTrace(ctx, trace))
}

// trustedAIProxy 仅认可部署自身的精确来源与兼容路由，不采用外部接口发送的诊断信号。
func trustedAIProxy(endpoint *url.URL) bool {
	if endpoint == nil || endpoint.User != nil || endpoint.Path != "/api/ai-compatible/v1/chat/completions" {
		return false
	}
	for _, base := range []string{os.Getenv("GOODHR_PUBLIC_BASE_URL"), os.Getenv("GOODHR_CLOUD_API_BASE"), config.DefaultCloudURL, "https://goodhr.58it.cn"} {
		u, err := url.Parse(strings.TrimSpace(base))
		if err == nil && u.Host != "" && u.User == nil && strings.EqualFold(u.Host, endpoint.Host) && strings.EqualFold(u.Scheme, endpoint.Scheme) {
			return true
		}
	}
	return false
}

// aiObservedBody 将读取的字节计入当前请求，包括心跳与计费回执，不保存正文。
type aiObservedBody struct {
	io.ReadCloser
	attempt *aiAttempt
}

// Read 转发读取结果并累计真正从连接读到的字节。
func (r *aiObservedBody) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.attempt.detail.ReceivedBytes += int64(n)
	if err != nil && !errors.Is(err, io.EOF) {
		r.attempt.readErr = err
	}
	return n, err
}

// headers 标记收到响应头并登记可校验的代理来源，重定向后的来源必须重新检查。
func (t *aiAttempt) headers(resp *http.Response) {
	t.detail.HeadersMS = time.Since(t.started).Milliseconds()
	t.detail.HTTPStatus = resp.StatusCode
	t.stream = strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream")
	t.trustedResponse = t.builtin && resp.Request != nil && trustedAIProxy(resp.Request.URL) && resp.Header.Get("X-GoodHR-Diagnostics") == "1"
	if t.trustedResponse {
		t.proxySignal([]byte(resp.Header.Get("X-GoodHR-Upstream-Failure")))
	}
	resp.Body = &aiObservedBody{ReadCloser: resp.Body, attempt: t}
}

// gotContent 只在收到模型正文或工具增量时记录首内容时间，保活包不算模型内容。
func (t *aiAttempt) gotContent() {
	if t != nil && !t.content {
		t.content = true
		t.detail.FirstContentMS = time.Since(t.started).Milliseconds()
	}
}

// aiTransportPhase 报告传输层最后证实的阶段，证据不足时不承诺已经送达服务端。
func (t *aiAttempt) aiTransportPhase() string {
	switch t.transport.Load() {
	case 1:
		return "解析 AI 服务域名"
	case 2:
		return "建立 AI 网络连接"
	case 3:
		return "AI TLS 握手"
	case 4:
		return "发送 AI 请求"
	case 5:
		return "等待 AI 响应头"
	default:
		return "连接 AI 服务或等待响应头"
	}
}

// aiContextReason 仅在真实错误链指向取消或到期时认领上下文，避免普通 429 与任务结束同时发生时误报。
func aiContextReason(ctx context.Context, err error) string {
	activeCause := context.Cause(ctx)
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) && !(activeCause != nil && errors.Is(err, activeCause)) {
		return ""
	}
	switch {
	case errors.Is(context.Cause(ctx), errScoreBudget):
		return "本轮 AI 评分时限已到"
	case errors.Is(context.Cause(ctx), errScoreParentDeadline):
		return "上层任务时限已到，取消 AI 等待"
	case errors.Is(ctx.Err(), context.Canceled):
		return "上层已取消 AI 请求"
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return "调用方等待时限已到"
	default:
		return ""
	}
}

// boundaryReason 使用明确的上下文、HTTP 状态和系统错误判断原因，不从外部响应正文推断。
func (t *aiAttempt) boundaryReason(err error) string {
	status := t.detail.HTTPStatus
	switch status {
	case 408:
		return "AI 接口返回 HTTP 408 请求超时"
	case 504:
		return "AI 接口返回 HTTP 504 网关超时"
	case 429:
		return "AI 接口返回 HTTP 429 限流"
	case 502:
		return "AI 接口返回 HTTP 502 网关错误"
	}
	if status >= 400 {
		return fmt.Sprintf("AI 接口返回 HTTP %d", status)
	}
	if reason := aiContextReason(t.ctx, err); reason != "" {
		return reason
	}
	if t.readErr != nil {
		if reason := aiContextReason(t.ctx, t.readErr); reason != "" {
			return reason
		}
	}
	var dns *net.DNSError
	var verify *tls.CertificateVerificationError
	var tlsRecord tls.RecordHeaderError
	var netErr net.Error
	switch {
	case errors.As(err, &dns):
		if dns.Timeout() {
			return "DNS 解析超时"
		}
		return "DNS 解析失败"
	case errors.As(err, &verify):
		return "TLS 证书校验失败"
	case errors.As(err, &tlsRecord):
		return "TLS 握手失败"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "连接被拒绝"
	case errors.Is(err, syscall.ECONNRESET):
		return "连接被重置"
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
		return "连接提前关闭"
	case errors.As(err, &netErr) && netErr.Timeout():
		ownDeadline := strings.HasSuffix(netErr.Error(), " (Client.Timeout exceeded while awaiting headers)") ||
			strings.HasSuffix(netErr.Error(), " (Client.Timeout or context cancellation while reading body)")
		if t.limit > 0 && ownDeadline && t.ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) && time.Since(t.started) >= t.limit {
			return "AI HTTP 客户端等待时限已到"
		}
		return "AI 网络请求超时"
	}
	if t.detail.HTTPStatus == 0 {
		return "AI 请求未取得响应头"
	}
	if t.proxy != nil {
		return "内置代理报告上游故障"
	}
	if t.readErr != nil {
		return "读取 AI 响应中断"
	}
	return ""
}

// failure 在错误外层附加安全证据，不替换原错误的重试、停岗、计费或解析属性。
func (t *aiAttempt) failure(err error) error {
	if err == nil {
		return nil
	}
	reason := t.boundaryReason(err)
	if reason == "" {
		return err
	}
	d := t.detail
	d.Reason, d.ElapsedMS = reason, time.Since(t.started).Milliseconds()
	deadline, ok := t.ctx.Deadline()
	if errors.Is(context.Cause(t.ctx), errScoreParentDeadline) {
		if parentDeadline, known := t.ctx.Value(scoreParentDeadlineKey{}).(time.Time); known {
			deadline, ok = parentDeadline, true
		}
	}
	if ok && (t.limit <= 0 || deadline.Before(t.started.Add(t.limit))) {
		d.LimitMS = max(int64(0), deadline.Sub(t.started).Milliseconds())
	} else {
		d.LimitMS = t.limit.Milliseconds()
	}
	switch {
	case d.HTTPStatus == 0:
		d.Phase = t.aiTransportPhase()
	case d.HTTPStatus >= 400:
		d.Phase = "接收 AI 接口错误响应"
	case t.stream && !t.content:
		d.Phase = "等待首段 AI 内容"
	case t.stream:
		d.Phase = "读取 AI 流式内容"
	case d.ReceivedBytes == 0:
		d.Phase = "等待 AI 响应正文"
	default:
		d.Phase = "读取 AI 响应正文"
	}
	if t.early {
		d.Impact = "评分已返回，完整输出未收完"
	} else if !t.builtin && (d.HTTPStatus == 408 || d.HTTPStatus == 502 || d.HTTPStatus == 504) {
		d.Impact = "外部服务内部原因未知"
	}
	if t.proxy != nil {
		err = failurediag.Wrap(err, *t.proxy)
	}
	if d.HTTPStatus >= 400 && t.readErr != nil {
		bodyDetail := d
		bodyDetail.Phase, bodyDetail.Impact = "读取 AI 错误响应正文", ""
		bodyDetail.Reason = aiContextReason(t.ctx, t.readErr)
		if bodyDetail.Reason == "" {
			var timeout net.Error
			switch {
			case errors.As(t.readErr, &timeout) && timeout.Timeout():
				bodyDetail.Reason = "等待 AI 错误响应正文超时"
			case errors.Is(t.readErr, io.ErrUnexpectedEOF):
				bodyDetail.Reason = "错误响应正文提前结束"
			default:
				bodyDetail.Reason = "错误响应正文未收完"
			}
		}
		err = failurediag.Wrap(err, bodyDetail)
	}
	return failurediag.Wrap(err, d)
}

// retryWaitError 将上一次失败的诊断附到取消错误，而不把原先可重试的服务错误加入其错误链。
func retryWaitError(ctx context.Context, err error, previous error, operation string, attempt int, started time.Time, delay time.Duration) error {
	err = withAIHistory(err, failurediag.Details(previous))
	reason := aiContextReason(ctx, err)
	if reason == "" {
		reason = "调用方取消了重试"
	}
	return failurediag.Wrap(err, failurediag.Detail{Source: "AI 客户端", Operation: operation, Phase: "等待下一次重试", Reason: reason, Attempt: int64(attempt), ElapsedMS: time.Since(started).Milliseconds(), LimitMS: delay.Milliseconds()})
}

// proxySignal 验证内置代理诊断协议，只把已知枚举翻成中文，丢弃所有未知或越界值。
func (t *aiAttempt) proxySignal(payload []byte) {
	if t == nil || !t.trustedResponse || len(payload) == 0 || len(payload) > 2048 {
		return
	}
	var s struct {
		Phase   string `json:"phase"`
		Reason  string `json:"reason"`
		Elapsed int64  `json:"elapsed_ms"`
		Limit   int64  `json:"limit_ms"`
		Bytes   int64  `json:"received_bytes"`
		Status  int    `json:"http_status"`
	}
	if json.Unmarshal(payload, &s) != nil || s.Elapsed < 0 || s.Limit < 0 || s.Bytes < 0 || s.Status != 0 && (s.Status < 100 || s.Status > 599) {
		return
	}
	phase := map[string]string{"connect": "连接上游模型服务", "dns_lookup": "解析上游模型域名", "tcp_connect": "建立上游 TCP 连接", "tls_handshake": "与上游进行 TLS 握手", "write_request": "向上游发送请求", "wait_headers": "等待上游响应头", "read_body": "读取上游响应体", "read_stream": "读取上游事件流", "upstream_status": "收到上游 HTTP 状态", "billing": "处理计费回执"}[s.Phase]
	reason := map[string]string{"dns_error": "上游域名解析失败", "connection_refused": "上游连接被拒绝", "connection_reset": "上游连接被重置", "connection_closed": "上游连接提前关闭", "tls_error": "上游 TLS 握手失败", "timeout": "等待上游超时", "cancelled": "上游请求被取消", "transport_error": "连接上游失败", "stream_interrupted": "上游事件流中断", "completion_unknown": "上游连接已结束，未见流结束标记，完整性未知", "http_status": "上游返回错误状态", "billing_failed": "计费回执未完成"}[s.Reason]
	if s.Reason == "http_status" && s.Status >= 400 {
		reason = fmt.Sprintf("上游返回 HTTP %d", s.Status)
	}
	if phase == "" || reason == "" {
		return
	}
	d := &failurediag.Detail{Source: "内置 AI 代理", Operation: t.detail.Operation, Phase: phase, Reason: reason, ElapsedMS: s.Elapsed, LimitMS: s.Limit, ReceivedBytes: s.Bytes, HTTPStatus: s.Status, Attempt: t.detail.Attempt}
	if s.Reason == "completion_unknown" {
		if s.Phase == "read_stream" {
			t.upstreamCompletion = d
		}
		return
	}
	t.proxy = d
}

// streamError 在已协商的代理告知流中断时标记完整输出未到；计费事件沿原有规则单独处理。
func (t *aiAttempt) streamError() error {
	if t != nil && t.trustedResponse && t.proxy != nil && t.proxy.Phase != "处理计费回执" {
		return errors.New("内置 AI 代理未收完上游响应")
	}
	return nil
}

// withAIHistory 收集已经失败的重试证据，保留最后一次错误的分类和停止策略。
func withAIHistory(err error, previous []failurediag.Detail) error {
	if len(previous) == 0 {
		return err
	}
	latest := failurediag.Details(err)
	for i := len(previous) - 1; i >= 0; i-- {
		err = failurediag.Wrap(err, previous[i])
	}
	for i := len(latest) - 1; i >= 0; i-- {
		err = failurediag.Wrap(err, latest[i])
	}
	return err
}

// streamCompletion 仅在正常 EOF 且收到过模型正文、却没有服务端结束标记时保留证据，直到解析结果已知。
type streamCompletion struct{ unmarkedEOF *failurediag.Detail }

// parseError 只细化已经发生的解析错误；无标记但可以解析的兼容响应继续成功。
func (s *streamCompletion) parseError(err error) error {
	if err == nil || s == nil || s.unmarkedEOF == nil {
		return err
	}
	if response, ok := ResponseDiagnosticFromError(err); ok {
		response.ResponseCompletionUnknown = true
		response.ResponseComplete = false
		err = &ResponseError{Cause: err, Diagnostic: response}
	}
	return failurediag.Wrap(err, *s.unmarkedEOF)
}

// streamEndEvidence 保留实际看到的 EOF；代理转发层补写的 DONE 不能证明上游流发送了结束标记。
func (t *aiAttempt) streamEndEvidence(marked bool) *streamCompletion {
	if t == nil || !t.content || t.streamError() != nil || marked && t.upstreamCompletion == nil {
		return nil
	}
	d := t.detail
	if t.upstreamCompletion != nil {
		d = *t.upstreamCompletion
	} else {
		d.Phase, d.Reason = "解析 AI 输出", "连接已结束，未见流结束标记，完整性未知"
		d.ElapsedMS = time.Since(t.started).Milliseconds()
	}
	if t.early {
		d.Impact = "评分已返回，完整输出解析失败"
	}
	if t.completion != nil {
		t.completion.unmarkedEOF = &d
	}
	return &streamCompletion{unmarkedEOF: &d}
}

// streamFinishReason 只认可可解析且非空的结束原因，保留兼容服务原有的成功解析行为。
func streamFinishReason(raw json.RawMessage) bool {
	var reason string
	return len(raw) > 0 && json.Unmarshal(raw, &reason) == nil && strings.TrimSpace(reason) != ""
}
