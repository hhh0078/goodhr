// Package cloud 本文件用独立 SQLite 和 HTTP 替身验证重新联系、旧写入隔离、附件证明、重启及重复删除。
package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"goodhr5/local-agent-go-new/internal/storage"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestNewContactAfterDeletion 验证同一平台线程可用新消息建立新代；旧回包、旧消息和旧附件不能进入新代。
func TestNewContactAfterDeletion(t *testing.T) {
	var received autoReplyCandidateRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auto-reply/agent/candidates" {
			t.Errorf("意外请求 %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"ok":true,"candidate_id":"new-cloud-id"}`))
	}))
	defer server.Close()
	c, store, cred := purgeTestClient(t, server.URL)
	ctx := context.Background()
	q := AutoReplyCandidateLookup{PlatformID: "liepin", PlatformAccountID: "account", PlatformThreadID: "same-thread", PlatformCandidateID: "person"}
	intent := AutoReplyCaptureIntent{PlatformThreadID: q.PlatformThreadID, PlatformCandidateID: q.PlatformCandidateID, CandidateName: "合成人选", PositionID: "position"}
	clock := time.Now().UTC().Truncate(time.Minute).Add(-5 * time.Minute)
	event := CandidateDeletion{Sequence: 3, CandidateID: "old-cloud", DeletedAt: clock, SourceHashes: []string{autoReplyDigest("liepin\x00person")}}
	if err := c.BeginAutoReplyCapture(ctx, cred, q.PlatformID, q.PlatformAccountID, q.PlatformThreadID, intent); err != nil {
		t.Fatal(err)
	}
	if err := c.ApplyAutoReplyCandidateDeletion(ctx, cred, event); err != nil {
		t.Fatal(err)
	}
	b, err := c.AutoReplyDeletionBoundary(ctx, cred, q)
	if err != nil || b == nil || b.Sequence != 3 {
		t.Fatal("缺少边界", b, err)
	}
	noCID := q
	noCID.PlatformCandidateID = ""
	if err := c.BeginAutoReplyContactProbe(ctx, cred, noCID, intent); err != nil {
		t.Fatal(err)
	}
	restart := New(server.URL)
	restart.EnableLocalAutoReply(store, c.localAutoDir)
	restart.localAutoOwners.Store(autoReplyDigest(cred.Token), "owner-A")
	c = restart
	pending, err := c.ListPendingAutoReplyConversations(ctx, cred, q.PlatformID, q.PlatformAccountID)
	if err != nil || len(pending) != 1 || pending[0].CaptureIntent.PlatformThreadID != q.PlatformThreadID {
		t.Fatal("重启丢失核实线索", err)
	}
	at := clock.Add(time.Minute)
	first := AutoReplyMessage{PlatformMessageID: "new-message", Direction: "candidate", MessageType: "text", PlatformSentAt: &at}
	for _, invalid := range []AutoReplyMessage{{PlatformMessageID: "same-minute", Direction: "candidate", PlatformSentAt: &clock}, {PlatformMessageID: "self", Direction: "self", PlatformSentAt: &at}, {Direction: "candidate", PlatformSentAt: &at}} {
		if _, err := c.ActivateAutoReplyContact(ctx, cred, q, *b, invalid, intent); !errors.Is(err, storage.ErrAutoReplyCandidateDeleted) {
			t.Fatal("不可信消息激活", err)
		}
	}
	one, err := c.ActivateAutoReplyContact(ctx, cred, q, *b, first, intent)
	if err != nil {
		t.Fatal(err)
	}
	if one.ID == originalContactID(q) || one.PlatformThreadID != q.PlatformThreadID || one.CandidateID == "" {
		t.Fatal("新代未隔离或平台线程变化")
	}
	repeated, err := c.ActivateAutoReplyContact(ctx, cred, q, *b, first, intent)
	if err != nil || repeated.ID != one.ID || repeated.CandidateID != one.CandidateID {
		t.Fatal("重复激活", err)
	}
	if err = c.ApplyAutoReplyCandidateDeletion(ctx, cred, event); err != nil {
		t.Fatal(err)
	}
	state, err := c.AutoReplyCandidateState(ctx, cred, q)
	if err != nil || state.Conversation == nil || state.Conversation.ID != one.ID || state.Candidate.ID != one.CandidateID {
		t.Fatal("删除事件重投误删新资料", err)
	}
	old := AutoReplyConversation{ID: originalContactID(q), PlatformID: q.PlatformID, PlatformAccountID: q.PlatformAccountID, PlatformThreadID: q.PlatformThreadID}
	if _, err := c.SaveAutoReplyConversation(ctx, cred, old); !errors.Is(err, storage.ErrAutoReplyCandidateDeleted) {
		t.Fatal("旧任务回写成功", err)
	}
	input := AutoReplyCandidateInput{PlatformID: q.PlatformID, PlatformAccountID: q.PlatformAccountID, PlatformCandidateID: q.PlatformCandidateID, LocalCandidateID: one.CandidateID, PositionID: "position", ResumeSourceMessageID: "old-card", StructuredCandidate: StructuredCandidate{CandidateName: "合成人选", Phone: "13800138000"}}
	if _, err := c.saveLocalAutoReplyCandidate(ctx, cred, input); err == nil {
		t.Fatal("旧卡片能保存到新简历")
	}
	if err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		_, _, e := syncLocalAutoReplyMessages(tx, AutoReplyMessageSyncRequest{ConversationID: one.ID, Messages: []AutoReplyMessage{{Direction: "candidate", Fingerprint: "old-card", MessageType: "resume", PlatformSentAt: &clock}}})
		return e
	}); err == nil {
		t.Fatal("删除时或更早的历史可写入")
	}
	attachmentAt := at.Add(time.Minute)
	if err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		_, _, e := syncLocalAutoReplyMessages(tx, AutoReplyMessageSyncRequest{ConversationID: one.ID, Messages: []AutoReplyMessage{{Direction: "candidate", Fingerprint: "fresh-card", MessageType: "resume", PlatformSentAt: &attachmentAt}}})
		return e
	}); err != nil {
		t.Fatal(err)
	}
	input.ResumeSourceMessageID = "fresh-card"
	if _, err := c.saveLocalAutoReplyCandidate(ctx, cred, input); err == nil {
		t.Fatal("尚未取得真实附件就能正式建档")
	}
	msgs, err := c.AutoReplyMessages(ctx, cred, one.ID)
	if err != nil {
		t.Fatal(err)
	}
	var localRow string
	for _, m := range msgs {
		if m.Fingerprint == "fresh-card" {
			localRow = m.ID
		}
	}
	pdf := filepath.Join(t.TempDir(), "合成.pdf")
	if err = os.WriteFile(pdf, []byte("synthetic attachment bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = c.saveLocalAutoReplyAttachment(ctx, cred, AutoReplyAttachmentUpload{ConversationID: one.ID, SourceMessageID: localRow, PlatformID: "liepin", FilePath: pdf}); err != nil {
		t.Fatal("本地附件来源核验失败", err)
	}

	if _, err := c.saveLocalAutoReplyCandidate(ctx, cred, input); err != nil {
		t.Fatal(err)
	}
	if err = c.SyncAutoReplyStructuredCandidate(ctx, cred, one.CandidateID, input); err != nil {
		t.Fatal(err)
	}
	proof := received.ReopenProof
	if proof == nil || proof.DeletionSequence != 3 || proof.InboundMessageHash != autoReplyDigest("new-message") || proof.AttachmentMessageHash != autoReplyDigest("fresh-card") || !proof.AttachmentAt.Equal(attachmentAt) {
		t.Fatal("云端上传依据不完整", proof)
	}
	event.CandidateID = "new-cloud-id"
	event.Sequence = 5
	event.DeletedAt = attachmentAt.Add(time.Minute)
	if err = c.ApplyAutoReplyCandidateDeletion(ctx, cred, event); err != nil {
		t.Fatal(err)
	}
	if _, err = c.ActivateAutoReplyContact(ctx, cred, q, *b, first, intent); !errors.Is(err, storage.ErrAutoReplyCandidateDeleted) {
		t.Fatal("二次删除仍接受旧代", err)
	}
	next, err := c.AutoReplyDeletionBoundary(ctx, cred, q)
	if err != nil || next.Sequence != 5 {
		t.Fatal("缺少最新边界", err)
	}
	newAt := event.DeletedAt.Add(time.Minute)
	first.PlatformSentAt = &newAt
	first.PlatformMessageID = "generation-three"
	third, err := c.ActivateAutoReplyContact(ctx, cred, q, *next, first, intent)
	if err != nil || third.ID == one.ID || third.CandidateID == one.CandidateID {
		t.Fatal("再次联系未进入新代", err)
	}
}

// TestDeletionFeedUpgradesOldCursorOnce 验证老云先同步无时间事件，新云上线后即使当前页为空也补收一次。
func TestDeletionFeedUpgradesOldCursorOnce(t *testing.T) {
	var cursors []string
	version := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cursor := r.URL.Query().Get("after")
		cursors = append(cursors, cursor)
		var events []CandidateDeletion
		if cursor == "0" {
			events = []CandidateDeletion{{Sequence: 1, CandidateID: "cloud"}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "boundary_version": version, "events": events, "next_cursor": 1})
	}))
	defer server.Close()
	c, _, cred := purgeTestClient(t, server.URL)
	ctx := context.Background()
	apply := func(context.Context, CandidateDeletion) error { return nil }
	for _, v := range []int{0, 0, 1, 1} {
		version = v
		if err := c.SyncCandidateDeletions(ctx, cred, apply); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.Join(cursors, ","); got != "0,1,1,0,1" {
		t.Fatal("旧游标升级次数错误", got)
	}
}

// TestBoundaryWithoutDeletePermission 验证新设备收下截止时间但不按已重新使用的来源删除其他会话。
func TestBoundaryWithoutDeletePermission(t *testing.T) {
	c, _, cred := purgeTestClient(t, "")
	ctx := context.Background()
	q := AutoReplyCandidateLookup{PlatformID: "liepin", PlatformAccountID: "other-account", PlatformThreadID: "other-thread", PlatformCandidateID: "reused-id"}
	intent := AutoReplyCaptureIntent{CandidateName: "独立合成人选", PlatformCandidateID: q.PlatformCandidateID, PlatformThreadID: q.PlatformThreadID}
	if err := c.BeginAutoReplyCapture(ctx, cred, q.PlatformID, q.PlatformAccountID, q.PlatformThreadID, intent); err != nil {
		t.Fatal(err)
	}
	event := CandidateDeletion{Sequence: 3, CandidateID: "obsolete-cloud", DeletedAt: time.Now().UTC(), BoundarySourceHashes: []string{autoReplyDigest("liepin\x00reused-id")}}
	if err := c.ApplyAutoReplyCandidateDeletion(ctx, cred, event); err != nil {
		t.Fatal(err)
	}
	state, err := c.AutoReplyCandidateState(ctx, cred, q)
	if err != nil || state.Conversation == nil || state.Conversation.ID != originalContactID(q) {
		t.Fatal("仅用于截止时间的来源删除了其他活跃会话", err)
	}
	q.PlatformAccountID = "cold-install"
	q.PlatformThreadID = "unknown-thread"
	q.PlatformCandidateID = ""
	any, err := c.HasAutoReplyDeletionBoundaries(ctx, cred)
	if err != nil || !any {
		t.Fatal("新设备未发现来源边界", err)
	}
	bound, err := c.AutoReplyDeletionBoundary(ctx, cred, q)
	if err != nil || bound != nil {
		t.Fatal("未知线程冒用了他人归属")
	}
	intent.PlatformThreadID = q.PlatformThreadID
	intent.PlatformCandidateID = ""
	if err = c.BeginAutoReplyContactProbe(ctx, cred, q, intent); err != nil {
		t.Fatal("缺编号的新设备无法保留核实意图", err)
	}
	q.PlatformCandidateID = "reused-id"
	intent.PlatformCandidateID = q.PlatformCandidateID
	if err = c.BeginAutoReplyCapture(ctx, cred, q.PlatformID, q.PlatformAccountID, q.PlatformThreadID, intent); !errors.Is(err, storage.ErrAutoReplyCandidateDeleted) {
		t.Fatal("新设备可绕过边界完整采集", err)
	}
	if bound, err = c.AutoReplyDeletionBoundary(ctx, cred, q); err != nil || bound == nil || bound.Sequence != 3 {
		t.Fatal("已核实的来源无法找回截止时间", err)
	}
}

// TestDeletionDocumentDigestsReplay 验证仅受管的已确认候选文件能登记摘要，正式资料清理后仍可准确重试。
func TestDeletionDocumentDigestsReplay(t *testing.T) {
	ctx := context.Background()
	c, _, cred := purgeTestClient(t, "")
	ev := CandidateDeletion{Sequence: 1, CandidateID: "cloud"}
	good := autoReplyDigest("good")
	other := autoReplyDigest("other")
	if err := c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		if err := tx.Put("cloud_links", "candidate:local", localCloudLink{"cloud"}); err != nil {
			return err
		}
		for id, hash := range map[string]string{"local": good, "unrelated": other} {
			file := localAttachment{StoredResumeAttachment: StoredResumeAttachment{ID: id, CandidateID: id, SHA256: hash}, RelativePath: "owned/" + hash}
			if err := tx.Put("attachments", id, file); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	first, err := c.CandidateDeletionDocumentHashes(ctx, cred, ev)
	if err != nil || len(first) != 1 || first[0] != good {
		t.Fatal("回收锚未限定已核实的云映射", err)
	}
	if err := c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error { return tx.Delete("cloud_links", "candidate:local") }); err != nil {
		t.Fatal(err)
	}
	again, err := c.CandidateDeletionDocumentHashes(ctx, cred, ev)
	if err != nil || len(again) != 1 || again[0] != good {
		t.Fatal("重试丢失已确认的文件依据", err)
	}
}
