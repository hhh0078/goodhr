// Package taskrunner 负责测试 Go 本地任务运行器。
package taskrunner

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
	"goodhr5/local-agent-go/internal/localdb"
	"goodhr5/local-agent-go/internal/ocr"
	"goodhr5/local-agent-go/internal/platformcore"
	"goodhr5/local-agent-go/internal/platforms"
)

// TestRunnerStartStop 验证任务启动会校验会员、读取平台配置、扫描候选人并更新状态。
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
	var task localdb.Task
	savedCandidates := []map[string]any{}
	var processedResumeCount int64
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
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
			if strings.HasPrefix(r.URL.Path, "/api/tasks/") && strings.HasSuffix(r.URL.Path, "/candidates") {
				var candidate map[string]any
				if err := json.NewDecoder(r.Body).Decode(&candidate); err != nil {
					t.Fatalf("decode candidate: %v", err)
				}
				savedCandidates = append(savedCandidates, candidate)
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
				return
			}
			if strings.HasPrefix(r.URL.Path, "/api/tasks/") && strings.HasSuffix(r.URL.Path, "/processed-resumes") {
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
			if strings.HasPrefix(r.URL.Path, "/api/tasks/") {
				requestedID := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
				taskName := "本地任务"
				if requestedID != task.ID {
					taskName = "本地任务2"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "task": map[string]any{"id": requestedID, "name": taskName, "platform_id": "boss", "mode": "ai", "match_limit": 1, "enable_sound": requestedID != task.ID, "position": map[string]any{"name": taskName}}})
				return
			}
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer cloud.Close()

	db := openRunnerTestDB(t)
	task, err := db.CreateTask(map[string]any{"name": "本地任务", "platform_id": "boss", "position_snapshot": map[string]any{"name": "本地任务"}})
	if err != nil {
		t.Fatal(err)
	}
	worker := &fakeWorker{}
	runner := newTestRunner(t, db, worker)
	result, err := runner.Start(t.Context(), task.ID, StartOptions{CloudAPIBase: cloud.URL, Token: "token-1", PageReadyDelay: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result["running"] != true {
		t.Fatalf("result = %+v", result)
	}
	updated := waitForTaskStatus(t, db, task.ID, "completed")
	if updated.ScannedCount != 1 {
		t.Fatalf("scanned count = %d", updated.ScannedCount)
	}
	status, err := runner.Status(task.ID)
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
	stopResult, err := runner.Stop(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopResult["running"] != false || runner.IsRunning(task.ID) {
		t.Fatalf("stopResult = %+v", stopResult)
	}
	stopped, err := db.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Status != "stopped" {
		t.Fatalf("stopped status = %s", stopped.Status)
	}
	for _, call := range worker.calls {
		if call == "/api/v1/browser/stop" {
			t.Fatal("停止任务不应该关闭浏览器")
		}
	}

	task2, err := db.CreateTask(map[string]any{"name": "本地任务2", "platform_id": "boss", "match_limit": 1, "position_snapshot": map[string]any{"name": "本地任务2"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Start(t.Context(), task2.ID, StartOptions{CloudAPIBase: cloud.URL, Token: "token-1", EnableGreet: true, PageReadyDelay: 1}); err != nil {
		t.Fatal(err)
	}
	waitForTaskStatus(t, db, task2.ID, "completed")
	if len(savedCandidates) < 2 || savedCandidates[len(savedCandidates)-1]["status"] != "greeted" {
		t.Fatalf("savedCandidates after task2 = %+v", savedCandidates)
	}
	assertTaskLogContains(t, db, task2.ID, "音频文件不存在或为空")
}

// TestRunnerStatusPendingWhenTaskMissing 验证未启动的云端任务查询本地状态时不会报错。
func TestRunnerStatusPendingWhenTaskMissing(t *testing.T) {
	db := openRunnerTestDB(t)
	runner := newTestRunner(t, db, &fakeWorker{})

	status, err := runner.Status("cloud-task-1")
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
	if progress.Stage != "pending" || progress.Message != "本地任务尚未启动" {
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
	runner := &Runner{running: map[string]*runState{"task-rest": &runState{progress: Progress{Stage: "running"}}}}
	options := StartOptions{
		RestAfterCandidatesMin: 1,
		RestAfterCandidatesMax: 1,
		RestTimesMin:           1,
		RestTimesMax:           1,
		RestDurationMin:        1,
		RestDurationMax:        1,
	}
	runner.initRestState("task-rest", options)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := runner.maybeRestAfterCandidate(ctx, "task-rest", options); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if runner.running["task-rest"].restUsed != 1 {
		t.Fatalf("restUsed = %d", runner.running["task-rest"].restUsed)
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
	task, err := db.CreateTask(map[string]any{"name": "本地任务", "platform_id": "boss", "position_snapshot": map[string]any{"name": "本地任务"}})
	if err != nil {
		t.Fatal(err)
	}
	runner := newTestRunner(t, db, &fakeWorker{})
	if _, err := runner.Start(t.Context(), task.ID, StartOptions{CloudAPIBase: "https://goodhr5.58it.cn"}); err == nil || err.Error() != "请先登录后再校验会员" {
		t.Fatalf("err = %v", err)
	}
	updated, err := db.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status == "running" {
		t.Fatalf("空 token 不应启动任务，当前状态=%s", updated.Status)
	}
}

// TestBuildTaskRuntimeSnapshotAllowsFreeKeywordTask 验证会员过期时仍允许非 AI 任务启动。
func TestBuildTaskRuntimeSnapshotAllowsFreeKeywordTask(t *testing.T) {
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
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
	task := localdb.Task{
		ID:               "task-free-keyword",
		PlatformID:       "boss",
		Mode:             "keyword",
		PositionSnapshot: map[string]any{"common_config": map[string]any{"detail_mode": "ocr"}},
	}
	snapshot, err := runner.buildTaskRuntimeSnapshot(t.Context(), cloudapi.New(cloud.URL), task, StartOptions{Token: "token-1"}, 1)
	if err != nil {
		t.Fatalf("keyword task should be allowed when subscription expired: %v", err)
	}
	if snapshot.Task.ID != task.ID || len(snapshot.PlatformConfig) == 0 {
		t.Fatalf("unexpected snapshot = %+v", snapshot)
	}
}

// TestBuildTaskRuntimeSnapshotBlocksExpiredAIFeature 验证会员过期时会拦截 AI 功能任务。
func TestBuildTaskRuntimeSnapshotBlocksExpiredAIFeature(t *testing.T) {
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/subscription/status" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "subscription": map[string]any{"active": false}})
	}))
	defer cloud.Close()

	runner := newTestRunner(t, openRunnerTestDB(t), &fakeWorker{})
	task := localdb.Task{
		ID:               "task-ai",
		PlatformID:       "boss",
		Mode:             "ai",
		PositionSnapshot: map[string]any{"common_config": map[string]any{"detail_mode": "ocr"}},
	}
	_, err := runner.buildTaskRuntimeSnapshot(t.Context(), cloudapi.New(cloud.URL), task, StartOptions{Token: "token-1"}, 1)
	if err == nil || !strings.Contains(err.Error(), "当前任务使用了 AI 筛选或 AI 详情识别") {
		t.Fatalf("err = %v", err)
	}
}

// TestValidateAIConfig 验证 AI 配置会在任务启动阶段提前校验。
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
	task := localdb.Task{ID: "task-1", PlatformID: "boss"}
	worker := &fakeWorker{}
	runner := newTestRunner(t, db, worker)
	if _, err := runner.scanOnce(t.Context(), task, cloudapi.PlatformConfig{"auth": map[string]any{"pages": []any{}}}, StartOptions{}); err == nil || err.Error() != "云端平台配置缺少入口页面地址" {
		t.Fatalf("err = %v", err)
	}
	if len(worker.calls) != 0 {
		t.Fatalf("缺少入口页时不应启动浏览器，calls=%v", worker.calls)
	}
}

// TestEnsureTaskPageReadyRetries 验证页面刚打开时会等待多次检查。
func TestEnsureTaskPageReadyRetries(t *testing.T) {
	speedUpPageEntryCheck(t)

	db := openRunnerTestDB(t)
	task := localdb.Task{ID: "task-1", PlatformID: "boss", PositionSnapshot: map[string]any{"name": "本地任务"}}
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
	exec := platformExecutor{runner: runner, taskID: task.ID}
	if err := runner.ensureTaskPageReady(t.Context(), task, platformRuntime, exec, platformConfig); err != nil {
		t.Fatal(err)
	}
	if worker.pageListCalls != 6 {
		t.Fatalf("页面检查次数 = %d", worker.pageListCalls)
	}
}

// TestApplyKeywordFilter 验证关键词和排除词过滤。
func TestApplyKeywordFilter(t *testing.T) {
	task := localdb.Task{
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
	filtered, skipped := applyKeywordFilter(task, candidates, func(message string) {
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
	task := localdb.Task{
		Mode: "keyword",
		PositionSnapshot: map[string]any{
			"keywords":      []any{"本科"},
			"common_config": map[string]any{"detail_mode": "ocr"},
		},
	}
	candidates := []map[string]any{{"id": "1", "raw_text": "候选人基础信息较少"}}
	filtered, skipped := prepareCandidatesForFirstStage(task, candidates)
	if skipped != 0 || len(filtered) != 1 || filtered[0]["status"] != "passed" {
		t.Fatalf("filtered = %+v, skipped = %d", filtered, skipped)
	}
}

// TestApplyKeywordGreetDecision 验证详情文本出来后再做关键词最终判断。
func TestApplyKeywordGreetDecision(t *testing.T) {
	task := localdb.Task{
		Mode: "keyword",
		PositionSnapshot: map[string]any{
			"keywords":         []any{"本科", "销售"},
			"exclude_keywords": []any{"外包"},
			"is_and_mode":      true,
		},
	}
	logs := []string{}
	passed := map[string]any{"name": "张三", "detail_text": "本科，五年销售经验"}
	if skipped := applyKeywordGreetDecisionWithLog(task, passed, func(message string) {
		logs = append(logs, message)
	}); skipped != 0 || passed["status"] != "passed" {
		t.Fatalf("passed = %+v, skipped = %d", passed, skipped)
	}
	if joinedLogs := strings.Join(logs, "\n"); !strings.Contains(joinedLogs, "详情关键词通过：name=张三") || !strings.Contains(joinedLogs, "命中=本科、销售") {
		t.Fatalf("logs = %s", joinedLogs)
	}
	rejected := map[string]any{"detail_text": "本科，外包项目经验"}
	if skipped := applyKeywordGreetDecision(task, rejected); skipped != 1 || rejected["status"] != "skipped" {
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

// TestRunOptionBounds 验证任务运行参数默认值和上限。
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

// TestRunnerStopWaitsForCurrentStep 验证停止任务不会取消当前 Worker 调用。
func TestRunnerStopWaitsForCurrentStep(t *testing.T) {
	speedUpPageEntryCheck(t)
	var task localdb.Task
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
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
			if strings.HasPrefix(r.URL.Path, "/api/tasks/") {
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "task": map[string]any{"id": task.ID, "name": "可停止任务", "platform_id": "boss", "mode": "keyword", "position": map[string]any{"name": "可停止任务"}}})
				return
			}
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer cloud.Close()

	db := openRunnerTestDB(t)
	task, err := db.CreateTask(map[string]any{"name": "可停止任务", "platform_id": "boss", "mode": "keyword", "position_snapshot": map[string]any{"name": "可停止任务"}})
	if err != nil {
		t.Fatal(err)
	}
	worker := &blockingWorker{extractStarted: make(chan struct{}), allowFinish: make(chan struct{}), released: make(chan struct{})}
	runner := newTestRunner(t, db, worker)
	if _, err := runner.Start(t.Context(), task.ID, StartOptions{CloudAPIBase: cloud.URL, Token: "token-1", PageReadyDelay: 1}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-worker.extractStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("等待 Worker 提取开始超时")
	}
	status, err := runner.Status(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status["running"] != true {
		t.Fatalf("running status = %+v", status)
	}
	if _, err := runner.Stop(task.ID); err != nil {
		t.Fatal(err)
	}
	status, err = runner.Status(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status["running"] != false {
		t.Fatalf("stopping status = %+v", status)
	}
	select {
	case <-worker.released:
		t.Fatal("停止任务不应该立刻取消当前 Worker 调用")
	case <-time.After(100 * time.Millisecond):
	}
	close(worker.allowFinish)
	select {
	case <-worker.released:
	case <-time.After(2 * time.Second):
		t.Fatal("当前步骤结束后 Worker 未释放")
	}
	waitForTaskStatus(t, db, task.ID, "stopped")
}

// TestRunnerUserStopSkipsDetailClose 验证用户主动停止后不再执行详情关闭动作。
func TestRunnerUserStopSkipsDetailClose(t *testing.T) {
	db := openRunnerTestDB(t)
	task, err := db.CreateTask(map[string]any{
		"name":        "停止详情任务",
		"platform_id": "boss",
		"mode":        "keyword",
		"position_snapshot": map[string]any{
			"name":          "停止详情任务",
			"common_config": map[string]any{"detail_mode": "dom"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := newTestRunner(t, db, &fakeWorker{})
	runner.markUserStoppedAndCancel(task.ID)
	runtime := &detailCloseProbeRuntime{fetchErr: errors.New("详情读取已取消")}
	_, err = runner.enrichCandidateWithDetail(
		t.Context(),
		task,
		runtime,
		platformExecutor{runner: runner, taskID: task.ID},
		cloudapi.PlatformConfig{},
		map[string]any{"candidate_name": "候选人A", "status": "scanned"},
		nil,
		StartOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.closeCalls != 0 {
		t.Fatalf("用户停止后不应关闭详情，closeCalls=%d", runtime.closeCalls)
	}
}

// TestRunnerBrowserClosedStopsTask 验证用户关闭浏览器后任务会结束。
func TestRunnerBrowserClosedStopsTask(t *testing.T) {
	speedUpPageEntryCheck(t)
	var task localdb.Task
	var failNoticeCalled atomic.Bool
	var failNoticeMessage atomic.Value
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
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
			if strings.HasPrefix(r.URL.Path, "/api/tasks/") {
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "task": map[string]any{"id": task.ID, "name": "浏览器关闭任务", "platform_id": "boss", "mode": "keyword", "position": map[string]any{"name": "本地任务"}}})
				return
			}
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer cloud.Close()

	db := openRunnerTestDB(t)
	task, err := db.CreateTask(map[string]any{"name": "浏览器关闭任务", "platform_id": "boss", "mode": "keyword", "position_snapshot": map[string]any{"name": "本地任务"}})
	if err != nil {
		t.Fatal(err)
	}
	worker := &fakeWorker{extractErr: errors.New("浏览器已关闭，请重新启动浏览器")}
	runner := newTestRunner(t, db, worker)
	if _, err := runner.Start(t.Context(), task.ID, StartOptions{CloudAPIBase: cloud.URL, Token: "token-1", PageReadyDelay: 1}); err != nil {
		t.Fatal(err)
	}
	waitForTaskStatus(t, db, task.ID, "stopped")
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
			position = "本地任务"
		}
		return map[string]any{"data": map[string]any{"text": position, "texts": []any{position}}}, nil
	}
	if path == "/api/v1/page/find-elements" {
		return map[string]any{"data": map[string]any{"items": []any{
			map[string]any{"index": 0, "text": "本地任务", "fields": map[string]any{"position_name": "本地任务"}},
			map[string]any{"index": 1, "text": "本地任务2", "fields": map[string]any{"position_name": "本地任务2"}},
		}}}, nil
	}
	if path == "/api/v1/page/list-click-by-index" {
		index := intFromMap(mapValue(payload), "index")
		if index == 1 {
			w.currentPosition = "本地任务2"
		} else {
			w.currentPosition = "本地任务"
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
						"filter_text":    "候选人A 28岁 本科 5年",
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

// OpenEntryPage 模拟打开入口页。
func (r *detailCloseProbeRuntime) OpenEntryPage(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, entryURL string) error {
	return nil
}

// PrepareEntryPage 模拟入口页准备动作。
func (r *detailCloseProbeRuntime) PrepareEntryPage(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig) error {
	return nil
}

// IsTaskEntryPage 模拟入口页检测。
func (r *detailCloseProbeRuntime) IsTaskEntryPage(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig) (bool, error) {
	return true, nil
}

// CurrentPositionName 模拟读取当前岗位。
func (r *detailCloseProbeRuntime) CurrentPositionName(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig) (string, error) {
	return "停止详情任务", nil
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
		return map[string]any{"data": map[string]any{"text": "可停止任务", "texts": []any{"可停止任务"}}}, nil
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
				"filter_text":    "停止候选人 本科 5年",
			}}}}, nil
		case <-ctx.Done():
			close(w.released)
			return nil, ctx.Err()
		}
	}
	return map[string]any{"data": map[string]any{}}, nil
}

// waitForTaskStatus 等待任务进入指定状态。
// t 为测试对象，db 为本地数据库，taskID 为任务 ID，status 为目标状态。
func waitForTaskStatus(t *testing.T, db *localdb.DB, taskID string, status string) localdb.Task {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := db.GetTask(taskID)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status == status {
			return task
		}
		time.Sleep(20 * time.Millisecond)
	}
	task, err := db.GetTask(taskID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("等待任务状态超时，当前状态=%s，目标状态=%s", task.Status, status)
	return task
}

// assertTaskLogContains 断言任务日志包含指定文本。
// t 为测试对象，db 为本地数据库，taskID 为任务 ID，text 为期望文本。
func assertTaskLogContains(t *testing.T, db *localdb.DB, taskID string, text string) {
	t.Helper()
	logs, err := db.ListTaskLogs(taskID, 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range logs {
		if strings.Contains(item.Message, text) {
			return
		}
	}
	t.Fatalf("任务日志未包含 %q，logs=%+v", text, logs)
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

// openRunnerTestDB 创建任务运行器测试数据库。
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

// newTestRunner 创建带临时目录的任务运行器。
// t 为测试对象，db 为测试数据库，worker 为模拟 Worker。
func newTestRunner(t *testing.T, db *localdb.DB, worker BrowserWorker) *Runner {
	t.Helper()
	root := t.TempDir()
	return New(db, worker, fakeOCR{}, root+"/profiles", root+"/downloads", root+"/screenshots", root+"/audio", "")
}
