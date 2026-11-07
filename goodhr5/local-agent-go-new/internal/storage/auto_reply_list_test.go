// Package storage 文件作用：直接审计列表水位表，确认它既不存联系人姓名、正文和明文线程号，也不留下限定范围外的行。
package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestAutoReplyListStoresOnlyScopedDigests 检查真实迁移后的 SQLite 行及列，并核对限定模式下首次旧红点只建立基线。
func TestAutoReplyListStoresOnlyScopedDigests(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "scan.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	foreign := AutoReplyListObservation{ThreadID: "other-private-id", Signature: "other-private-text"}
	private := AutoReplyListObservation{ThreadID: "private-id", Signature: "private-object", PreviewDigest: "candidate-private-text"}
	err = s.AutoReplyTransaction(ctx, "owner", func(tx *AutoReplyTx) error {
		v, e := tx.ObserveAutoReplyList("liepin", "account", private.ThreadID, []AutoReplyListObservation{foreign, private}, time.Now().UTC())
		if e == nil && len(v) != 0 {
			t.Fatal("没有时间或游标依据的旧红点不应立即打开")
		}
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err = s.db.QueryRow("SELECT COUNT(*) FROM auto_reply_list_rows").Scan(&n); err != nil || n != 1 {
		t.Fatalf("限定范围外产生记录：%d %v", n, err)
	}
	var scope, source, sig, preview, witness string
	if err = s.db.QueryRow("SELECT scope_hash,thread_hash,signature,preview_digest,witness_digest FROM auto_reply_list_rows").Scan(&scope, &source, &sig, &preview, &witness); err != nil {
		t.Fatal(err)
	}
	if source != listDigest(private.ThreadID) || scope != source || sig != listDigest(private.Signature) || preview != "" || witness != "" {
		t.Fatal("匿名摘要列泄露了原始列表内容或错误持久化见证")
	}
	if err = s.db.QueryRow("SELECT COUNT(*) FROM auto_reply_candidates").Scan(&n); err != nil || n != 0 {
		t.Fatalf("建立列表基线时创建了简历：%d %v", n, err)
	}
}
