// Package cloudserver 本文件验证附件回执整批核对、原会话绑定与服务器受管卷入库，避免候选人切换时串档。
package cloudserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/replybridge"
	"goodhr5/local-agent-go-new/internal/storage"
)

// TestServerReceiptPinsOriginalConversationAndVerifiesAllFiles 在两份会话使用同一平台消息键时验明附件不能串档或部分入库。
func TestServerReceiptPinsOriginalConversationAndVerifiesAllFiles(t *testing.T) {
	ctx := context.Background()
	fixture := &stateEndpointFixture{data: map[string]storage.AutoReplyStateChange{}}
	backend := httptest.NewServer(http.HandlerFunc(fixture.handle))
	defer backend.Close()
	authority := newStateAuthority(backend.URL, "1234567890abcdef", "tenant", "user")
	db, err := storage.OpenAutoReplyScratch(authority)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := cloud.New(backend.URL)
	cred := cloud.AgentCredentials{Token: "user", MachineID: "machine"}
	if _, err = c.ValidateSession(ctx, cred.Token); err != nil {
		t.Fatal(err)
	}
	volume := t.TempDir()
	scopeHash := sha256.Sum256([]byte("tenant\x00user"))
	c.EnableLocalAutoReply(db, filepath.Join(volume, hex.EncodeToString(scopeHash[:])))
	runs := &activeRun{id: "run", scope: runScope{machine: "machine"}, client: c, cred: cred, leaseKey: "lease:one", platform: "liepin", position: "p"}
	if err = runs.updateLease(ctx, "claim"); err != nil {
		t.Fatal(err)
	}
	convs := map[string]string{}
	for _, thread := range []string{"first", "other"} {
		conv, e := c.SaveAutoReplyConversation(ctx, cred, cloud.AutoReplyConversation{PlatformID: "liepin", PlatformAccountID: "account", PlatformThreadID: thread, PositionID: "p"})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = c.SyncAutoReplyMessages(ctx, cred, cloud.AutoReplyMessageSyncRequest{ConversationID: conv.ID, Messages: []cloud.AutoReplyMessage{{Direction: "candidate", MessageType: "resume", PlatformMessageID: "card", TextContent: "简历卡片"}}}); e != nil {
			t.Fatal(e)
		}
		convs[thread] = conv.ID
	}
	payload, _ := json.Marshal(replybridge.Request{Snapshot: model.AutoReplyConversationSnapshot{PlatformAccountID: "account", PlatformThreadID: "first", ResumeSourceMessageID: "card"}})
	cmd := replybridge.Command{ID: "command", RunID: "run", Action: "collect_resume", PlatformID: "liepin", Deadline: time.Now().Add(time.Minute), Payload: payload}
	// 还没绑定候选的只读命令也可能带页面标识，账号级账本不能保留这些私有字段。
	unbound := cmd
	unbound.ID = "unbound"
	unbound.Action = "messages"
	if err = runs.persistCommand(ctx, unbound, "pending", replybridge.Result{}); err != nil {
		t.Fatal(err)
	}
	var redacted commandState
	if err = json.Unmarshal(fixture.data["runtime_commands|unbound"].Payload, &redacted); err != nil || redacted.SourceKey != "" || redacted.ThreadID != "" || redacted.AccountID != "" {
		t.Fatal("未绑定命令保留候选身份", err)
	}
	runs.conversation = convs["first"]
	if err = runs.persistCommand(ctx, cmd, "pending", replybridge.Result{}); err != nil {
		t.Fatal(err)
	}
	if err = runs.persistCommand(ctx, cmd, "executing", replybridge.Result{}); err != nil {
		t.Fatal(err)
	}
	runs.conversation = convs["other"]
	file := replybridge.File{Name: "resume.txt", Data: []byte("resume one")}
	sum := sha256.Sum256(file.Data)
	file.SHA256 = hex.EncodeToString(sum[:])
	invalid := replybridge.File{Name: "resume2.txt", Data: []byte("corrupt"), SHA256: file.SHA256}
	complete := func(files ...replybridge.File) error {
		raw, _ := json.Marshal(replybridge.Response{Files: files, Resume: model.AutoReplyResumeBundle{ResumeSourceMessageID: "card"}})
		return runs.persistCommand(ctx, cmd, "completed", replybridge.Result{ID: cmd.ID, Value: raw})
	}
	if err = complete(file, invalid); err == nil {
		t.Fatal("accepted corrupt second attachment")
	}
	for _, id := range convs {
		items, e := c.AutoReplyAttachments(ctx, cred, "", id)
		if e != nil || len(items) != 0 {
			t.Fatal("partially registered an attachment", e)
		}
	}
	// 数据库拒绝提交时只能留下临时副本；设备不可收到成功，最终受管卷与业务库一致。
	reject := true
	fixture.beforeCommit = func(changes []storage.AutoReplyStateChange) error {
		for _, ch := range changes {
			if ch.Kind != "attachments" {
				continue
			}
			if ch.StagingName == "" || filepath.Base(ch.StagingName) != ch.StagingName {
				t.Error("没有提交服务端专属的安全暂存文件名")
			}
			if data, e := os.ReadFile(filepath.Join(volume, ".runtime-stage", ch.StagingName)); e != nil || string(data) != string(file.Data) {
				t.Error("PG确认时原始暂存数据不可用", e)
			}
			if reject {
				return errors.New("拒绝当前事务")
			}
		}
		return nil
	}
	if err = complete(file); err == nil {
		t.Fatal("PG拒绝后设备仍收到成功")
	}
	if entries, e := os.ReadDir(filepath.Join(volume, ".runtime-stage")); e != nil || len(entries) != 0 {
		t.Fatal("PG失败后服务器临时原件没有清理", e)
	}
	reject = false
	if err = complete(file); err != nil {
		t.Fatal(err)
	}
	for thread, id := range convs {
		items, e := c.AutoReplyAttachments(ctx, cred, "", id)
		if e != nil || thread == "first" && len(items) != 1 || thread == "other" && len(items) != 0 {
			t.Fatal("attachment does not belong to original thread", thread, e)
		}
	}
}
