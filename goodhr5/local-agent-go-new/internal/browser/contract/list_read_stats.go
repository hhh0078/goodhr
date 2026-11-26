// Package contract 文件作用：定义列表整批读取的数量与耗时，仅包含同请求的安全计数，不包含字段值。
package contract

// ListReadStats 保存 Worker 观测到的本次读取进度；空指针表示尚未确认，不等于零人。
type ListReadStats struct {
	RequestID         string `json:"request_id"`
	PlannedCount      *int   `json:"planned_count,omitempty"`
	RawCompletedCount *int   `json:"raw_completed_count,omitempty"`
	ReturnedCount     *int   `json:"returned_count,omitempty"`
	ElapsedMS         int64  `json:"elapsed_ms"`
}
