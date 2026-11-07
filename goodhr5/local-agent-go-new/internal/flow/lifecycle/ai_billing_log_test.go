// Package lifecycle 本文件通过合成 AI 与真实本地日志存储验证费用可见、四位小数及并发岗位和候选人的归属。
package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// TestAIBillingLogsKeepConcurrentOwners 验证多岗位并发与当前页面候选人改变时，每笔费用仍写入请求现场的岗位日志。
func TestAIBillingLogsKeepConcurrentOwners(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	logger := NewTaskLogger(store, nil)
	logger.SetTaskPosition("task", "unrelated-position")
	logger.candidateTaskID, logger.candidateName = "task", "当前页面其他候选人"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-GoodHR-Billing", "1")
		fmt.Fprintf(w, `{"choices":[{"message":{"content":"{\"score\":80,\"reason\":\"符合岗位要求\"}"}}],"goodhr_billing":{"status":"charged","cost_units":23,"model":%q}}`, request.Model)
	}))
	defer server.Close()
	client := ai.New()
	var group sync.WaitGroup
	for index := 0; index < 5; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			position, name := fmt.Sprintf("position-%d", index), fmt.Sprintf("候选人%d", index)
			ctx := shared.WithAIBilling(context.Background(), logger, "task", position, name, "AI 基础预分析")
			_, err := client.EvaluateCandidatePreview(ctx, cloud.AIConfig{BaseURL: server.URL, APIKey: "ghai_test", Model: "test-model"}, cloud.PositionSnapshot{}, model.Candidate{Name: name})
			if err != nil {
				t.Error(err)
			}
		}(index)
	}
	group.Wait()
	for index := 0; index < 5; index++ {
		position, name := fmt.Sprintf("position-%d", index), fmt.Sprintf("候选人%d", index)
		items, err := store.ListPositionLogs(context.Background(), position, 20)
		if err != nil || len(items) != 1 {
			t.Fatalf("岗位费用日志缺失：%s %+v %v", position, items, err)
		}
		item := items[0]
		if !strings.Contains(item.Message, "本次消耗 ¥0.0023") || !strings.Contains(item.Message, name) || strings.Contains(item.Message, "其他候选人") || item.Step != "ai_billing" || item.IsError {
			t.Fatalf("费用日志显示或归属错误：%+v", item)
		}
	}
}

// TestAIBillingUnknownAndZero 验证零费用可显示、未知费用不能伪装成零，自定义接口有明确提示。
func TestAIBillingUnknownAndZero(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	logger := NewTaskLogger(store, nil)
	zero := int64(0)
	for _, tc := range []struct {
		name, status, want string
		units              *int64
	}{
		{"zero", "charged", "本次消耗 ¥0.0000", &zero},
		{"unknown", "unavailable", "金额暂未确认", nil},
		{"failed", "failed", "金额暂未确认", nil},
		{"external", "external", "费用由服务商结算", nil},
	} {
		logger.AIBilling("task", tc.name, "候选人", "AI 详情分析", ai.Billing{Status: tc.status, CostUnits: tc.units})
		items, err := store.ListPositionLogs(context.Background(), tc.name, 10)
		if err != nil || len(items) != 1 || !strings.Contains(items[0].Message, tc.want) {
			t.Fatalf("日志提示错误：%+v %v", items, err)
		}
		if tc.units == nil && strings.Contains(items[0].Message, "¥") {
			t.Fatal("未知金额显示成免费")
		}
	}
}

// TestAIBillingUploadKeepsCostPrivateName 验证自动回复费用上传后仍可见，候选人姓名继续只保存在本机。
func TestAIBillingUploadKeepsCostPrivateName(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err = store.SaveTask(ctx, storage.TaskRun{TaskID: "reply-task", PositionID: "position", TaskType: "auto_reply", StartedAt: time.Now(), Status: "running"}); err != nil {
		t.Fatal(err)
	}
	units := int64(23)
	NewTaskLogger(store, nil).AIBilling("reply-task", "position", "仅本机可见的候选人", "自动回复 AI 第1轮", ai.Billing{Status: "charged", CostUnits: &units, Model: "test-model"})
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
	runner := &Runner{store: store, cloud: cloud.New(server.URL)}
	if err = runner.UploadPositionLogs(ctx, "test-token", "position"); err != nil {
		t.Fatal(err)
	}
	if len(captured.Logs) != 1 || !strings.Contains(captured.Logs[0].Message, "¥0.0023") || strings.Contains(captured.Logs[0].Message, "候选人") {
		t.Fatalf("云端费用丢失或包含本地资料：%+v", captured.Logs)
	}
}
