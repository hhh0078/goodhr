// Package storage 本文件模拟跨进程PG版本提交，验证云编排断线、重启、历史排序和删除边界恢复。
package storage

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type scratchAuthorityFixture struct {
	version int64
	rows    map[string]AutoReplyStateChange
	fail    bool
}

// Load 模拟服务器条件快照。
func (f *scratchAuthorityFixture) Load(_ context.Context, known *int64) (AutoReplyStateSnapshot, bool, error) {
	if known != nil && *known == f.version {
		return AutoReplyStateSnapshot{}, true, nil
	}
	s := AutoReplyStateSnapshot{Version: f.version}
	for _, r := range f.rows {
		s.Entities = append(s.Entities, r)
	}
	return s, false, nil
}

// Commit 模拟版本冲突、网络故障和原子提交。
func (f *scratchAuthorityFixture) Commit(_ context.Context, v int64, changes []AutoReplyStateChange) (int64, error) {
	if f.fail || v != f.version {
		return 0, errors.New("PG unavailable")
	}
	if f.rows == nil {
		f.rows = map[string]AutoReplyStateChange{}
	}
	for _, r := range changes {
		k := r.Kind + "|" + r.ID
		if r.Deleted {
			delete(f.rows, k)
		} else {
			f.rows[k] = r
		}
	}
	f.version++
	return f.version, nil
}

// TestServerScratchCommitsAndReloads 验证多次更新同一消息、来源删除水位与错误时回滚，另一个服务器进程可继续读取。
func TestServerScratchCommitsAndReloads(t *testing.T) {
	ctx := context.Background()
	a := &scratchAuthorityFixture{}
	s, err := OpenAutoReplyScratch(a)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	err = s.AutoReplyTransaction(ctx, "owner", func(tx *AutoReplyTx) error {
		if err := tx.Put("messages", "m", map[string]any{"conversation_id": "thread", "platform_message_id": "in", "direction": "candidate", "text_content": "第一条"}); err != nil {
			return err
		}
		if err := tx.MarkHistoricalMessage("m", "sent", 0.5); err != nil {
			return err
		}
		if err := tx.RememberContactDeletion("source", 5, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		_, err := tx.ServerLedger("runtime_runs", "lease", map[string]string{"status": "running"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	msg := a.rows["messages|m"]
	if msg.Sequence <= 0 || msg.MessageOrder != 0.5 || msg.ProcessedStatus != "sent" || a.version != 1 {
		t.Fatalf("missing order or status: %+v", msg)
	}
	restarted, err := OpenAutoReplyScratch(a)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	read := func(tx *AutoReplyTx) error {
		seq, found, err := tx.ContactDeletion("source")
		if err != nil {
			return err
		}
		if seq != 5 || found == "" {
			t.Fatalf("missing deletion: %d", seq)
		}
		rows, err := tx.ExportAutoReplyOwner()
		if err != nil {
			return err
		}
		if len(rows) != 2 {
			t.Fatalf("业务导出不应混入执行器租约: %d", len(rows))
		}
		return nil
	}
	if err = restarted.AutoReplyTransaction(ctx, "owner", read); err != nil {
		t.Fatal(err)
	}
	a.fail = true
	if err = restarted.AutoReplyTransaction(ctx, "owner", func(tx *AutoReplyTx) error { return tx.Delete("messages", "m") }); err == nil {
		t.Fatal("unacknowledged deletion returned success")
	}
	a.fail = false
	if err = restarted.AutoReplyTransaction(ctx, "owner", read); err != nil {
		t.Fatal(err)
	}
	if err = s.AutoReplyTransaction(ctx, "another", read); err == nil {
		t.Fatal("server scratch crossed owners")
	}
	if err = s.AutoReplyTransaction(ctx, "owner", func(tx *AutoReplyTx) error { return tx.Delete("messages", "m") }); err != nil {
		t.Fatal(err)
	}
	if err = restarted.AutoReplyTransaction(ctx, "owner", func(tx *AutoReplyTx) error {
		_, found, err := tx.Get("messages", "m")
		if found {
			t.Fatal("deleted message reappeared")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(a.rows)
	if len(raw) == 0 {
		t.Fatal("missing retained state")
	}
}
