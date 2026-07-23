// Package positionrunner 负责测试 Go 本地岗位运行运行器。
package positionrunner

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"goodhr5/local-agent-go/internal/browser"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/config"
	"goodhr5/local-agent-go/internal/localai"
	"goodhr5/local-agent-go/internal/localdb"
	"goodhr5/local-agent-go/internal/ocr"
	"goodhr5/local-agent-go/internal/platformcore"
	"goodhr5/local-agent-go/internal/platforms"
)

// TestAITimeoutsUnifiedAtThreeMinutes 验证岗位运行的 AI 操作和候选人总处理上限统一为 180 秒。
func TestAITimeoutsUnifiedAtThreeMinutes(t *testing.T) {
	timeouts := map[string]time.Duration{
		"candidate_total": candidateTotalTimeout,
		"precheck":        aiPrecheckTimeout,
		"detail":          aiDetailTimeout,
		"score":           aiScoreTimeout,
		"vision_output":   pendingAIVisionOutputTimeout,
	}
	for name, timeout := range timeouts {
		if timeout != 180*time.Second {
			t.Fatalf("%s timeout = %s, want 3m0s", name, timeout)
		}
	}
	config := aiConfigFromCloud(map[string]any{})
	if config.Timeout != localai.DefaultRequestTimeoutSeconds {
		t.Fatalf("cloud ai timeout = %d, want %d", config.Timeout, localai.DefaultRequestTimeoutSeconds)
	}
}

// TestMergeVisionDecisionKeepsAIScoreFields 验证结构化简历不会覆盖两次真实 AI 分析结果。
func TestMergeVisionDecisionKeepsAIScoreFields(t *testing.T) {
	candidate := map[string]any{
		"ai_detail_score":  72.0,
		"ai_detail_reason": "第一次真实原因",
		"ai_greet_score":   75.0,
		"ai_greet_reason":  "第二次真实原因",
	}
	mergeVisionDecisionIntoCandidate(candidate, localai.Decision{
		ResumeData: map[string]any{
			"candidate_name":   "徐英",
			"ai_detail_score":  10.0,
			"ai_detail_reason": "不该覆盖",
			"ai_greet_score":   20.0,
			"ai_greet_reason":  "也不该覆盖",
		},
	})
	if candidate["candidate_name"] != "徐英" {
		t.Fatalf("candidate_name = %v", candidate["candidate_name"])
	}
	if candidate["ai_detail_score"] != 72.0 || candidate["ai_detail_reason"] != "第一次真实原因" {
		t.Fatalf("detail score fields were overwritten: %+v", candidate)
	}
	if candidate["ai_greet_score"] != 75.0 || candidate["ai_greet_reason"] != "第二次真实原因" {
		t.Fatalf("greet score fields were overwritten: %+v", candidate)
	}
}

// TestPersistPositionCountProgressWritesOnlyNewDelta 验证运行中反复保存统计时只累计新增部分，不会重复记账。
func TestPersistPositionCountProgressWritesOnlyNewDelta(t *testing.T) {
	db := openRunnerTestDB(t)
	position, err := db.CreatePosition(map[string]any{"name": "实时统计岗位", "platform_id": "boss"})
	if err != nil {
		t.Fatal(err)
	}
	runner := newTestRunner(t, db, &fakeWorker{})
	persisted := batchProcessResult{}
	current := batchProcessResult{Scanned: 3, Saved: 1, Greeted: 1, Skipped: 2}

	persisted, err = runner.persistPositionCountProgress(t.Context(), position, current, persisted, StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// 同一个统计快照再次保存，数据库累计值不应重复增加。
	persisted, err = runner.persistPositionCountProgress(t.Context(), position, current, persisted, StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	current = batchProcessResult{Scanned: 4, Saved: 2, Greeted: 1, Skipped: 3, Failed: 1}
	persisted, err = runner.persistPositionCountProgress(t.Context(), position, current, persisted, StartOptions{})
	if err != nil {
		t.Fatal(err)
	}

	updated, err := db.GetPosition(position.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ScannedCount != 4 || updated.GreetedCount != 1 || updated.SkippedCount != 3 || updated.FailedCount != 1 {
		t.Fatalf("position counts = %+v", updated)
	}
	if persisted != current {
		t.Fatalf("persisted = %+v, current = %+v", persisted, current)
	}
}

// TestPositionProfileNameUsesGlobalDefault 验证开始岗位运行始终复用统一浏览器目录。
func TestPositionProfileNameUsesGlobalDefault(t *testing.T) {
	if got := positionProfileName(localdb.Position{PlatformID: "boss"}); got != "default" {
		t.Fatalf("profile = %q, want default", got)
	}
	if got := positionProfileName(localdb.Position{PlatformAccountID: "account-1"}); got != "default" {
		t.Fatalf("profile = %q, want default", got)
	}
}

// TestCandidateInfoRequestFromPositionReadsCommonConfig 验证本地主流程从岗位快照读取三个索要选项和首次打招呼语。
func TestCandidateInfoRequestFromPositionReadsCommonConfig(t *testing.T) {
	request := candidateInfoRequestFromPosition(localdb.Position{PositionSnapshot: map[string]any{
		"common_config": map[string]any{
			"request_phone": true, "request_wechat": true, "request_resume": true,
		},
		"greet_message": "  你好，想和你沟通岗位。  ",
	}})
	if !request.RequestPhone || !request.RequestWechat || !request.RequestResume {
		t.Fatalf("request switches = %+v", request)
	}
	if request.GreetMessage != "你好，想和你沟通岗位。" {
		t.Fatalf("greet message = %q", request.GreetMessage)
	}
	if !candidateInfoRequestConfigured(request) {
		t.Fatal("candidate info request should be configured")
	}
}

// TestCandidateInfoScoreDecisionRequiresScoreAboveThreshold 验证索要信息仅在最终 AI 评分严格大于索要分数时执行。
func TestCandidateInfoScoreDecisionRequiresScoreAboveThreshold(t *testing.T) {
	position := localdb.Position{PositionSnapshot: map[string]any{
		"ai_config": map[string]any{
			"greet_score_threshold":   70.0,
			"request_score_threshold": 80.0,
		},
	}}
	tests := []struct {
		name       string
		candidate  map[string]any
		wantAllow  bool
		wantScore  float64
		wantHasAIS bool
	}{
		{name: "没有AI评分", candidate: map[string]any{}, wantAllow: false, wantScore: 0, wantHasAIS: false},
		{name: "等于索要分数", candidate: map[string]any{"ai_greet_score": 80.0}, wantAllow: false, wantScore: 80, wantHasAIS: true},
		{name: "低于索要分数", candidate: map[string]any{"ai_greet_score": 79.9}, wantAllow: false, wantScore: 79.9, wantHasAIS: true},
		{name: "高于索要分数", candidate: map[string]any{"ai_greet_score": 80.1}, wantAllow: true, wantScore: 80.1, wantHasAIS: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			allowed, score, threshold, hasScore := candidateInfoScoreDecision(position, test.candidate)
			if allowed != test.wantAllow || score != test.wantScore || threshold != 80 || hasScore != test.wantHasAIS {
				t.Fatalf("allowed=%v score=%v threshold=%v hasScore=%v", allowed, score, threshold, hasScore)
			}
		})
	}
}

// TestCandidateInfoScoreDecisionFallsBackToGreetThreshold 验证旧岗位没有索要分数时默认使用打招呼阈值分。
func TestCandidateInfoScoreDecisionFallsBackToGreetThreshold(t *testing.T) {
	position := localdb.Position{PositionSnapshot: map[string]any{
		"ai_config": map[string]any{"greet_score_threshold": 75.0},
	}}
	allowed, score, threshold, hasScore := candidateInfoScoreDecision(position, map[string]any{"ai_greet_score": 76.0})
	if !allowed || score != 76 || threshold != 75 || !hasScore {
		t.Fatalf("allowed=%v score=%v threshold=%v hasScore=%v", allowed, score, threshold, hasScore)
	}
}

// candidateInfoErrorRuntime 模拟打招呼成功但索要信息失败的平台。
type candidateInfoErrorRuntime struct {
	detailCloseProbeRuntime
	requestCalls     int
	greetWillRequest bool
}

// GreetCandidate 记录主流程是否告知平台本候选人打招呼后会立即索要信息。
func (r *candidateInfoErrorRuntime) GreetCandidate(_ context.Context, _ platformcore.Executor, _ cloudapi.PlatformConfig, candidate platformcore.Candidate) error {
	r.greetWillRequest = boolFromMap(candidate, "_candidate_info_after_greet")
	return nil
}

// RequestCandidateInfo 返回索要信息错误，用于验证主流程不会把候选人改成失败。
func (r *candidateInfoErrorRuntime) RequestCandidateInfo(context.Context, platformcore.Executor, cloudapi.PlatformConfig, platformcore.Candidate, platformcore.CandidateInfoRequest) error {
	r.requestCalls++
	return errors.New("索要信息测试失败")
}

// TestCandidateInfoFailureKeepsGreetSuccess 验证索要信息失败只记录警告，候选人仍算打招呼成功。
func TestCandidateInfoFailureKeepsGreetSuccess(t *testing.T) {
	runner := newTestRunner(t, nil, &fakeWorker{})
	runtime := &candidateInfoErrorRuntime{}
	position := localdb.Position{ID: "position-1", PositionSnapshot: map[string]any{
		"common_config": map[string]any{"request_phone": true},
		"ai_config":     map[string]any{"request_score_threshold": 70.0},
	}}
	candidate := map[string]any{"candidate_name": "张三", "status": "passed", "ai_greet_score": 80.0}
	greeted, failed, skipped, err := runner.consumeCandidateForGreet(
		context.Background(), position, runtime, platformExecutor{runner: runner, positionID: position.ID}, nil, candidate, 0, StartOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if greeted != 1 || failed != 0 || skipped != 0 || stringFromMap(candidate, "status") != "greeted" {
		t.Fatalf("result greeted=%d failed=%d skipped=%d candidate=%+v", greeted, failed, skipped, candidate)
	}
	if runtime.requestCalls != 1 {
		t.Fatalf("request calls = %d", runtime.requestCalls)
	}
	if !runtime.greetWillRequest {
		t.Fatal("greet should know candidate info will run")
	}
	if _, exists := candidate["_candidate_info_after_greet"]; exists {
		t.Fatal("temporary candidate info hint should be removed after greet")
	}
}

// TestCandidateInfoWithoutAIScoreSkipsRequester 验证候选人没有最终 AI 评分时不调用平台索要接口。
func TestCandidateInfoWithoutAIScoreSkipsRequester(t *testing.T) {
	runner := newTestRunner(t, nil, &fakeWorker{})
	runtime := &candidateInfoErrorRuntime{}
	position := localdb.Position{ID: "position-no-score", PositionSnapshot: map[string]any{
		"common_config": map[string]any{"request_phone": true},
		"ai_config":     map[string]any{"request_score_threshold": 70.0},
	}}
	candidate := map[string]any{"candidate_name": "李四", "status": "passed"}
	greeted, failed, skipped, err := runner.consumeCandidateForGreet(
		context.Background(), position, runtime, platformExecutor{runner: runner, positionID: position.ID}, nil, candidate, 0, StartOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if greeted != 1 || failed != 0 || skipped != 0 || runtime.requestCalls != 0 {
		t.Fatalf("greeted=%d failed=%d skipped=%d requestCalls=%d", greeted, failed, skipped, runtime.requestCalls)
	}
	if runtime.greetWillRequest {
		t.Fatal("greet should not preserve chat when candidate info is skipped")
	}
}

// TestCloneCandidateForCloudIncludesAIResult 验证同步云端前会组装 ai.detail 和 ai.greet。
func TestCloneCandidateForCloudIncludesAIResult(t *testing.T) {
	payload := cloneCandidateForCloud(localdb.Position{ID: "position-1", PlatformID: "boss"}, map[string]any{
		"candidate_name":   "徐英",
		"ai_detail_score":  72.0,
		"ai_detail_reason": "第一次真实原因",
		"ai_greet_score":   75.0,
		"ai_greet_reason":  "第二次真实原因",
	})
	ai := payload["ai"].(map[string]any)
	detail := ai["detail"].(map[string]any)
	greet := ai["greet"].(map[string]any)
	if detail["score"] != 72.0 || detail["reason"] != "第一次真实原因" {
		t.Fatalf("detail = %+v", detail)
	}
	if greet["score"] != 75.0 || greet["reason"] != "第二次真实原因" {
		t.Fatalf("greet = %+v", greet)
	}
}

// TestRunnerStartStop 验证岗位运行启动会校验会员、读取平台配置、扫描候选人并更新状态。
func TestRunnerStartStop(t *testing.T) {
	speedUpPageEntryCheck(t)
	aiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected ai path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": `{"score":82,"reason":"符合要求"}`}},
			},
			"usage": map[string]any{"total_tokens": 12},
		})
	}))
	defer aiServer.Close()
	var position localdb.Position
	savedCandidates := []map[string]any{}
	var processedResumeCount int64
	var completedStatusCount int64
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/me":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "user": map[string]any{"email": "runner@example.com"}})
		case "/api/subscription/status":
			if r.Header.Get("Authorization") != "Bearer token-1" {
				t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":           true,
				"subscription": map[string]any{"active": true},
			})
		case "/api/platforms/config/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"configs": []map[string]any{
					{"config_key": "platform.boss", "config_value": `{"id":"boss","name":"Boss直聘","auth":{"pages":[{"url":"https://www.zhipin.com/web/chat/other"},{"url":"https://www.zhipin.com/web/chat/recommend","entry":true}]},"position":{"current":{"target_classes":[["current-position"]]},"switchBtn":{"target_classes":[["switch-position"]]},"list":{"target_classes":[["position-list"]]},"item":{"target_classes":[["position-item"]]},"itemText":{"target_classes":[["position-name"]]}}}`},
				},
			})
		case "/api/config/user-preferences":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "config": map[string]any{}})
		case "/api/config/effective-ai":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "config": map[string]any{"base_url": aiServer.URL, "api_key": "test-key", "model": "test-model", "temperature": 0.2}})
		default:
			if strings.HasPrefix(r.URL.Path, "/api/positions/") && strings.HasSuffix(r.URL.Path, "/candidates") {
				var candidate map[string]any
				if err := json.NewDecoder(r.Body).Decode(&candidate); err != nil {
					t.Fatalf("decode candidate: %v", err)
				}
				savedCandidates = append(savedCandidates, candidate)
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
				return
			}
			if strings.HasPrefix(r.URL.Path, "/api/positions/") && strings.HasSuffix(r.URL.Path, "/processed-resumes") {
				var payload struct {
					Count int `json:"count"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("decode processed resumes: %v", err)
				}
				atomic.AddInt64(&processedResumeCount, int64(payload.Count))
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
				return
			}
			if strings.HasPrefix(r.URL.Path, "/api/positions/") && strings.HasSuffix(r.URL.Path, "/status") {
				var payload struct {
					Status string `json:"status"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("decode position status: %v", err)
				}
				response := map[string]any{"ok": true, "status": payload.Status, "notice_sent": false}
				if payload.Status == "completed" {
					atomic.AddInt64(&completedStatusCount, 1)
					response["notice_sent"] = true
				}
				_ = json.NewEncoder(w).Encode(response)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/api/positions/") {
				requestedID := strings.TrimPrefix(r.URL.Path, "/api/positions/")
				positionName := "本地岗位运行"
				if requestedID != position.ID {
					positionName = "本地岗位运行2"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "position": map[string]any{"id": requestedID, "name": positionName, "platform_id": "boss", "mode": "ai", "match_limit": 1, "enable_sound": requestedID != position.ID, "position": map[string]any{"name": positionName}}})
				return
			}
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer cloud.Close()

	db := openRunnerTestDB(t)
	position, err := db.CreatePosition(map[string]any{"name": "本地岗位运行", "platform_id": "boss", "position_snapshot": map[string]any{"name": "本地岗位运行"}})
	if err != nil {
		t.Fatal(err)
	}
	worker := &fakeWorker{}
	runner := newTestRunner(t, db, worker)
	result, err := runner.Start(t.Context(), position.ID, StartOptions{CloudAPIBase: cloud.URL, Token: "token-1", PageReadyDelay: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result["running"] != true {
		t.Fatalf("result = %+v", result)
	}
	updated := waitForPositionStatus(t, db, position.ID, "completed")
	if updated.ScannedCount != 1 {
		t.Fatalf("scanned count = %d", updated.ScannedCount)
	}
	status, err := runner.Status(position.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status["running"] != false {
		t.Fatalf("status = %+v", status)
	}
	if status["progress"] == nil || status["logs"] == nil {
		t.Fatalf("status missing progress/logs: %+v", status)
	}
	if len(savedCandidates) != 1 || savedCandidates[0]["candidate_name"] != "候选人A" {
		t.Fatalf("savedCandidates = %+v", savedCandidates)
	}
	if atomic.LoadInt64(&processedResumeCount) == 0 {
		t.Fatal("processed resume count was not synced")
	}
	if savedCandidates[0]["status"] != "ai_passed" || savedCandidates[0]["ai_greet_score"] == nil {
		t.Fatalf("candidate ai fields = %+v", savedCandidates[0])
	}
	stopResult, err := runner.Stop(position.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopResult["running"] != false || runner.IsRunning(position.ID) {
		t.Fatalf("stopResult = %+v", stopResult)
	}
	stopped, err := db.GetPosition(position.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Status != "stopped" {
		t.Fatalf("stopped status = %s", stopped.Status)
	}
	for _, call := range worker.calls {
		if call == "/api/v1/browser/stop" {
			t.Fatal("停止岗位运行不应该关闭浏览器")
		}
	}

	position2, err := db.CreatePosition(map[string]any{"name": "本地岗位运行2", "platform_id": "boss", "match_limit": 1, "position_snapshot": map[string]any{"name": "本地岗位运行2"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Start(t.Context(), position2.ID, StartOptions{CloudAPIBase: cloud.URL, Token: "token-1", EnableGreet: true, PageReadyDelay: 1}); err != nil {
		t.Fatal(err)
	}
	waitForPositionStatus(t, db, position2.ID, "completed")
	if len(savedCandidates) < 2 || savedCandidates[len(savedCandidates)-1]["status"] != "greeted" {
		t.Fatalf("savedCandidates after position2 = %+v", savedCandidates)
	}
	if atomic.LoadInt64(&completedStatusCount) < 2 {
		t.Fatalf("completed status sync count = %d, want at least 2", completedStatusCount)
	}
	assertPositionLogContains(t, db, position2.ID, "岗位运行完成：本次运行结束，扫描=1，打招呼=1，跳过=0，失败=0")
	assertPositionLogContains(t, db, position2.ID, "完成邮件已发送")
	assertPositionLogContains(t, db, position2.ID, "音频文件不存在或为空")
}

// TestSyncCloudPositionCompletedRetriesMailFailure 验证完成邮件同步失败后本地程序会自动重试。
func TestSyncCloudPositionCompletedRetriesMailFailure(t *testing.T) {
	var completedCalls int64
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt64(&completedCalls, 1)
		if call < 3 {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "smtp unavailable"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":          true,
			"status":      "completed",
			"notice_sent": true,
		})
	}))
	defer cloud.Close()

	db := openRunnerTestDB(t)
	position, err := db.CreatePosition(map[string]any{"name": "完成邮件重试岗位", "platform_id": "boss"})
	if err != nil {
		t.Fatal(err)
	}
	runner := newTestRunner(t, db, &fakeWorker{})
	runner.notifyCloudPositionCompleted(position.ID, StartOptions{
		CloudAPIBase: cloud.URL,
		Token:        "token-1",
	})

	if calls := atomic.LoadInt64(&completedCalls); calls != 3 {
		t.Fatalf("completed status calls = %d, want 3", calls)
	}
	assertPositionLogContains(t, db, position.ID, "第1次同步失败")
	assertPositionLogContains(t, db, position.ID, "完成邮件已发送")
}

// TestCancelRunningPositionsAfterSleep 验证检测到休眠恢复后会取消岗位运行并写入日志。
func TestCancelRunningPositionsAfterSleep(t *testing.T) {
	db := openRunnerTestDB(t)
	position, err := db.CreatePosition(map[string]any{"name": "休眠测试岗位运行", "platform_id": "boss", "position_snapshot": map[string]any{"name": "休眠测试岗位运行"}})
	if err != nil {
		t.Fatal(err)
	}
	runner := newTestRunner(t, db, &fakeWorker{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if !runner.setRunning(position.ID, cancel, StartOptions{}) {
		t.Fatal("setRunning failed")
	}
	runner.cancelRunningPositionsAfterSleep(3 * time.Minute)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("岗位运行没有被休眠检测取消")
	}
	logs, err := db.ListPositionLogs(position.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, item := range logs {
		joined += item.Message + "\n"
	}
	if !strings.Contains(joined, "电脑休眠检测") || !strings.Contains(joined, "心跳中断=3m0s") {
		t.Fatalf("logs = %s", joined)
	}
	runner.clear(position.ID)
}

// TestRunnerStatusPendingWhenPositionMissing 验证未启动的云端岗位运行查询本地状态时不会报错。
func TestRunnerStatusPendingWhenPositionMissing(t *testing.T) {
	db := openRunnerTestDB(t)
	runner := newTestRunner(t, db, &fakeWorker{})

	status, err := runner.Status("cloud-position-1")
	if err != nil {
		t.Fatal(err)
	}
	if status["running"] != false {
		t.Fatalf("running = %+v", status["running"])
	}
	progress, ok := status["progress"].(Progress)
	if !ok {
		t.Fatalf("progress = %+v", status["progress"])
	}
	if progress.Stage != "pending" || progress.Message != "本地岗位运行尚未启动" {
		t.Fatalf("progress = %+v", progress)
	}
}

// TestFreshCandidatesDedupesByPlatformID 验证主流程只按平台候选人 ID 去重。
func TestFreshCandidatesDedupesByPlatformID(t *testing.T) {
	seen := map[string]struct{}{}
	first, duplicates := freshCandidates([]map[string]any{
		{
			"id":             "boss_same",
			"candidate_name": "范召",
			"raw_text":       "范召 29岁 本科 5年 带货主播",
			"fields":         map[string]any{"name": "范召", "basic_info": "29岁 本科 5年 带货主播"},
		},
	}, seen)
	if len(first) != 1 || duplicates != 0 {
		t.Fatalf("first=%+v duplicates=%d", first, duplicates)
	}
	second, duplicates := freshCandidates([]map[string]any{
		{
			"id":             "boss_same",
			"candidate_name": "范召",
			"raw_text":       "范召 29岁 本科 6年 带货主播",
			"fields":         map[string]any{"name": "范召", "basic_info": "29岁 本科 6年 带货主播"},
		},
	}, seen)
	if len(second) != 0 || duplicates != 1 {
		t.Fatalf("second=%+v duplicates=%d", second, duplicates)
	}
}

// TestMaybeRestAfterCandidate 验证候选人处理后会按模拟休息配置进入休息路径。
func TestMaybeRestAfterCandidate(t *testing.T) {
	runner := &Runner{running: map[string]*runState{"position-rest": &runState{progress: Progress{Stage: "running"}}}}
	options := StartOptions{
		RestAfterCandidatesMin: 1,
		RestAfterCandidatesMax: 1,
		RestTimesMin:           1,
		RestTimesMax:           1,
		RestDurationMin:        1,
		RestDurationMax:        1,
	}
	runner.initRestState("position-rest", options)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := runner.maybeRestAfterCandidate(ctx, "position-rest", platformExecutor{}, options); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if runner.running["position-rest"].restUsed != 1 {
		t.Fatalf("restUsed = %d", runner.running["position-rest"].restUsed)
	}
}

// TestFormatRestDuration 验证休息时长会格式化为清楚的分钟和秒数。
func TestFormatRestDuration(t *testing.T) {
	if got := formatRestDuration(6*time.Minute + 42*time.Second); got != "6 分 42 秒" {
		t.Fatalf("formatRestDuration() = %s", got)
	}
}

// TestPlatformEntryURL 验证平台入口页读取规则与云端运行时一致。
func TestPlatformEntryURL(t *testing.T) {
	config := cloudapi.PlatformConfig{
		"auth": map[string]any{
			"pages": []any{
				map[string]any{"url": "https://example.com/first"},
				map[string]any{"url": "https://example.com/entry", "entry": true},
			},
		},
	}
	if url := platformEntryURL(config); url != "https://example.com/entry" {
		t.Fatalf("entry url = %s", url)
	}
	fallbackConfig := cloudapi.PlatformConfig{
		"auth": map[string]any{
			"pages": []any{
				map[string]any{"url": "https://example.com/first"},
			},
		},
	}
	if url := platformEntryURL(fallbackConfig); url != "https://example.com/first" {
		t.Fatalf("fallback url = %s", url)
	}
	legacyConfig := cloudapi.PlatformConfig{
		"pages": []any{
			map[string]any{"url": "https://example.com/legacy"},
		},
	}
	if url := platformEntryURL(legacyConfig); url != "https://example.com/legacy" {
		t.Fatalf("legacy url = %s", url)
	}
}

// TestRunnerStartRequiresToken 验证空 token 会在启动前被拦截。
func TestRunnerStartRequiresToken(t *testing.T) {
	db := openRunnerTestDB(t)
	position, err := db.CreatePosition(map[string]any{"name": "本地岗位运行", "platform_id": "boss", "position_snapshot": map[string]any{"name": "本地岗位运行"}})
	if err != nil {
		t.Fatal(err)
	}
	runner := newTestRunner(t, db, &fakeWorker{})
	if _, err := runner.Start(t.Context(), position.ID, StartOptions{CloudAPIBase: "https://goodhr5.58it.cn"}); err == nil || err.Error() != "请先登录后再校验会员" {
		t.Fatalf("err = %v", err)
	}
	updated, err := db.GetPosition(position.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status == "running" {
		t.Fatalf("空 token 不应启动岗位运行，当前状态=%s", updated.Status)
	}
}

// TestBuildPositionRuntimeSnapshotAllowsFreeKeywordPosition 验证会员过期时仍允许非 AI 岗位运行启动。
func TestBuildPositionRuntimeSnapshotAllowsFreeKeywordPosition(t *testing.T) {
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/me":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "user": map[string]any{"email": "runner@example.com"}})
		case "/api/subscription/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "subscription": map[string]any{"active": false}})
		case "/api/config/user-preferences":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "config": map[string]any{}})
		case "/api/platforms/config/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"configs": []map[string]any{
					{"config_key": "platform.boss", "config_value": `{"id":"boss","auth":{"pages":[{"url":"https://www.zhipin.com/web/chat/recommend","entry":true}]}}`},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer cloud.Close()

	runner := newTestRunner(t, openRunnerTestDB(t), &fakeWorker{})
	position := localdb.Position{
		ID:               "position-free-keyword",
		PlatformID:       "boss",
		Mode:             "keyword",
		PositionSnapshot: map[string]any{"common_config": map[string]any{"detail_mode": "ocr"}},
	}
	snapshot, err := runner.buildPositionRuntimeSnapshot(t.Context(), cloudapi.New(cloud.URL), position, StartOptions{Token: "token-1"}, 1)
	if err != nil {
		t.Fatalf("keyword position should be allowed when subscription expired: %v", err)
	}
	if snapshot.Position.ID != position.ID || len(snapshot.PlatformConfig) == 0 {
		t.Fatalf("unexpected snapshot = %+v", snapshot)
	}
}

// TestBuildPositionRuntimeSnapshotBlocksExpiredAIFeature 验证会员过期时会拦截 AI 功能岗位运行。
func TestBuildPositionRuntimeSnapshotBlocksExpiredAIFeature(t *testing.T) {
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/subscription/status" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "subscription": map[string]any{"active": false}})
	}))
	defer cloud.Close()

	runner := newTestRunner(t, openRunnerTestDB(t), &fakeWorker{})
	position := localdb.Position{
		ID:               "position-ai",
		PlatformID:       "boss",
		Mode:             "ai",
		PositionSnapshot: map[string]any{"common_config": map[string]any{"detail_mode": "ocr"}},
	}
	_, err := runner.buildPositionRuntimeSnapshot(t.Context(), cloudapi.New(cloud.URL), position, StartOptions{Token: "token-1"}, 1)
	if err == nil || !strings.Contains(err.Error(), "当前岗位运行使用了 AI 筛选或 AI 详情识别") {
		t.Fatalf("err = %v", err)
	}
}

// TestValidateAIConfig 验证 AI 配置会在岗位运行启动阶段提前校验。
func TestValidateAIConfig(t *testing.T) {
	cases := []struct {
		name    string
		config  localdb.AIConfig
		wantErr string
	}{
		{
			name:    "缺少接口地址",
			config:  localdb.AIConfig{APIKey: "key", Model: "model"},
			wantErr: "请先在个人配置里填写云端 AI 接口地址",
		},
		{
			name:    "缺少密钥",
			config:  localdb.AIConfig{BaseURL: "https://example.com", Model: "model"},
			wantErr: "请先在个人配置里填写云端 AI Key",
		},
		{
			name:    "缺少模型",
			config:  localdb.AIConfig{BaseURL: "https://example.com", APIKey: "key"},
			wantErr: "请先在个人配置里填写 AI 模型",
		},
		{
			name:   "配置完整",
			config: localdb.AIConfig{BaseURL: "https://example.com", APIKey: "key", Model: "model"},
		},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			err := validateAIConfig(item.config)
			if item.wantErr == "" {
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err == nil || err.Error() != item.wantErr {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

// TestRunnerMissingEntryURLDoesNotStartBrowser 验证缺少入口页时不会启动浏览器。
func TestRunnerMissingEntryURLDoesNotStartBrowser(t *testing.T) {
	db := openRunnerTestDB(t)
	position := localdb.Position{ID: "position-1", PlatformID: "boss"}
	worker := &fakeWorker{}
	runner := newTestRunner(t, db, worker)
	if _, err := runner.scanOnce(t.Context(), position, cloudapi.PlatformConfig{"auth": map[string]any{"pages": []any{}}}, StartOptions{}); err == nil || err.Error() != "云端平台配置缺少入口页面地址" {
		t.Fatalf("err = %v", err)
	}
	if len(worker.calls) != 0 {
		t.Fatalf("缺少入口页时不应启动浏览器，calls=%v", worker.calls)
	}
}

// TestEnsurePositionPageReadyRetries 验证页面刚打开时会等待多次检查。
func TestEnsurePositionPageReadyRetries(t *testing.T) {
	speedUpPageEntryCheck(t)

	db := openRunnerTestDB(t)
	position := localdb.Position{ID: "position-1", PlatformID: "boss", PositionSnapshot: map[string]any{"name": "本地岗位运行"}}
	worker := &fakeWorker{pageListEmptyBefore: 5}
	runner := newTestRunner(t, db, worker)
	platformConfig := cloudapi.PlatformConfig{
		"auth": map[string]any{
			"pages": []any{map[string]any{"url": "https://www.zhipin.com/web/chat/recommend", "entry": true}},
		},
		"position": map[string]any{
			"current": map[string]any{"target_classes": []any{[]any{"current-position"}}},
		},
	}
	platformRuntime, err := platforms.RuntimeFor("boss")
	if err != nil {
		t.Fatal(err)
	}
	exec := platformExecutor{runner: runner, positionID: position.ID}
	if err := runner.ensurePositionPageReady(t.Context(), position, platformRuntime, exec, platformConfig); err != nil {
		t.Fatal(err)
	}
	if worker.pageListCalls != 6 {
		t.Fatalf("页面检查次数 = %d", worker.pageListCalls)
	}
}

// TestShouldSkipPositionSelection 验证猎聘猎头端跳过全部页面岗位处理。
// t 为测试对象。
func TestShouldSkipPositionSelection(t *testing.T) {
	platformRuntime, err := platforms.RuntimeFor("hliepin")
	if err != nil {
		t.Fatal(err)
	}
	if !shouldSkipPositionSelection(platformRuntime) {
		t.Fatal("猎聘猎头端应跳过页面岗位处理")
	}
}

// TestPageReadyDelayUsesShortDefault 验证候选人提取前默认只等待两秒。
// t 为测试对象。
func TestPageReadyDelayUsesShortDefault(t *testing.T) {
	if delay := pageReadyDelay(StartOptions{}); delay != 2*time.Second {
		t.Fatalf("默认页面稳定等待 = %s", delay)
	}
	if delay := pageReadyDelay(StartOptions{PageReadyDelay: 750}); delay != 750*time.Millisecond {
		t.Fatalf("自定义页面稳定等待 = %s", delay)
	}
}

// TestApplyKeywordFilter 验证关键词和排除词过滤。
func TestApplyKeywordFilter(t *testing.T) {
	position := localdb.Position{
		PositionSnapshot: map[string]any{
			"keywords":         []any{"本科", "销售"},
			"exclude_keywords": []any{"外包"},
			"is_and_mode":      true,
		},
	}
	candidates := []map[string]any{
		{"id": "1", "raw_text": "本科 三年 销售经验"},
		{"id": "2", "raw_text": "本科 外包 项目"},
		{"id": "3", "raw_text": "本科 客服"},
	}
	logs := []string{}
	filtered, skipped := applyKeywordFilter(position, candidates, func(message string) {
		logs = append(logs, message)
	})
	if skipped != 2 || len(filtered) != 1 || filtered[0]["id"] != "1" {
		t.Fatalf("filtered = %+v, skipped = %d", filtered, skipped)
	}
	joinedLogs := strings.Join(logs, "\n")
	if !strings.Contains(joinedLogs, "列表关键词通过") || !strings.Contains(joinedLogs, "命中=本科、销售") {
		t.Fatalf("logs = %s", joinedLogs)
	}
	if !strings.Contains(joinedLogs, "命中排除词=外包") {
		t.Fatalf("logs = %s", joinedLogs)
	}
}

// TestPrepareCandidatesForFirstStageWithDetail 验证有详情阶段时列表阶段不做关键词终判。
func TestPrepareCandidatesForFirstStageWithDetail(t *testing.T) {
	position := localdb.Position{
		Mode: "keyword",
		PositionSnapshot: map[string]any{
			"keywords":      []any{"本科"},
			"common_config": map[string]any{"detail_mode": "ocr"},
		},
	}
	candidates := []map[string]any{{"id": "1", "raw_text": "候选人基础信息较少"}}
	filtered, skipped := prepareCandidatesForFirstStage(position, candidates)
	if skipped != 0 || len(filtered) != 1 || filtered[0]["status"] != "passed" {
		t.Fatalf("filtered = %+v, skipped = %d", filtered, skipped)
	}
}

// TestApplyKeywordGreetDecision 验证详情文本出来后再做关键词最终判断。
func TestApplyKeywordGreetDecision(t *testing.T) {
	position := localdb.Position{
		Mode: "keyword",
		PositionSnapshot: map[string]any{
			"keywords":         []any{"本科", "销售"},
			"exclude_keywords": []any{"外包"},
			"is_and_mode":      true,
		},
	}
	logs := []string{}
	passed := map[string]any{"name": "张三", "detail_text": "本科，五年销售经验"}
	if skipped := applyKeywordGreetDecisionWithLog(position, passed, func(message string) {
		logs = append(logs, message)
	}); skipped != 0 || passed["status"] != "passed" {
		t.Fatalf("passed = %+v, skipped = %d", passed, skipped)
	}
	if joinedLogs := strings.Join(logs, "\n"); !strings.Contains(joinedLogs, "列表过滤：详情关键词通过，候选人=张三") || !strings.Contains(joinedLogs, "命中=本科、销售") {
		t.Fatalf("logs = %s", joinedLogs)
	}
	rejected := map[string]any{"detail_text": "本科，外包项目经验"}
	if skipped := applyKeywordGreetDecision(position, rejected); skipped != 1 || rejected["status"] != "skipped" {
		t.Fatalf("rejected = %+v, skipped = %d", rejected, skipped)
	}
}

// TestStringListFromMapSplitsKeywordText 验证本地关键词读取兼容中文分隔符。
func TestStringListFromMapSplitsKeywordText(t *testing.T) {
	item := map[string]any{"keywords": "本科，销售 主播、直播\n带货;运营；运营"}
	words := stringListFromMap(item, "keywords")
	want := []string{"本科", "销售", "主播", "直播", "带货", "运营"}
	if len(words) != len(want) {
		t.Fatalf("words = %+v", words)
	}
	for index, word := range want {
		if words[index] != word {
			t.Fatalf("words = %+v", words)
		}
	}
}

// TestRunOptionBounds 验证岗位运行运行参数默认值和上限。
func TestRunOptionBounds(t *testing.T) {
	if scanRounds(StartOptions{}) != defaultScanRounds {
		t.Fatal("scanRounds 默认值不正确")
	}
	if maxItemsPerRound(StartOptions{}) != 0 {
		t.Fatal("maxItems 默认值不正确")
	}
	if scanRounds(StartOptions{ScanRounds: 99}) != 20 {
		t.Fatal("scanRounds 上限不正确")
	}
	if maxItemsPerRound(StartOptions{MaxItems: 999}) != 999 {
		t.Fatal("maxItems 应保留用户配置")
	}
	if scrollDistance(StartOptions{ScrollDistance: 9999}) != 3000 {
		t.Fatal("scrollDistance 上限不正确")
	}
	if detailOpenProbability(StartOptions{}) != 100 {
		t.Fatal("未读取个人配置时打开详情概率应默认 100")
	}
	if shouldOpenDetailByProbability(StartOptions{DetailOpenProbability: 0, detailOpenProbabilitySet: true}) {
		t.Fatal("打开详情概率为 0 时不应打开详情")
	}
	prefsOptions := applyCloudPreferences(StartOptions{}, map[string]any{"detail_open_probability": 0})
	if detailOpenProbability(prefsOptions) != 0 || shouldOpenDetailByProbability(prefsOptions) {
		t.Fatal("个人配置里的 0 概率应生效")
	}
	for i := 0; i < 20; i++ {
		distance := randomScrollDistance(StartOptions{})
		if distance < 560 || distance > 880 {
			t.Fatalf("随机滚动距离超出范围：%d", distance)
		}
	}
	if candidatePipelineConcurrency(2) != 2 || candidatePipelineConcurrency(15) != defaultCandidatePipelineConcurrency {
		t.Fatal("候选人流水线并发数不正确")
	}
}

// TestRunnerStopWaitsForCurrentStep 验证停止岗位运行不会取消当前 Worker 调用。
func TestRunnerStopWaitsForCurrentStep(t *testing.T) {
	speedUpPageEntryCheck(t)
	var position localdb.Position
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/me":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "user": map[string]any{"email": "runner@example.com"}})
		case "/api/subscription/status":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":           true,
				"subscription": map[string]any{"active": true},
			})
		case "/api/platforms/config/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"configs": []map[string]any{
					{"config_key": "platform.boss", "config_value": `{"id":"boss","pages":[{"url":"https://www.zhipin.com/web/chat/recommend"}],"position":{"current":{"target_classes":[["current-position"]]},"switchBtn":{"target_classes":[["switch-position"]]},"list":{"target_classes":[["position-list"]]},"item":{"target_classes":[["position-item"]]},"itemText":{"target_classes":[["position-name"]]}}}`},
				},
			})
		case "/api/config/user-preferences":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "config": map[string]any{}})
		default:
			if strings.HasPrefix(r.URL.Path, "/api/positions/") {
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "position": map[string]any{"id": position.ID, "name": "可停止岗位运行", "platform_id": "boss", "mode": "keyword", "position": map[string]any{"name": "可停止岗位运行"}}})
				return
			}
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer cloud.Close()

	db := openRunnerTestDB(t)
	position, err := db.CreatePosition(map[string]any{"name": "可停止岗位运行", "platform_id": "boss", "mode": "keyword", "position_snapshot": map[string]any{"name": "可停止岗位运行"}})
	if err != nil {
		t.Fatal(err)
	}
	worker := &blockingWorker{extractStarted: make(chan struct{}), allowFinish: make(chan struct{}), released: make(chan struct{})}
	runner := newTestRunner(t, db, worker)
	if _, err := runner.Start(t.Context(), position.ID, StartOptions{CloudAPIBase: cloud.URL, Token: "token-1", PageReadyDelay: 1}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-worker.extractStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("等待 Worker 提取开始超时")
	}
	status, err := runner.Status(position.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status["running"] != true {
		t.Fatalf("running status = %+v", status)
	}
	stopDone := make(chan error, 1)
	go func() {
		_, err := runner.Stop(position.ID)
		stopDone <- err
	}()
	stopMarked := false
	deadline := time.After(2 * time.Second)
	for !stopMarked {
		status, err = runner.Status(position.ID)
		if err != nil {
			t.Fatal(err)
		}
		if status["running"] == false {
			stopMarked = true
			break
		}
		select {
		case <-deadline:
			t.Fatalf("等待停止标记超时：%+v", status)
		case <-time.After(20 * time.Millisecond):
		}
	}
	select {
	case err := <-stopDone:
		t.Fatalf("停止岗位运行不应该在当前 Worker 完成前返回：%v", err)
	case <-time.After(100 * time.Millisecond):
	}
	select {
	case <-worker.released:
		t.Fatal("停止岗位运行不应该立刻取消当前 Worker 调用")
	case <-time.After(100 * time.Millisecond):
	}
	close(worker.allowFinish)
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("当前步骤结束后停止接口未返回")
	}
	select {
	case <-worker.released:
	case <-time.After(2 * time.Second):
		t.Fatal("当前步骤结束后 Worker 未释放")
	}
	waitForPositionStatus(t, db, position.ID, "stopped")
}

// TestRunnerUserStopClosesCandidateDetail 验证用户主动停止后仍会执行详情关闭动作。
func TestRunnerUserStopClosesCandidateDetail(t *testing.T) {
	db := openRunnerTestDB(t)
	position, err := db.CreatePosition(map[string]any{
		"name":        "停止详情岗位运行",
		"platform_id": "boss",
		"mode":        "keyword",
		"position_snapshot": map[string]any{
			"name":          "停止详情岗位运行",
			"common_config": map[string]any{"detail_mode": "dom"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := newTestRunner(t, db, &fakeWorker{})
	_, stopCancel := context.WithCancel(t.Context())
	defer stopCancel()
	if !runner.setRunning(position.ID, stopCancel, StartOptions{}) {
		t.Fatal("创建测试运行状态失败")
	}
	defer runner.clear(position.ID)
	runner.markUserStoppedAndCancel(position.ID)
	runtime := &detailCloseProbeRuntime{fetchErr: errors.New("详情读取已取消")}
	_, _, err = runner.enrichCandidateWithDetail(
		t.Context(),
		position,
		runtime,
		platformExecutor{runner: runner, positionID: position.ID},
		cloudapi.PlatformConfig{},
		map[string]any{"candidate_name": "候选人A", "status": "scanned"},
		nil,
		StartOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.closeCalls != 1 {
		t.Fatalf("用户停止后应关闭详情一次，closeCalls=%d", runtime.closeCalls)
	}
}

// TestRunnerMissingCandidateDetailStopsPosition 验证5秒内找不到详情容器时错误会向上返回并停止岗位运行。
func TestRunnerMissingCandidateDetailStopsPosition(t *testing.T) {
	db := openRunnerTestDB(t)
	position, err := db.CreatePosition(map[string]any{
		"name":        "详情缺失岗位运行",
		"platform_id": "boss",
		"mode":        "keyword",
		"position_snapshot": map[string]any{
			"name":          "详情缺失岗位运行",
			"common_config": map[string]any{"detail_mode": "dom"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := newTestRunner(t, db, &fakeWorker{})
	runtime := &detailCloseProbeRuntime{fetchErr: errors.New("候选人详情没找到：5秒内未找到可见详情容器")}
	_, _, err = runner.enrichCandidateWithDetail(
		t.Context(),
		position,
		runtime,
		platformExecutor{runner: runner, positionID: position.ID},
		cloudapi.PlatformConfig{},
		map[string]any{"candidate_name": "候选人A", "status": "scanned"},
		nil,
		StartOptions{},
	)
	if err == nil || !strings.Contains(err.Error(), "岗位运行已自动停止") {
		t.Fatalf("详情容器缺失应停止岗位运行：%v", err)
	}
	if runtime.closeCalls != 1 {
		t.Fatalf("停止岗位运行前应尝试关闭详情一次，closeCalls=%d", runtime.closeCalls)
	}
}

// TestDetailScrollingWaitsForCurrentActionAfterAnalysis 验证 AI 返回后会等待当前滚轮动作完成再退出。
// t 为测试对象。
func TestDetailScrollingWaitsForCurrentActionAfterAnalysis(t *testing.T) {
	runner := newTestRunner(t, openRunnerTestDB(t), &fakeWorker{})
	runtime := &detailScrollProbeRuntime{started: make(chan struct{}, 1), release: make(chan struct{})}
	stop := runner.startCandidateDetailScrolling(
		t.Context(),
		"",
		runtime,
		platformExecutor{runner: runner},
		cloudapi.PlatformConfig{},
		map[string]any{"candidate_name": "候选人A"},
	)
	select {
	case <-runtime.started:
	case <-time.After(time.Second):
		t.Fatal("详情滚动未启动")
	}
	stopped := make(chan struct{})
	go func() {
		stop()
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("当前滚轮动作尚未完成时不应关闭详情")
	case <-time.After(50 * time.Millisecond):
	}
	close(runtime.release)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("当前滚轮动作完成后滚动协程未及时退出")
	}
	if runtime.calls.Load() != 1 {
		t.Fatalf("取消后仍触发额外滚动，calls=%d", runtime.calls.Load())
	}
}

// TestCandidateDetailScrollDelayStaysWithinOneSecond 验证详情滚动间隔始终位于零到一秒范围内。
// t 为测试对象。
func TestCandidateDetailScrollDelayStaysWithinOneSecond(t *testing.T) {
	for index := 0; index < 1000; index++ {
		delay := candidateDetailScrollDelay()
		if delay < 0 || delay > time.Second {
			t.Fatalf("详情滚动随机等待越界：%s", delay)
		}
	}
}

// TestRunnerBrowserClosedStopsPosition 验证用户关闭浏览器后岗位运行会结束。
func TestRunnerBrowserClosedStopsPosition(t *testing.T) {
	speedUpPageEntryCheck(t)
	var position localdb.Position
	var failNoticeCalled atomic.Bool
	var failNoticeMessage atomic.Value
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/me":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "user": map[string]any{"email": "runner@example.com"}})
		case "/api/subscription/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "subscription": map[string]any{"active": true}})
		case "/api/platforms/config/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"configs": []map[string]any{
					{"config_key": "platform.boss", "config_value": `{"id":"boss","pages":[{"url":"https://www.zhipin.com/web/chat/recommend"}],"position":{"current":{"target_classes":[["current-position"]]}}}`},
				},
			})
		case "/api/config/user-preferences":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "config": map[string]any{}})
		case "/api/fail-notice":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode fail notice: %v", err)
			}
			failNoticeCalled.Store(true)
			failNoticeMessage.Store(strings.TrimSpace(payload["error_message"].(string)))
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			if strings.HasPrefix(r.URL.Path, "/api/positions/") {
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "position": map[string]any{"id": position.ID, "name": "本地岗位", "platform_id": "boss", "common_config": map[string]any{"mode_default": "keyword"}}})
				return
			}
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer cloud.Close()

	db := openRunnerTestDB(t)
	position, err := db.CreatePosition(map[string]any{"name": "浏览器关闭岗位运行", "platform_id": "boss", "mode": "keyword", "position_snapshot": map[string]any{"name": "本地岗位运行"}})
	if err != nil {
		t.Fatal(err)
	}
	worker := &fakeWorker{currentPosition: "本地岗位", extractErr: errors.New("浏览器已关闭，请重新启动浏览器")}
	runner := newTestRunner(t, db, worker)
	if _, err := runner.Start(t.Context(), position.ID, StartOptions{CloudAPIBase: cloud.URL, Token: "token-1", PageReadyDelay: 1}); err != nil {
		t.Fatal(err)
	}
	waitForPositionStatus(t, db, position.ID, "stopped")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !failNoticeCalled.Load() {
		time.Sleep(20 * time.Millisecond)
	}
	if !failNoticeCalled.Load() {
		t.Fatal("浏览器关闭后未发送失败通知")
	}
	if message, _ := failNoticeMessage.Load().(string); !strings.Contains(message, "浏览器已关闭") {
		t.Fatalf("失败通知原因不正确：%s", message)
	}
}

// fakeWorker 模拟浏览器 Worker。
type fakeWorker struct {
	calls               []string
	currentPosition     string
	pageListCalls       int
	pageListEmptyBefore int
	extractErr          error
}

// fakeOCR 模拟 OCR 识别器。
type fakeOCR struct{}

// Recognize 模拟 OCR 图片识别。
// ctx 为请求上下文，imagePath 为图片路径。
func (f fakeOCR) Recognize(ctx context.Context, imagePath string) (ocr.Result, error) {
	return ocr.Result{Text: "OCR 识别文本"}, nil
}

// Start 模拟启动 Worker。
// ctx 为请求上下文。
func (w *fakeWorker) Start(ctx context.Context) (browser.WorkerStatus, error) {
	w.calls = append(w.calls, "start")
	return browser.WorkerStatus{Running: true, BaseURL: "http://127.0.0.1:9101"}, nil
}

// Call 模拟调用 Worker API。
// ctx 为请求上下文，path 为 Worker 路径，payload 为请求体。
func (w *fakeWorker) Call(ctx context.Context, path string, payload any) (map[string]any, error) {
	w.calls = append(w.calls, path)
	if path == "/api/v1/page/list" {
		w.pageListCalls++
		if w.pageListCalls <= w.pageListEmptyBefore {
			return map[string]any{"data": map[string]any{"pages": []any{}}}, nil
		}
		return map[string]any{"data": map[string]any{"pages": []any{map[string]any{
			"page_id":    "0",
			"url":        "https://www.zhipin.com/web/chat/recommend",
			"is_default": true,
		}}}}, nil
	}
	if path == "/api/v1/page/extract-text" {
		position := strings.TrimSpace(w.currentPosition)
		if position == "" {
			position = "本地岗位运行"
		}
		return map[string]any{"data": map[string]any{"text": position, "texts": []any{position}}}, nil
	}
	if path == "/api/v1/page/find-elements" {
		return map[string]any{"data": map[string]any{"items": []any{
			map[string]any{"index": 0, "text": "本地岗位运行", "fields": map[string]any{"position_name": "本地岗位运行"}},
			map[string]any{"index": 1, "text": "本地岗位运行2", "fields": map[string]any{"position_name": "本地岗位运行2"}},
		}}}, nil
	}
	if path == "/api/v1/page/list-click-by-index" {
		index := intFromMap(mapValue(payload), "index")
		if index == 1 {
			w.currentPosition = "本地岗位运行2"
		} else {
			w.currentPosition = "本地岗位运行"
		}
		return map[string]any{"data": map[string]any{"clicked": true}}, nil
	}
	if path == "/api/v1/boss/candidates/extract" {
		if w.extractErr != nil {
			return nil, w.extractErr
		}
		return map[string]any{
			"data": map[string]any{
				"candidates": []any{
					map[string]any{
						"id":             "boss_1",
						"candidate_name": "候选人A",
						"name":           "候选人A",
						"status":         "scanned",
						"raw_text":       "候选人A 28岁 本科 5年",
						"fields": map[string]any{
							"name":       "候选人A",
							"basic_info": "28岁 本科 5年",
						},
					},
				},
			},
		}, nil
	}
	if path == "/api/v1/boss/candidates/greet" {
		return map[string]any{"data": map[string]any{"greeted": true}}, nil
	}
	if path == "/api/v1/boss/candidates/detail" {
		return map[string]any{"data": map[string]any{"detail_text": "本科 5年 销售管理经验"}}, nil
	}
	return map[string]any{"data": map[string]any{}}, nil
}

// blockingWorker 模拟会阻塞到 ctx 取消的 Worker。
type blockingWorker struct {
	extractStarted chan struct{}
	allowFinish    chan struct{}
	released       chan struct{}
}

// detailCloseProbeRuntime 用于测试详情关闭动作是否被调用。
type detailCloseProbeRuntime struct {
	fetchErr   error
	closeCalls int
}

// detailScrollProbeRuntime 模拟可在 AI 分析期间持续滚动且响应取消的平台。
type detailScrollProbeRuntime struct {
	detailCloseProbeRuntime
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

// ScrollCandidateDetail 模拟一次持续到上下文取消的详情滚动。
// ctx 为滚动上下文，exec 为执行器，cfg 为平台配置，candidate 为候选人，distance 为滚动距离。
func (r *detailScrollProbeRuntime) ScrollCandidateDetail(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate, distance int) error {
	r.calls.Add(1)
	select {
	case r.started <- struct{}{}:
	default:
	}
	select {
	case <-r.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// OpenEntryPage 模拟打开入口页。
func (r *detailCloseProbeRuntime) OpenEntryPage(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, entryURL string) error {
	return nil
}

// PrepareEntryPage 模拟入口页准备动作。
func (r *detailCloseProbeRuntime) PrepareEntryPage(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig) error {
	return nil
}

// IsPositionEntryPage 模拟入口页检测。
func (r *detailCloseProbeRuntime) IsPositionEntryPage(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig) (bool, error) {
	return true, nil
}

// CurrentPositionName 模拟读取当前岗位。
func (r *detailCloseProbeRuntime) CurrentPositionName(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig) (string, error) {
	return "停止详情岗位运行", nil
}

// SelectPosition 模拟切换岗位。
func (r *detailCloseProbeRuntime) SelectPosition(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, positionName string) error {
	return nil
}

// ListVisibleCandidates 模拟读取候选人。
func (r *detailCloseProbeRuntime) ListVisibleCandidates(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, maxItems int) ([]platformcore.Candidate, error) {
	return nil, nil
}

// ScrollCandidateList 模拟滚动候选人列表。
func (r *detailCloseProbeRuntime) ScrollCandidateList(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, distance int) error {
	return nil
}

// FetchCandidateDetail 模拟读取详情失败。
func (r *detailCloseProbeRuntime) FetchCandidateDetail(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate, request platformcore.DetailRequest) (platformcore.DetailResult, error) {
	return platformcore.DetailResult{}, r.fetchErr
}

// CloseCandidateDetail 记录详情关闭调用次数。
func (r *detailCloseProbeRuntime) CloseCandidateDetail(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate) error {
	r.closeCalls++
	return nil
}

// GreetCandidate 模拟打招呼。
func (r *detailCloseProbeRuntime) GreetCandidate(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate) error {
	return nil
}

// CandidateFilterText 返回候选人筛选文本。
func (r *detailCloseProbeRuntime) CandidateFilterText(candidate platformcore.Candidate) string {
	return stringFromMap(candidate, "candidate_name")
}

// CandidateFingerprint 返回候选人去重标识。
func (r *detailCloseProbeRuntime) CandidateFingerprint(candidate platformcore.Candidate) string {
	return stringFromMap(candidate, "candidate_name")
}

// CleanCandidateDetailText 模拟平台详情文本清理。
// text 为原始详情文本。
func (r *detailCloseProbeRuntime) CleanCandidateDetailText(text string) string {
	return strings.TrimSpace(text)
}

// Start 模拟启动阻塞 Worker。
// ctx 为请求上下文。
func (w *blockingWorker) Start(ctx context.Context) (browser.WorkerStatus, error) {
	return browser.WorkerStatus{Running: true}, nil
}

// Call 模拟 Worker API，并在候选人提取时等待当前步骤完成。
// ctx 为请求上下文，path 为 Worker 路径，payload 为请求体。
func (w *blockingWorker) Call(ctx context.Context, path string, payload any) (map[string]any, error) {
	if path == "/api/v1/page/list" {
		return map[string]any{"data": map[string]any{"pages": []any{map[string]any{
			"page_id":    "0",
			"url":        "https://www.zhipin.com/web/chat/recommend",
			"is_default": true,
		}}}}, nil
	}
	if path == "/api/v1/page/extract-text" {
		return map[string]any{"data": map[string]any{"text": "可停止岗位运行", "texts": []any{"可停止岗位运行"}}}, nil
	}
	if path == "/api/v1/boss/candidates/extract" {
		close(w.extractStarted)
		select {
		case <-w.allowFinish:
			close(w.released)
			return map[string]any{"data": map[string]any{"candidates": []any{map[string]any{
				"id":             "boss_stop_1",
				"candidate_name": "停止候选人",
				"name":           "停止候选人",
				"status":         "scanned",
				"raw_text":       "停止候选人 本科 5年",
			}}}}, nil
		case <-ctx.Done():
			close(w.released)
			return nil, ctx.Err()
		}
	}
	return map[string]any{"data": map[string]any{}}, nil
}

// waitForPositionStatus 等待岗位运行进入指定状态。
// t 为测试对象，db 为本地数据库，positionID 为岗位运行 ID，status 为目标状态。
func waitForPositionStatus(t *testing.T, db *localdb.DB, positionID string, status string) localdb.Position {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		position, err := db.GetPosition(positionID)
		if err != nil {
			t.Fatal(err)
		}
		if position.Status == status {
			return position
		}
		time.Sleep(20 * time.Millisecond)
	}
	position, err := db.GetPosition(positionID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("等待岗位运行状态超时，当前状态=%s，目标状态=%s", position.Status, status)
	return position
}

// assertPositionLogContains 断言岗位运行日志包含指定文本。
// t 为测试对象，db 为本地数据库，positionID 为岗位运行 ID，text 为期望文本。
func assertPositionLogContains(t *testing.T, db *localdb.DB, positionID string, text string) {
	t.Helper()
	logs, err := db.ListPositionLogs(positionID, 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range logs {
		if strings.Contains(item.Message, text) {
			return
		}
	}
	t.Fatalf("岗位运行日志未包含 %q，logs=%+v", text, logs)
}

// speedUpPageEntryCheck 加快测试中的页面入口等待。
// t 为测试对象，测试结束后自动恢复默认等待配置。
func speedUpPageEntryCheck(t *testing.T) {
	t.Helper()
	oldAttempts := pageEntryCheckAttempts
	oldDelay := pageEntryCheckDelay
	oldCurrentAttempts := currentPositionCheckAttempts
	oldCurrentDelay := currentPositionCheckDelay
	pageEntryCheckAttempts = 10
	pageEntryCheckDelay = time.Millisecond
	currentPositionCheckAttempts = 10
	currentPositionCheckDelay = time.Millisecond
	t.Cleanup(func() {
		pageEntryCheckAttempts = oldAttempts
		pageEntryCheckDelay = oldDelay
		currentPositionCheckAttempts = oldCurrentAttempts
		currentPositionCheckDelay = oldCurrentDelay
	})
}

// openRunnerTestDB 创建岗位运行运行器测试数据库。
// t 为测试对象。
func openRunnerTestDB(t *testing.T) *localdb.DB {
	t.Helper()
	db, err := localdb.Open(&config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newTestRunner 创建带临时目录的岗位运行运行器。
// t 为测试对象，db 为测试数据库，worker 为模拟 Worker。
func newTestRunner(t *testing.T, db *localdb.DB, worker BrowserWorker) *Runner {
	t.Helper()
	root := t.TempDir()
	return New(db, worker, fakeOCR{}, root+"/profiles", root+"/downloads", root+"/screenshots", root+"/audio", "")
}
