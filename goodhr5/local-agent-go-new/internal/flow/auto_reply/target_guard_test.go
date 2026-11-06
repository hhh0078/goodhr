// Package auto_reply 本文件验证限制外会话、持久待办、同名人和发送时身份变化均不能越过单会话守卫。
package auto_reply

import (
	"context"
	"fmt"
	"testing"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// targetRuntimeStub 记录实际打开的会话，并模拟当前页面稳定身份校验结果。
type targetRuntimeStub struct {
	autoReplyRuntimeStub
	openedConversations []model.Conversation
	identityErr         error
}

// VerifyAutoReplyConversationIdentity 返回合成页面的身份结果，不连接真实招聘平台。
func (r *targetRuntimeStub) VerifyAutoReplyConversationIdentity(context.Context, model.Browser, model.Config, model.AutoReplyConversationSnapshot) error {
	return r.identityErr
}

// OpenAutoReplyConversation 记录守卫放行的目标并返回测试快照。
func (r *targetRuntimeStub) OpenAutoReplyConversation(_ context.Context, _ model.Browser, _ model.Config, conversation model.Conversation, _ []string, _ int) (model.AutoReplyConversationSnapshot, error) {
	r.openedConversations = append(r.openedConversations, conversation)
	return r.opened, nil
}

// targetScannerStub 返回包含多个候选人的列表，验证筛选发生在单轮数量限制之前。
type targetScannerStub struct{ conversations []model.Conversation }

// ScanUnreadConversations 返回合成未读列表，不执行页面动作。
func (r targetScannerStub) ScanUnreadConversations(context.Context, model.Browser, model.Config) ([]model.Conversation, error) {
	return r.conversations, nil
}

// targetFixture 生成一份具有完整平台编号的指定会话。
func targetFixture() (shared.AutoReplyTarget, model.AutoReplyConversationSnapshot) {
	target := shared.AutoReplyTarget{CandidateName: "目标候选人", PlatformThreadID: "target-thread", PlatformCandidateID: "target-candidate"}
	conversation := model.Conversation{Name: target.CandidateName, Key: target.PlatformThreadID, PlatformThreadID: target.PlatformThreadID}
	return target, model.AutoReplyConversationSnapshot{Conversation: conversation, CandidateName: target.CandidateName, PlatformThreadID: target.PlatformThreadID, PlatformCandidateID: target.PlatformCandidateID, CommunicationPosition: "工程师"}
}

// TestTargetGuardRejectsOutsideOpenAndChangedIdentity 验证同名不同线程不打开，打开错人不返回数据，身份变化后不发送或索要。
func TestTargetGuardRejectsOutsideOpenAndChangedIdentity(t *testing.T) {
	target, snapshot := targetFixture()
	delegate := &targetRuntimeStub{autoReplyRuntimeStub: autoReplyRuntimeStub{opened: snapshot}}
	runtime, err := restrictAutoReplyRuntime(delegate, &target)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	other := snapshot.Conversation
	other.PlatformThreadID, other.Key = "other-thread", "other-thread"
	if _, err = runtime.OpenAutoReplyConversation(ctx, nil, model.Config{}, other, nil, 1); err == nil || len(delegate.openedConversations) != 0 {
		t.Fatal("同名其他会话被打开", err)
	}
	delegate.opened.PlatformCandidateID = "other-candidate"
	if _, err = runtime.OpenAutoReplyConversation(ctx, nil, model.Config{}, snapshot.Conversation, nil, 1); err == nil {
		t.Fatal("打开后的不同候选人编号未被拒绝")
	}
	delegate.opened = snapshot
	if _, err = runtime.OpenAutoReplyConversation(ctx, nil, model.Config{}, snapshot.Conversation, nil, 1); err != nil {
		t.Fatal(err)
	}
	if delegate.openedConversations[len(delegate.openedConversations)-1].PlatformCandidateID != target.PlatformCandidateID {
		t.Fatal("打开前未传入精确候选人编号")
	}
	delegate.identityErr = fmt.Errorf("页面已切到另一个同名候选人")
	if err = runtime.SendAutoReplyMessage(ctx, nil, model.Config{}, snapshot, "测试回复"); err == nil || delegate.sent {
		t.Fatal("身份变化后仍发送", err)
	}
	if err = runtime.RequestAutoReplyResume(ctx, nil, model.Config{}, snapshot); err == nil || delegate.resumeRequests != 0 {
		t.Fatal("身份变化后仍索要简历", err)
	}
	if _, err = runtime.CollectAutoReplyResume(ctx, nil, model.Config{}, snapshot); err == nil || delegate.resumeCollects != 0 {
		t.Fatal("身份变化后仍采集简历", err)
	}
	if _, _, err = runtime.ReadAutoReplyMessages(ctx, nil, model.Config{}, snapshot, nil, 1); err == nil {
		t.Fatal("身份变化后仍读取聊天")
	}
}

// TestTargetGuardFiltersUnreadAndPersistentPending 验证非目标持久待办和未读均不打开，目标在列表末尾也不会被批次截断。
func TestTargetGuardFiltersUnreadAndPersistentPending(t *testing.T) {
	flow, prepared := newReliabilityFlow(t, "")
	target, snapshot := targetFixture()
	prepared.Request.AutoReplyTarget = &target
	message := model.ConversationMessage{PlatformMessageID: "m", Direction: "candidate", TextContent: "合成问题"}
	snapshot.Messages = []model.ConversationMessage{message}
	delegate := &targetRuntimeStub{autoReplyRuntimeStub: autoReplyRuntimeStub{opened: snapshot, latestBefore: message}}
	runtime, err := restrictAutoReplyRuntime(delegate, &target)
	if err != nil {
		t.Fatal(err)
	}
	flow.Responder = &reliabilityResponder{failure: fmt.Errorf("合成AI失败，保留目标待办")}
	ctx := context.Background()
	other, err := flow.Cloud.SaveAutoReplyConversation(ctx, credentials(prepared), cloud.AutoReplyConversation{PlatformID: prepared.Platform.ID, PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: "other-thread", CandidateName: target.CandidateName, PositionID: "position"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = flow.Cloud.SyncAutoReplyMessages(ctx, credentials(prepared), cloud.AutoReplyMessageSyncRequest{ConversationID: other.ID, Messages: []cloud.AutoReplyMessage{{PlatformMessageID: "outside", Direction: "candidate", TextContent: "不应处理"}}}); err != nil {
		t.Fatal(err)
	}
	scanner := targetScannerStub{conversations: []model.Conversation{{Name: "其他候选人", Key: "other", PlatformThreadID: "other"}, snapshot.Conversation}}
	positions := []cloud.AutoReplyPositionSnapshot{{Position: cloud.AutoReplyPosition{ID: "position", Name: "工程师"}}}
	processed, _, err := flow.processCheckpoint(ctx, prepared, scanner, runtime, positions, 1, &shared.Stats{}, &shared.ConsecutiveErrorPolicy{}, nil)
	if err != nil || processed != 1 || len(delegate.openedConversations) != 1 || delegate.openedConversations[0].PlatformThreadID != target.PlatformThreadID {
		t.Fatalf("单会话限制未覆盖扫描和恢复：processed=%d opened=%+v err=%v", processed, delegate.openedConversations, err)
	}
	pending, err := flow.Cloud.PendingAutoReplyMessages(ctx, credentials(prepared), other.ID)
	if err != nil || len(pending) != 1 {
		t.Fatal("限制外待办被处理或删除", pending, err)
	}
}

// TestTargetGuardRequiresPlatformIdentityProof 验证不具备实时稳定身份能力的平台不能启动限制模式。
func TestTargetGuardRequiresPlatformIdentityProof(t *testing.T) {
	target, _ := targetFixture()
	delegate := &autoReplyRuntimeStub{}
	if _, err := restrictAutoReplyRuntime(delegate, &target); err == nil {
		t.Fatal("无身份校验的平台被放行")
	}
	if runtime, err := restrictAutoReplyRuntime(delegate, nil); err != nil || runtime != delegate {
		t.Fatal("普通模式行为改变", err)
	}
}
