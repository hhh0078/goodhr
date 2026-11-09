// 本文件在内置 AI 代理边界记录上游阶段、状态与耗时；原始请求、服务地址和响应正文不进入诊断协议。
package httpapi

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
	"sync/atomic"
	"syscall"
	"time"
)

const aiDiagnosticHeader = "X-GoodHR-Diagnostics"
const aiUpstreamFailureHeader = "X-GoodHR-Upstream-Failure"

// aiProxyFailure 是客户端可识别的有限字段，HTTPStatus 只代表实际收到的上游响应。
type aiProxyFailure struct {
	Phase         string `json:"phase"`
	Reason        string `json:"reason"`
	ElapsedMS     int64  `json:"elapsed_ms"`
	LimitMS       int64  `json:"limit_ms"`
	ReceivedBytes int64  `json:"received_bytes"`
	HTTPStatus    int    `json:"http_status,omitempty"`
}

type aiProxyObservation struct {
	started      time.Time
	limit        time.Duration
	connectPhase atomic.Pointer[string]
	connected    atomic.Bool
	written      atomic.Bool
	bytes        int64
}

// observeAIProxy 仅向明确支持此协议的客户端报告；HTTP 追踪使用标准传输事件确认连线及写入阶段。
func observeAIProxy(w http.ResponseWriter, original, req *http.Request, limit time.Duration) (*aiProxyObservation, *http.Request) {
	if original.Header.Get(aiDiagnosticHeader) != "1" {
		return nil, req
	}
	w.Header().Set(aiDiagnosticHeader, "1")
	o := &aiProxyObservation{started: time.Now(), limit: limit}
	mark := func(phase string) { o.connectPhase.Store(&phase) }
	trace := &httptrace.ClientTrace{
		DNSStart:          func(httptrace.DNSStartInfo) { mark("dns_lookup") },
		ConnectStart:      func(string, string) { mark("tcp_connect") },
		TLSHandshakeStart: func() { mark("tls_handshake") },
		GotConn:           func(httptrace.GotConnInfo) { o.connected.Store(true) },
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err == nil {
				o.written.Store(true)
			}
		},
	}
	return o, req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
}

// Write 仅统计已收到的上游字节，不缓存内容。
func (o *aiProxyObservation) Write(p []byte) (int, error) {
	o.bytes += int64(len(p))
	return len(p), nil
}

// aiProxyReason 将标准错误链归到有限类别，禁止将可能含网址或凭据的 err.Error() 放进公开信号。
func aiProxyReason(err error, fallback string) string {
	var dns *net.DNSError
	var timeout net.Error
	var cert *tls.CertificateVerificationError
	var record tls.RecordHeaderError
	switch {
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.As(err, &timeout) && timeout.Timeout():
		return "timeout"
	case errors.As(err, &dns):
		return "dns_error"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection_refused"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection_reset"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "connection_closed"
	case errors.As(err, &cert), errors.As(err, &record):
		return "tls_error"
	default:
		return fallback
	}
}

// report 写入非流响应头或流错误事件，保留兼容客户端原来的 HTTP 状态和计费回执。
func (o *aiProxyObservation) report(w http.ResponseWriter, phase, reason string, status int, stream bool) {
	if o == nil {
		return
	}
	if phase == "" {
		switch {
		case !o.connected.Load():
			phase = "connect"
			if sample := o.connectPhase.Load(); sample != nil {
				phase = *sample
			}
		case !o.written.Load():
			phase = "write_request"
		default:
			phase = "wait_headers"
		}
	}
	d := aiProxyFailure{Phase: phase, Reason: reason, ElapsedMS: time.Since(o.started).Milliseconds(), LimitMS: o.limit.Milliseconds(), ReceivedBytes: o.bytes, HTTPStatus: status}
	if stream {
		payload, _ := json.Marshal(struct {
			Choices []struct{}     `json:"choices"`
			Failure aiProxyFailure `json:"goodhr_upstream_failure"`
		}{[]struct{}{}, d})
		_, _ = fmt.Fprintf(w, "\ndata: %s\n\n", payload)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	} else {
		b, _ := json.Marshal(d)
		w.Header().Set(aiUpstreamFailureHeader, string(b))
	}
}
