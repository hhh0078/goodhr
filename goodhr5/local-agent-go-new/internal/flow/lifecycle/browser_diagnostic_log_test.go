// Package lifecycle 文件作用：从真实平台入口模拟列表请求中断，验证同一条岗位错误从落库到上传仍可排查。
package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/job51"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
	"goodhr5/local-agent-go-new/internal/version"
)

// TestCandidateListFailureReachesPositionLog 模拟加载探测成功后读取卡片被取消，校验平台与请求两侧的证据都进入分享上传。
func TestCandidateListFailureReachesPositionLog(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const taskID, positionID = "diagnostic-task", "diagnostic-position"
	if err = store.SaveTask(ctx, storage.TaskRun{TaskID: taskID, PositionID: positionID, TaskType: "greeting", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := platform.LoadConfig("51job")
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithTimeout(client.WithTraceID(ctx, taskID), 3*time.Second)
	defer cancel()
	var captured struct {
		Logs []cloud.PositionDiagnosticLog `json:"logs"`
	}
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/positions/"+positionID+"/logs":
			if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
				t.Error(err)
			}
			fmt.Fprint(w, `{"ok":true}`)
		case r.URL.Path == "/health":
			fmt.Fprint(w, `{"ok":true,"data":{"status":"ok"}}`)
		case r.URL.Path == "/api/v1/element/find-all":
			var request struct {
				ProbeOnly bool `json:"probe_only"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			if request.ProbeOnly {
				fmt.Fprint(w, `{"ok":true,"data":[]}`)
				return
			}
			cancel()
			<-r.Context().Done()
		default:
			t.Errorf("意外触发请求：%s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer worker.Close()
	logger := NewTaskLogger(store, nil)
	logged := newLoggedRuntime(job51.NewRuntime(), taskID, logger)
	_, err = logged.FindCandidates(runCtx, client.New(worker.URL), cfg, model.CandidateListOptions{})
	if err == nil {
		t.Fatal("列表读取中断未返回错误")
	}
	logger.Step(taskID, "lifecycle", "finished", "failed", time.Now(), fmt.Errorf("岗位结束：%w", err))
	runner := &Runner{store: store, cloud: cloud.New(worker.URL)}
	if err := runner.UploadPositionLogs(ctx, "test-token", positionID); err != nil {
		t.Fatal(err)
	}
	if len(captured.Logs) != 2 {
		t.Fatalf("预期平台错误和任务结束各有一条记录，实际 %d 条", len(captured.Logs))
	}
	var firstRequestID string
	for _, entry := range captured.Logs {
		var output struct {
			Browser struct {
				List struct {
					Stage       string   `json:"stage"`
					LastSuccess string   `json:"last_success_stage"`
					Timeline    []string `json:"timeline"`
				} `json:"list"`
				Request struct {
					ID       string `json:"request_id"`
					Reason   string `json:"reason"`
					Received *bool  `json:"response_received"`
				} `json:"request"`
				Version string `json:"agent_version"`
				OS      string `json:"os"`
			} `json:"browser_diagnostic"`
		}
		if err := json.Unmarshal(entry.Output, &output); err != nil {
			t.Fatal(err)
		}
		e := output.Browser
		if !entry.IsError || e.List.Stage == "" || e.List.LastSuccess == "" || len(e.List.Timeline) == 0 ||
			e.Request.ID == "" || e.Request.Reason == "" || e.Request.Received == nil || *e.Request.Received ||
			e.Version != version.Value || e.OS != runtime.GOOS {
			t.Fatalf("上传的 %s 缺少关键证据：%s", entry.Step, entry.Output)
		}
		if firstRequestID != "" && firstRequestID != e.Request.ID {
			t.Fatalf("上层任务错误未保留同一请求：%s", e.Request.ID)
		}
		firstRequestID = e.Request.ID
	}
}
