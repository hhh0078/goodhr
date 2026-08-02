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

// TestBindAgentSendsStableDevice 验证本地程序会携带 Token 和稳定设备信息请求云端绑定。
func TestBindAgentSendsStableDevice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/agents/bind" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer browser-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		var request AgentBindRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.MachineID != "goodhr-device-v1-test" || request.LocalPort != 43129 {
			t.Fatalf("binding request = %+v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"agent":{"machine_id":"goodhr-device-v1-test","agent_version":"6","local_port":43129,"bind_status":"active"}}`))
	}))
	defer server.Close()

	binding, err := New(server.URL).BindAgent(context.Background(), "browser-token", AgentBindRequest{
		MachineID: "goodhr-device-v1-test", AgentVersion: "6", LocalPort: 43129,
	})
	if err != nil {
		t.Fatal(err)
	}
	if binding.MachineID != "goodhr-device-v1-test" || binding.BindStatus != "active" {
		t.Fatalf("binding = %+v", binding)
	}
}

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
			TaskType  string `json:"task_type"`
			MachineID string `json:"machine_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.TaskType != "greeting" || payload.MachineID != "goodhr-device-v1-test" {
			t.Fatalf("start payload = %+v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"status":"running"}`))
	}))
	defer server.Close()

	if err := New(server.URL).RequestPositionStart(context.Background(), "token", "position-1", "greeting", "goodhr-device-v1-test"); err != nil {
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

	err := New(server.URL).RequestPositionStart(context.Background(), "token", "position-1", "greeting", "goodhr-device-v1-test")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want APIError", err)
	}
	if apiErr.Code != "POSITION_TASK_CONFLICT" || apiErr.Message != "已有岗位正在运行" {
		t.Fatalf("unexpected api error: %+v", apiErr)
	}
}

// TestSavePositionCandidateUsesStructuredEndpoint 验证结构化候选人会使用岗位简历库接口和强类型字段。
func TestSavePositionCandidateUsesStructuredEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/positions/position-1/candidates" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		var payload CandidateUpload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.CandidateName != "候选人甲" || payload.PlatformID != "hliepin" {
			t.Fatalf("candidate payload = %+v", payload)
		}
		if payload.AIGreetScore == nil || *payload.AIGreetScore != 88 {
			t.Fatalf("ai greet score = %+v", payload.AIGreetScore)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	score := 88.0
	err := New(server.URL).SavePositionCandidate(context.Background(), "token", "position-1", CandidateUpload{
		StructuredCandidate: StructuredCandidate{CandidateName: "候选人甲"},
		PlatformID:          "hliepin",
		Status:              "greeted",
		AIGreetScore:        &score,
	})
	if err != nil {
		t.Fatalf("同步结构化候选人失败：%v", err)
	}
}

// TestNormalizePositionKeepsGreetingBatchesUnlimitedByDefault 验证未配置扫描批数时不会偷偷限制为三批。
func TestNormalizePositionKeepsGreetingBatchesUnlimitedByDefault(t *testing.T) {
	position := normalizePosition(PositionSnapshot{})
	if position.MaxBatches != 0 {
		t.Fatalf("未配置扫描批数时应持续加载，实际批数上限为 %d", position.MaxBatches)
	}
}

// TestNormalizePositionHonorsConfiguredScanRounds 验证明确配置的扫描轮数仍会生效。
func TestNormalizePositionHonorsConfiguredScanRounds(t *testing.T) {
	position := normalizePosition(PositionSnapshot{
		CommonConfig: PositionCommonConfig{ScanRounds: 5},
	})
	if position.MaxBatches != 5 {
		t.Fatalf("配置扫描轮数没有生效：%d", position.MaxBatches)
	}
}
