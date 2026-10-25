// Package screening_sync 文件作用：用真实SQLite和本机HTTP桩验证断网重传、旧回执、新版本与登录账号隔离。
package screening_sync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/screening"
	"goodhr5/local-agent-go-new/internal/storage"
)

// uploadFixture 创建不含Token的持久化快照，返回独立数据库位置供重启和秘密扫描。
func uploadFixture(t *testing.T, owner string) (*storage.Store, string, screening.Snapshot) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "records.db")
	store, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	id, err := screening.NewID()
	if err != nil {
		t.Fatal(err)
	}
	item := screening.Snapshot{AttemptID: id, TaskID: "task", PlatformID: "boss", CandidateKey: "candidate", CandidateName: "合成候选人", OccurredAt: time.Now().UTC(), Revision: 1, First: screening.NotExecuted("尚未开始"), Second: screening.NotExecuted("尚未进入")}
	if err = store.CreateScreeningRecord(context.Background(), storage.ScreeningRecord{OwnerID: owner, PositionID: "position", Snapshot: item}); err != nil {
		t.Fatal(err)
	}
	return store, path, item
}

// TestScreeningUploadAckDoesNotClearNewRevision 验证慢上传期间新增二轮结果，旧回包只能确认已发送版本。
func TestScreeningUploadAckDoesNotClearNewRevision(t *testing.T) {
	store, path, item := uploadFixture(t, "owner-a")
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/me" {
			_, _ = w.Write([]byte(`{"user":{"id":"owner-a","email":"a@example.test"}}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer never-store-this-token" {
			t.Error("凭证不属于已验证账号")
		}
		var got screening.Snapshot
		if json.NewDecoder(r.Body).Decode(&got) != nil {
			t.Error("快照不是JSON")
		}
		if posts.Add(1) == 1 {
			close(entered)
			<-release
		}
		_ = json.NewEncoder(w).Encode(cloud.ScreeningRecordReceipt{OK: true, ID: "server-record", Revision: got.Revision})
	}))
	defer server.Close()
	defer once.Do(func() { close(release) })
	u := New(store, cloud.New(server.URL))
	u.Bind("owner-a", "never-store-this-token")
	done := make(chan struct{})
	go func() { u.Flush(context.Background()); close(done) }()
	<-entered
	if _, err := store.UpdateScreeningRecord(context.Background(), item.AttemptID, func(snapshot *screening.Snapshot) error {
		snapshot.First.Reason = "上传期间新增的结果"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })
	<-done
	pending, err := store.PendingScreeningRecords(context.Background(), "owner-a", 30)
	if err != nil || len(pending) != 1 || pending[0].Snapshot.Revision != 2 {
		t.Fatal("旧回执吞掉新版本")
	}
	u.Flush(context.Background())
	pending, _ = store.PendingScreeningRecords(context.Background(), "owner-a", 30)
	if len(pending) != 0 || posts.Load() != 2 {
		t.Fatal("最新版本未继续补传")
	}
	raw, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(raw), "never-store-this-token") {
		t.Fatal("Token被写入SQLite")
	}
}

// TestScreeningUploadOfflineAndInvalidReceipt 验证失败或不完整成功回执保留原快照，恢复网络后按原ID上传。
func TestScreeningUploadOfflineAndInvalidReceipt(t *testing.T) {
	for _, reply := range []string{`{}`, `{"ok":true,"id":"id","revision":0}`, "offline"} {
		t.Run(reply, func(t *testing.T) {
			store, _, item := uploadFixture(t, "owner-a")
			var valid atomic.Bool
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/auth/me" {
					_, _ = w.Write([]byte(`{"user":{"id":"owner-a"}}`))
					return
				}
				calls.Add(1)
				if !valid.Load() {
					if reply == "offline" {
						w.WriteHeader(503)
					}
					_, _ = w.Write([]byte(reply))
					return
				}
				var got screening.Snapshot
				if json.NewDecoder(r.Body).Decode(&got) != nil || got.AttemptID != item.AttemptID {
					t.Error("重传改变原始编号")
				}
				_ = json.NewEncoder(w).Encode(cloud.ScreeningRecordReceipt{OK: true, ID: "id", Revision: got.Revision})
			}))
			defer server.Close()
			u := New(store, cloud.New(server.URL))
			u.Bind("owner-a", "token")
			u.Flush(context.Background())
			stored, err := store.ScreeningRecord(context.Background(), item.AttemptID)
			if err != nil || stored.Snapshot.AttemptID != item.AttemptID {
				t.Fatal("失败丢失原快照")
			}
			valid.Store(true)
			// 新真实结果生成新版本会清除旧失败退避，立即允许恢复；回执必须确认这个新版本。
			updated, err := store.UpdateScreeningRecord(context.Background(), item.AttemptID, func(snapshot *screening.Snapshot) error { snapshot.TerminationReason = "已恢复"; return nil })
			if err != nil {
				t.Fatal(err)
			}
			if updated.Revision != 2 {
				t.Fatal("新结果没有生成独立版本")
			}
			u.Flush(context.Background())
			pending, _ := store.PendingScreeningRecords(context.Background(), "owner-a", 30)
			if len(pending) != 0 || calls.Load() != 2 {
				t.Fatal("恢复后没有使用原编号补传")
			}
		})
	}
}

// TestScreeningUploadSwitchAccountCancelsOldRequest 验证切号取消旧HTTP且不会把旧账号快照交给新账号。
func TestScreeningUploadSwitchAccountCancelsOldRequest(t *testing.T) {
	store, _, item := uploadFixture(t, "owner-a")
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	var foreignPosts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if r.URL.Path == "/api/auth/me" {
			owner := "owner-a"
			if token == "Bearer token-b" {
				owner = "owner-b"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"user": map[string]string{"id": owner}})
			return
		}
		if token == "Bearer token-b" {
			foreignPosts.Add(1)
		}
		close(entered)
		<-release
		_ = json.NewEncoder(w).Encode(cloud.ScreeningRecordReceipt{OK: true, ID: "id", Revision: 1})
	}))
	defer server.Close()
	defer once.Do(func() { close(release) })
	u := New(store, cloud.New(server.URL))
	u.Bind("owner-a", "token-a")
	done := make(chan struct{})
	go func() { u.Flush(context.Background()); close(done) }()
	<-entered
	u.Bind("owner-b", "token-b")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("切号未取消旧网络请求")
	}
	u.Flush(context.Background())
	if foreignPosts.Load() != 0 {
		t.Fatal("旧账号数据被发给新账号")
	}
	stored, err := store.ScreeningRecord(context.Background(), item.AttemptID)
	if err != nil || stored.OwnerID != "owner-a" {
		t.Fatal("切号改变本地数据归属")
	}
	pending, err := store.PendingScreeningRecords(context.Background(), "owner-a", 30)
	if err != nil || len(pending) != 1 {
		t.Fatal("切号后的旧回包确认了尚未完成上传的记录")
	}
	once.Do(func() { close(release) })
}

// TestScreeningUploadRejectsWrongAndLimitedSession 验证补传前重新校验账号，不能凭绑定缓存使用另一账号或插件局部会话。
func TestScreeningUploadRejectsWrongAndLimitedSession(t *testing.T) {
	for _, limited := range []bool{false, true} {
		t.Run(fmt.Sprint(limited), func(t *testing.T) {
			store, _, _ := uploadFixture(t, "owner-a")
			var posts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/auth/me" {
					if limited {
						_, _ = w.Write([]byte(`{"user":{"id":"owner-a"},"session":{"kind":"browser_extension"}}`))
					} else {
						_, _ = w.Write([]byte(`{"user":{"id":"owner-b"}}`))
					}
					return
				}
				posts.Add(1)
			}))
			defer server.Close()
			u := New(store, cloud.New(server.URL))
			u.Bind("owner-a", "token")
			u.Flush(context.Background())
			if posts.Load() != 0 {
				t.Fatal("没有完整账号权限仍上传了筛选快照")
			}
		})
	}
}

// TestScreeningEmptyQueueDoesNotAuthenticate 验证正常空闲补传循环不对云端发无意义鉴权请求。
func TestScreeningEmptyQueueDoesNotAuthenticate(t *testing.T) {
	store, _, item := uploadFixture(t, "owner-a")
	if err := store.AckScreeningRecord(context.Background(), "owner-a", item.AttemptID, item.Revision); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	u := New(store, cloud.New(server.URL))
	u.Bind("owner-a", "token")
	u.Flush(context.Background())
	u.Flush(context.Background())
	if calls.Load() != 0 {
		t.Fatalf("空队列仍发了%d次HTTP请求", calls.Load())
	}
}
