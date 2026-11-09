// Package replybridge 本文件验证跨网络发送只领取一次、回执幂等及身份锚点的序列化。
package replybridge

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestRejectedQueuedGoalNeverReachesDevice 验证领取前失效的追问直接取消，不按临时故障反复领取，也不把新业务字段发给设备。
func TestRejectedQueuedGoalNeverReachesDevice(t *testing.T) {
	b := NewBroker(func(_ context.Context, _ Command, status string, _ Result) error {
		if status == "executing" {
			return &CommandRejectedError{Message: "目标已删除"}
		}
		return nil
	})
	b.RunID, b.PlatformID = "run", "liepin"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- b.Call(ctx, "send", Request{Message: "已经失效的追问"}, nil) }()
	next, stop := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer stop()
	if command, err := b.Next(next); err == nil || command.ID != "" {
		t.Fatal("已删除目标的命令仍交给了设备")
	}
	if err := <-done; !errors.Is(err, model.ErrAutoReplySupersededBeforeSend) {
		t.Fatal("取消未返回可安全放弃的旧草稿类型", err)
	}
}

// TestBrokerPersistsBeforeExecutingAndAcceptsSameReceipt 验证持久化顺序和重复回执不触发重复操作。
func TestBrokerPersistsBeforeExecutingAndAcceptsSameReceipt(t *testing.T) {
	var mu sync.Mutex
	var states []string
	b := NewBroker(func(_ context.Context, _ Command, status string, _ Result) error {
		mu.Lock()
		defer mu.Unlock()
		states = append(states, status)
		return nil
	})
	b.RunID = "run"
	b.PlatformID = "liepin"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { var result Response; done <- b.Call(ctx, "send", Request{Message: "合成回复"}, &result) }()
	command, err := b.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	value, _ := json.Marshal(Response{Latest: model.ConversationMessage{PlatformMessageID: "out-new", Direction: "self"}})
	result := Result{ID: command.ID, Value: value}
	if err = b.Complete(ctx, result); err != nil {
		t.Fatal(err)
	}
	if err = b.Complete(ctx, result); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(states) != 3 || states[0] != "pending" || states[1] != "executing" || states[2] != "completed" {
		t.Fatalf("错误的提交顺序 %v", states)
	}
}

// TestBrokerDoesNotRedeliverClaimedSend 验证设备领取后失联只标未知，再次轮询不能重获发送。
func TestBrokerDoesNotRedeliverClaimedSend(t *testing.T) {
	b := NewBroker(func(context.Context, Command, string, Result) error { return nil })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- b.Call(ctx, "send", Request{}, nil) }()
	if _, err := b.Next(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("失联未返回未知")
	}
	next, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	if command, err := b.Next(next); err == nil || command.ID != "" {
		t.Fatal("失联发送被再次领取")
	}
}

// TestBridgeSnapshotRetainsIdentityGuards 验证删除边界、同文见证和发送消息锚点跨网络保持。
func TestBridgeSnapshotRetainsIdentityGuards(t *testing.T) {
	now := time.Now().UTC()
	input := Request{Snapshot: model.AutoReplyConversationSnapshot{ExpectedSendMessageKey: "candidate-2", DeliveryPageURL: "https://example.invalid/chat", HistoryAfter: &now}, Conversation: model.Conversation{ReplyAfter: now, WitnessDigest: "known-digest"}}
	raw, _ := json.Marshal(input)
	var output Request
	if err := json.Unmarshal(raw, &output); err != nil {
		t.Fatal(err)
	}
	if output.Snapshot.ExpectedSendMessageKey != "candidate-2" || output.Snapshot.HistoryAfter == nil || output.Conversation.WitnessDigest != "known-digest" || !output.Conversation.ReplyAfter.Equal(now) {
		t.Fatal("桥接丢失了安全边界")
	}
}

// TestCancelledQueuedCommandIsNeverClaimed 验证提前取消的队列发送即使原截止时间未到也不能执行。
func TestCancelledQueuedCommandIsNeverClaimed(t *testing.T) {
	pending := make(chan struct{})
	b := NewBroker(func(_ context.Context, _ Command, s string, _ Result) error {
		if s == "pending" {
			close(pending)
		}
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.Call(ctx, "send", Request{Message: "撤销操作"}, nil) }()
	<-pending
	cancel()
	if err := <-done; err == nil {
		t.Fatal("提前取消的操作返回成功")
	}
	poll, stop := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer stop()
	if cmd, err := b.Next(poll); err == nil || cmd.ID != "" {
		t.Fatal("取消后发送仍被设备领取")
	}
}

// TestAbandonedReceiptKeepsOnlyDigest 验证迟到的回执可重复确认，但附件内容不再占用任务内存。
func TestAbandonedReceiptKeepsOnlyDigest(t *testing.T) {
	b := NewBroker(func(context.Context, Command, string, Result) error { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.Call(ctx, "collect_resume", Request{}, nil) }()
	cmd, err := b.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	<-done
	res := Result{ID: cmd.ID, Value: json.RawMessage(`{"files":[]}`)}
	if err = b.Complete(context.Background(), res); err != nil {
		t.Fatal(err)
	}
	if err = b.Complete(context.Background(), res); err != nil {
		t.Fatal(err)
	}
	if len(b.items[cmd.ID].done) != 0 || len(b.items[cmd.ID].command.Payload) != 0 {
		t.Fatal("迟到的回执仍保留完整正文")
	}
	res.Value = json.RawMessage(`{"files":["new"]}`)
	if err = b.Complete(context.Background(), res); err == nil {
		t.Fatal("矛盾回执覆盖了已确认结果")
	}
}
