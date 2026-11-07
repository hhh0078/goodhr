// Package auto_reply 本文件验证已删除的平台会话重新联系时，只有核身成功的新入站可以建立新代会话。
package auto_reply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

type recontactRuntimeStub struct {
	autoReplyRuntimeStub
	messages []model.ConversationMessage
	after    time.Time
}

// InspectAutoReplyContact 模拟从聊天头核实来源，不打开旧消息或旧附件。
func (r *recontactRuntimeStub) InspectAutoReplyContact(_ context.Context, _ model.Browser, _ model.Config, c model.Conversation) (model.AutoReplyConversationSnapshot, error) {
	return model.AutoReplyConversationSnapshot{Conversation: c, CandidateName: c.Name, PlatformThreadID: c.PlatformThreadID, PlatformAccountID: c.PlatformAccountID, PlatformCandidateID: "source"}, nil
}

// ReadAutoReplyMessages 验证公共流程已经将可信时间传给页面能力，再返回指定的合成消息。
func (r *recontactRuntimeStub) ReadAutoReplyMessages(_ context.Context, _ model.Browser, _ model.Config, s model.AutoReplyConversationSnapshot, _ []string, _ int) ([]model.ConversationMessage, bool, error) {
	if s.HistoryAfter == nil || !s.HistoryAfter.Equal(r.after) {
		return nil, false, errors.New("未限制可读取的消息时间")
	}
	return r.messages, true, nil
}

// TestRecontactFlowRejectsOldContentAndSelfOnly 通过真实 SQLite 删除事件验证过去的消息和 HR 自己的留言均不能重建档案。
func TestRecontactFlowRejectsOldContentAndSelfOnly(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"user": cloud.UserSession{UserID: "test-owner"}})
	}))
	defer server.Close()
	dir := t.TempDir()
	store, err := storage.Open(filepath.Join(dir, "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	client := cloud.New(server.URL)
	client.EnableLocalAutoReply(store, dir)
	if _, err = client.ValidateSession(ctx, "token"); err != nil {
		t.Fatal(err)
	}
	f := Flow{Cloud: client, Store: store}
	prepared := shared.PreparedTask{Request: shared.StartRequest{Token: "token", PlatformAccountID: "account"}, MachineID: "machine", Platform: model.Config{ID: "liepin"}}
	cred := credentials(prepared)
	old := time.Now().UTC().Add(-time.Hour)
	fresh := old.Add(time.Minute)
	hash := sha256.Sum256([]byte("liepin\x00source"))
	digest := hex.EncodeToString(hash[:])
	event := cloud.CandidateDeletion{Sequence: 11, CandidateID: "old", DeletedAt: old, SourceHashes: []string{digest}}
	if err = client.ApplyAutoReplyCandidateDeletion(ctx, cred, event); err != nil {
		t.Fatal(err)
	}
	c := model.Conversation{PlatformThreadID: "thread", PlatformAccountID: "account", Name: "候选人"}
	q := cloud.AutoReplyCandidateLookup{PlatformID: "liepin", PlatformAccountID: "account", PlatformThreadID: "thread", PlatformCandidateID: "source"}
	intent := cloud.AutoReplyCaptureIntent{PlatformThreadID: "thread", CandidateName: c.Name, PositionID: "position-does-not-exist"}
	boundary, err := client.AutoReplyDeletionBoundary(ctx, cred, q)
	if err != nil || boundary == nil {
		t.Fatal(err)
	}
	rt := &recontactRuntimeStub{after: old}
	for _, row := range []struct {
		dir   string
		sent  *time.Time
		works bool
	}{{"candidate", &old, false}, {"self", &fresh, false}, {"candidate", &fresh, true}} {
		rt.messages = []model.ConversationMessage{{Key: "test-new", TextContent: "请问职位", Direction: row.dir, SentAt: row.sent}}
		_, err = f.readPostDeletionConversation(ctx, prepared, rt, c, nil, nil, q, intent, *boundary, cloud.AutoReplyCandidateState{}, nil)
		if (err == nil) != row.works {
			t.Fatalf("方向%s不符合预期: %v", row.dir, err)
		}
		st, e := client.AutoReplyCandidateState(ctx, cred, q)
		if e != nil || (st.Candidate != nil) != row.works {
			t.Fatalf("实际建档与入站证据不一致 %+v %v", st, e)
		}
	}
	// 模拟已重建会话的一轮正常同步，再让下一轮在打开后核对时间时失败；已读红点消失后仍须能恢复。
	st, err := client.AutoReplyCandidateState(ctx, cred, q)
	if err != nil || st.Conversation == nil {
		t.Fatal("缺少重建会话", err)
	}
	if _, err = client.SyncAutoReplyMessages(ctx, cred, cloud.AutoReplyMessageSyncRequest{ConversationID: st.Conversation.ID, CaptureCompleted: true}); err != nil {
		t.Fatal(err)
	}
	rt.messages = []model.ConversationMessage{{Key: "uncertain-time", Direction: "candidate"}}
	if _, err = f.readPostDeletionConversation(ctx, prepared, rt, c, nil, nil, q, intent, *boundary, st, nil); err == nil {
		t.Fatal("无法核实时间时不应完成")
	}
	pending, err := client.ListPendingAutoReplyConversations(ctx, cred, q.PlatformID, q.PlatformAccountID)
	if err != nil || len(pending) != 1 || pending[0].CaptureIntent == nil || pending[0].ID != st.Conversation.ID {
		t.Fatalf("新代未完成采集应能在未读角标消失后恢复: %+v %v", pending, err)
	}
}
