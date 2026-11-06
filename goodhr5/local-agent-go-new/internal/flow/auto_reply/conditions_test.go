// Package auto_reply 本文件回归岗位条件更改、证据继承和发送后计数不会覆盖新判断的行为。
package auto_reply

import (
	"context"
	"path/filepath"
	"testing"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/storage"
)

// TestPositionConditionChangesInvalidateEvidence 验证条件正文更改会清除旧资格，而相同正文的证据可安全关联岗位条件。
func TestPositionConditionChangesInvalidateEvidence(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := storage.Open(filepath.Join(dir, "local.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	client := cloud.New("http://127.0.0.1:1")
	client.EnableLocalAutoReply(store, filepath.Join(dir, "private"))
	creds := cloud.AgentCredentials{Token: "synthetic", MachineID: "machine"}
	conv, err := client.SaveAutoReplyConversation(ctx, creds, cloud.AutoReplyConversation{PlatformID: "boss", PlatformAccountID: "account", PlatformThreadID: "thread", PositionID: "position", CandidateID: "candidate"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := client.SaveAutoReplyConfirmationItem(ctx, creds, cloud.CandidateConfirmationItem{ConversationID: conv.ID, CandidateID: conv.CandidateID, PositionID: conv.PositionID, ItemType: "required", Content: "接受出差", Status: "matched", SourceType: "ai", SourceRef: "message", EvidenceText: "我接受出差", AskCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	flow := &Flow{Cloud: client}
	position := cloud.AutoReplyPositionSnapshot{Position: cloud.AutoReplyPosition{ID: "position"}, Config: cloud.PositionAutoReplyConfig{Conditions: []cloud.PositionReplyCondition{{ID: "condition", Type: "required", Content: "接受出差", Enabled: true}}}}
	linked, err := flow.syncPositionConfirmationItems(ctx, creds, position, conv, []cloud.CandidateConfirmationItem{item})
	if err != nil || len(linked) != 1 || linked[0].SourceRef != "message" || linked[0].Status != "matched" {
		t.Fatalf("关联丢失证据：%+v %v", linked, err)
	}
	position.Config.Conditions[0].Content = "接受长期驻外"
	changed, err := flow.syncPositionConfirmationItems(ctx, creds, position, conv, linked)
	if err != nil || len(changed) != 1 || changed[0].Status != "pending" || changed[0].EvidenceText != "" || changed[0].AskCount != 0 {
		t.Fatalf("旧资格未失效：%+v %v", changed, err)
	}
	// 模拟AI把判断改成pending后，发送路径仍带旧matched快照；只能递增计数。
	if err = flow.markConfirmationAsked(ctx, creds, conv, changed[0].ID, linked); err != nil {
		t.Fatal(err)
	}
	persisted, err := client.AutoReplyConfirmationItems(ctx, creds, conv.ID)
	if err != nil || len(persisted) != 1 || persisted[0].Status != "pending" || persisted[0].AskCount != 1 {
		t.Fatalf("旧快照覆盖新判断：%+v %v", persisted, err)
	}
	position.Config.Conditions = nil
	remaining, err := flow.syncPositionConfirmationItems(ctx, creds, position, conv, persisted)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("删除条件未归档：%+v %v", remaining, err)
	}
}
