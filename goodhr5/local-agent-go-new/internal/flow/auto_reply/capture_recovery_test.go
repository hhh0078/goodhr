// Package auto_reply 本文件验证点击清除未读后采集失败的持久恢复，以及恢复时继续执行指定候选人限制。
package auto_reply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// TestCheckpointSkipsPurgedUnreadConversation 验证已删除的采集目标即使长期位于未读首位，也不会占据名额或停止其他会话。
func TestCheckpointSkipsPurgedUnreadConversation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/me" {
			t.Errorf("未预期的请求 %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"user": cloud.UserSession{UserID: "test-owner"}})
	}))
	defer server.Close()
	store, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	client := cloud.New(server.URL)
	client.EnableLocalAutoReply(store, dir)
	prepared := shared.PreparedTask{Request: shared.StartRequest{Token: "token", TaskID: "test", PlatformAccountID: "account"}, MachineID: "machine", Platform: model.Config{ID: "liepin", Name: "猎聘"}}
	if _, err = client.ValidateSession(ctx, "token"); err != nil {
		t.Fatal(err)
	}
	if err = client.BeginAutoReplyCapture(ctx, credentials(prepared), "liepin", "account", "removed", cloud.AutoReplyCaptureIntent{PlatformCandidateID: "distinct", CandidateName: "删除目标"}); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte("liepin\x00distinct"))
	if err = client.PurgeLocalAutoReplyCandidate(ctx, credentials(prepared), "deleted", []string{hex.EncodeToString(hash[:])}); err != nil {
		t.Fatal(err)
	}
	scanner := captureUnreadScanner{[]model.Conversation{{PlatformThreadID: "removed", PlatformCandidateID: "distinct", Name: "删除目标"}, {PlatformThreadID: "other", PlatformCandidateID: "other", Name: "保留目标"}}}
	runtime := &captureRecoveryRuntime{openError: fmt.Errorf("保留目标进入正常采集")}
	flow := &Flow{Store: store, Cloud: client, Browser: &autoReplyBrowserStub{}}
	stats := &shared.Stats{}
	processed, _, err := flow.processCheckpoint(ctx, prepared, scanner, runtime, nil, 1, stats, &shared.ConsecutiveErrorPolicy{}, nil)
	if err != nil || processed != 1 || runtime.opens != 1 || stats.Processed != 1 || stats.Failed != 1 {
		t.Fatalf("删除目标干扰其他候选人：processed=%d opens=%d stats=%+v err=%v", processed, runtime.opens, stats, err)
	}
	state, err := client.AutoReplyCandidateState(ctx, credentials(prepared), cloud.AutoReplyCandidateLookup{PlatformID: "liepin", PlatformAccountID: "account", PlatformThreadID: "other"})
	if err != nil || state.Conversation == nil || state.Conversation.CaptureIntent == nil {
		t.Fatalf("正常候选人没有取得本轮采集名额 %v", err)
	}
}

// TestCheckpointYieldsBusyChatWithoutLosingItsIncoming 验证单名额下其他未读能轮到，已打开聊天的新来信同时保留恢复线索。
func TestCheckpointYieldsBusyChatWithoutLosingItsIncoming(t *testing.T) {
	ctx := context.Background()
	flow, prepared := newReliabilityFlow(t, "")
	prepared.Position.ID = "position"
	positions := []cloud.AutoReplyPositionSnapshot{{Position: cloud.AutoReplyPosition{ID: "position", Name: "工程师"}}}
	busy := model.Conversation{Key: "busy", PlatformThreadID: "busy", Name: "候选人甲"}
	other := model.Conversation{Key: "other", PlatformThreadID: "other", Name: "候选人乙"}
	current := &openAutoReplyConversation{conversation: busy, scanNext: true, snapshot: model.AutoReplyConversationSnapshot{CandidateName: busy.Name, PlatformThreadID: busy.PlatformThreadID}}
	runtime := &captureRecoveryRuntime{autoReplyRuntimeStub: autoReplyRuntimeStub{
		latestBefore: model.ConversationMessage{PlatformMessageID: "arrived-in-open-chat", Direction: "candidate", TextContent: "还有一个问题"},
		opened:       model.AutoReplyConversationSnapshot{CandidateName: other.Name, PlatformThreadID: other.PlatformThreadID, CommunicationPosition: "工程师"},
	}}
	scanner := captureUnreadScanner{[]model.Conversation{busy, other}}
	n, next, err := flow.processCheckpoint(ctx, prepared, scanner, runtime, positions, 1, &shared.Stats{}, &shared.ConsecutiveErrorPolicy{}, current)
	if err != nil || n != 1 || next == nil || next.conversation.Key != other.Key || runtime.opens != 1 {
		t.Fatalf("其他未读没有轮到：n=%d next=%+v opens=%d err=%v", n, next, runtime.opens, err)
	}
	pending, err := flow.Cloud.ListPendingAutoReplyConversations(ctx, credentials(prepared), prepared.Platform.ID, prepared.Request.PlatformAccountID)
	if err != nil || len(pending) != 1 || pending[0].CaptureIntent == nil || pending[0].CaptureIntent.PlatformThreadID != busy.PlatformThreadID {
		t.Fatalf("离开的已读聊天缺少恢复线索：pending=%+v err=%v", pending, err)
	}
}
