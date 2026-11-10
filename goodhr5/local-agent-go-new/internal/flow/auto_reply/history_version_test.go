// Package auto_reply 本文件验证旧版本会话只完整重采一次，失败保留待办，成功后不重放已经回答的历史。
package auto_reply

import (
	"context"
	"fmt"
	"testing"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// historyVersionRuntime 记录流程是否真正移除旧增量锚点，并模拟到顶成功或截尾失败。
type historyVersionRuntime struct {
	autoReplyRuntimeStub
	fullReads int
	readErr   error
}

// OpenAutoReplyConversation 仅在无增量锚点时返回准备好的完整历史。
func (r *historyVersionRuntime) OpenAutoReplyConversation(_ context.Context, _ model.Browser, _ model.Config, _ model.Conversation, known []string, _ int) (model.AutoReplyConversationSnapshot, error) {
	if len(known) != 0 {
		return model.AutoReplyConversationSnapshot{}, fmt.Errorf("旧标识版本仍使用增量锚点")
	}
	r.fullReads++
	return r.opened, r.readErr
}

// TestHistoryVersionFlowReadsFullOnlyOnce 验证旧87条首次补到89条，后续轮询及重启都不再全量读取或发送。
func TestHistoryVersionFlowReadsFullOnlyOnce(t *testing.T) {
	for _, failure := range []string{"", "read-failed", "not-complete"} {
		t.Run(failure, func(t *testing.T) {
			f, prepared := newReliabilityFlow(t, "")
			ctx := context.Background()
			cred := credentials(prepared)
			prepared.Platform.Behavior.AutoReplyMessageIdentityVersion = 1
			v, err := f.Cloud.SaveAutoReplyConversation(ctx, cred, cloud.AutoReplyConversation{PlatformID: prepared.Platform.ID, PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: "thread", CandidateName: "候选人", PositionID: "position", PagePositionText: "工程师"})
			if err != nil {
				t.Fatal(err)
			}
			full := []model.ConversationMessage{}
			old := []cloud.AutoReplyMessage{}
			for i := 0; i < 89; i++ {
				m := model.ConversationMessage{Key: fmt.Sprintf("m%d", i), Direction: "candidate", MessageType: "text", TextContent: fmt.Sprintf("历史%d", i)}
				if i == 56 || i == 84 {
					m.TextContent = fmt.Sprintf("历史%d", i-1)
				}
				if i == 88 {
					m.Direction = "self"
				}
				full = append(full, m)
				if i != 56 && i != 84 {
					old = append(old, cloud.AutoReplyMessage{Fingerprint: m.Key, Direction: m.Direction, MessageType: m.MessageType, TextContent: m.TextContent})
				}
			}
			if _, err = f.Cloud.SyncAutoReplyMessages(ctx, cred, cloud.AutoReplyMessageSyncRequest{ConversationID: v.ID, HistoryComplete: true, Messages: old}); err != nil {
				t.Fatal(err)
			}
			runtime := &historyVersionRuntime{autoReplyRuntimeStub: autoReplyRuntimeStub{opened: model.AutoReplyConversationSnapshot{CandidateName: "候选人", PlatformThreadID: "thread", CommunicationPosition: "工程师", HistoryComplete: true, Messages: full}, latestBefore: full[88]}}
			if failure == "read-failed" {
				runtime.readErr = fmt.Errorf("合成历史读取超时")
			}
			if failure == "not-complete" {
				runtime.opened.HistoryComplete = false
			}
			positions := []cloud.AutoReplyPositionSnapshot{{Position: cloud.AutoReplyPosition{ID: "position", Name: "工程师"}, Config: cloud.PositionAutoReplyConfig{Enabled: true}}}
			if err := f.queueHistoryRefresh(ctx, prepared, positions); err != nil {
				t.Fatal(err)
			}
			processed, current, err := f.processCheckpoint(ctx, prepared, unreadScannerStub{}, runtime, positions, 1, &shared.Stats{}, &shared.ConsecutiveErrorPolicy{}, nil)
			if err != nil || processed != 1 || runtime.fullReads != 1 || runtime.sent {
				t.Fatalf("首次重采不正确：processed=%d reads=%d sent=%v err=%v", processed, runtime.fullReads, runtime.sent, err)
			}
			state, err := f.Cloud.AutoReplyCandidateState(ctx, cred, cloud.AutoReplyCandidateLookup{PlatformID: prepared.Platform.ID, PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: "thread"})
			if err != nil {
				t.Fatal(err)
			}
			if failure != "" {
				if state.Conversation.MessageIdentityVersion != 0 || state.Conversation.CaptureIntent == nil {
					t.Fatalf("失败升级或丢失待办：%+v", state.Conversation)
				}
				return
			}
			if state.Conversation.MessageIdentityVersion != 1 || state.Conversation.CaptureIntent != nil {
				t.Fatalf("完成后没有升级：%+v", state.Conversation)
			}
			messages, err := f.Cloud.AutoReplyMessages(ctx, cred, v.ID)
			if err != nil || len(messages) != 89 {
				t.Fatalf("没有补齐89条：%d %v", len(messages), err)
			}
			for _, open := range []*openAutoReplyConversation{current, nil} {
				processed, _, err = f.processCheckpoint(ctx, prepared, unreadScannerStub{}, runtime, positions, 1, &shared.Stats{}, &shared.ConsecutiveErrorPolicy{}, open)
				if err != nil || processed != 0 || runtime.fullReads != 1 || runtime.sent {
					t.Fatalf("已完成版本仍全量重采或回复：processed=%d reads=%d err=%v", processed, runtime.fullReads, err)
				}
			}
		})
	}
}

// TestHistoryVersionKeepsEmptyConversationIdle 验证没有任何旧消息的空会话不因版本标记进入失败或重采循环。
func TestHistoryVersionKeepsEmptyConversationIdle(t *testing.T) {
	f, prepared := newReliabilityFlow(t, "")
	prepared.Platform.Behavior.AutoReplyMessageIdentityVersion = 1
	ctx := context.Background()
	runtime := &historyVersionRuntime{autoReplyRuntimeStub: autoReplyRuntimeStub{opened: model.AutoReplyConversationSnapshot{CandidateName: "空会话", PlatformThreadID: "empty", CommunicationPosition: "工程师"}}}
	positions := []cloud.AutoReplyPositionSnapshot{{Position: cloud.AutoReplyPosition{ID: "position", Name: "工程师"}, Config: cloud.PositionAutoReplyConfig{Enabled: true}}}
	snapshot := model.AutoReplyConversationSnapshot{}
	if err := f.processConversation(ctx, prepared, runtime, positions, model.Conversation{Key: "empty", PlatformThreadID: "empty", Name: "空会话"}, &snapshot, &shared.Stats{}); err != nil {
		t.Fatalf("空会话被版本升级阻断：%v", err)
	}
	if err := f.queueHistoryRefresh(ctx, prepared, positions); err != nil {
		t.Fatal(err)
	}
	state, err := f.Cloud.AutoReplyCandidateState(ctx, credentials(prepared), cloud.AutoReplyCandidateLookup{PlatformID: prepared.Platform.ID, PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: "empty"})
	if err != nil || state.Conversation == nil || state.Conversation.MessageIdentityVersion != 0 || state.Conversation.CaptureIntent != nil || runtime.sent {
		t.Fatalf("空会话被错误升级或重新采集：%+v sent=%v err=%v", state.Conversation, runtime.sent, err)
	}
	pending, err := f.Cloud.ListPendingAutoReplyConversations(ctx, credentials(prepared), prepared.Platform.ID, prepared.Request.PlatformAccountID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("空会话产生待办：%+v %v", pending, err)
	}
}
