// Package auto_reply 本文件用服务器RAM权威验证附件号码冲突、旧交换卡重读与正常问答互不阻断，不访问真实候选人或数据库。
package auto_reply

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestNativeContactConflictKeepsOrdinaryAnswer 验证已核实号码不被附件改绑，历史冲突只记一次，HR卡不成为资料来源且正常问答可继续发送。
func TestNativeContactConflictKeepsOrdinaryAnswer(t *testing.T) {
	const phoneA, phoneB = "13800138001", "13800138002"
	for _, scenario := range []string{"native-then-attachment", "legacy-conflict", "same-format", "hr-card", "server-save-failed"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			flow, prepared, authority := outgoingTextFlow(t, "", "boss")
			cred := credentials(prepared)
			identity, err := flow.Cloud.SaveAutoReplyIdentity(ctx, cred, cloud.CandidatePlatformIdentity{PlatformID: "boss", PlatformAccountID: prepared.Request.PlatformAccountID, PlatformCandidateID: "synthetic-native", CandidateName: "合成候选人"})
			if err != nil {
				t.Fatal(err)
			}
			conversation, err := flow.Cloud.SaveAutoReplyConversation(ctx, cred, cloud.AutoReplyConversation{PlatformID: "boss", PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: "synthetic-thread", PlatformIdentityID: identity.ID, CandidateName: "合成候选人", PositionID: "position"})
			if err != nil {
				t.Fatal(err)
			}
			actor, err := flow.Cloud.EnsureLocalAutoReplyCandidate(ctx, cred, conversation)
			if err != nil {
				t.Fatal(err)
			}
			initial := phoneA
			if scenario != "native-then-attachment" {
				initial = phoneB
			}
			if err = flow.Cloud.SetAutoReplyInfoRequestProgress(ctx, cred, conversation.ID, "phone", model.AutoReplyInfoReceived, "native-card"); err != nil {
				t.Fatal(err)
			}
			if err = flow.Cloud.SaveAutoReplyNativeContact(ctx, cred, conversation.ID, "phone", model.AutoReplyInfoRequestResult{Status: model.AutoReplyInfoReceived, Phone: initial}); err != nil {
				t.Fatal(err)
			}
			incoming, direction := phoneA, "system"
			if scenario == "same-format" {
				incoming = "+86 138-0013-8002"
			}
			if scenario == "hr-card" {
				direction = "self"
			}
			text := "合成候选人的手机号：" + incoming + "复制手机号"
			card, _ := json.Marshal(struct {
				InfoType   string `json:"info_type"`
				InfoStatus string `json:"info_status"`
				Phone      string `json:"phone"`
				OwnerName  string `json:"owner_name"`
				Summary    string `json:"summary"`
			}{"phone", model.AutoReplyInfoReceived, incoming, "合成候选人", text})
			messages := []cloud.AutoReplyMessage{{PlatformMessageID: "native-card", Direction: direction, MessageType: "card", TextContent: text, CardContent: card}, {PlatformMessageID: "attachment", Direction: "candidate", MessageType: "resume", TextContent: "附件已发送"}, {PlatformMessageID: "ordinary-question", Direction: "candidate", MessageType: "text", TextContent: "请问在哪里上班？"}}
			if _, err = flow.Cloud.SyncAutoReplyMessages(ctx, cred, cloud.AutoReplyMessageSyncRequest{ConversationID: conversation.ID, Messages: messages}); err != nil {
				t.Fatal(err)
			}
			lookup := cloud.AutoReplyCandidateLookup{PlatformID: "boss", PlatformAccountID: prepared.Request.PlatformAccountID, PlatformCandidateID: "synthetic-native", PlatformThreadID: "synthetic-thread"}
			if scenario == "native-then-attachment" {
				before, err := flow.Cloud.AutoReplyCandidateState(ctx, cred, lookup)
				if err != nil {
					t.Fatal(err)
				}
				// 正式循环先用没有手机号的聊天头刷新身份；这一步不得擦掉已经收到的原生号码。
				refreshed, err := flow.savePlatformIdentity(ctx, prepared, model.AutoReplyConversationSnapshot{CandidateName: conversation.CandidateName, PlatformAccountID: prepared.Request.PlatformAccountID, PlatformCandidateID: "synthetic-native", PlatformThreadID: "synthetic-thread"}, before)
				if err != nil || !cloud.AutoReplyContactValuesEqual("phone", refreshed.NormalizedPhone, initial) {
					t.Fatal("普通聊天头刷新清掉了已核实的原生手机号", err)
				}
				_, err = flow.Cloud.SaveAutoReplyCandidate(ctx, cred, cloud.AutoReplyCandidateInput{LocalCandidateID: actor.ID, PlatformID: "boss", PlatformAccountID: prepared.Request.PlatformAccountID, PlatformCandidateID: "synthetic-native", PositionID: "position", ResumeSourceMessageID: "attachment", StructuredCandidate: cloud.StructuredCandidate{CandidateName: "合成候选人", Phone: phoneB, RawText: "合成附件，保留不同联系方式来源"}})
				if err != nil {
					t.Fatal(err)
				}
			}
			state, err := flow.Cloud.AutoReplyCandidateState(ctx, cred, lookup)
			if err != nil || state.Conversation == nil || state.Candidate == nil || state.Identity == nil {
				t.Fatal("合成身份或会话未正确关联", err)
			}
			conversation = *state.Conversation
			snapshot := model.AutoReplyConversationSnapshot{CandidateName: conversation.CandidateName, PlatformAccountID: prepared.Request.PlatformAccountID, PlatformCandidateID: "synthetic-native", PlatformThreadID: "synthetic-thread", Messages: []model.ConversationMessage{{PlatformMessageID: "native-card", Direction: direction, MessageType: "card", TextContent: text, CardContent: card}}}
			if scenario == "server-save-failed" {
				if err = flow.Store.Close(); err != nil {
					t.Fatal(err)
				}
				if _, err = flow.syncNativeChatContacts(ctx, prepared, conversation, snapshot, state); err == nil {
					t.Fatal("服务器仓储失败被当成普通号码冲突吞掉")
				}
				return
			}
			for repeat := 0; repeat < 2; repeat++ {
				state, err = flow.syncNativeChatContacts(ctx, prepared, conversation, snapshot, state)
				if err != nil {
					t.Fatal("旧联系方式阻断了普通问答", err)
				}
			}
			persisted, err := flow.Cloud.AutoReplyCandidateState(ctx, cred, lookup)
			if err != nil || persisted.Candidate.Phone != initial || persisted.Identity.NormalizedPhone != initial {
				t.Fatal("附件或旧卡片改变了已核实号码", err)
			}
			events := 0
			for _, row := range authority.rows {
				if row.Kind == "runtime_events" {
					events++
					if strings.Contains(string(row.Payload), phoneA) || strings.Contains(string(row.Payload), phoneB) || !strings.Contains(string(row.Payload), "联系方式待核对") {
						t.Fatal("冲突说明泄漏号码或缺少中文提示")
					}
				}
			}
			wantEvents := 0
			if scenario == "native-then-attachment" || scenario == "legacy-conflict" {
				wantEvents = 1
			}
			if events != wantEvents {
				t.Fatalf("冲突事件未幂等或HR卡被当作候选人资料：got=%d want=%d", events, wantEvents)
			}
			anchor := model.ConversationMessage{PlatformMessageID: "ordinary-question", Direction: "candidate", MessageType: "text", TextContent: "请问在哪里上班？"}
			runtime := &outgoingTextRuntime{answeringRuntime: &answeringRuntime{autoReplyRuntimeStub: &autoReplyRuntimeStub{latestBefore: anchor}}, flow: flow, prepared: prepared}
			if sent, err := flow.sendVerifiedMessage(ctx, prepared, runtime, conversation, snapshot, anchor.PlatformMessageID, "工作地点在成都。", true, 1); err != nil || !sent || len(runtime.replies) != 1 {
				t.Fatal("联系方式冲突后正常问题未继续答复", err)
			}
		})
	}
}

// TestContactConflictRejectsUnstoredSource 验证未同步、其它会话及HR来源不能凭自报编号登记候选人联系方式冲突。
func TestContactConflictRejectsUnstoredSource(t *testing.T) {
	flow, prepared, _ := outgoingTextFlow(t, "", "boss")
	ctx := context.Background()
	conversation, err := flow.Cloud.SaveAutoReplyConversation(ctx, credentials(prepared), cloud.AutoReplyConversation{PlatformID: "boss", PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: "thread", PositionID: "position"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = flow.Cloud.EnsureLocalAutoReplyCandidate(ctx, credentials(prepared), conversation); err != nil {
		t.Fatal(err)
	}
	if err = flow.Cloud.RecordAutoReplyContactConflict(ctx, credentials(prepared), conversation.ID, "phone", "never-synced"); err == nil {
		t.Fatal("未同步来源被接受")
	}
	if _, err = flow.Cloud.SyncAutoReplyMessages(ctx, credentials(prepared), cloud.AutoReplyMessageSyncRequest{ConversationID: conversation.ID, Messages: []cloud.AutoReplyMessage{{PlatformMessageID: "hr-card", Direction: "self", MessageType: "card"}}}); err != nil {
		t.Fatal(err)
	}
	if err = flow.Cloud.RecordAutoReplyContactConflict(ctx, credentials(prepared), conversation.ID, "phone", "hr-card"); err == nil {
		t.Fatal("HR来源被当成候选人资料")
	}
	other, err := flow.Cloud.SaveAutoReplyConversation(ctx, credentials(prepared), cloud.AutoReplyConversation{PlatformID: "boss", PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: "other-thread", PositionID: "position"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = flow.Cloud.SyncAutoReplyMessages(ctx, credentials(prepared), cloud.AutoReplyMessageSyncRequest{ConversationID: other.ID, Messages: []cloud.AutoReplyMessage{{ID: "foreign-source", PlatformMessageID: "other-native", Direction: "system", MessageType: "card"}}}); err != nil {
		t.Fatal(err)
	}
	if err = flow.Cloud.RecordAutoReplyContactConflict(ctx, credentials(prepared), conversation.ID, "phone", "foreign-source"); err == nil {
		t.Fatal("其它会话来源被借用")
	}
}
