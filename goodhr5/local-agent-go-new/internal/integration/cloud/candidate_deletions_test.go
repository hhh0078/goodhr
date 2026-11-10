// Package cloud 本文件用隔离 HTTP 和 SQLite 验证删除游标、设备认证、重启恢复和账号隔离。
package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// TestCandidateDeletionCursorRetries 证明一条清理失败不会跳过，重启后不再重复完成项，另一个账号从头读取。
func TestCandidateDeletionCursorRetries(t *testing.T) {
	ctx := context.Background()
	c, cred, store := entityTestClient(t)
	var cursors []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != autoReplyAgentBasePath+"/candidate-deletions" || r.Header.Get("X-GoodHR-Machine-ID") != cred.MachineID || r.Header.Get("Authorization") == "" {
			t.Error("清理请求缺少设备归属")
		}
		after := r.URL.Query().Get("after")
		cursors = append(cursors, after)
		events := []CandidateDeletion{}
		if after == "0" {
			events = append(events, CandidateDeletion{Sequence: 1, CandidateID: "one"})
		}
		if after != "2" {
			events = append(events, CandidateDeletion{Sequence: 2, CandidateID: "two"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "events": events, "next_cursor": 2})
	}))
	defer server.Close()
	c.baseURL = server.URL
	var calls []string
	busy := true
	apply := func(_ context.Context, event CandidateDeletion) error {
		calls = append(calls, event.CandidateID)
		if busy && event.CandidateID == "one" {
			return errors.New("文件正在使用")
		}
		return nil
	}
	if err := c.SyncCandidateDeletions(ctx, cred, apply); err == nil {
		t.Fatal("未报告文件占用")
	}
	restarted := New(server.URL)
	restarted.EnableLocalAutoReply(store, c.localAutoDir)
	busy = false
	for _, token := range []string{cred.Token, cred.Token, "another"} {
		if err := restarted.SyncCandidateDeletions(ctx, AgentCredentials{MachineID: cred.MachineID, Token: token}, apply); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(cursors, []string{"0", "2", "2", "0"}) || !slices.Equal(calls, []string{"one", "two", "one", "one", "two"}) {
		t.Fatalf("错误的续读顺序 %v %v", cursors, calls)
	}
}
