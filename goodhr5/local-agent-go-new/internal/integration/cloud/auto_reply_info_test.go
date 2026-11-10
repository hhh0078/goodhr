// Package cloud 本文件验证服务器资料索要状态不会被普通快照抹掉，未知请求跨任务防重，并拒绝未确认或冲突的联系方式。
package cloud

import (
	"context"
	"testing"

	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestInfoRequestProgressSurvivesSnapshots 验证一次暂缓保留水位、普通保存不重置、unknown不能被新计划改回可执行。
func TestInfoRequestProgressSurvivesSnapshots(t *testing.T) {
	c, cred, _ := entityTestClient(t)
	ctx := context.Background()
	conversation := entityTestConversation(t, c, cred)
	syncEntityMessages(t, c, cred, conversation, AutoReplyMessage{PlatformMessageID: "m1", Direction: "candidate", MessageType: "text", TextContent: "你好"})
	if err := c.PlanAutoReplyInfoRequests(ctx, cred, conversation.ID, "m1", model.CandidateInfoRequest{RequestPhone: true}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetAutoReplyInfoRequestProgress(ctx, cred, conversation.ID, "phone", model.AutoReplyInfoDeferred, "m1"); err != nil {
		t.Fatal(err)
	}
	saved, err := c.SaveAutoReplyConversation(ctx, cred, conversation)
	if err != nil || saved.InfoRequests["phone"].LastAttemptMessageKey != "m1" {
		t.Fatal("普通保存抹掉资料待办", err)
	}
	if err = c.SetAutoReplyInfoRequestProgress(ctx, cred, conversation.ID, "phone", "unknown", "m1"); err != nil {
		t.Fatal(err)
	}
	if err = c.PlanAutoReplyInfoRequests(ctx, cred, conversation.ID, "m1", model.CandidateInfoRequest{RequestPhone: true}); err != nil {
		t.Fatal(err)
	}
	if err = c.SetAutoReplyInfoRequestProgress(ctx, cred, conversation.ID, "phone", model.AutoReplyInfoDeferred, "m1"); err != nil {
		t.Fatal(err)
	}
	current, _, err := c.AutoReplyAnswerConversation(ctx, cred, conversation.ID)
	if err != nil || current.InfoRequests["phone"].Status != "unknown" {
		t.Fatal("unknown被恢复为自动重试", err)
	}
}

// TestNativeContactsRequireReceivedProof 验证非received不能写联系方式，取得后普通重复保存不覆盖原手机号。
func TestNativeContactsRequireReceivedProof(t *testing.T) {
	c, cred, _ := entityTestClient(t)
	ctx := context.Background()
	conversation := entityTestConversation(t, c, cred)
	if _, err := c.EnsureLocalAutoReplyCandidate(ctx, cred, conversation); err != nil {
		t.Fatal(err)
	}
	result := model.AutoReplyInfoRequestResult{Status: model.AutoReplyInfoReceived, Phone: "13800138001"}
	if err := c.SaveAutoReplyNativeContact(ctx, cred, conversation.ID, "phone", result); err == nil {
		t.Fatal("没有已获得状态仍写入了号码")
	}
	if err := c.SetAutoReplyInfoRequestProgress(ctx, cred, conversation.ID, "phone", model.AutoReplyInfoReceived, "phone-card"); err != nil {
		t.Fatal(err)
	}
	if err := c.SaveAutoReplyNativeContact(ctx, cred, conversation.ID, "phone", result); err != nil {
		t.Fatal(err)
	}
	result.Phone = "13800138002"
	if err := c.SaveAutoReplyNativeContact(ctx, cred, conversation.ID, "phone", result); err == nil {
		t.Fatal("不同手机号静默覆盖了原号码")
	}
}

// TestConfirmedInfoProgressRejectsLateCallbacks 验证迟到的动作回调不能抹掉已取得或已拒绝的真实结果，但拒绝后可以接受新取得的资料。
func TestConfirmedInfoProgressRejectsLateCallbacks(t *testing.T) {
	c, cred, _ := entityTestClient(t)
	ctx := context.Background()
	conversation := entityTestConversation(t, c, cred)
	for _, kind := range []string{"phone", "wechat"} {
		confirmed := model.AutoReplyInfoReceived
		if kind == "wechat" {
			confirmed = model.AutoReplyInfoDeclined
		}
		if err := c.SetAutoReplyInfoRequestProgress(ctx, cred, conversation.ID, kind, confirmed, "confirmed-card"); err != nil {
			t.Fatal(err)
		}
		for _, late := range []string{"unknown", "requested", "deferred", "unavailable", "planned"} {
			if err := c.SetAutoReplyInfoRequestProgress(ctx, cred, conversation.ID, kind, late, "older-attempt"); err != nil {
				t.Fatal(err)
			}
		}
		current, _, err := c.AutoReplyAnswerConversation(ctx, cred, conversation.ID)
		if err != nil || current.InfoRequests[kind].Status != confirmed || current.InfoRequests[kind].LastAttemptMessageKey != "confirmed-card" {
			t.Fatal("迟到回调覆盖了已确认的资料结果", kind, err)
		}
	}
	if err := c.SetAutoReplyInfoRequestProgress(ctx, cred, conversation.ID, "wechat", model.AutoReplyInfoReceived, "new-received-card"); err != nil {
		t.Fatal(err)
	}
	current, _, err := c.AutoReplyAnswerConversation(ctx, cred, conversation.ID)
	if err != nil || current.InfoRequests["wechat"].Status != model.AutoReplyInfoReceived {
		t.Fatal("拒绝后的真实收到没有推进", err)
	}
}
