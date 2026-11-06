// Package auto_reply 本文件验证点击清除未读后采集失败的持久恢复，以及恢复时继续执行指定候选人限制。
package auto_reply

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// captureRecoveryRuntime 模拟点击已清未读，但随后历史读取失败，禁止访问真实招聘页面。
type captureRecoveryRuntime struct {
	autoReplyRuntimeStub
	openError     error
	opens         int
	verifications int
}

// OpenAutoReplyConversation 返回首次历史失败或测试快照，并记录是否真的重新打开目标。
func (r *captureRecoveryRuntime) OpenAutoReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, conversation model.Conversation, known []string, limit int) (model.AutoReplyConversationSnapshot, error) {
	r.opens++
	if r.openError != nil {
		return model.AutoReplyConversationSnapshot{}, r.openError
	}
	return r.autoReplyRuntimeStub.OpenAutoReplyConversation(ctx, browser, cfg, conversation, known, limit)
}

// VerifyAutoReplyConversationIdentity 记录限定目标在恢复后仍必须执行的页面身份核验。
func (r *captureRecoveryRuntime) VerifyAutoReplyConversationIdentity(context.Context, model.Browser, model.Config, model.AutoReplyConversationSnapshot) error {
	r.verifications++
	return nil
}

// captureUnreadScanner 仅返回测试指定的未读列表，下一轮可为空模拟已读状态。
type captureUnreadScanner struct{ items []model.Conversation }

// ScanUnreadConversations 返回合成列表，不读取平台或用户数据。
func (s captureUnreadScanner) ScanUnreadConversations(context.Context, model.Browser, model.Config) ([]model.Conversation, error) {
	return s.items, nil
}

// TestHistoryFailureRecoversAfterDatabaseReopen 验证首次打开后历史失败，即使未读已清且进程重启仍恢复同一会话。
func TestHistoryFailureRecoversAfterDatabaseReopen(t *testing.T) {
	directory := t.TempDir()
	database := filepath.Join(directory, "capture.db")
	newFlow := func() *Flow {
		store, err := storage.Open(database)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		client := cloud.New("")
		client.EnableLocalAutoReply(store, directory)
		return &Flow{Browser: &autoReplyBrowserStub{}, Store: store, Cloud: client}
	}
	ctx := context.Background()
	flow := newFlow()
	prepared := shared.PreparedTask{Request: shared.StartRequest{TaskID: "first-task", Token: "token", PositionID: "position", PlatformAccountID: "bound-account", AutoReplyTarget: &shared.AutoReplyTarget{CandidateName: "候选人甲", PlatformThreadID: "thread", PlatformCandidateID: "candidate"}}, MachineID: "machine", Platform: model.Config{ID: "boss", Name: "BOSS"}}
	positions := []cloud.AutoReplyPositionSnapshot{{Position: cloud.AutoReplyPosition{ID: "position", Name: "工程师"}}}
	conversation := model.Conversation{Key: "thread", PlatformThreadID: "thread", Name: "候选人甲"}
	runtime := &captureRecoveryRuntime{openError: fmt.Errorf("点击后历史读取超时")}
	guarded, err := restrictAutoReplyRuntime(runtime, prepared.Request.AutoReplyTarget)
	if err != nil {
		t.Fatal(err)
	}
	processed, opened, err := flow.processCheckpoint(ctx, prepared, captureUnreadScanner{[]model.Conversation{conversation}}, guarded, positions, 3, &shared.Stats{}, &shared.ConsecutiveErrorPolicy{}, nil)
	if err != nil || processed != 1 || runtime.opens != 1 || opened != nil {
		t.Fatalf("首次历史失败状态不正确：processed=%d opens=%d opened=%v err=%v", processed, runtime.opens, opened, err)
	}
	state, err := flow.Cloud.AutoReplyCandidateState(ctx, credentials(prepared), cloud.AutoReplyCandidateLookup{PlatformID: "boss", PlatformAccountID: "bound-account", PlatformThreadID: "thread"})
	if err != nil || state.Conversation == nil || state.Conversation.CaptureIntent == nil || state.Candidate != nil || state.Identity != nil || state.Conversation.LastSyncedMessageKey != "" {
		t.Fatalf("历史失败只能保留采集意图：%+v %v", state, err)
	}
	if err = flow.Store.Close(); err != nil {
		t.Fatal(err)
	}
	flow = newFlow()
	prepared.Request.TaskID = "restarted-task"
	responder := &reliabilityResponder{failure: fmt.Errorf("模拟后续AI超时")}
	flow.Responder = responder
	message := model.ConversationMessage{PlatformMessageID: "question", Direction: "candidate", MessageType: "text", TextContent: "工作地点？"}
	runtime = &captureRecoveryRuntime{autoReplyRuntimeStub: autoReplyRuntimeStub{opened: model.AutoReplyConversationSnapshot{Conversation: conversation, CandidateName: conversation.Name, PlatformThreadID: "thread", PlatformCandidateID: "candidate", CommunicationPosition: "工程师", Messages: []model.ConversationMessage{message}}, latestBefore: message}}
	guarded, err = restrictAutoReplyRuntime(runtime, prepared.Request.AutoReplyTarget)
	if err != nil {
		t.Fatal(err)
	}
	processed, _, err = flow.processCheckpoint(ctx, prepared, captureUnreadScanner{}, guarded, positions, 3, &shared.Stats{}, &shared.ConsecutiveErrorPolicy{}, nil)
	if err != nil || processed != 1 || runtime.opens != 1 || runtime.verifications == 0 || responder.calls != 1 || len(responder.pending) != 1 {
		t.Fatalf("清未读并重启后没有恢复到受保护的问答：processed=%d opens=%d verified=%d ai=%d pending=%d err=%v", processed, runtime.opens, runtime.verifications, responder.calls, len(responder.pending), err)
	}
	pending, err := flow.Cloud.ListPendingAutoReplyConversations(ctx, credentials(prepared), "boss", "bound-account")
	if err != nil || len(pending) != 1 || pending[0].CaptureIntent != nil {
		t.Fatalf("同步成功后AI失败应转为消息待办：%+v %v", pending, err)
	}
}

// TestCaptureRecoveryKeepsTargetAndEnabledPositionScope 验证恢复不会打开指定目标外会话，关闭启动岗位后不恢复旧采集。
func TestCaptureRecoveryKeepsTargetAndEnabledPositionScope(t *testing.T) {
	flow, prepared := newReliabilityFlow(t, "")
	prepared.Request.AutoReplyTarget = &shared.AutoReplyTarget{CandidateName: "候选人甲", PlatformThreadID: "allowed", PlatformCandidateID: "candidate"}
	ctx := context.Background()
	for _, intent := range []cloud.AutoReplyCaptureIntent{{PositionID: "position", PlatformThreadID: "other", CandidateName: "候选人甲"}, {PositionID: "disabled", PlatformThreadID: "allowed", CandidateName: "候选人甲"}} {
		if err := flow.Cloud.BeginAutoReplyCapture(ctx, credentials(prepared), "boss", prepared.Request.PlatformAccountID, intent.PlatformThreadID, intent); err != nil {
			t.Fatal(err)
		}
	}
	runtime := &captureRecoveryRuntime{}
	processed, _, err := flow.processCheckpoint(ctx, prepared, captureUnreadScanner{}, runtime, []cloud.AutoReplyPositionSnapshot{{Position: cloud.AutoReplyPosition{ID: "position"}}}, 3, &shared.Stats{}, &shared.ConsecutiveErrorPolicy{}, nil)
	if err != nil || processed != 0 || runtime.opens != 0 {
		t.Fatalf("恢复越过目标或岗位开关：processed=%d opens=%d err=%v", processed, runtime.opens, err)
	}
}
