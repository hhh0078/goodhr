// Package client 文件作用：区分上层取消、请求预算和 Worker 错误，按编号采集一次有界的进度快照。
package client

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"regexp"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/version"
)

var requestStepPattern = regexp.MustCompile(`^[a-z][a-z0-9_.]{0,47}$`)
var requestIDPattern = regexp.MustCompile(`^(?:[A-Z2-7]{26}|[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12})$`)
var requestVersionPattern = regexp.MustCompile(`^[0-9][0-9A-Za-z.+-]{0,31}$`)

// requestObservation 采集本次请求的网络阶段和预算，绝不保存目标 URL、页面内容和请求正文。
type requestObservation struct {
	diagnostic contract.RequestDiagnostic
	connected  atomic.Bool
	written    atomic.Bool
}

// newRequestObservation 从发起方取得工作预算，已提前取消的请求也可以保留“尚未发出”的证据。
func newRequestObservation(parent context.Context, path string, started time.Time, limit time.Duration) *requestObservation {
	d := &requestObservation{diagnostic: contract.RequestDiagnostic{
		RequestID: rand.Text(), Action: path, ClientVersion: version.Value,
		GoTimeoutMS: limit.Milliseconds(),
	}}
	if deadline, ok := parent.Deadline(); ok {
		ms := max(int64(0), deadline.Sub(started).Milliseconds())
		d.diagnostic.ParentRemainingMS = &ms
	}
	return d
}

// observeRequest 传递随机请求编号，使用标准 HTTP 追踪钩子记录连接与写入两个明确的界限。
func observeRequest(request *http.Request, parent context.Context, started time.Time, limit, budget time.Duration) (*http.Request, *requestObservation) {
	d := newRequestObservation(parent, request.URL.Path, started, limit)
	d.diagnostic.WorkerTimeoutMS = budget.Milliseconds()
	request.Header.Set("X-Request-ID", d.diagnostic.RequestID)
	trace := &httptrace.ClientTrace{
		GotConn: func(httptrace.GotConnInfo) { d.connected.Store(true) },
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err == nil {
				d.written.Store(true)
			}
		},
	}
	return request.WithContext(httptrace.WithClientTrace(request.Context(), trace)), d
}

// transportDiagnosticError 保留普通 HTTP/上下文错误类型和原文，只额外提供白名单现场。
type transportDiagnosticError struct {
	err        error
	diagnostic contract.RequestDiagnostic
}

// Error 返回原先的错误描述，保持既有错误归并规则。
func (e *transportDiagnosticError) Error() string { return e.err.Error() }

// Unwrap 暴露传输或上下文错误，保持 errors.Is 和 errors.As 的继承关系。
func (e *transportDiagnosticError) Unwrap() error { return e.err }

// BrowserRequestDiagnostic 供岗位日志读取本次请求的安全快照。
func (e *transportDiagnosticError) BrowserRequestDiagnostic() contract.RequestDiagnostic {
	return e.diagnostic
}

// transportFailure 记录未发出、连接或收包失败；保持普通错误，不把它改成 Worker 拒绝执行。
func (o *requestObservation) transportFailure(probe *Client, err error, code, phase, reason, original string, started time.Time, status int) error {
	o.recordOutcome(code, "", phase, reason, original, started, status)
	if probe != nil {
		o.inspectProgress(probe)
	}
	return &transportDiagnosticError{err: err, diagnostic: o.diagnostic}
}

// recordOutcome 记录动作结束时已观测的网络阶段和耗时，不把健康探测耗时计入其中。
func (o *requestObservation) recordOutcome(code, step, phase, reason, original string, started time.Time, status int) {
	d := &o.diagnostic
	d.ElapsedMS = time.Since(started).Milliseconds()
	d.ConnectionAcquired, d.RequestWritten = o.connected.Load(), o.written.Load()
	if phase == "wait_response_headers" && !d.RequestWritten {
		if d.ConnectionAcquired {
			phase = "send_request"
		} else {
			phase = "connect_worker"
		}
	}
	d.Phase, d.Reason, d.OriginalError = phase, reason, original
	d.ResponseReceived, d.HTTPStatus, d.ErrorCode = status != 0, status, code
	if requestStepPattern.MatchString(step) {
		d.WorkerStep = step
	}
}

// recordError 将白名单证据附于 Worker 原有详情，保留已有选择器或输入故障证据和错误码。
func (o *requestObservation) recordError(probe *Client, body *contract.WorkerErrorBody, phase, reason, original string, started time.Time, status int) {
	o.recordOutcome(body.Code, body.Step, phase, reason, original, started, status)
	d := &o.diagnostic
	var details map[string]json.RawMessage
	_ = json.Unmarshal(body.Details, &details)
	if details == nil {
		details = make(map[string]json.RawMessage)
	}
	if status != 0 {
		reachable := true
		d.WorkerReachable = &reachable
		var progress contract.RequestDiagnostic
		if json.Unmarshal(details["request_progress"], &progress) == nil {
			o.mergeProgress(progress)
		}
		delete(details, "request_progress")
		if phase == "worker_response" {
			var sourceID, workerReason string
			var nativeReadMS int64
			_ = json.Unmarshal(details["read_value_timeout_ms"], &nativeReadMS)
			_ = json.Unmarshal(details["request_id"], &sourceID)
			_ = json.Unmarshal(details["reason"], &workerReason)
			switch {
			case progress.RequestID == d.RequestID && sourceID != "" && sourceID != d.RequestID:
				d.Reason = "queue_blocked_by_prior_request"
			case body.Action == "element.read" && body.Step == "read" && body.Code == "INTERNAL_ERROR" && d.WorkerPhase == "read_value" && nativeReadMS > 0 && nativeReadMS <= 180000:
				d.Reason, d.OriginalError, d.ReadValueTimeoutMS = "read_value_timeout", "playwright_timeout", nativeReadMS
			case workerReason == "deadline_exceeded":
				d.Reason, d.OriginalError = "worker_deadline", workerReason
			case workerReason == "client_disconnected" || workerReason == "request_cancelled" || workerReason == "native_input_deadline":
				d.Reason, d.OriginalError = workerReason, workerReason
			}
		}
	}
	if probe != nil {
		o.inspectProgress(probe)
	}
	details["request_diagnostic"], _ = json.Marshal(o.diagnostic)
	body.Details, _ = json.Marshal(details)
}

// inspectProgress 独立限时读取同一请求的 /health 缓存快照，绝不排队、访问招聘网页或重放原动作。
func (o *requestObservation) inspectProgress(client *Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	reachable := false
	o.diagnostic.WorkerReachable = &reachable
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL+"/health", nil)
	if err != nil {
		return
	}
	req.Header.Set("X-Inspect-Request-ID", o.diagnostic.RequestID)
	resp, err := client.http.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var result struct {
		OK   bool `json:"ok"`
		Data struct {
			Status   string                     `json:"status"`
			Progress contract.RequestDiagnostic `json:"request_progress"`
		} `json:"data"`
	}
	if resp.StatusCode == http.StatusOK && json.NewDecoder(io.LimitReader(resp.Body, 32<<10)).Decode(&result) == nil && result.OK && result.Data.Status == "ok" {
		reachable = true
		o.mergeProgress(result.Data.Progress)
	}
}

// mergeProgress 只合并同编号的只读快照；字段键、版本、动作仅允许预期格式，禁止携带外部字段值。
func (o *requestObservation) mergeProgress(p contract.RequestDiagnostic) {
	d := &o.diagnostic
	if p.RequestID == "" || p.RequestID != d.RequestID {
		return
	}
	switch p.WorkerPhase {
	case "received", "queued", "executing", "locate_read_target", "read_value", "require_page", "resolve_list", "list_resolved", "read_card", "locate_field", "read_field", "scroll_target", "scroll_anchor", "scroll_move", "scroll_wheel", "scroll_verify", "screenshot_prepare", "screenshot_capture", "screenshot_top", "screenshot_save", "screenshot_advance":
		d.WorkerPhase = p.WorkerPhase
	}
	d.FieldKey = safeReadField(p.FieldKey)
	for _, read := range p.PendingReads {
		if len(d.PendingReads) >= 4 {
			break
		}
		switch read.WorkerPhase {
		case "read_card", "locate_field", "read_field":
			if read.ItemIndex >= 0 && read.ElapsedMS >= 0 {
				read.FieldKey = safeReadField(read.FieldKey)
				d.PendingReads = append(d.PendingReads, read)
			}
		}
	}
	if requestVersionPattern.MatchString(p.WorkerVersion) {
		d.WorkerVersion = p.WorkerVersion
	}
	if requestVersionPattern.MatchString(strings.TrimPrefix(p.NodeVersion, "v")) {
		d.NodeVersion = p.NodeVersion
	}
	if requestVersionPattern.MatchString(p.BrowserVersion) {
		d.BrowserVersion = p.BrowserVersion
	}
	if requestStepPattern.MatchString(p.ActiveAction) {
		d.ActiveAction = p.ActiveAction
	}
	if requestIDPattern.MatchString(p.ActiveRequestID) {
		d.ActiveRequestID = p.ActiveRequestID
	}
	d.ProgressAgeMS, d.ItemIndex, d.ItemCount = p.ProgressAgeMS, p.ItemIndex, p.ItemCount
	d.QueuePending, d.QueueBlocked = p.QueuePending, p.QueueBlocked
	d.PageClosed, d.BrowserConnected, d.FrameCount = p.PageClosed, p.BrowserConnected, p.FrameCount
}

// safeReadField 仅返回平台静态配置使用的字段键，其他字符串不进入诊断。
func safeReadField(field string) string {
	switch field {
	case "name", "card_text", "basic_info", "education", "university", "description", "platform_candidate_id":
		return field
	default:
		return ""
	}
}

// listDiagnosticPath 对名单读取、滚轮、翻页控件和截图记录传输类异常，其他动作保留原有错误语义。
func listDiagnosticPath(path string) bool {
	switch path {
	case "/api/v1/page/screenshot", "/api/v1/element/screenshot", "/api/v1/element/screenshot-long":
		return true
	case "/api/v1/element/read", "/api/v1/element/find-all", "/api/v1/element/find", "/api/v1/element/scroll", "/api/v1/page/scroll", "/api/v1/element/click":
		return true
	default:
		return false
	}
}

// transportReason 只返回可由标准错误类型验证的原因，不把路径、地址或错误正文传入公开日志。
func transportReason(parent context.Context, err error) (string, string) {
	switch {
	case errors.Is(err, context.Canceled):
		if errors.Is(parent.Err(), context.Canceled) {
			return "parent_cancelled", "context_canceled"
		}
		return "request_cancelled", "context_canceled"
	case errors.Is(err, context.DeadlineExceeded):
		if errors.Is(parent.Err(), context.DeadlineExceeded) {
			return "parent_deadline", "context_deadline_exceeded"
		}
		return "request_deadline", "context_deadline_exceeded"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection_refused", "connection_refused"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection_reset", "connection_reset"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "connection_closed", "connection_closed"
	default:
		return "transport_error", "transport_error"
	}
}
