// Package lifecycle 文件作用：验证任务异常恢复和休眠时间断层的统一生命周期行为。
package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/greeting"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/profile"
	"goodhr5/local-agent-go-new/internal/storage"
	"goodhr5/local-agent-go-new/internal/system/failurediag"
	"goodhr5/local-agent-go-new/internal/system/power"
)

// TestInterruptAfterSleep 验证休眠时间断层会取消任务并保留失败原因。
func TestInterruptAfterSleep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	active := &activeTask{
		state:  storage.TaskRun{TaskID: "task-sleep"},
		cancel: cancel,
		done:   make(chan struct{}),
	}
	runner := &Runner{active: map[string]*activeTask{"task-sleep": active}}
	runner.interruptAfterSleep(active, 3*time.Minute)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("休眠时间断层没有取消任务")
	}
	if active.interrupt == nil || !strings.Contains(active.interrupt.Error(), "心跳中断=3m0s") {
		t.Fatalf("休眠失败原因不完整：%v", active.interrupt)
	}
}

// TestStopTaskWaitsWithoutCancelingCurrentWork 验证用户停止只发送安全信号，不取消当前候选人上下文。
func TestStopTaskWaitsWithoutCancelingCurrentWork(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatalf("打开测试数据库失败：%v", err)
	}
	defer store.Close()
	runCtx, cancel := context.WithCancel(context.Background())
	task := storage.TaskRun{
		TaskID: "task-stop", PositionID: "position-stop", Status: "running",
		StartedAt: time.Now().UTC(),
	}
	if err = store.SaveTask(context.Background(), task); err != nil {
		t.Fatalf("保存测试任务失败：%v", err)
	}
	active := &activeTask{
		state: task, cancel: cancel, done: make(chan struct{}), stop: make(chan struct{}),
	}
	runner := &Runner{
		active: map[string]*activeTask{"task-stop": active}, store: store,
	}
	result := make(chan error, 1)
	go func() {
		_, stopErr := runner.StopTask(context.Background(), "task-stop")
		result <- stopErr
	}()
	select {
	case <-active.stop:
	case <-time.After(time.Second):
		t.Fatal("安全停止信号没有发出")
	}
	select {
	case <-runCtx.Done():
		t.Fatal("用户停止不应取消当前候选人上下文")
	default:
	}
	if active.stopOrigin != "岗位停止请求" {
		t.Fatalf("停止发起来源缺失 %q", active.stopOrigin)
	}
	close(active.done)
	if err = <-result; err != nil {
		t.Fatalf("StopTask() error = %v", err)
	}
	cancel()
}

// TestPanicFailureUsesNormalFinish 验证 panic 转换后的错误会保存为统一失败终态并释放任务。
func TestPanicFailureUsesNormalFinish(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatalf("打开测试数据库失败：%v", err)
	}
	defer store.Close()
	profiles := profile.New(filepath.Join(t.TempDir(), "profiles"))
	if err = profiles.Acquire("profile-1", "task-panic"); err != nil {
		t.Fatalf("占用测试 Profile 失败：%v", err)
	}
	active := &activeTask{
		prepared: shared.PreparedTask{Request: shared.StartRequest{
			TaskID: "task-panic", PositionID: "position-1", ProfileID: "profile-1",
		}},
		state: storage.TaskRun{
			TaskID: "task-panic", PositionID: "position-1", PlatformID: "boss",
			TaskType: "greeting", Status: "running", StartedAt: time.Now().UTC(),
		},
		cancel: func() {},
		done:   make(chan struct{}),
	}
	if err = store.SaveTask(context.Background(), active.state); err != nil {
		t.Fatalf("保存测试任务失败：%v", err)
	}
	runner := &Runner{
		active: map[string]*activeTask{"task-panic": active}, store: store,
		profiles: profiles, power: &power.Guard{},
	}
	runner.finish(active, shared.Stats{}, panicTaskError("boom"))
	task, err := store.Task(context.Background(), "task-panic")
	if err != nil {
		t.Fatalf("读取 panic 收尾任务失败：%v", err)
	}
	if task.Status != "failed" || !strings.Contains(task.ErrorMessage, "boom") || runner.HasActive() {
		t.Fatalf("panic 没有正常收尾：%+v", task)
	}
}

// TestNotifyFinishedSyncsPositionCounts 验证任务结束时同步岗位累计统计和本次打招呼数。
func TestNotifyFinishedSyncsPositionCounts(t *testing.T) {
	var counts struct {
		Scanned int `json:"scanned_count"`
		Skipped int `json:"skipped_count"`
		Failed  int `json:"failed_count"`
	}
	paths := make([]string, 0, 2)
	var decodeErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		paths = append(paths, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/counts") {
			decodeErr = json.NewDecoder(r.Body).Decode(&counts)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"notice_sent":true}`))
	}))
	defer server.Close()
	runner := &Runner{cloud: cloud.New(server.URL)}
	prepared := shared.PreparedTask{
		Request: shared.StartRequest{TaskID: "task-counts", TaskType: "greeting", Token: "token"},
		Position: cloud.PositionSnapshot{
			ID: "position-1", ScannedCount: 100,
			SkippedCount: 70, FailedCount: 10,
		},
	}
	runner.notifyFinished(prepared, storage.TaskRun{Status: "completed"}, shared.Stats{
		Processed: 5, Succeeded: 2, Skipped: 2, Failed: 1,
	}, nil)
	if decodeErr != nil {
		t.Fatalf("解析累计统计失败：%v", decodeErr)
	}
	if counts.Scanned != 105 || counts.Skipped != 72 || counts.Failed != 11 {
		t.Fatalf("累计统计不正确：%+v", counts)
	}
	if len(paths) != 2 || !strings.HasSuffix(paths[0], "/counts") || !strings.HasSuffix(paths[1], "/status") {
		t.Fatalf("完成收尾没有按统计、状态顺序同步：%v", paths)
	}
}

// TestNotifyFailedSyncsCountsBeforeFailNotice 验证失败任务先同步累计统计，再由失败通知更新云端状态。
func TestNotifyFailedSyncsCountsBeforeFailNotice(t *testing.T) {
	paths := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()
	runner := &Runner{cloud: cloud.New(server.URL)}
	prepared := shared.PreparedTask{
		Request:  shared.StartRequest{TaskID: "task-failed", TaskType: "greeting", Token: "token"},
		Position: cloud.PositionSnapshot{ID: "position-1"},
	}
	runner.notifyFinished(
		prepared,
		storage.TaskRun{Status: "failed", ErrorMessage: "测试失败"},
		shared.Stats{Processed: 1, Failed: 1},
		context.DeadlineExceeded,
	)
	if len(paths) != 2 ||
		!strings.HasSuffix(paths[0], "/counts") ||
		!strings.HasSuffix(paths[1], "/api/fail-notice") {
		t.Fatalf("失败收尾调用顺序不正确：%v", paths)
	}
}

// TestStopOutcomeDistinguishesOrigins 验证收尾只根据已确认的发起方记录停机原因，并区分两种登录响应。
func TestStopOutcomeDistinguishesOrigins(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// 复现候选人计时器到期，AI 在等待首段 SSE 时收到父级取消，然后岗位按原策略收尾。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, ": heartbeat\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	candidate, stopCandidate := context.WithTimeoutCause(context.Background(), 250*time.Millisecond, greeting.ErrCandidateProcessingTimeout)
	defer stopCandidate()
	scoring, stopScoring, _ := ai.ScoringContext(candidate, time.Minute)
	defer stopScoring()
	_, aiErr := ai.New().EvaluateCandidatePreviewEarly(scoring, cloud.AIConfig{BaseURL: server.URL, APIKey: "fixture", Model: "fixture"}, cloud.PositionSnapshot{}, model.Candidate{})
	overlap := fmt.Errorf("%w：%w", context.Cause(candidate), aiErr)
	if !errors.Is(overlap, greeting.ErrCandidateProcessingTimeout) || !errors.Is(overlap, context.Canceled) || !shared.ShouldStopTaskImmediately(overlap) {
		t.Fatalf("没有重现候选人时限触发 AI 取消的原停止策略 %v", overlap)
	}
	for _, tc := range []struct {
		origin string
		err    error
		reason string
	}{
		{"岗位停止请求", nil, "已收到主动停止请求"},
		{"本地程序退出", context.Canceled, "本地程序退出时停止任务"},
		{"", overlap, "候选人总时限已到"},
		{"岗位停止请求", overlap, "已收到主动停止请求"},
		{"本地程序退出", overlap, "本地程序退出时停止任务"},
		{"", context.Canceled, "收到停止信号，未确认发起来源"},
		{"", &cloud.APIError{StatusCode: http.StatusUnauthorized}, "云端拒绝登录凭证"},
		{"", &cloud.APIError{StatusCode: http.StatusForbidden}, "云端拒绝当前访问权限"},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			collector := &runtimeLogCollector{}
			active := &activeTask{
				cancel: func() {}, done: make(chan struct{}), stopped: tc.origin != "", stopOrigin: tc.origin,
				state: storage.TaskRun{TaskID: "task-stop-test", PositionID: "position-stop-test", TaskType: "greeting", Status: "running", StartedAt: time.Now().UTC()},
			}
			if err := store.SaveTask(context.Background(), active.state); err != nil {
				t.Fatal(err)
			}
			runner := &Runner{active: map[string]*activeTask{active.state.TaskID: active}, store: store, logger: collector, profiles: profile.New(t.TempDir()), power: &power.Guard{}}
			runner.finish(active, shared.Stats{}, tc.err)
			if active.state.Status != "stopped" {
				t.Fatalf("终态变更 %q", active.state.Status)
			}
			for i, event := range collector.events {
				if event.step != "finished" {
					continue
				}
				details := failurediag.Details(collector.errors[i])
				if len(details) == 0 || details[0].Reason != tc.reason || (tc.reason == "候选人总时限已到" && active.state.Summary != "候选人总时限已到，岗位已停止") {
					t.Fatalf("终态来源不符 %+v", details)
				}
				return
			}
			t.Fatal("终态没有写入岗位日志")
		})
	}
}
