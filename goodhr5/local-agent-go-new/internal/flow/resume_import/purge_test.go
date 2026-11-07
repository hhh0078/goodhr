// Package resume_import 本文件以临时目录验证批量草稿中的单人删除、原件隔离和收据重试。
package resume_import

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
)

// TestPurgeCandidateKeepsSibling 确认云端回包丢失也能清理准确条目、保留其他原件，并可在重启后再执行。
func TestPurgeCandidateKeepsSibling(t *testing.T) {
	m, store, client, parser, root := newTestManager(t, "13800138001")
	ctx := context.Background()
	task, err := m.Create(ctx, "owner-a", "token", []Upload{{Name: "same.txt", Reader: strings.NewReader("first")}, {Name: "same.txt", Reader: strings.NewReader("second")}}, "")
	if err != nil {
		t.Fatal(err)
	}
	task = waitProcessed(t, m, task.ID)
	m.Close()
	m.mu.Lock()
	stored := m.tasks[task.ID]
	first, second := task.Items[0].ID, task.Items[1].ID
	path, other := stored.Paths[first], stored.Paths[second]
	stored.Confirmations[first] = &confirmation{Key: "test-import-lost-response"}
	if err = m.persistLocked(stored); err != nil {
		t.Fatal(err)
	}
	m.mu.Unlock()
	hash := sha256.Sum256([]byte("test-import-lost-response"))
	hashes := []string{hex.EncodeToString(hash[:])}
	if err = m.PurgeCandidate(ctx, "owner-b", "deleted-id", hashes, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatal("误删其他账号文件", err)
	}
	if err = m.PurgeCandidate(ctx, "owner-a", "deleted-id", hashes, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("目标原件仍可读 %v", err)
	}
	if b, err := os.ReadFile(other); err != nil || string(b) != "second" {
		t.Fatal("另一份同名原件受影响", err)
	}
	restored, err := New(store, client, parser, fakeExtractor{}, root)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err = restored.PurgeCandidate(ctx, "owner-a", "deleted-id", hashes, nil); err != nil {
		t.Fatal(err)
	}
	batch, err := restored.Get("owner-a", "token", task.ID)
	if err != nil || len(batch.Items) != 1 || batch.Items[0].ID != second || batch.Total != 1 {
		t.Fatalf("重启后批次异常: %+v %v", batch, err)
	}
}
