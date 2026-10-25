// Package storage 文件作用：验证真实SQLite筛选版本、并发回执、账号隔离和重启恢复，不连接云端。
package storage

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/screening"
)

// screeningFixture 为每次测试创建独立编号和空轮次，不使用平台指纹合并重复筛选。
func screeningFixture(t *testing.T, owner string) ScreeningRecord {
	t.Helper()
	id, err := screening.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return ScreeningRecord{OwnerID: owner, PositionID: "position", Snapshot: screening.Snapshot{AttemptID: id, TaskID: "task", PlatformID: "boss", CandidateKey: "same-card", CandidateName: "测试人选", OccurredAt: time.Now().UTC(), Revision: 1, First: screening.NotExecuted("未开始"), Second: screening.NotExecuted("未进入")}}
}

// TestScreeningRevisionAckAndIsolation 验证上传旧版本时产生新结果不会被旧回执清空，其他账号无法领取。
func TestScreeningRevisionAckAndIsolation(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "records.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	first := screeningFixture(t, "owner-a")
	second := screeningFixture(t, "owner-b")
	for _, item := range []ScreeningRecord{first, second} {
		if err = store.CreateScreeningRecord(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	sent, err := store.PendingScreeningRecords(ctx, "owner-a", 30)
	if err != nil || len(sent) != 1 {
		t.Fatal("队列账号隔离失败")
	}
	zero, accepted, stamp, duration := float64(0), false, time.Now().UTC(), int64(0)
	updated, err := store.UpdateScreeningRecord(ctx, first.Snapshot.AttemptID, func(item *screening.Snapshot) error {
		item.First = screening.Round{Kind: "ai", Status: "rejected", Score: &zero, Threshold: &zero, Accepted: &accepted, Reason: "真实零分", StartedAt: &stamp, FinishedAt: &stamp, DurationMS: &duration}
		item.Completed = true
		return nil
	})
	if err != nil || updated.Revision != 2 {
		t.Fatal(err)
	}
	if err = store.AckScreeningRecord(ctx, "owner-a", first.Snapshot.AttemptID, sent[0].Snapshot.Revision); err != nil {
		t.Fatal(err)
	}
	pending, err := store.PendingScreeningRecords(ctx, "owner-a", 30)
	if err != nil || len(pending) != 1 || pending[0].Snapshot.Revision != 2 {
		t.Fatal("旧回执清除了上传期间的新版本")
	}
	raw, _ := json.Marshal(pending[0].Snapshot)
	if !strings.Contains(string(raw), `"score":0`) || !strings.Contains(string(raw), `"duration_ms":0`) || !strings.Contains(string(raw), `"second":{"kind":"none","status":"not_executed","accepted":null,"score":null`) {
		t.Fatal("零值或null丢失")
	}
	if err = store.AckScreeningRecord(ctx, "owner-b", first.Snapshot.AttemptID, 2); err != nil {
		t.Fatal(err)
	}
	pending, _ = store.PendingScreeningRecords(ctx, "owner-a", 30)
	if len(pending) != 1 {
		t.Fatal("其他账号确认了本人的快照")
	}
	if err = store.AckScreeningRecord(ctx, "owner-a", first.Snapshot.AttemptID, 2); err != nil {
		t.Fatal(err)
	}
	pending, _ = store.PendingScreeningRecords(ctx, "owner-a", 30)
	if len(pending) != 0 {
		t.Fatal("已确认版本仍被重复领取")
	}
}

// TestScreeningRecoveryAndPendingRetention 验证异常重启不补造时间，待传数据不随普通90日清理丢失。
func TestScreeningRecoveryAndPendingRetention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	record := screeningFixture(t, "owner")
	if err = store.CreateScreeningRecord(ctx, record); err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC().Add(-time.Minute)
	if _, err = store.UpdateScreeningRecord(ctx, record.Snapshot.AttemptID, func(item *screening.Snapshot) error {
		item.First = screening.Round{Kind: "ai", Status: "running", Reason: "判定中", StartedAt: &started}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	recovered, err := store.ScreeningRecord(ctx, record.Snapshot.AttemptID)
	if err != nil || !recovered.Snapshot.Completed || recovered.Snapshot.First.Status != "cancelled" || recovered.Snapshot.First.StartedAt == nil || !recovered.Snapshot.First.StartedAt.Equal(started) || recovered.Snapshot.First.FinishedAt != nil || recovered.Snapshot.First.DurationMS != nil || recovered.Snapshot.Second.StartedAt != nil {
		t.Fatalf("恢复伪造了判定结果: %+v err=%v", recovered, err)
	}
	if _, err = store.CleanupExpired(ctx, time.Now().Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ScreeningRecord(ctx, record.Snapshot.AttemptID); err != nil {
		t.Fatal("未上传快照被普通清理删除")
	}
}

// TestScreeningConcurrentUpdatesAndAttempts 验证同一尝试的并发修改不丢版本，重新筛选同一卡片仍生成独立编号。
func TestScreeningConcurrentUpdatesAndAttempts(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "records.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	item := screeningFixture(t, "owner")
	if err = store.CreateScreeningRecord(ctx, item); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	problems := make(chan error, 20)
	for i := 0; i < 20; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := store.UpdateScreeningRecord(ctx, item.Snapshot.AttemptID, func(snapshot *screening.Snapshot) error { snapshot.TerminationReason += "x"; return nil })
			problems <- err
		}()
	}
	group.Wait()
	close(problems)
	for err := range problems {
		if err != nil {
			t.Fatal(err)
		}
	}
	actual, err := store.ScreeningRecord(ctx, item.Snapshot.AttemptID)
	if err != nil || actual.Snapshot.Revision != 21 || len(actual.Snapshot.TerminationReason) != 20 {
		t.Fatal("并发快照覆盖了其他修改")
	}
	repeated := screeningFixture(t, "owner")
	if err = store.CreateScreeningRecord(ctx, repeated); err != nil {
		t.Fatal(err)
	}
	items, err := store.PendingScreeningRecords(ctx, "owner", 30)
	if err != nil || len(items) != 2 || items[0].Snapshot.AttemptID == items[1].Snapshot.AttemptID {
		t.Fatal("相同指纹的不同次筛选被合并")
	}
}
