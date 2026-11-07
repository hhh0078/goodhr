// Package storage 本文件验证旧 AI 门槛阻断的最新资格收据可在升级后恢复，不触发旧消息重发。
package storage

import (
	"context"
	"path/filepath"
	"testing"
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
