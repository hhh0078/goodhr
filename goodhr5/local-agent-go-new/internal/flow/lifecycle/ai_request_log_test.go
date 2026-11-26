// Package lifecycle 文件作用：验证真实AI请求重试经固定岗位、SQLite和既有上传链仍保留完整关联，并输出合成payload供真实分享接口联合验收。
package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// TestAIRequestAttemptsPersistAndUpload 验证本机失败后成功及上游主备历史都有独立岗位记录，自动回复隐私裁剪也不丢安全元数据。
func TestAIRequestAttemptsPersistAndUpload(t *testing.T) {
	for _, taskType := range []string{"greeting", "auto_reply"} {
		t.Run(taskType, func(t *testing.T) {
			ctx := context.Background()
			store, err := storage.Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			const taskID, positionID = "request-log-task", "request-log-position"
			if err := store.SaveTask(ctx, storage.TaskRun{TaskID: taskID, PositionID: positionID, TaskType: taskType, Status: "running"}); err != nil {
				t.Fatal(err)
			}
			logger := NewTaskLogger(store, nil)
			logger.SetTaskPosition(taskID, "unrelated-current-position")
			var calls atomic.Int32
			modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					w.WriteHeader(503)
					fmt.Fprint(w, "temporary")
					return
				}
				id, group := r.Header.Get("X-GoodHR-Client-Request-ID"), r.Header.Get("X-GoodHR-Analysis-Group-ID")
				now := time.Now().UTC()
				events := []ai.RequestAttempt{{Layer: "server", ClientRequestID: id, AnalysisGroupID: group, ServerAttempt: 1, MaxAttempts: 4, Operation: "候选人基础预分析", Model: "primary-model", Provider: "primary", StartedAt: now.Add(-20 * time.Millisecond).Format(time.RFC3339Nano), FinishedAt: now.Add(-10 * time.Millisecond).Format(time.RFC3339Nano), ElapsedMS: 10, HTTPStatus: 502, Result: "failed", Reason: "http_status", WillRetry: true, BackoffMS: 500}, {Layer: "server", ClientRequestID: id, AnalysisGroupID: group, ServerAttempt: 2, MaxAttempts: 4, Operation: "候选人基础预分析", Model: "fallback-model", Provider: "fallback", StartedAt: now.Add(-9 * time.Millisecond).Format(time.RFC3339Nano), FinishedAt: now.Format(time.RFC3339Nano), ElapsedMS: 9, HTTPStatus: 200, Result: "success", Reason: "success", ProviderRequestID: "safe-provider-id"}}
				encoded, _ := json.Marshal(events)
				w.Header().Set("X-GoodHR-Diagnostics", "1")
				w.Header().Set("X-GoodHR-AI-Attempt-Details", string(encoded))
				fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"score\":80,\"reason\":\"ok\"}"}}]}`)
			}))
			defer modelServer.Close()
			t.Setenv("GOODHR_PUBLIC_BASE_URL", modelServer.URL)
			ctx = shared.WithAIBilling(ctx, logger, taskID, positionID, "PRIVATE_CANDIDATE_NAME", "AI 基础预分析")
			decision, err := ai.New().EvaluateCandidatePreview(ctx, cloud.AIConfig{BaseURL: modelServer.URL + "/api/ai-compatible/v1", APIKey: "ghai_PRIVATE_KEY", Model: "requested-model"}, cloud.PositionSnapshot{}, model.Candidate{Summary: "PRIVATE_RESUME_PROMPT"})
			if err != nil || decision.Score != 80 || calls.Load() != 2 {
				t.Fatalf("请求行为发生变化：%+v %v calls=%d", decision, err, calls.Load())
			}
			rows, err := store.ListPositionLogs(context.Background(), positionID, 100)
			if err != nil {
				t.Fatal(err)
			}
			local := map[string]ai.RequestAttempt{}
			failures := 0
			for _, row := range rows {
				if row.Step != "ai_request_attempt" {
					continue
				}
				var output struct {
					Attempt ai.RequestAttempt `json:"ai_request_attempt"`
				}
				if err := json.Unmarshal(row.Output, &output); err != nil {
					t.Fatal(err)
				}
				event := output.Attempt
				key := event.Layer + "/" + event.ClientRequestID + "/" + fmt.Sprint(event.ServerAttempt) + "/" + event.Phase
				local[key] = event
				if row.IsError {
					failures++
				}
				if row.TaskID != taskID || row.PositionID != positionID || row.Flow != "ai" || strings.Contains(row.Message, "PRIVATE_") || strings.Contains(string(row.Output), "PRIVATE_RESUME_PROMPT") || strings.Contains(string(row.Output), "PRIVATE_CANDIDATE_NAME") || strings.Contains(string(row.Output), "ghai_PRIVATE_KEY") {
					t.Fatal("归属被当前任务覆盖或新记录泄露请求资料")
				}
			}
			if len(local) != 8 || failures != 2 {
				t.Fatalf("本机及主备的开始/结束、历史失败丢失：%d failures=%d", len(local), failures)
			}
			wrong, _ := store.ListPositionLogs(context.Background(), "unrelated-current-position", 100)
			if len(wrong) != 0 {
				t.Fatal("异步请求记录串到当前岗位")
			}
			var captured struct {
				Logs []cloud.PositionDiagnosticLog `json:"logs"`
			}
			cloudServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut || r.URL.Path != "/api/positions/"+positionID+"/logs" {
					t.Error("意外的上传路由")
				}
				if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
					t.Error(err)
				}
				fmt.Fprint(w, `{"ok":true}`)
			}))
			defer cloudServer.Close()
			if err := (&Runner{store: store, cloud: cloud.New(cloudServer.URL)}).UploadPositionLogs(context.Background(), "synthetic-token", positionID); err != nil {
				t.Fatal(err)
			}
			actual := make([]cloud.PositionDiagnosticLog, 0, 8)
			for _, entry := range captured.Logs {
				if entry.Step != "ai_request_attempt" {
					continue
				}
				actual = append(actual, entry)
				var output struct {
					Attempt ai.RequestAttempt `json:"ai_request_attempt"`
				}
				if err := json.Unmarshal(entry.Output, &output); err != nil {
					t.Fatal(err)
				}
				event := output.Attempt
				key := event.Layer + "/" + event.ClientRequestID + "/" + fmt.Sprint(event.ServerAttempt) + "/" + event.Phase
				if !reflect.DeepEqual(event, local[key]) || entry.Action != "ai" || entry.Message != ai.RequestAttemptMessage(event) || strings.Contains(entry.Message, "PRIVATE_") {
					t.Fatalf("上传后层级、重试或字段改变：%s", entry.Output)
				}
			}
			if len(actual) != 8 {
				t.Fatal("分享上传丢失请求记录")
			}
			if evidence := os.Getenv("GOODHR_AI_LOG_EVIDENCE"); evidence != "" {
				if err := os.MkdirAll(evidence, 0700); err != nil {
					t.Fatal(err)
				}
				encoded, _ := json.MarshalIndent(struct {
					Logs []cloud.PositionDiagnosticLog `json:"logs"`
				}{actual}, "", "  ")
				if err := os.WriteFile(filepath.Join(evidence, "local-ai-attempt-"+taskType+"-upload.json"), encoded, 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
