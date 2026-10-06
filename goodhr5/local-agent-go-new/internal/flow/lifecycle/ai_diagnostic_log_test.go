// Package lifecycle 文件作用：验证 AI 失败原文完整落库及上传，普通日志限制与凭据脱敏保持生效。
package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/storage"
)

// TestAIResponseLogStoredAndUploadedWhole 验证基础、最终及后台错误都可附完整诊断，外层错误不会反复复制原文。
func TestAIResponseLogStoredAndUploadedWhole(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveTask(ctx, storage.TaskRun{TaskID: "ai-task", PositionID: "ai-position", Status: "running", TaskType: "greeting"}); err != nil {
		t.Fatal(err)
	}
	logger := NewTaskLogger(store, nil)
	response := "开始\n" + strings.Repeat("AI 实际返回内容\n", 4000) + "\n末尾证据"
	for _, step := range []string{"candidate_preview", "ai_decision", "complete_ai_output"} {
		aiError := &ai.ResponseError{Cause: errors.New("AI 格式不对"), Diagnostic: ai.ResponseDiagnostic{Model: "test-model", Response: response, ResponseKind: "model_content", ParseError: "字段 score 类型不正确", ParseField: "score", ParseOffset: 42, ResponseComplete: true}}
		logger.Step("ai-task", "greeting", step, "warning", time.Now(), fmt.Errorf("处理 AI 失败：%w", aiError))
		logger.Step("ai-task", "greeting", "scan_decide_and_greet", "failed", time.Now(), fmt.Errorf("上层包装：%w", aiError))
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
	runner := &Runner{store: store, cloud: cloud.New(server.URL)}
	if err := runner.UploadPositionLogs(ctx, "test-token", "ai-position"); err != nil {
		t.Fatal(err)
	}
	if len(captured.Logs) != 6 {
		t.Fatalf("日志数量不正确：%d", len(captured.Logs))
	}
	for index, log := range captured.Logs {
		var output struct {
			Diagnostic *ai.ResponseDiagnostic `json:"ai_response_diagnostic"`
			Reference  string                 `json:"ai_response_reference"`
		}
		if err := json.Unmarshal(log.Output, &output); err != nil {
			t.Fatal(err)
		}
		if index%2 == 0 {
			if output.Diagnostic == nil || output.Diagnostic.Response != response || output.Diagnostic.Model != "test-model" || output.Diagnostic.ParseOffset != 42 {
				t.Fatalf("完整诊断丢失：index=%d output=%+v", index, output.Diagnostic)
			}
		} else if output.Diagnostic != nil || output.Reference == "" {
			t.Fatalf("外层包装重复保存原文：index=%d", index)
		}
	}
}

// TestUploadObjectPreservesOnlyCompleteAIField 验证 AI 原文豁免普通对象限额，但其他超长出参继续收敛。
func TestUploadObjectPreservesOnlyCompleteAIField(t *testing.T) {
	response := strings.Repeat("原始返回", 8000) + ` {"token":"private-token"} password=private-password 13800138000 candidate@example.com`
	raw, _ := json.Marshal(map[string]any{"ordinary": strings.Repeat("x", 20000), "ai_response_diagnostic": ai.ResponseDiagnostic{Response: response, Model: "test-model"}})
	value := uploadLogObject(raw)
	var output map[string]json.RawMessage
	if err := json.Unmarshal(value, &output); err != nil {
		t.Fatal(err)
	}
	if _, ok := output["ordinary"]; ok {
		t.Fatal("普通超长对象没有维持限制")
	}
	if !strings.Contains(string(value), "原始返回") || !strings.Contains(string(value), "13800138000") || !strings.Contains(string(value), "candidate@example.com") {
		t.Fatal("授权的 AI 完整原文被普通脱敏或限额规则改写")
	}
	for _, secret := range []string{"private-token", "private-password"} {
		if strings.Contains(string(value), secret) {
			t.Fatalf("访问凭据没有脱敏：%s", secret)
		}
	}
	if len(output["ai_response_diagnostic"]) < 12<<10 {
		t.Fatal("AI 失败原文仍被 12 KiB 限额截断")
	}
}

// TestAIResponseDedupDoesNotReferenceExpiredLog 验证千条保留、清空及去重缓存淘汰后都会重新保存完整原文。
func TestAIResponseDedupDoesNotReferenceExpiredLog(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveTask(ctx, storage.TaskRun{TaskID: "retention-task", PositionID: "retention-position", Status: "running", TaskType: "greeting"}); err != nil {
		t.Fatal(err)
	}
	logger := NewTaskLogger(store, nil)
	responseErr := &ai.ResponseError{Cause: errors.New("AI 格式不对"), Diagnostic: ai.ResponseDiagnostic{Response: "完整原文", Model: "test-model"}}
	logger.Step("retention-task", "greeting", "ai_decision", "failed", time.Now(), responseErr)
	for index := 0; index < 999; index++ {
		if _, err := store.SaveTaskLog(ctx, storage.TaskLog{TaskID: "retention-task", Message: "后续普通日志"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, scenario := range []string{"追加后刚好淘汰原文", "手动清空", "去重缓存淘汰"} {
		switch scenario {
		case "手动清空":
			if err := store.ClearPositionLogs(ctx, "retention-position"); err != nil {
				t.Fatal(err)
			}
		case "去重缓存淘汰":
			for index := 0; index < 257; index++ {
				other := &ai.ResponseError{Cause: errors.New("其他AI失败"), Diagnostic: ai.ResponseDiagnostic{Response: "其他原文"}}
				logger.Step("retention-task", "greeting", "ai_decision", "failed", time.Now(), other)
			}
		}
		logger.Step("retention-task", "greeting", "scan_decide_and_greet", "failed", time.Now(), fmt.Errorf("外层错误：%w", responseErr))
		logs, err := store.ListPositionLogs(ctx, "retention-position", 1000)
		if err != nil {
			t.Fatal(err)
		}
		var output struct {
			Diagnostic *ai.ResponseDiagnostic `json:"ai_response_diagnostic"`
		}
		if err := json.Unmarshal(logs[len(logs)-1].Output, &output); err != nil {
			t.Fatal(err)
		}
		if output.Diagnostic == nil || output.Diagnostic.Response != "完整原文" {
			t.Fatalf("%s后只保存引用，丢失原文", scenario)
		}
	}
}
