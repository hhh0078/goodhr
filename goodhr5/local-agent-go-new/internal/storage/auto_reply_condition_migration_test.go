// Package storage 本文件验证旧 AI 门槛阻断的最新资格收据可在升级后恢复，不触发旧消息重发。
package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestLegacyAIGateRestoresOneReceiptPerCurrentConversation 验证仅有未绑定 AI 门槛且尚无报告任务的账号恢复最新收据。
func TestLegacyAIGateRestoresOneReceiptPerCurrentConversation(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	for _, owner := range []string{"affected", "already-enqueued", "other-account"} {
		err = store.AutoReplyTransaction(ctx, owner, func(tx *AutoReplyTx) error {
			if err := tx.Put("conversations", "thread", map[string]string{"candidate_id": "candidate", "position_id": "position", "last_candidate_message_key": "latest"}); err != nil {
				return err
			}
			if owner != "other-account" {
				if err := tx.Put("confirmations", "legacy", map[string]string{"candidate_id": "candidate", "position_id": "position", "source_type": "ai", "item_type": "confirm", "status": "unmatched"}); err != nil {
					return err
				}
			}
			if owner == "already-enqueued" {
				if err := tx.Put("recommendations", "job", map[string]any{"conversation_id": "thread", "config_version": 1, "based_on_message_key": "latest"}); err != nil {
					return err
				}
			}
			for _, key := range []string{"earlier", "latest", "latest"} {
				// 同一消息的普通答复和简历补审各有一份收据，都保留原始业务编号。
				id := key
				if _, exists, err := tx.Get("proposal_receipts", id); err != nil {
					return err
				} else if exists {
					id += "-review"
				}
				if err := tx.Put("proposal_receipts", id, map[string]any{"conversation_id": "thread", "config_version": 1, "based_on_message_key": key, "status": "completed"}); err != nil {
					return err
				}
			}
			return tx.Put("sends", "sent", map[string]string{"conversation_id": "thread", "status": "confirmed"})
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.db.ExecContext(ctx, "DELETE FROM schema_migrations WHERE version = ?", "010_auto_reply_condition_receipt_retry.sql"); err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{"affected", "already-enqueued", "other-account"} {
		err = store.AutoReplyTransaction(ctx, owner, func(tx *AutoReplyTx) error {
			rows, readErr := tx.PendingProposalReceipts(30)
			if readErr != nil {
				return readErr
			}
			if owner == "affected" {
				if len(rows) != 1 || rows[0].ID != "latest-review" {
					t.Fatalf("升级应恢复一份最新收据：%+v", rows)
				}
			} else if len(rows) != 0 {
				t.Fatalf("不应恢复账号 %s 的收据：%+v", owner, rows)
			}
			sent, ok, readErr := tx.Get("sends", "sent")
			if readErr == nil && (!ok || sent.Revision != 1) {
				t.Fatalf("升级改写了发送记录：%+v", sent)
			}
			return readErr
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestProposalReceiptFIFOAndExactDeadline 验证旧格式、跨时区及同毫秒的实际到期边界，并保证旧收据更新后仍按首次顺序恢复。
func TestProposalReceiptFIFOAndExactDeadline(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "receipts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 11, 8, 2, 0, 0, 120000000, time.UTC)
	err = store.AutoReplyTransaction(context.Background(), "owner", func(tx *AutoReplyTx) error {
		cases := []struct{ id, due string }{
			{"nanosecond-later", "2026-11-08T02:00:00.120000001Z"},
			{"zone-past", "2026-11-08T10:00:00.119999999+08:00"},
			{"exact", "2026-11-08T02:00:00.12Z"},
			{"zone-future", "2026-11-07T21:00:00.120000001-05:00"},
			{"legacy", ""},
		}
		for _, item := range cases {
			if err := tx.Put("proposal_receipts", item.id, map[string]string{"status": "pending", "next_attempt_at": item.due}); err != nil {
				return err
			}
		}
		if err := tx.Put("proposal_receipts", "zone-past", map[string]string{"status": "pending", "next_attempt_at": cases[1].due}); err != nil {
			return err
		}
		rows, err := tx.pendingProposalReceiptsAt(2, now)
		if err != nil {
			return err
		}
		if len(rows) != 2 || rows[0].ID != "zone-past" || rows[1].ID != "exact" {
			t.Fatalf("到期过滤或入队顺序错误：%v", rows)
		}
		rows, err = tx.pendingProposalReceiptsAt(30, now)
		if err == nil && (len(rows) != 3 || rows[2].ID != "legacy") {
			t.Fatalf("未保留旧的无到期时间收据：%v", rows)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
