// Package cloud 本文件验证人工重试只恢复当前账号阻塞报告，不破坏缓存、有效租约和发送记录。
package cloud

import (
	"context"
	"encoding/json"
	"goodhr5/local-agent-go-new/internal/storage"
	"testing"
	"time"
)

// TestRetryBlockedAutoReplyRecommendationsPreservesUnrelatedState 验证重试范围、缓存复用和正在生成报告的保护。
func TestRetryBlockedAutoReplyRecommendationsPreservesUnrelatedState(t *testing.T) {
	c, cred, _ := entityTestClient(t)
	ctx := context.Background()
	other := AgentCredentials{Token: "other-fixture-token", MachineID: cred.MachineID}
	oldLease := time.Now().UTC().Add(-10 * time.Minute)
	states := map[string]localReportState{
		"failed":    {Attempts: 3, LeaseAt: oldLease},
		"cached":    {Attempts: 2, Report: json.RawMessage(`{"summary":"已生成"}`)},
		"published": {Attempts: 3, Published: true},
		"running":   {Attempts: 3, LeaseAt: time.Now().UTC()},
	}
	send := localAutoReplySend{ConversationID: "conv", MessageKey: "message", Content: "已经发过", Status: "sent"}
	if err := c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		for key, state := range states {
			if err := tx.Put("reports", key, state); err != nil {
				return err
			}
			job := AutoReplyRecommendationJob{ID: key, InputHash: key, Status: "blocked", Attempts: 3, LastError: "人工确认", NextAttemptAt: oldLease, LeaseUntil: oldLease}
			if err := tx.Put("recommendations", key, job); err != nil {
				return err
			}
		}
		if err := tx.Put("recommendations", "done", AutoReplyRecommendationJob{ID: "done", Status: "completed"}); err != nil {
			return err
		}
		return tx.Put("sends", "sent", send)
	}); err != nil {
		t.Fatal(err)
	}
	if err := c.autoReplyTransaction(ctx, other, func(tx *storage.AutoReplyTx) error {
		return tx.Put("recommendations", "other", AutoReplyRecommendationJob{ID: "other", Status: "blocked"})
	}); err != nil {
		t.Fatal(err)
	}
	count, err := c.CountBlockedAutoReplyRecommendations(ctx, cred)
	if err != nil || count != 4 {
		t.Fatal("阻塞数量不正确", count, err)
	}
	count, err = c.RetryBlockedAutoReplyRecommendations(ctx, cred)
	if err != nil || count != 3 {
		t.Fatal("重试未保留有效租约", count, err)
	}
	if err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		for key, before := range states {
			after, _, err := getAutoReplyEntity[localReportState](tx, "reports", key)
			if err != nil {
				return err
			}
			if key == "failed" {
				if after.Attempts != 0 || !after.LeaseAt.IsZero() {
					t.Fatal("未完成报告未重置", after)
				}
			} else {
				left, _ := json.Marshal(before)
				right, _ := json.Marshal(after)
				if string(left) != string(right) {
					t.Fatal("缓存或生成租约被改写", key, string(right))
				}
			}
			job, _, err := getAutoReplyEntity[AutoReplyRecommendationJob](tx, "recommendations", key)
			if err != nil {
				return err
			}
			if key == "running" {
				if job.Status != "blocked" {
					t.Fatal("正在生成的报告被重复入队", job)
				}
			} else if job.Status != "pending" || job.Attempts != 0 || job.LastError != "" || !job.NextAttemptAt.IsZero() || !job.LeaseUntil.IsZero() {
				t.Fatal("人工重试任务状态不正确", job)
			}
		}
		oldSend, _, err := getAutoReplyEntity[localAutoReplySend](tx, "sends", "sent")
		if err == nil && oldSend != send {
			t.Fatal("人工重试改变了发送意图", oldSend)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	count, err = c.CountBlockedAutoReplyRecommendations(ctx, other)
	if err != nil || count != 1 {
		t.Fatal("人工重试改变了另一账号", count, err)
	}
}
