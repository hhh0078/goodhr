// Package diagnostics 文件作用：验证无岗位浏览器退出也能先持久化并在网络恢复后补传。
package diagnostics

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"goodhr5/local-agent-go-new/internal/storage"
)

// fakeBrowserUploader 模拟云端暂时离线和恢复。
type fakeBrowserUploader struct{ failing atomic.Bool }

// UploadBrowserEvents 验证上传调用中包含待补传的退出事件。
func (u *fakeBrowserUploader) UploadBrowserEvents(_ context.Context, _ string, events []storage.BrowserEvent) error {
	if len(events) != 1 {
		return errors.New("事件数量不正确")
	}
	if u.failing.Load() {
		return errors.New("云端暂时不可用")
	}
	return nil
}

// TestBrowserIncidentPersistsWithoutTaskAndRetries 验证浏览器退出即使无任务也保存，失败后仍可补传。
func TestBrowserIncidentPersistsWithoutTaskAndRetries(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	uploader := &fakeBrowserUploader{}
	uploader.failing.Store(true)
	recorder := NewBrowserIncidentRecorder(store, uploader)
	recorder.WorkerLine(Normalize([]byte(`{"action":"browser.exit","category":"异常退出","is_error":true,"error_message":"连接断开"}`)))
	ctx := context.Background()
	pending, err := store.PendingBrowserEvents(ctx, "")
	if err != nil || len(pending) != 1 || pending[0].PositionID != "" {
		t.Fatalf("无岗位日志未保存：%v %+v", err, pending)
	}
	recorder.Bind("user-1", "valid-token")
	pending, err = store.PendingBrowserEvents(ctx, "user-1")
	if err != nil || len(pending) != 1 {
		t.Fatalf("日志未归属账号：%v %+v", err, pending)
	}
	uploader.failing.Store(false)
	recorder.Flush(ctx)
	pending, err = store.PendingBrowserEvents(ctx, "user-1")
	if err != nil || len(pending) != 0 {
		t.Fatalf("补传成功后队列未清空：%v %+v", err, pending)
	}
}

// TestBrowserIncidentIncludesRunningPosition 验证运行中的岗位能随异常事件一起上传。
func TestBrowserIncidentIncludesRunningPosition(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveTask(context.Background(), storage.TaskRun{TaskID: "task-1", PositionID: "position-1", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	uploader := &fakeBrowserUploader{}
	uploader.failing.Store(true)
	recorder := NewBrowserIncidentRecorder(store, uploader)
	recorder.Bind("user-1", "valid-token")
	recorder.WorkerLine(Normalize([]byte(`{"action":"browser.exit","category":"异常退出","is_error":true,"error_message":"连接断开"}`)))
	pending, err := store.PendingBrowserEvents(context.Background(), "user-1")
	if err != nil || len(pending) != 1 || pending[0].PositionID != "position-1" {
		t.Fatalf("运行岗位关联失败：%v %+v", err, pending)
	}
}
