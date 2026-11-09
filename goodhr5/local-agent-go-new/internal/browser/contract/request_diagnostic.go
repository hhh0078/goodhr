// Package contract 定义浏览器请求排障字段，允许日志链路只取无请求正文的观测结果。
package contract

// PendingBrowserRead 记录失败瞬间仍持有读取槽的操作；序号从零开始，耗时按该阶段开始计算。
type PendingBrowserRead struct {
	WorkerPhase string `json:"worker_phase"`
	ItemIndex   int    `json:"item_index"`
	FieldKey    string `json:"field_key,omitempty"`
	ElapsedMS   int64  `json:"elapsed_ms"`
}

// RequestDiagnostic 记录本机请求及与其编号匹配的 Worker 现场，可选数字与布尔值用指针区分零值和未观测。
type RequestDiagnostic struct {
	RequestID          string               `json:"request_id"`
	Action             string               `json:"action"`
	Phase              string               `json:"phase"`
	Reason             string               `json:"reason"`
	ErrorCode          string               `json:"error_code"`
	WorkerStep         string               `json:"worker_step,omitempty"`
	OriginalError      string               `json:"original_error,omitempty"`
	ElapsedMS          int64                `json:"elapsed_ms"`
	GoTimeoutMS        int64                `json:"go_timeout_ms"`
	WorkerTimeoutMS    int64                `json:"worker_timeout_ms"`
	ParentRemainingMS  *int64               `json:"parent_remaining_ms,omitempty"`
	ResponseReceived   bool                 `json:"response_received"`
	HTTPStatus         int                  `json:"http_status,omitempty"`
	ConnectionAcquired bool                 `json:"connection_acquired"`
	RequestWritten     bool                 `json:"request_written"`
	WorkerReachable    *bool                `json:"worker_reachable,omitempty"`
	ClientVersion      string               `json:"client_version"`
	WorkerPhase        string               `json:"worker_phase,omitempty"`
	WorkerVersion      string               `json:"worker_version,omitempty"`
	NodeVersion        string               `json:"node_version,omitempty"`
	BrowserVersion     string               `json:"browser_version,omitempty"`
	ProgressAgeMS      *int64               `json:"progress_age_ms,omitempty"`
	ItemIndex          *int                 `json:"item_index,omitempty"`
	ItemCount          *int                 `json:"item_count,omitempty"`
	FieldKey           string               `json:"field_key,omitempty"`
	PendingReads       []PendingBrowserRead `json:"pending_reads,omitempty"`
	QueuePending       *int                 `json:"queue_pending,omitempty"`
	QueueBlocked       *bool                `json:"queue_blocked,omitempty"`
	ActiveAction       string               `json:"active_action,omitempty"`
	ActiveRequestID    string               `json:"active_request_id,omitempty"`
	PageClosed         *bool                `json:"page_closed,omitempty"`
	BrowserConnected   *bool                `json:"browser_connected,omitempty"`
	FrameCount         *int                 `json:"frame_count,omitempty"`
}
