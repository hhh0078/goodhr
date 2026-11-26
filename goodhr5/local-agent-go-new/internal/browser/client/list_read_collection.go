// Package client 文件作用：在一次正式候选批次内收集同请求的安全读取统计，不改变原返回值或错误。
package client

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
)

type listReadCollectionKey struct{}

// ListReadRecord 保存一次真实字段读取请求；统计缺失时保持未知，不从列表序号推算完成数。
type ListReadRecord struct {
	RequestID         string                  `json:"request_id"`
	Attempt           int                     `json:"attempt"`
	RequestDurationMS int64                   `json:"request_duration_ms"`
	Stats             *contract.ListReadStats `json:"read_stats,omitempty"`
	Status            string                  `json:"status"`
	ErrorCode         string                  `json:"error_code,omitempty"`
}

// ListReadCollection 只归属当前正式批次，避免不同任务、平台或探测请求混入同一统计。
type ListReadCollection struct {
	mu      sync.Mutex
	records []ListReadRecord
}

// WithListReadCollection 建立当前批次的收集器；只有请求显式启用 ReadStats 才会写入。
func WithListReadCollection(ctx context.Context) (context.Context, *ListReadCollection) {
	collection := &ListReadCollection{}
	return context.WithValue(ctx, listReadCollectionKey{}, collection), collection
}

// Records 返回不可与内部状态相互修改的请求快照，允许调用方安全格式化和保存日志。
func (c *ListReadCollection) Records() []ListReadRecord {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	result := append([]ListReadRecord(nil), c.records...)
	for i := range result {
		result[i].Stats = copyListReadStats(result[i].Stats)
	}
	return result
}

// record 保存独立请求统计，后续格式化不能改写先前请求的现场。
func (c *ListReadCollection) record(value ListReadRecord) {
	value.Stats = copyListReadStats(value.Stats)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, value)
}

// formalListReadRequest 明确排除加载探测、视口定位和无字段读取，不能只凭外层上下文开启统计。
func formalListReadRequest(value any) (contract.ElementFindAllRequest, bool) {
	request, ok := value.(contract.ElementFindAllRequest)
	return request, ok && request.ReadStats && !request.ProbeOnly && request.ViewportStart == nil && len(request.Fields) > 0
}

// readCountCopy 保留已观测零值，并防止日志消费方改写收集器中的计数。
func readCountCopy(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

// copyListReadStats 复制少量数字字段，不保留任何额外字段或候选人内容。
func copyListReadStats(value *contract.ListReadStats) *contract.ListReadStats {
	if value == nil {
		return nil
	}
	copy := *value
	copy.PlannedCount = readCountCopy(value.PlannedCount)
	copy.RawCompletedCount = readCountCopy(value.RawCompletedCount)
	copy.ReturnedCount = readCountCopy(value.ReturnedCount)
	return &copy
}

// validatedListReadStats 仅接收同一请求且相互一致的非负计数；异常元数据不影响业务结果。
func validatedListReadStats(value *contract.ListReadStats, requestID string) *contract.ListReadStats {
	if value == nil || requestID == "" || value.RequestID != requestID || value.ElapsedMS < 0 {
		return nil
	}
	for _, count := range []*int{value.PlannedCount, value.RawCompletedCount, value.ReturnedCount} {
		if count != nil && *count < 0 {
			return nil
		}
	}
	if value.RawCompletedCount != nil && value.PlannedCount == nil {
		return nil
	}
	if value.RawCompletedCount != nil && *value.RawCompletedCount > *value.PlannedCount {
		return nil
	}
	if value.ReturnedCount != nil && (value.RawCompletedCount == nil || *value.ReturnedCount > *value.RawCompletedCount) {
		return nil
	}
	return copyListReadStats(value)
}

// decodeListReadStats 在可选元数据边界独立解析，避免旧组件或坏统计使原本成功的读取失败。
func decodeListReadStats(raw json.RawMessage, requestID string) *contract.ListReadStats {
	var stats contract.ListReadStats
	if len(raw) == 0 || json.Unmarshal(raw, &stats) != nil {
		return nil
	}
	return validatedListReadStats(&stats, requestID)
}

// listReadOutcome 只按取消类型与稳定诊断判断结果，不用错误文案猜测或改变原错误链。
func listReadOutcome(err error, parent context.Context, observation *requestObservation) string {
	if err == nil {
		return "success"
	}
	if errors.Is(parent.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(parent.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if observation != nil {
		switch observation.diagnostic.Reason {
		case "worker_deadline", "parent_deadline", "request_deadline", "native_input_deadline", "read_value_timeout":
			return "timeout"
		case "parent_cancelled", "request_cancelled", "request_cancelled_by_client", "client_disconnected":
			return "cancelled"
		}
	}
	var workerErr *contract.WorkerError
	if errors.As(err, &workerErr) {
		switch workerErr.Body.Code {
		case "ACTION_TIMEOUT":
			return "timeout"
		case "ACTION_CANCELLED":
			return "cancelled"
		}
	}
	return "failed"
}

// collectListRead 在请求真正返回后保存完整观测；健康补证耗时不混入单次请求耗时。
func collectListRead(collection *ListReadCollection, request contract.ElementFindAllRequest, parent context.Context, started time.Time, observation *requestObservation, stats *contract.ListReadStats, result any, err error) {
	record := ListReadRecord{Attempt: max(1, request.DiagnosticAttempt), RequestDurationMS: max(int64(0), time.Since(started).Milliseconds()), Status: listReadOutcome(err, parent, observation)}
	if observation != nil {
		record.RequestID = observation.diagnostic.RequestID
		record.ErrorCode = observation.diagnostic.ErrorCode
		if observation.diagnostic.Phase != "" {
			record.RequestDurationMS = observation.diagnostic.ElapsedMS
		}
		if stats == nil {
			stats = observation.diagnostic.ReadStats
		}
	}
	record.Stats = validatedListReadStats(stats, record.RequestID)
	if stats := record.Stats; stats != nil {
		if request.MaxItems > 0 && stats.PlannedCount != nil && *stats.PlannedCount > request.MaxItems {
			record.Stats = nil
		}
		if items, ok := result.(*[]contract.FindAllItem); err == nil && ok && stats.ReturnedCount != nil && *stats.ReturnedCount != len(*items) {
			record.Stats = nil
		}
	}
	collection.record(record)
}
