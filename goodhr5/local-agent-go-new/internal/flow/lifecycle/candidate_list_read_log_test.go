// Package lifecycle 文件作用：验证候选人批次统计区分末次实读与最终可用人数，并完整通过本地落库和日志上传。
package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/storage"
	"goodhr5/local-agent-go-new/internal/system/diagnostics"
)

// TestCandidateListReadStructuredLogKeepsReadableBody 验证标准输出经过 Normalize 和岗位上传后仍为中文摘要，技术字段、原错误与堆栈只放结构化详情。
func TestCandidateListReadStructuredLogKeepsReadableBody(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprintf("failed=%t", failed), func(t *testing.T) {
			store, err := storage.Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			const taskID, positionID = "structured-batch-task", "structured-batch-position"
			logger := NewTaskLogger(store, nil)
			logger.SetTaskPosition(taskID, positionID)
			var buffer bytes.Buffer
			previousWriter, previousFlags := log.Writer(), log.Flags()
			log.SetOutput(&buffer)
			log.SetFlags(0)
			t.Cleanup(func() { log.SetOutput(previousWriter); log.SetFlags(previousFlags) })
			count := 2
			records := []client.ListReadRecord{{RequestID: "structured-request", Attempt: 1, Status: "success", Stats: &contract.ListReadStats{RequestID: "structured-request", PlannedCount: &count, RawCompletedCount: &count, ReturnedCount: &count, ElapsedMS: 20}}}
			var cause error
			if failed {
				cause = &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "READ_FIELDS_FAILED", Message: "合成读取失败：\"name\"\n请重试", Details: json.RawMessage(`{"original_error":"Synthetic field failure","stack":"at syntheticRead\nat syntheticBatch"}`)}}
				records[0].Status, records[0].ErrorCode = "failed", "READ_FIELDS_FAILED"
			}
			logger.CandidateListReadBatch(taskID, "boss", time.Now().Add(-100*time.Millisecond), count, records, cause)
			if bytes.Count(buffer.Bytes(), []byte{'\n'}) != 1 {
				t.Fatal("每批应只新增一条完整结构化标准日志")
			}
			var normalized struct {
				TaskID       string                     `json:"task_id"`
				PositionID   string                     `json:"position_id"`
				Action       string                     `json:"action"`
				Message      string                     `json:"message"`
				ErrorMessage string                     `json:"error_message"`
				IsError      bool                       `json:"is_error"`
				Output       candidateListReadLogOutput `json:"output"`
			}
			if err := json.Unmarshal(diagnostics.Normalize(buffer.Bytes()), &normalized); err != nil {
				t.Fatal(err)
			}
			if normalized.TaskID != taskID || normalized.PositionID != positionID || normalized.Action != "platform" || normalized.IsError != failed || !strings.HasPrefix(normalized.Message, "这批候选人读取") || strings.Contains(normalized.Message, "task_id=") || !reflect.DeepEqual(normalized.Output.Batch.Requests, records) {
				t.Fatalf("归一化把技术行放回正文或丢失详情：%+v", normalized)
			}
			if failed && (normalized.ErrorMessage != cause.Error() || normalized.Output.ErrorCode != "READ_FIELDS_FAILED" || normalized.Output.OriginalError != "Synthetic field failure" || normalized.Output.Stack != "at syntheticRead\nat syntheticBatch") {
				t.Fatal("归一化丢失完整原错误、错误码或堆栈")
			}
			var captured struct {
				Logs []cloud.PositionDiagnosticLog `json:"logs"`
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
					t.Error(err)
				}
				fmt.Fprint(w, `{"ok":true}`)
			}))
			defer server.Close()
			if err := (&Runner{store: store, cloud: cloud.New(server.URL)}).UploadPositionLogs(context.Background(), "synthetic-token", positionID); err != nil {
				t.Fatal(err)
			}
			if len(captured.Logs) != 1 || captured.Logs[0].Message != normalized.Message || captured.Logs[0].ErrorMessage != normalized.ErrorMessage || captured.Logs[0].IsError != normalized.IsError {
				t.Fatalf("上传正文和结构化日志不一致：%+v", captured.Logs)
			}
			var uploaded candidateListReadLogOutput
			if err := json.Unmarshal(captured.Logs[0].Output, &uploaded); err != nil || !reflect.DeepEqual(uploaded, normalized.Output) {
				t.Fatalf("上传后完整统计或故障详情变化：%v", err)
			}
		})
	}
}

// TestCandidateListReadSummaryUsesLastAttemptAndTotalTime 验证补读不累计人数，均值使用包含补读的整批耗时，且各批独立计算。
func TestCandidateListReadSummaryUsesLastAttemptAndTotalTime(t *testing.T) {
	planned, firstCompleted, firstReturned, completed, returned := 30, 22, 20, 30, 30
	records := []client.ListReadRecord{
		{RequestID: "read-first", Attempt: 1, RequestDurationMS: 700, Status: "success", Stats: &contract.ListReadStats{RequestID: "read-first", PlannedCount: &planned, RawCompletedCount: &firstCompleted, ReturnedCount: &firstReturned, ElapsedMS: 690}},
		{RequestID: "read-second", Attempt: 2, RequestDurationMS: 900, Status: "success", Stats: &contract.ListReadStats{RequestID: "read-second", PlannedCount: &planned, RawCompletedCount: &completed, ReturnedCount: &returned, ElapsedMS: 890}},
	}
	summary := buildCandidateListReadSummary("boss", 2400, 28, records, nil)
	if summary.PlatformID != "boss" || summary.Status != "success" || summary.DurationMS != 2400 || summary.AttemptCount != 2 ||
		summary.PlannedCount == nil || *summary.PlannedCount != 30 || summary.LastRawCompletedCount == nil || *summary.LastRawCompletedCount != 30 ||
		summary.LastWorkerReturnedCount == nil || *summary.LastWorkerReturnedCount != 30 || summary.UsableCount != 28 ||
		summary.AvgMSPerRawCompleted == nil || *summary.AvgMSPerRawCompleted != 80 {
		t.Fatalf("批次人数、重读均值或最终可用数不正确：%+v", summary)
	}
	if !reflect.DeepEqual(summary.Requests, records) {
		t.Fatalf("各次请求的原始统计被改写：%+v", summary.Requests)
	}
	message := candidateListReadMessage(summary)
	for _, part := range []string{"计划30 人", "末次读完30 人", "总耗时2400毫秒", "最终可用28人", "尝试2次", "平均每人80.0毫秒（含重读耗时）"} {
		if !strings.Contains(message, part) {
			t.Errorf("中文摘要缺少 %q：%s", part, message)
		}
	}
	nextCompleted := 5
	next := buildCandidateListReadSummary("liepin", 100, 5, []client.ListReadRecord{{RequestID: "another-batch", Attempt: 1, Status: "success", Stats: &contract.ListReadStats{RawCompletedCount: &nextCompleted}}}, nil)
	if next.AttemptCount != 1 || next.AvgMSPerRawCompleted == nil || *next.AvgMSPerRawCompleted != 20 || next.UsableCount != 5 {
		t.Fatalf("下一批混入了上一批的耗时或人数：%+v", next)
	}
}

// TestCandidateListReadSummaryKeepsUnknownZeroAndInterrupted 验证未知数不伪装成零，末次缺少统计不偷用旧数，失败与空批次不产生均值。
func TestCandidateListReadSummaryKeepsUnknownZeroAndInterrupted(t *testing.T) {
	planned, completed, returned, zero := 30, 7, 7, 0
	partial := &contract.ListReadStats{PlannedCount: &planned, RawCompletedCount: &completed, ReturnedCount: &returned, ElapsedMS: 800}
	cases := []struct {
		name          string
		records       []client.ListReadRecord
		usable        int
		cause         error
		status        string
		wantUsable    int
		wantCompleted *int
		unknown       bool
	}{
		{name: "旧接口没有统计", records: []client.ListReadRecord{{RequestID: "legacy", Attempt: 1, Status: "success"}}, usable: 2, status: "success", wantUsable: 2, unknown: true},
		{name: "没有真实请求", usable: 2, status: "success", wantUsable: 2, unknown: true},
		{name: "末次没有统计不取前次", records: []client.ListReadRecord{{RequestID: "observed", Attempt: 1, Status: "success", Stats: partial}, {RequestID: "unobserved", Attempt: 2, Status: "success"}}, usable: 3, status: "success", wantUsable: 3, unknown: true},
		{name: "真实零人", records: []client.ListReadRecord{{RequestID: "empty", Attempt: 1, Status: "success", Stats: &contract.ListReadStats{PlannedCount: &zero, RawCompletedCount: &zero, ReturnedCount: &zero}}}, status: "success", wantCompleted: &zero},
		{name: "部分完成后超时", records: []client.ListReadRecord{{RequestID: "timeout", Attempt: 1, Status: "timeout", Stats: partial}}, usable: 7, cause: fmt.Errorf("批次预算到期：%w", context.DeadlineExceeded), status: "timeout", wantCompleted: &completed},
		{name: "部分完成后取消", records: []client.ListReadRecord{{RequestID: "cancelled", Attempt: 1, Status: "cancelled", Stats: partial}}, usable: 7, cause: fmt.Errorf("用户停止：%w", context.Canceled), status: "cancelled", wantCompleted: &completed},
		{name: "Worker超时不伪装成功", records: []client.ListReadRecord{{RequestID: "worker-timeout", Attempt: 1, Status: "timeout", Stats: partial}}, usable: 7, cause: errors.New("浏览器读取超时"), status: "timeout", wantCompleted: &completed},
		{name: "返回有数据但Go校验失败", records: []client.ListReadRecord{{RequestID: "unusable", Attempt: 1, Status: "success", Stats: partial}}, usable: 7, cause: errors.New("本批候选人字段校验未通过"), status: "failed", wantCompleted: &completed},
		{name: "成功但最终可用为零", records: []client.ListReadRecord{{RequestID: "filtered-empty", Attempt: 1, Status: "success", Stats: partial}}, status: "success", wantCompleted: &completed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			summary := buildCandidateListReadSummary("zhaopin", 1200, tc.usable, tc.records, tc.cause)
			if summary.Status != tc.status || summary.UsableCount != tc.wantUsable || summary.AttemptCount != len(tc.records) || summary.AvgMSPerRawCompleted != nil || !reflect.DeepEqual(summary.LastRawCompletedCount, tc.wantCompleted) {
				t.Fatalf("批次未知值、结果或均值不正确：%+v", summary)
			}
			if tc.unknown {
				if summary.PlannedCount != nil || summary.LastRawCompletedCount != nil || summary.LastWorkerReturnedCount != nil || !strings.Contains(candidateListReadMessage(summary), "未确认") {
					t.Fatalf("未观测的计数被伪装为已知值：%+v", summary)
				}
				encoded, err := json.Marshal(summary)
				if err != nil || !strings.Contains(string(encoded), `"planned_count":null`) || !strings.Contains(string(encoded), `"last_raw_completed_count":null`) || !strings.Contains(string(encoded), `"last_worker_returned_count":null`) {
					t.Fatalf("未知计数没有以空值保存：%s err=%v", encoded, err)
				}
			}
			if tc.wantCompleted == &zero && strings.Contains(candidateListReadMessage(summary), "未确认") {
				t.Fatalf("真实零人误显示为未知：%s", candidateListReadMessage(summary))
			}
		})
	}
}

// TestCandidateListReadBatchPersistsAndUploadsCounts 验证完整批次统计和原始故障经 SQLite 落库后按平台日志上传，不被读操作隐私裁剪或普通大小限制吞掉。
func TestCandidateListReadBatchPersistsAndUploadsCounts(t *testing.T) {
	planned, completed, returned := 30, 30, 28
	largeRecords := make([]client.ListReadRecord, 128)
	for index := range largeRecords {
		requestID := fmt.Sprintf("synthetic-read-request-%03d", index)
		largeRecords[index] = client.ListReadRecord{RequestID: requestID, Attempt: index + 1, RequestDurationMS: int64(index + 10), Status: "success", Stats: &contract.ListReadStats{RequestID: requestID, PlannedCount: &planned, RawCompletedCount: &completed, ReturnedCount: &returned, ElapsedMS: int64(index + 9)}}
	}
	partial := 7
	partialStats := &contract.ListReadStats{RequestID: "partial-request", PlannedCount: &planned, RawCompletedCount: &partial, ReturnedCount: &partial, ElapsedMS: 800}
	workerError := &contract.WorkerError{Status: http.StatusRequestTimeout, Body: contract.WorkerErrorBody{Code: "READ_BATCH_TIMEOUT", Message: "本批候选人读取超时", Action: "element.findAll", Step: "read_fields", Details: json.RawMessage(`{"original_error":"Synthetic read loop reached its deadline","stack":"at readCandidateFields (synthetic-worker.js:10:2)\nat readBatch (synthetic-worker.js:20:4)"}`)}}
	cases := []struct {
		name       string
		records    []client.ListReadRecord
		usable     int
		cause      error
		status     string
		large      bool
		expectCode string
	}{
		{name: "大批请求完整上传", records: largeRecords, usable: 25, status: "success", large: true},
		{name: "部分超时原始故障保留", records: []client.ListReadRecord{{RequestID: "partial-request", Attempt: 1, RequestDurationMS: 810, Stats: partialStats, Status: "timeout", ErrorCode: "READ_BATCH_TIMEOUT"}}, usable: 7, cause: fmt.Errorf("读取没有完成：%w", workerError), status: "timeout", expectCode: "READ_BATCH_TIMEOUT"},
		{name: "部分取消完整上传", records: []client.ListReadRecord{{RequestID: "partial-request", Attempt: 1, RequestDurationMS: 810, Stats: partialStats, Status: "cancelled", ErrorCode: "REQUEST_CANCELLED"}}, usable: 7, cause: context.Canceled, status: "cancelled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store, err := storage.Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			const taskID, positionID = "candidate-batch-task", "candidate-batch-position"
			if err := store.SaveTask(ctx, storage.TaskRun{TaskID: taskID, PositionID: positionID, TaskType: "greeting", Status: "running"}); err != nil {
				t.Fatal(err)
			}
			logger := NewTaskLogger(store, nil)
			logger.CandidateListReadBatch(taskID, "hliepin", time.Now().Add(-2400*time.Millisecond), tc.usable, tc.records, tc.cause)
			rows, err := store.ListPositionLogs(ctx, positionID, 20)
			if err != nil || len(rows) != 1 {
				t.Fatalf("每批必须只落库一条统计：count=%d err=%v", len(rows), err)
			}
			row := rows[0]
			if row.Flow != "platform" || row.Step != "find_candidates" || row.Status != tc.status || row.IsError != (tc.cause != nil) || !strings.Contains(row.Message, "这批候选人读取") {
				t.Fatalf("批次未按真实结果保存到平台日志：%+v", row)
			}
			if tc.large && len(row.Output) <= 12<<10 {
				t.Fatalf("用例没有超过普通输出大小保护：%d", len(row.Output))
			}
			var local candidateListReadLogOutput
			if err := json.Unmarshal(row.Output, &local); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(local.Batch.Requests, tc.records) || local.DurationMS != row.DurationMS || local.Batch.DurationMS != row.DurationMS {
				t.Fatalf("本地批次字段或请求记录丢失：%s", row.Output)
			}
			if tc.cause != nil && (local.Batch.UsableCount != 0 || local.Batch.AvgMSPerRawCompleted != nil || row.ErrorMessage != tc.cause.Error()) {
				t.Fatalf("中断误报可用人数、均值或丢失原错误：%+v error=%q", local.Batch, row.ErrorMessage)
			}
			if tc.expectCode != "" && (local.ErrorCode != tc.expectCode || local.OriginalError != "Synthetic read loop reached its deadline" || local.Stack != "at readCandidateFields (synthetic-worker.js:10:2)\nat readBatch (synthetic-worker.js:20:4)") {
				t.Fatalf("稳定错误码、原始错误或堆栈丢失：%s", row.Output)
			}
			var captured struct {
				Logs []cloud.PositionDiagnosticLog `json:"logs"`
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut || r.URL.Path != "/api/positions/"+positionID+"/logs" {
					t.Errorf("意外的日志上传接口：%s %s", r.Method, r.URL.Path)
				}
				if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
					t.Errorf("解析上传正文失败：%v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":true}`))
			}))
			defer server.Close()
			runner := &Runner{store: store, cloud: cloud.New(server.URL)}
			if err := runner.UploadPositionLogs(ctx, "synthetic-token", positionID); err != nil {
				t.Fatal(err)
			}
			if len(captured.Logs) != 1 {
				t.Fatalf("上传批次数不正确：%d", len(captured.Logs))
			}
			uploaded := captured.Logs[0]
			var output candidateListReadLogOutput
			if err := json.Unmarshal(uploaded.Output, &output); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(output.Batch, local.Batch) || output.ErrorCode != local.ErrorCode || output.OriginalError != local.OriginalError || output.Stack != local.Stack || uploaded.DurationMS != row.DurationMS || uploaded.Action != "platform" || uploaded.Message != row.Message || uploaded.ErrorMessage != row.ErrorMessage || uploaded.Status != row.Status || uploaded.IsError != row.IsError {
				t.Fatalf("上传时批统计数值、全部请求或原始故障被改写：local=%s uploaded=%s", row.Output, uploaded.Output)
			}
		})
	}
}
