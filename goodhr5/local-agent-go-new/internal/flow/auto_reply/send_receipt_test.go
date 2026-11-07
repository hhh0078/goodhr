// Package auto_reply 本文件验证发送后的迟到气泡、同文历史、抢先来信及不确定结果的防重发保护。
package auto_reply

import (
	"context"
	"errors"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// delayedDeliveryRuntime 模拟真实发送一次后页面晚一步呈现新气泡，记录送达探测和实际发送次数。
type delayedDeliveryRuntime struct {
	autoReplyRuntimeStub
	latest                []model.ConversationMessage
	history               []model.ConversationMessage
	delay                 time.Duration
	started               time.Time
	reads, windows, sends int
}

// SendAutoReplyMessage 标记唯一的真实发送尝试。
func (r *delayedDeliveryRuntime) SendAutoReplyMessage(context.Context, model.Browser, model.Config, model.AutoReplyConversationSnapshot, string) error {
	r.sends++
	r.started = time.Now()
	return nil
}

// ReadOpenAutoReplyLatestMessage 返回实际可见的末条，模拟事件回执、换人和渲染延迟。
func (r *delayedDeliveryRuntime) ReadOpenAutoReplyLatestMessage(ctx context.Context, _ model.Browser, _ model.Config, s model.AutoReplyConversationSnapshot) (model.ConversationMessage, bool, error) {
	r.reads++
	if ctx.Err() != nil || s.DeliveryPageURL == "" {
		return model.ConversationMessage{}, false, ctx.Err()
	}
	if len(r.latest) == 0 {
		return model.ConversationMessage{}, false, nil
	}
	if len(r.latest) > 1 && time.Since(r.started) >= r.delay {
		return r.latest[1], true, nil
	}
	return r.latest[0], true, nil
}

// ReadAutoReplyMessages 返回真实末端窗口，未带原页约束或读取过多时直接拒绝。
func (r *delayedDeliveryRuntime) ReadAutoReplyMessages(ctx context.Context, _ model.Browser, _ model.Config, s model.AutoReplyConversationSnapshot, _ []string, n int) ([]model.ConversationMessage, bool, error) {
	r.windows++
	if s.DeliveryPageURL == "" || n > 16 {
		return nil, false, errors.New("不能整段扫历史")
	}
	return r.history, false, ctx.Err()
}

// TestDelayedAutoReplyDelivery 通过正式 outbox 验证各个送达边界，未确定时下次调用也不能重复操作。
func TestDelayedAutoReplyDelivery(t *testing.T) {
	anchor := model.ConversationMessage{PlatformMessageID: "candidate-first", Direction: "candidate", MessageType: "text", TextContent: "想了解下"}
	incoming := model.ConversationMessage{PlatformMessageID: "candidate-next", Direction: "candidate", MessageType: "text", TextContent: "对了还有个问题"}
	self := model.ConversationMessage{PlatformMessageID: "new-self", Direction: "self", MessageType: "text", TextContent: "收到"}
	for _, tc := range []struct {
		name            string
		latest, history []model.ConversationMessage
		delay           time.Duration
		sent            bool
		windows         bool
	}{
		{"立即送达", []model.ConversationMessage{self}, nil, 0, true, false},
		{"气泡晚500毫秒", []model.ConversationMessage{anchor, self}, nil, 500 * time.Millisecond, true, false},
		{"只有原消息直到超时", []model.ConversationMessage{anchor}, nil, 0, false, false},
		{"新候选人消息先可见", []model.ConversationMessage{incoming}, []model.ConversationMessage{anchor, self, incoming}, 0, true, true},
		{"原锚点前的同文不是证据", []model.ConversationMessage{incoming}, []model.ConversationMessage{self, anchor, incoming}, 0, false, true},
		{"窗口没有原锚点", []model.ConversationMessage{incoming}, []model.ConversationMessage{self, incoming}, 0, false, true},
		{"用户切换聊天", nil, nil, 0, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			f, p := newReliabilityFlow(t, "")
			f.Browser = &autoReplyBrowserStub{pages: []contract.PageInfo{{URL: "https://example.test/recruit", Current: true}}}
			p.Platform.Behavior.AutoReplySendEnterFirst = true
			c, err := f.Cloud.SaveAutoReplyConversation(ctx, credentials(p), cloud.AutoReplyConversation{PlatformID: p.Platform.ID, PlatformAccountID: p.Request.PlatformAccountID, PlatformThreadID: "thread", PositionID: "position"})
			if err != nil {
				t.Fatal(err)
			}
			r := &delayedDeliveryRuntime{latest: tc.latest, history: tc.history, delay: tc.delay}
			s := model.AutoReplyConversationSnapshot{ExpectedSendMessageKey: anchor.PlatformMessageID}
			got, err := f.sendVerifiedMessage(ctx, p, r, c, s, anchor.PlatformMessageID, "收到", false, 1)
			if got != tc.sent || (err == nil) != tc.sent || r.sends != 1 || (r.windows > 0) != tc.windows {
				t.Fatalf("已确认=%v 发送次数=%d 末条读取=%d 尾窗读取=%d 错误=%v", got, r.sends, r.reads, r.windows, err)
			}
			if tc.sent && tc.delay == 0 && r.reads != 1 {
				t.Fatalf("已有送达证据却重复读末条：%d", r.reads)
			}
			if tc.delay > 0 && r.reads < 2 {
				t.Fatal("迟到的气泡没有再次核实")
			}
			got, err = f.sendVerifiedMessage(ctx, p, r, c, s, anchor.PlatformMessageID, "收到", false, 1)
			if got || r.sends != 1 || (err == nil) != tc.sent {
				t.Fatalf("重复检查不应再次发送：%v %v %d", got, err, r.sends)
			}
		})
	}
}
