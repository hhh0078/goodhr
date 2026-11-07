// Package cloud 本文件验证实体迁移、账号隔离、稳定消息水位、发送意图、资料保护与可搬迁附件。
package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"goodhr5/local-agent-go-new/internal/storage"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// entityTestClient 为每个测试创建独立临时SQLite，不访问用户数据。
func entityTestClient(t *testing.T) (*Client, AgentCredentials, *storage.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	c := New("")
	c.EnableLocalAutoReply(store, dir)
	return c, AgentCredentials{Token: "fixture-token", MachineID: "fixture-machine"}, store
}

// entityTestConversation 建立带实际账号隔离字段的测试会话。
func entityTestConversation(t *testing.T, c *Client, cred AgentCredentials) AutoReplyConversation {
	t.Helper()
	v, err := c.SaveAutoReplyConversation(context.Background(), cred, AutoReplyConversation{PlatformID: "boss", PlatformAccountID: "account-one", PlatformThreadID: "thread", PositionID: "position", CandidateName: "测试候选人"})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// syncEntityMessages 写入明确顺序的合成消息。
func syncEntityMessages(t *testing.T, c *Client, cred AgentCredentials, v AutoReplyConversation, messages ...AutoReplyMessage) {
	t.Helper()
	if _, err := c.SyncAutoReplyMessages(context.Background(), cred, AutoReplyMessageSyncRequest{ConversationID: v.ID, Messages: messages}); err != nil {
		t.Fatal(err)
	}
}

// TestAutoReplyEntityMigrationPreservesOriginalAndDoesNotReplay 验证旧快照只迁移一次且旧消息不自动补发。
func TestAutoReplyEntityMigrationPreservesOriginalAndDoesNotReplay(t *testing.T) {
	c, cred, store := entityTestClient(t)
	ctx := context.Background()
	legacy := localAutoReplyData{Candidates: map[string]AutoReplyStoredCandidate{"legacy-person": {ID: "legacy-person", Phone: "13800000000"}}, Conversations: map[string]AutoReplyConversation{"legacy-conv": {ID: "legacy-conv", PlatformID: "boss", PlatformThreadID: "old", CandidateID: "legacy-person", LastSyncedMessageKey: "old-message", LastCandidateMessageKey: "old-message"}}, Messages: map[string][]AutoReplyMessage{"legacy-conv": {{ID: "old-id", PlatformMessageID: "old-message", Direction: "candidate", TextContent: "历史资料"}}}}
	raw, _ := json.Marshal(legacy)
	if err := store.SetSetting(ctx, c.localAutoReplyKey(cred), string(raw)); err != nil {
		t.Fatal(err)
	}
	messages, err := c.AutoReplyMessages(ctx, cred, "legacy-conv")
	if err != nil || len(messages) != 1 || messages[0].ID != "old-id" {
		t.Fatalf("migration: %+v %v", messages, err)
	}
	if _, err = c.SyncAutoReplyMessages(ctx, cred, AutoReplyMessageSyncRequest{ConversationID: "legacy-conv", Messages: legacy.Messages["legacy-conv"]}); err != nil {
		t.Fatal("原消息ID迁移后无法幂等同步", err)
	}
	pending, err := c.PendingAutoReplyMessages(ctx, cred, "legacy-conv")
	if err != nil || len(pending) != 0 {
		t.Fatalf("旧消息被自动补发：%+v %v", pending, err)
	}
	if err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		return tx.Put("candidates", "legacy-person", AutoReplyStoredCandidate{ID: "legacy-person", CandidateName: "更新后的资料"})
	}); err != nil {
		t.Fatal(err)
	}
	restarted := New("")
	restarted.EnableLocalAutoReply(store, c.localAutoDir)
	var candidate AutoReplyStoredCandidate
	err = restarted.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		var e error
		candidate, _, e = getAutoReplyEntity[AutoReplyStoredCandidate](tx, "candidates", "legacy-person")
		return e
	})
	if err != nil || candidate.CandidateName != "更新后的资料" {
		t.Fatal("重复迁移覆盖现有实体", err)
	}
	preserved, _ := store.GetSetting(ctx, c.localAutoReplyKey(cred))
	if preserved != string(raw) {
		t.Fatal("旧快照未原样保留")
	}
}

// TestAutoReplyEntityMigrationRollsBackAllRows 验证导入中途发现坏消息时所有实体一起回滚且可重试。
func TestAutoReplyEntityMigrationRollsBackAllRows(t *testing.T) {
	c, cred, store := entityTestClient(t)
	ctx := context.Background()
	raw := `{"candidates":{"p":{"ID":"p","Phone":"13800000000"}},"messages":{"c":[{"text_content":"缺少稳定编号"}]}}`
	if err := store.SetSetting(ctx, c.localAutoReplyKey(cred), raw); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AutoReplyMessages(ctx, cred, "c"); err == nil {
		t.Fatal("错误迁移没有停止")
	}
	err := store.AutoReplyTransaction(ctx, c.localAutoReplyKey(cred), func(tx *storage.AutoReplyTx) error {
		_, found, err := tx.Get("candidates", "p")
		if found {
			return fmt.Errorf("失败迁移留下了半份候选人")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetSetting(ctx, c.localAutoReplyKey(cred), `{"candidates":{"p":{"ID":"p"}}}`); err != nil {
		t.Fatal(err)
	}
	if _, err = c.AutoReplyMessages(ctx, cred, "c"); err != nil {
		t.Fatal(err)
	}
}

// TestAutoReplyPendingAndSendIntentSurviveRestart 验证已读不等于完成，发送意图跨重启保持且正文变化不能重发。
func TestAutoReplyPendingAndSendIntentSurviveRestart(t *testing.T) {
	c, cred, store := entityTestClient(t)
	ctx := context.Background()
	v := entityTestConversation(t, c, cred)
	syncEntityMessages(t, c, cred, v, AutoReplyMessage{PlatformMessageID: "one", Direction: "candidate", TextContent: "问题一"}, AutoReplyMessage{PlatformMessageID: "two", Direction: "candidate", TextContent: "问题二"})
	restarted := New("")
	restarted.EnableLocalAutoReply(store, c.localAutoDir)
	pending, err := restarted.PendingAutoReplyMessages(ctx, cred, v.ID)
	if err != nil || len(pending) != 2 {
		t.Fatalf("未完成批次丢失：%d %v", len(pending), err)
	}
	status, claimed, err := c.ClaimAutoReplySend(ctx, cred, v.ID, "two", "固定回复", 0)
	if err != nil || !claimed || status != "sending" {
		t.Fatal(status, claimed, err)
	}
	status, claimed, err = restarted.ClaimAutoReplySend(ctx, cred, v.ID, "two", "固定回复", 0)
	if err != nil || claimed || status != "sending" {
		t.Fatal("重启后重复发送", status, claimed, err)
	}
	if _, _, err = restarted.ClaimAutoReplySend(ctx, cred, v.ID, "two", "模型重写的回复", 0); err == nil {
		t.Fatal("不同措辞绕过去重")
	}
	if err = c.FinishAutoReplySend(ctx, cred, v.ID, "two", 0, "sent"); err != nil {
		t.Fatal(err)
	}
	if err = c.CompleteAutoReplyMessage(ctx, cred, v.ID, "two", "sent"); err != nil {
		t.Fatal(err)
	}
	pending, err = c.PendingAutoReplyMessages(ctx, cred, v.ID)
	if err != nil || len(pending) != 0 {
		t.Fatal("完成批次仍待答", err)
	}
	other, err := c.AutoReplyMessages(ctx, AgentCredentials{Token: "other", MachineID: "fixture-machine"}, v.ID)
	if err != nil || len(other) != 0 {
		t.Fatal("账号数据交叉", err)
	}
}

// TestAutoReplyHistoryBackfillNeverMasksNewMessage 验证补采旧HR消息不会覆盖新待答消息，完整无交集历史需人工核对。
func TestAutoReplyHistoryBackfillNeverMasksNewMessage(t *testing.T) {
	c, cred, _ := entityTestClient(t)
	ctx := context.Background()
	v := entityTestConversation(t, c, cred)
	newMessage := AutoReplyMessage{PlatformMessageID: "new", Direction: "candidate", TextContent: "新问题"}
	syncEntityMessages(t, c, cred, v, newMessage)
	_, err := c.SyncAutoReplyMessages(ctx, cred, AutoReplyMessageSyncRequest{ConversationID: v.ID, HistoryComplete: true, Messages: []AutoReplyMessage{{PlatformMessageID: "old-self", Direction: "self", TextContent: "过去的回复"}, newMessage}})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := c.PendingAutoReplyMessages(ctx, cred, v.ID)
	if err != nil || len(pending) != 1 || pending[0].PlatformMessageID != "new" {
		t.Fatal("补采历史屏蔽了新消息", pending, err)
	}
	history, err := c.AutoReplyMessages(ctx, cred, v.ID)
	if err != nil || history[0].PlatformMessageID != "old-self" {
		t.Fatal("补历史顺序错误", history, err)
	}
	_, err = c.SyncAutoReplyMessages(ctx, cred, AutoReplyMessageSyncRequest{ConversationID: v.ID, HistoryComplete: true, Messages: []AutoReplyMessage{{PlatformMessageID: "unrelated-old", Direction: "candidate", TextContent: "无法定位的旧历史"}}})
	if err == nil {
		t.Fatal("完全无交集历史被当新消息")
	}
	pending, err = c.PendingAutoReplyMessages(ctx, cred, v.ID)
	if err != nil || len(pending) != 1 {
		t.Fatal("不可靠历史进入待答队列", err)
	}
	syncEntityMessages(t, c, cred, v, AutoReplyMessage{PlatformMessageID: "delta", Direction: "candidate", TextContent: "真正的新差量"})
	pending, err = c.PendingAutoReplyMessages(ctx, cred, v.ID)
	if err != nil || len(pending) != 2 {
		t.Fatal("正常无交集差量没有入队", err)
	}
}

// TestAutoReplyCandidatePartialDataAndNoPhone 验证无手机号本机身份与不同来源空字段不会破坏已有资料。
func TestAutoReplyCandidatePartialDataAndNoPhone(t *testing.T) {
	c, cred, _ := entityTestClient(t)
	ctx := context.Background()
	v := entityTestConversation(t, c, cred)
	temp, err := c.EnsureLocalAutoReplyCandidate(ctx, cred, v)
	if err != nil || temp.ID == "" || temp.Phone != "" {
		t.Fatal(temp, err)
	}
	input := AutoReplyCandidateInput{LocalCandidateID: temp.ID, PositionID: "position", PlatformID: "boss", PlatformAccountID: "account-one", PlatformCandidateID: "person", StructuredCandidate: StructuredCandidate{CandidateName: "合成候选人", Email: "fixture@example.invalid", WorkYears: "10"}}
	saved, err := c.SaveAutoReplyCandidate(ctx, cred, input)
	if err != nil || saved.CandidateID != temp.ID {
		t.Fatal(saved, err)
	}
	input.Email = ""
	input.WorkYears = ""
	input.Phone = "13800000000"
	if _, err = c.SaveAutoReplyCandidate(ctx, cred, input); err != nil {
		t.Fatal(err)
	}
	state, err := c.AutoReplyCandidateState(ctx, cred, AutoReplyCandidateLookup{PlatformID: "boss", PlatformAccountID: "account-one", PlatformCandidateID: "person"})
	if err != nil || state.Candidate == nil || state.Candidate.Email == "" || state.Candidate.WorkYears != "10" || !state.Candidate.ResumeStructured {
		t.Fatal("已有资料被空值覆盖", state, err)
	}
}

// TestAutoReplyAttachmentSurvivesDirectoryMove 验证关闭数据库后移动整个数据目录，附件仍按新根路径读取。
func TestAutoReplyAttachmentSurvivesDirectoryMove(t *testing.T) {
	c, cred, store := entityTestClient(t)
	ctx := context.Background()
	source := filepath.Join(t.TempDir(), "fixture.pdf")
	if err := os.WriteFile(source, []byte("synthetic attachment"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := c.UploadAutoReplyAttachment(ctx, cred, AutoReplyAttachmentUpload{ConversationID: "c", FilePath: source})
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	moved := filepath.Join(t.TempDir(), "moved")
	if err = os.Rename(c.localAutoDir, moved); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(moved, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	restarted := New("")
	restarted.EnableLocalAutoReply(db, moved)
	paths, err := restarted.LocalAutoReplyAttachmentPaths(ctx, cred, "c", []string{a.ID})
	if err != nil || len(paths) != 1 || !strings.HasPrefix(paths[0], moved) {
		t.Fatal(paths, err)
	}
}

// TestAutoReplyNotificationDoesNotLockLocalReads 验证网络等待与本地会话读取独立，且队列已可靠落盘。
func TestAutoReplyNotificationDoesNotLockLocalReads(t *testing.T) {
	c, cred, _ := entityTestClient(t)
	ctx := context.Background()
	v := entityTestConversation(t, c, cred)
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		fmt.Fprint(w, `{"ok":true,"email_sent":true}`)
	}))
	defer server.Close()
	c.baseURL = server.URL
	go func() {
		defer close(done)
		result, err := c.NotifyAutoReplyManualHandoff(ctx, cred, AutoReplyNotification{ConversationID: v.ID, PositionID: v.PositionID, BasedOnMessageKey: "m", ReasonKey: "reason", Reason: "合成原因"})
		if err != nil || !result.Queued {
			t.Error("通知未可靠入队", err)
			return
		}
		_ = c.processAutoReplyOutbox(ctx, cred, "notice:"+autoReplyDigest(v.ID+"\x00m\x00reason"))
	}()
	<-entered
	readDone := make(chan error, 1)
	go func() { _, err := c.AutoReplyMessages(ctx, cred, v.ID); readDone <- err }()
	select {
	case err := <-readDone:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(time.Second):
		t.Error("网络等待仍持有本地读锁")
	}
	close(release)
	<-done
}

// TestAutoReplySingleConversationReadDoesNotDecodeWholeAccount 验证持续增长后只解码目标会话，其他会话坏数据不能拖垮读取。
func TestAutoReplySingleConversationReadDoesNotDecodeWholeAccount(t *testing.T) {
	c, cred, _ := entityTestClient(t)
	ctx := context.Background()
	v := entityTestConversation(t, c, cred)
	syncEntityMessages(t, c, cred, v, AutoReplyMessage{PlatformMessageID: "only", Direction: "candidate", TextContent: "目标"})
	err := c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		for i := 0; i < 1500; i++ {
			if err := tx.Put("messages", fmt.Sprint(i), AutoReplyMessage{ConversationID: fmt.Sprint(i), PlatformMessageID: "m", Direction: "candidate", TextContent: strings.Repeat("x", 2048)}); err != nil {
				return err
			}
		}
		return tx.Put("candidates", "malformed-other", json.RawMessage(`{"ID":"malformed-other","Email":123}`))
	})
	if err != nil {
		t.Fatal(err)
	}
	messages, err := c.AutoReplyMessages(ctx, cred, v.ID)
	if err != nil || len(messages) != 1 {
		t.Fatal("单会话读取仍依赖整账号解码", len(messages), err)
	}
}

// TestAutoReplyOutboxRestartsWithoutPersistedCredentials 验证通知失败持久保留，重启使用当前账号恢复且重复完成不重发。
func TestAutoReplyOutboxRestartsWithoutPersistedCredentials(t *testing.T) {
	c, cred, store := entityTestClient(t)
	ctx := context.Background()
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/me" {
			fmt.Fprint(w, `{"user":{"id":"fixture-owner"}}`)
			return
		}
		attempts++
		if attempts == 1 {
			fmt.Fprint(w, `{"ok":true,"email_sent":false,"warning":"合成发送失败"}`)
		} else {
			fmt.Fprint(w, `{"ok":true,"email_sent":true}`)
		}
	}))
	defer server.Close()
	c.baseURL = server.URL
	if _, err := c.ValidateSession(ctx, cred.Token); err != nil {
		t.Fatal(err)
	}
	notification := AutoReplyNotification{ConversationID: "c", PositionID: "p", BasedOnMessageKey: "m", ReasonKey: "r", Reason: "合成原因"}
	result, err := c.NotifyAutoReplyManualHandoff(ctx, cred, notification)
	if err != nil || !result.Queued || attempts != 0 {
		t.Fatal("通知没有仅入队", result, err)
	}
	id := "notice:" + autoReplyDigest("c\x00m\x00r")
	if err = c.processAutoReplyOutbox(ctx, cred, id); err == nil {
		t.Fatal("发送失败没有保留错误")
	}
	if err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		item, _, err := getAutoReplyEntity[autoReplyOutboxItem](tx, "outbox", id)
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(item)
		if strings.Contains(string(raw), cred.Token) {
			return fmt.Errorf("队列保存了登录凭证")
		}
		if item.Status != "failed" {
			return fmt.Errorf("失败未持久化")
		}
		item.NextAttemptAt = time.Time{}
		return tx.Put("outbox", id, item)
	}); err != nil {
		t.Fatal(err)
	}
	restarted := New(server.URL)
	restarted.EnableLocalAutoReply(store, c.localAutoDir)
	if err = restarted.RetryAutoReplyOutbox(ctx, cred); err != nil {
		t.Fatal(err)
	}
	if err = restarted.RetryAutoReplyOutbox(ctx, cred); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("通知重试或去重错误：%d", attempts)
	}
}

// TestAutoReplyOutboxSkipsFutureLeasesBeforeLimit 验证未到期和报告收据不会占住后台恢复的批次名额。
func TestAutoReplyOutboxSkipsFutureLeasesBeforeLimit(t *testing.T) {
	c, cred, _ := entityTestClient(t)
	ctx := context.Background()
	err := c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		for i := 0; i < 40; i++ {
			if err := tx.Put("outbox", fmt.Sprint(i), autoReplyOutboxItem{ID: fmt.Sprint(i), Kind: "notice", Status: "failed", NextAttemptAt: time.Now().Add(time.Hour)}); err != nil {
				return err
			}
		}
		if err := tx.Put("outbox", "report", autoReplyOutboxItem{ID: "report", Kind: "report", Status: "pending"}); err != nil {
			return err
		}
		if err := tx.Put("outbox", "due", autoReplyOutboxItem{ID: "due", Kind: "notice", Status: "pending"}); err != nil {
			return err
		}
		rows, err := tx.DueAutoReplyRows("outbox", 30)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].ID != "due" {
			return fmt.Errorf("到期任务被历史任务挤出批次：%v", rows)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestAutoReplyProposalIsAtomicAndKeepsAskedCount 验证旧消息提议整体拒绝、正确提议与后续收据一起提交。
func TestAutoReplyProposalIsAtomicAndKeepsAskedCount(t *testing.T) {
	c, cred, _ := entityTestClient(t)
	ctx := context.Background()
	v := entityTestConversation(t, c, cred)
	candidate, err := c.EnsureLocalAutoReplyCandidate(ctx, cred, v)
	if err != nil {
		t.Fatal(err)
	}
	v.CandidateID = candidate.ID
	syncEntityMessages(t, c, cred, v, AutoReplyMessage{PlatformMessageID: "m1", Direction: "candidate", TextContent: "合成回答"})
	condition := CandidateConfirmationItem{ID: "confirmed", ConversationID: v.ID, CandidateID: candidate.ID, PositionID: v.PositionID, Content: "合成条件", ItemType: "confirm", Status: "pending", SourceType: "ai"}
	if _, err = c.SaveAutoReplyConfirmationItem(ctx, cred, condition); err != nil {
		t.Fatal(err)
	}
	if err = c.IncrementAutoReplyConfirmationAsked(ctx, cred, condition.ID); err != nil {
		t.Fatal(err)
	}
	condition.Status = "matched"
	condition.AskCount = 0
	if err = c.CommitAutoReplyProposal(ctx, cred, v.ID, "old", 1, []CandidateConfirmationItem{condition}, nil); err == nil {
		t.Fatal("过期提议被提交")
	}
	items, err := c.AutoReplyConfirmationItems(ctx, cred, v.ID)
	if err != nil || items[0].Status != "pending" {
		t.Fatal("拒绝提议留下半份修改", items, err)
	}
	if err = c.CommitAutoReplyProposal(ctx, cred, v.ID, "m1", 1, []CandidateConfirmationItem{condition}, nil); err != nil {
		t.Fatal(err)
	}
	items, err = c.AutoReplyConfirmationItems(ctx, cred, v.ID)
	if err != nil || items[0].AskCount != 1 || items[0].Status != "matched" {
		t.Fatal("提议覆盖询问次数", items, err)
	}
	receipts, err := c.PendingAutoReplyProposals(ctx, cred)
	if err != nil || len(receipts) != 1 {
		t.Fatal("提交判断遗漏后续收据", receipts, err)
	}
}

// TestAutoReplyPhoneNeverCrossesRecruitmentAccounts 验证相同手机号在两个招聘账号仍保留独立本地资料。
func TestAutoReplyPhoneNeverCrossesRecruitmentAccounts(t *testing.T) {
	c, cred, _ := entityTestClient(t)
	ctx := context.Background()
	input := AutoReplyCandidateInput{PositionID: "p", PlatformID: "boss", PlatformAccountID: "account-a", PlatformCandidateID: "person", StructuredCandidate: StructuredCandidate{Phone: "13800000000", CandidateName: "账号甲资料", Email: "a@example.invalid"}}
	a, err := c.SaveAutoReplyCandidate(ctx, cred, input)
	if err != nil {
		t.Fatal(err)
	}
	state, err := c.AutoReplyCandidateState(ctx, cred, AutoReplyCandidateLookup{PlatformID: "boss", PlatformAccountID: "account-b", PlatformCandidateID: "person", Phone: input.Phone})
	if err != nil || state.Candidate != nil {
		t.Fatal("新账号继承了旧手机号资料", state, err)
	}
	input.PlatformAccountID = "account-b"
	input.CandidateName = "账号乙资料"
	input.Email = "b@example.invalid"
	b, err := c.SaveAutoReplyCandidate(ctx, cred, input)
	if err != nil || a.CandidateID == b.CandidateID {
		t.Fatal("招聘账号被手机号合并", a, b, err)
	}
	state, err = c.AutoReplyCandidateState(ctx, cred, AutoReplyCandidateLookup{PlatformID: "boss", PlatformAccountID: "account-a", PlatformCandidateID: "person", Phone: input.Phone})
	if err != nil || state.Candidate == nil || state.Candidate.Email != "a@example.invalid" {
		t.Fatal("第二账号覆盖了第一账号", state, err)
	}
}

// TestAutoReplyProposalFailuresDoNotStarveLaterReceipts 验证前30条失败退避后第31条立即可恢复。
func TestAutoReplyProposalFailuresDoNotStarveLaterReceipts(t *testing.T) {
	c, cred, _ := entityTestClient(t)
	ctx := context.Background()
	if err := c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		for i := 0; i < 31; i++ {
			id := fmt.Sprint(i)
			if err := tx.Put("proposal_receipts", id, AutoReplyProposalReceipt{ID: id, Status: "pending"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	first, err := c.PendingAutoReplyProposals(ctx, cred)
	if err != nil || len(first) != 30 {
		t.Fatal(first, err)
	}
	for _, item := range first {
		if err = c.FailAutoReplyProposal(ctx, cred, item.ID, "合成暂时不可用"); err != nil {
			t.Fatal(err)
		}
	}
	next, err := c.PendingAutoReplyProposals(ctx, cred)
	if err != nil || len(next) != 1 || next[0].ID != "30" {
		t.Fatal("后续收据被失败项阻挡", next, err)
	}
}

// TestAllContactInitialCaptureReplyBoundary 验证首次读到的全部历史保留上下文，只有边界后的来信或同分钟最新且匹配的列表见证进入待答。
func TestAllContactInitialCaptureReplyBoundary(t *testing.T) {
	start := time.Date(2026, 11, 8, 1, 31, 42, 0, time.UTC)
	minute := start.Truncate(time.Minute)
	old := minute.Add(-time.Minute)
	later := minute.Add(time.Minute)
	for _, tc := range []struct {
		name, witness, direction string
		lastTime                 *time.Time
		expectPending            bool
	}{
		{"同分有见证", "同一条可见摘要", "candidate", &minute, true},
		{"同分无见证", "", "candidate", &minute, false},
		{"同分见证不符", "另一条摘要", "candidate", &minute, false},
		{"前一分钟相同正文", "同一条可见摘要", "candidate", &old, false},
		{"时间缺失", "同一条可见摘要", "candidate", nil, false},
		{"本方消息不能作来信", "同一条可见摘要", "self", &minute, false},
		{"晚于边界", "", "candidate", &later, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, cred, _ := entityTestClient(t)
			ctx := context.Background()
			v := entityTestConversation(t, c, cred)
			req := AutoReplyMessageSyncRequest{ConversationID: v.ID, ReplyAfter: start, CaptureCompleted: true, HistoryComplete: true, Messages: []AutoReplyMessage{
				{PlatformMessageID: "old", Direction: "candidate", TextContent: "同一条可见摘要", PlatformSentAt: &old},
				{PlatformMessageID: "unknown", Direction: "candidate", TextContent: "旧消息"},
				{PlatformMessageID: "same-minute-older", Direction: "candidate", TextContent: "同一条可见摘要", PlatformSentAt: &minute},
				{PlatformMessageID: "tail", Direction: tc.direction, TextContent: "同一条可见摘要", PlatformSentAt: tc.lastTime},
			}}
			if tc.witness != "" {
				req.WitnessDigest = autoReplyDigest(tc.witness)
			}
			for pass := 0; pass < 2; pass++ {
				result, err := c.SyncAutoReplyMessages(ctx, cred, req)
				if err != nil {
					t.Fatal(err)
				}
				if pass == 1 && result.Inserted != 0 {
					t.Fatal("再次采集生成重复记录", result)
				}
				pending, err := c.PendingAutoReplyMessages(ctx, cred, v.ID)
				if err != nil {
					t.Fatal(err)
				}
				if tc.expectPending {
					if len(pending) != 1 || pending[0].PlatformMessageID != "tail" {
						t.Fatalf("见证释放了错误来信：%+v", pending)
					}
				} else if len(pending) != 0 {
					t.Fatalf("历史或本方消息产生待答：%+v", pending)
				}
				stored, err := c.AutoReplyMessages(ctx, cred, v.ID)
				if err != nil || len(stored) != 4 {
					t.Fatalf("完整历史未保留作上下文：%d %v", len(stored), err)
				}
			}
		})
	}
}

// TestAllContactVerifiedLastTailKeepsSameMinuteBatch 验证同一会话的真实旧尾能证明之后全部新编号的顺序，更早的已知消息不能替代旧尾。
func TestAllContactVerifiedLastTailKeepsSameMinuteBatch(t *testing.T) {
	for _, tc := range []struct {
		anchor string
		want   int
	}{{"saved-tail", 2}, {"older-known", 0}} {
		t.Run(tc.anchor, func(t *testing.T) {
			c, cred, _ := entityTestClient(t)
			ctx := context.Background()
			v := entityTestConversation(t, c, cred)
			minute := time.Now().UTC().Truncate(time.Minute)
			msg := func(id, direction string) AutoReplyMessage {
				return AutoReplyMessage{PlatformMessageID: id, Direction: direction, TextContent: "同分钟同文", PlatformSentAt: &minute}
			}
			syncEntityMessages(t, c, cred, v, msg("older-known", "self"), msg("saved-tail", "self"))
			req := AutoReplyMessageSyncRequest{ConversationID: v.ID, ReplyAfter: minute.Add(51 * time.Second), Messages: []AutoReplyMessage{msg(tc.anchor, "self"), msg("arrived-first", "candidate"), msg("arrived-second", "candidate")}}
			if _, err := c.SyncAutoReplyMessages(ctx, cred, req); err != nil {
				t.Fatal(err)
			}
			pending, err := c.PendingAutoReplyMessages(ctx, cred, v.ID)
			if err != nil || len(pending) != tc.want {
				t.Fatalf("锚点后同分钟来信待答数为 %d，应为 %d：%v", len(pending), tc.want, err)
			}
			if tc.want == 2 && (pending[0].PlatformMessageID != "arrived-first" || pending[1].PlatformMessageID != "arrived-second") {
				t.Fatal("同文的两条真实消息没有保留各自编号及顺序", pending)
			}
		})
	}
}

// TestAllContactWitnessIgnoresOnlyTrailingSystem 验证尾部系统提示不能遮住最新来信，本方后续消息则不能作为该来信的见证。
func TestAllContactWitnessIgnoresOnlyTrailingSystem(t *testing.T) {
	for _, selfAfter := range []bool{false, true} {
		c, cred, _ := entityTestClient(t)
		ctx := context.Background()
		v := entityTestConversation(t, c, cred)
		minute := time.Now().UTC().Truncate(time.Minute)
		messages := []AutoReplyMessage{{PlatformMessageID: "incoming", Direction: "candidate", TextContent: "最新候选人消息", PlatformSentAt: &minute}}
		if selfAfter {
			messages = append(messages, AutoReplyMessage{PlatformMessageID: "self-after", Direction: "self", TextContent: "已人工回复", PlatformSentAt: &minute})
		}
		messages = append(messages, AutoReplyMessage{PlatformMessageID: "hint", Direction: "system", TextContent: "系统提示"})
		_, err := c.SyncAutoReplyMessages(ctx, cred, AutoReplyMessageSyncRequest{ConversationID: v.ID, ReplyAfter: minute.Add(time.Second), WitnessDigest: autoReplyDigest("最新候选人消息"), Messages: messages})
		if err != nil {
			t.Fatal(err)
		}
		pending, err := c.PendingAutoReplyMessages(ctx, cred, v.ID)
		if err != nil || (selfAfter && len(pending) != 0) || (!selfAfter && (len(pending) != 1 || pending[0].PlatformMessageID != "incoming")) {
			t.Fatalf("系统提示/本方消息的筛选错误：selfAfter=%t pending=%+v err=%v", selfAfter, pending, err)
		}
	}
}
