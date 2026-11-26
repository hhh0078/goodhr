// Package lifecycle 文件作用：将每批正式候选人读取的真实请求计数、耗时和原始故障写入同一条可上传日志。
package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/storage"
	"goodhr5/local-agent-go-new/internal/system/diagnostics"
	"goodhr5/local-agent-go-new/internal/system/failurediag"
)

// candidateListReadReporter 是任务日志器可选的批次记录能力，不改变旧日志器接口。
type candidateListReadReporter interface {
	CandidateListReadBatch(taskID, platformID string, startedAt time.Time, usableCount int, records []client.ListReadRecord, cause error)
}

// candidateListReadSummary 区分末次浏览器完成数、浏览器返回数和 Go 最终可用数，未知计数保留空值。
type candidateListReadSummary struct {
	PlatformID              string                  `json:"platform_id"`
	Status                  string                  `json:"status"`
	PlannedCount            *int                    `json:"planned_count"`
	LastRawCompletedCount   *int                    `json:"last_raw_completed_count"`
	LastWorkerReturnedCount *int                    `json:"last_worker_returned_count"`
	UsableCount             int                     `json:"usable_count"`
	AttemptCount            int                     `json:"attempt_count"`
	DurationMS              int64                   `json:"duration_ms"`
	AvgMSPerRawCompleted    *float64                `json:"avg_ms_per_raw_completed,omitempty"`
	Requests                []client.ListReadRecord `json:"requests"`
}

// candidateListReadLogOutput 保留批次统计及既有故障展开字段，不包含候选人字段值。
type candidateListReadLogOutput struct {
	Status             string                   `json:"status"`
	DurationMS         int64                    `json:"duration_ms"`
	Batch              candidateListReadSummary `json:"candidate_list_read_batch"`
	BrowserDiagnostic  *browserDiagnostic       `json:"browser_diagnostic,omitempty"`
	FailureDiagnostics []failurediag.Detail     `json:"failure_diagnostics,omitempty"`
	ErrorCode          string                   `json:"error_code,omitempty"`
	OriginalError      string                   `json:"original_error,omitempty"`
	Stack              string                   `json:"stack,omitempty"`
}

// buildCandidateListReadSummary 只取末次请求的现场计数；成功且非空时用整批耗时计算均值，包含补读开销。
func buildCandidateListReadSummary(platformID string, durationMS int64, usableCount int, records []client.ListReadRecord, cause error) candidateListReadSummary {
	summary := candidateListReadSummary{PlatformID: platformID, Status: "success", DurationMS: max(durationMS, 0), UsableCount: max(usableCount, 0), AttemptCount: len(records), Requests: append([]client.ListReadRecord{}, records...)}
	if cause != nil {
		summary.Status, summary.UsableCount = "failed", 0
		if errors.Is(cause, context.Canceled) {
			summary.Status = "cancelled"
		} else if errors.Is(cause, context.DeadlineExceeded) {
			summary.Status = "timeout"
		} else if len(records) > 0 && (records[len(records)-1].Status == "timeout" || records[len(records)-1].Status == "cancelled") {
			summary.Status = records[len(records)-1].Status
		}
	}
	if len(records) > 0 {
		if stats := records[len(records)-1].Stats; stats != nil {
			summary.PlannedCount = stats.PlannedCount
			summary.LastRawCompletedCount = stats.RawCompletedCount
			summary.LastWorkerReturnedCount = stats.ReturnedCount
		}
	}
	if summary.Status == "success" && summary.UsableCount > 0 && summary.LastRawCompletedCount != nil && *summary.LastRawCompletedCount > 0 {
		average := float64(summary.DurationMS) / float64(*summary.LastRawCompletedCount)
		summary.AvgMSPerRawCompleted = &average
	}
	return summary
}

// candidateListReadCount 把未观测人数显示为未确认，保留真实零人与非零人数的区别。
func candidateListReadCount(value *int) string {
	if value == nil || *value < 0 {
		return "未确认"
	}
	return strconv.Itoa(*value) + " 人"
}

// candidateListReadMessage 生成每批独立的中文摘要，不跨批次累计人数或均值。
func candidateListReadMessage(summary candidateListReadSummary) string {
	result := map[string]string{"success": "成功", "failed": "失败", "timeout": "超时", "cancelled": "已取消"}[summary.Status]
	message := fmt.Sprintf("这批候选人读取%s：计划%s，末次读完%s，总耗时%d毫秒，最终可用%d人，尝试%d次", result, candidateListReadCount(summary.PlannedCount), candidateListReadCount(summary.LastRawCompletedCount), summary.DurationMS, summary.UsableCount, summary.AttemptCount)
	if summary.AvgMSPerRawCompleted != nil {
		message += fmt.Sprintf("，平均每人%.1f毫秒（含重读耗时）", *summary.AvgMSPerRawCompleted)
	}
	return message
}

// CandidateListReadBatch 将整批汇总直接落在平台日志，取消后仍写入收尾记录，并保留完整原错误和安全故障字段。
func (l *TaskLogger) CandidateListReadBatch(taskID, platformID string, startedAt time.Time, usableCount int, records []client.ListReadRecord, cause error) {
	if l == nil {
		return
	}
	durationMS := time.Since(startedAt).Milliseconds()
	summary := buildCandidateListReadSummary(platformID, durationMS, usableCount, records, cause)
	if l.next != nil {
		l.next.Step(taskID, "platform", "find_candidates", summary.Status, startedAt, cause)
	}
	message := candidateListReadMessage(summary)
	browser := browserDiagnosticFromError(cause)
	output := candidateListReadLogOutput{Status: summary.Status, DurationMS: summary.DurationMS, Batch: summary, BrowserDiagnostic: browser, FailureDiagnostics: taskFailureDetails(cause, browser)}
	var workerError *contract.WorkerError
	if errors.As(cause, &workerError) && workerError != nil {
		output.ErrorCode = workerError.Body.Code
		var details struct {
			OriginalError string `json:"original_error"`
			Stack         string `json:"stack"`
		}
		if json.Unmarshal(workerError.Body.Details, &details) == nil {
			output.OriginalError, output.Stack = details.OriginalError, details.Stack
		}
	}
	encoded, _ := json.Marshal(output)
	errorMessage, category := "", "运行"
	if cause != nil {
		errorMessage = strings.TrimSpace(string(diagnostics.Redact([]byte(cause.Error()))))
		category = "错误"
	}
	outputJSON := diagnostics.Redact(encoded)
	positionID := l.taskPosition(taskID)
	if line, marshalErr := json.Marshal(struct {
		Timestamp    string          `json:"timestamp"`
		TaskID       string          `json:"task_id"`
		PositionID   string          `json:"position_id"`
		Action       string          `json:"action"`
		Step         string          `json:"step"`
		Status       string          `json:"status"`
		DurationMS   int64           `json:"duration_ms"`
		Message      string          `json:"message"`
		Category     string          `json:"category"`
		IsError      bool            `json:"is_error"`
		ErrorMessage string          `json:"error_message"`
		Input        json.RawMessage `json:"input"`
		Output       json.RawMessage `json:"output"`
	}{time.Now().UTC().Format(time.RFC3339Nano), taskID, positionID, "platform", "find_candidates", summary.Status, summary.DurationMS, message, category, cause != nil, errorMessage, json.RawMessage(`{}`), outputJSON}); marshalErr == nil {
		log.Print(string(line))
	}
	if l.store == nil {
		return
	}
	_, err := l.store.SaveTaskLog(context.Background(), storage.TaskLog{TaskID: taskID, PositionID: positionID, Flow: "platform", Step: "find_candidates", Status: summary.Status, Message: message, DurationMS: summary.DurationMS, Category: category, IsError: cause != nil, ErrorMessage: errorMessage, Input: json.RawMessage(`{}`), Output: outputJSON})
	if err != nil {
		log.Printf("task_id=%s flow=lifecycle step=save_candidate_list_batch status=warning error=%q", taskID, err)
	}
}
