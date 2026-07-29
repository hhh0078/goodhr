// Package cloud 验证个人运行配置和云端登录状态错误的强类型解析。
package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// TestPreferencesReadsStrongTypes 验证个人运行配置会完整进入强类型结构。
func TestPreferencesReadsStrongTypes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"config":{"detail_open_probability":66,"scroll_delay_min":2,"scroll_delay_max":5,"request_unused":1,"rest_times_min":1,"rest_times_max":2}}`))
	}))
	defer server.Close()

	preferences, err := New(server.URL).Preferences(context.Background(), "token")
	if err != nil {
		t.Fatalf("读取个人配置失败：%v", err)
	}
	if preferences.DetailOpenProbability != 66 || preferences.ScrollDelayMin != 2 || preferences.ScrollDelayMax != 5 {
		t.Fatalf("个人配置解析不正确：%+v", preferences)
	}
	if preferences.RestTimesMin != 1 || preferences.RestTimesMax != 2 {
		t.Fatalf("休息次数解析不正确：%+v", preferences)
	}
}

// TestValidateSessionReturnsAuthExpiredCode 验证 401 可以被运行中登录检查稳定识别。
func TestValidateSessionReturnsAuthExpiredCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"session is invalid or expired"}`))
	}))
	defer server.Close()

	_, err := New(server.URL).ValidateSession(context.Background(), "expired-token")
	if err == nil || !IsAuthExpired(err) {
		t.Fatalf("401 没有识别为登录失效：%v", err)
	}
}

// TestSyncCompletedSummaryRetriesNotice 验证完成邮件未确认时最多重试三次。
func TestSyncCompletedSummaryRetriesNotice(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		noticeSent := requests == 3
		_, _ = w.Write([]byte(`{"success":true,"notice_sent":` + strconv.FormatBool(noticeSent) + `}`))
	}))
	defer server.Close()

	err := New(server.URL).SyncCompletedSummary(context.Background(), "token", TaskSummary{PositionID: "position-1"})
	if err != nil {
		t.Fatalf("同步完成状态失败：%v", err)
	}
	if requests != 3 {
		t.Fatalf("完成邮件应在第三次确认，实际请求 %d 次", requests)
	}
}

// TestSyncCompletedSummaryUpdatesCountsBeforeStatus 验证完成邮件触发前岗位累计统计已经同步。
func TestSyncCompletedSummaryUpdatesCountsBeforeStatus(t *testing.T) {
	paths := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/status") {
			_, _ = w.Write([]byte(`{"success":true,"notice_sent":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	err := New(server.URL).SyncCompletedSummary(context.Background(), "token", TaskSummary{
		PositionID: "position-1", TaskType: "greeting", Processed: 3, Succeeded: 2,
	})
	if err != nil {
		t.Fatalf("同步完成摘要失败：%v", err)
	}
	if len(paths) != 2 || !strings.HasSuffix(paths[0], "/counts") || !strings.HasSuffix(paths[1], "/status") {
		t.Fatalf("完成同步顺序不正确：%v", paths)
	}
}

// TestSyncRunningStatusDoesNotResetCounts 验证任务启动只同步 running，不会把岗位累计统计写成零。
func TestSyncRunningStatusDoesNotResetCounts(t *testing.T) {
	paths := make([]string, 0, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	err := New(server.URL).SyncSummary(context.Background(), "token", TaskSummary{
		PositionID: "position-1", TaskType: "greeting", Status: "running",
	})
	if err != nil {
		t.Fatalf("同步运行状态失败：%v", err)
	}
	if len(paths) != 1 || !strings.HasSuffix(paths[0], "/status") {
		t.Fatalf("运行状态不应改写累计统计：%v", paths)
	}
}

// TestRequestPositionStartWaitsForCloudPermission 验证本地程序使用专门接口同步等待云端启动许可。
func TestRequestPositionStartWaitsForCloudPermission(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/positions/position-1/start" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		var payload struct {
			TaskType string `json:"task_type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.TaskType != "greeting" {
			t.Fatalf("task_type = %q", payload.TaskType)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"status":"running"}`))
	}))
	defer server.Close()

	if err := New(server.URL).RequestPositionStart(context.Background(), "token", "position-1", "greeting"); err != nil {
		t.Fatalf("申请启动失败：%v", err)
	}
}

// TestRequestPositionStartReadsStructuredError 验证云端拒绝启动时保留稳定错误码和中文信息。
func TestRequestPositionStartReadsStructuredError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"POSITION_TASK_CONFLICT","message":"已有岗位正在运行"}}`))
	}))
	defer server.Close()

	err := New(server.URL).RequestPositionStart(context.Background(), "token", "position-1", "greeting")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want APIError", err)
	}
	if apiErr.Code != "POSITION_TASK_CONFLICT" || apiErr.Message != "已有岗位正在运行" {
		t.Fatalf("unexpected api error: %+v", apiErr)
	}
}

// TestDecodeLegacyPlatformConfig 验证云端当前平台 JSON 能转换为新版 URL、选择器和行为配置。
func TestDecodeLegacyPlatformConfig(t *testing.T) {
	raw := json.RawMessage(`{
		"id":"zhaopin",
		"name":"智联招聘",
		"auth":{
			"entry_url":"https://rd6.zhaopin.com/app/recommend",
			"login_url_prefixes":["https://passport.zhaopin.com"]
		},
		"card":{
			"item":{"parent_classes":[["[role=group]"]],"target_classes":[[".candidate-card"]]},
			"scroll":{"target_classes":[["body"]]},
			"fields":[{"name":{"target_classes":[[".candidate-name"]]}}]
		},
		"actions":{"greetBtn":{"target_classes":[[".greet-button"]]}},
		"detail":{
			"openTarget":{"target_classes":[[".candidate-card"]]},
			"content":{"target_classes":[[".resume-detail"]]}
		},
		"position":{
			"switchBtn":{"target_classes":[[".position-switch"]]},
			"item":{"target_classes":[[".position-item"]]}
		},
		"behavior":{"needsDetailPage":true,"supportsPaging":false}
	}`)
	cfg, err := decodePlatformConfig(raw, "zhaopin")
	if err != nil {
		t.Fatalf("转换云端平台配置失败：%v", err)
	}
	if cfg.EntryURL != "https://rd6.zhaopin.com/app/recommend" ||
		cfg.LoginURL != "https://passport.zhaopin.com" {
		t.Fatalf("平台页面地址转换不完整：%+v", cfg)
	}
	if len(cfg.Selectors["candidate.item"].Parents) != 1 ||
		len(cfg.Selectors["candidate.greet"].Target.Selectors) != 1 ||
		len(cfg.CandidateFields["name"].Target.Selectors) != 1 {
		t.Fatalf("平台选择器转换不完整：%+v", cfg)
	}
	if !cfg.Behavior.DirectPositionSelection ||
		!cfg.Behavior.SelectFirstPositionResult ||
		!cfg.Behavior.NeedsDetail {
		t.Fatalf("智联平台行为转换不完整：%+v", cfg.Behavior)
	}
}
