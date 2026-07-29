// Package lifecycle 文件作用：验证任务异常恢复和休眠时间断层的统一生命周期行为。
package lifecycle

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/profile"
	"goodhr5/local-agent-go-new/internal/storage"
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

// TestNotifyFinishedSyncsCumulativeCounts 验证每次任务统计会叠加岗位已有累计值后再同步。
func TestNotifyFinishedSyncsCumulativeCounts(t *testing.T) {
	var counts struct {
		Scanned int `json:"scanned_count"`
		Greeted int `json:"greeted_count"`
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
			ID: "position-1", ScannedCount: 100, GreetedCount: 20,
			SkippedCount: 70, FailedCount: 10,
		},
	}
	runner.notifyFinished(prepared, storage.TaskRun{Status: "completed"}, shared.Stats{
		Processed: 5, Succeeded: 2, Skipped: 2, Failed: 1,
	}, nil)
	if decodeErr != nil {
		t.Fatalf("解析累计统计失败：%v", decodeErr)
	}
	if counts.Scanned != 105 || counts.Greeted != 22 || counts.Skipped != 72 || counts.Failed != 11 {
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
