// Package auto_reply 本文件验证平台最终正文从服务器发送意图贯穿严格回执与目标追问，测试只使用RAM权威和合成页面。
package auto_reply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

type outgoingTextAuthority struct {
	version int64
	rows    map[string]storage.AutoReplyStateChange
}

// Load 按RAM权威版本恢复快照，不接触真实服务器或数据库。
func (a *outgoingTextAuthority) Load(_ context.Context, known *int64) (storage.AutoReplyStateSnapshot, bool, error) {
	if known != nil && *known == a.version {
		return storage.AutoReplyStateSnapshot{}, true, nil
	}
	snapshot := storage.AutoReplyStateSnapshot{Version: a.version}
	for _, row := range a.rows {
		snapshot.Entities = append(snapshot.Entities, row)
	}
	return snapshot, false, nil
}

// Commit 接收真实投影的增量事务，模拟服务器CAS并保留最终发送账本供断言。
func (a *outgoingTextAuthority) Commit(_ context.Context, version int64, changes []storage.AutoReplyStateChange) (int64, error) {
	if version != a.version {
		return a.version, errors.New("合成权威版本冲突")
	}
	for _, row := range changes {
		key := row.Kind + ":" + row.ID
		if row.Deleted {
			delete(a.rows, key)
		} else {
			a.rows[key] = row
		}
	}
	a.version++
	return a.version, nil
}

type outgoingTextRuntime struct {
	*answeringRuntime
	flow       *Flow
	prepared   shared.PreparedTask
	badReceipt bool
}

// RequestAutoReplyCandidateInfo 保留正式BOSS的可选能力合同；本组没有提议索要资料，意外调用必须失败。
func (r *outgoingTextRuntime) RequestAutoReplyCandidateInfo(context.Context, model.Browser, model.Config, model.AutoReplyConversationSnapshot, model.CandidateInfoRequest) (model.AutoReplyInfoRequestResult, error) {
	return model.AutoReplyInfoRequestResult{}, errors.New("合成用例不应执行未提议的原生资料索要")
}

// SendAutoReplyWithReceipt 沿真实服务器意图校验正文，再返回合成原生回执，不对已发气泡做空白归一化。
func (r *outgoingTextRuntime) SendAutoReplyWithReceipt(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, body string) (model.ConversationMessage, error) {
	id := cloud.AutoReplySendIntentFromContext(ctx)
	if id == "" {
		return model.ConversationMessage{}, errors.New("缺少服务器发送意图")
	}
	err := r.flow.Cloud.ServerAutoReplyTransaction(ctx, credentials(r.prepared), func(tx *storage.AutoReplyTx) error {
		row, found, err := tx.Get("sends", id)
		if err != nil || !found {
			return errors.Join(err, errors.New("设备未找到服务器发送账本"))
		}
		var intent struct {
			ConversationID string `json:"conversation_id"`
			Content        string `json:"content"`
		}
		if err = json.Unmarshal(row.Data, &intent); err != nil {
			return err
		}
		if intent.Content != body {
			return errors.New("服务器意图和设备输入正文不一致")
		}
		return cloud.ValidateAutoReplySendIntent(tx, id, intent.ConversationID, body)
	})
	if err != nil {
		return model.ConversationMessage{}, err
	}
	if err = r.answeringRuntime.SendAutoReplyMessage(ctx, browser, cfg, snapshot, body); err != nil {
		return model.ConversationMessage{}, err
	}
	if r.badReceipt {
		r.latestAfter.TextContent = strings.ReplaceAll(body, " ", "")
	}
	return r.latestAfter, nil
}

// outgoingTextFlow 复用真实Flow与云客户端，只将权威仓储换成纯RAM版本，并加载正式平台能力配置。
func outgoingTextFlow(t *testing.T, baseURL, platformID string) (*Flow, shared.PreparedTask, *outgoingTextAuthority) {
	t.Helper()
	flow, prepared := newReliabilityFlow(t, baseURL)
	authority := &outgoingTextAuthority{rows: map[string]storage.AutoReplyStateChange{}}
	store, err := storage.OpenAutoReplyScratch(authority)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	flow.Store = store
	flow.Cloud = cloud.New(baseURL)
	flow.Cloud.EnableLocalAutoReply(store, t.TempDir())
	prepared.Platform, err = platform.LoadConfig(platformID)
	if err != nil {
		t.Fatal(err)
	}
	return flow, prepared, authority
}

// TestOutgoingFinalTextBindsAuthorityReceiptAndFollowup 验证BOSS换行在Claim前定稿、其它平台保留换行，未知或不同气泡不会被模糊接受或重发。
func TestOutgoingFinalTextBindsAuthorityReceiptAndFollowup(t *testing.T) {
	raw := " \r\n工作地点是成都。\r\n这部分我需要确认下。\n "
	for _, tc := range []struct {
		platform, want string
		badReceipt     bool
	}{
		{"boss", "工作地点是成都。 这部分我需要确认下。", false},
		{"boss", "工作地点是成都。 这部分我需要确认下。", true},
		{"liepin", "工作地点是成都。\r\n这部分我需要确认下。", false},
	} {
		t.Run(fmt.Sprintf("%s/bad_receipt=%t", tc.platform, tc.badReceipt), func(t *testing.T) {
			ctx := context.Background()
			flow, prepared, authority := outgoingTextFlow(t, "", tc.platform)
			conversation, err := flow.Cloud.SaveAutoReplyConversation(ctx, credentials(prepared), cloud.AutoReplyConversation{PlatformID: tc.platform, PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: "thread", PositionID: "position"})
			if err != nil {
				t.Fatal(err)
			}
			anchor := model.ConversationMessage{PlatformMessageID: "source", Direction: "candidate", MessageType: "text", TextContent: "请问地点？"}
			runtime := &outgoingTextRuntime{answeringRuntime: &answeringRuntime{autoReplyRuntimeStub: &autoReplyRuntimeStub{latestBefore: anchor}}, flow: flow, prepared: prepared, badReceipt: tc.badReceipt}
			sent, err := flow.sendVerifiedMessage(ctx, prepared, runtime, conversation, model.AutoReplyConversationSnapshot{}, anchor.PlatformMessageID, raw, true, 1)
			if sent == tc.badReceipt || (err != nil) != tc.badReceipt || len(runtime.replies) != 1 || runtime.replies[0] != tc.want {
				t.Fatalf("最终正文或严格回执不正确：sent=%v replies=%q err=%v", sent, runtime.replies, err)
			}
			status := "sent"
			if tc.badReceipt {
				status = "unknown"
			}
			id := cloud.AutoReplySendIntentID(conversation.ID, anchor.PlatformMessageID, 1)
			var intent struct{ Content, Status string }
			if err = json.Unmarshal(authority.rows["sends:"+id].Payload, &intent); err != nil || intent.Content != tc.want || intent.Status != status {
				t.Fatal("权威发送账本未保存最终正文和真实结果", intent, err)
			}
			ready, _, err := flow.latestMessageAllowsFollowup(ctx, runtime, prepared, model.AutoReplyConversationSnapshot{}, raw)
			if err != nil || ready == tc.badReceipt {
				t.Fatal("追问使用了旧换行正文或接受了不同气泡", ready, err)
			}
			if _, err = flow.sendVerifiedMessage(ctx, prepared, runtime, conversation, model.AutoReplyConversationSnapshot{}, anchor.PlatformMessageID, raw, false, 1); len(runtime.replies) != 1 || (err != nil) != tc.badReceipt {
				t.Fatal("送达或未知结果被自动重发", runtime.replies, err)
			}
		})
	}
}

// TestOutgoingKnownAnswerAndManualReceiptShareFinalText 验证混合问题的已知答案与固定待确认回执合成后，仍按同一最终正文登记和送达。
func TestOutgoingKnownAnswerAndManualReceiptShareFinalText(t *testing.T) {
	position := cloud.AutoReplyPositionSnapshot{OK: true, Position: cloud.AutoReplyPosition{ID: "position", Name: "工程师"},
		Config:         cloud.PositionAutoReplyConfig{PositionID: "position", Enabled: true, Version: 1, Phrases: []cloud.AutoReplyPhrase{{ID: "manual_followup", Content: "这个问题我需要确认下，\n稍后再回复你。"}}},
		CompanyProfile: cloud.CompanyProfile{ID: "company"}, Subscription: cloud.AutoReplySubscription{Active: true, AllowAutoReply: true}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/notifications") {
			_ = json.NewEncoder(w).Encode(cloud.AutoReplyNotificationResult{EmailSent: true})
			return
		}
		_ = json.NewEncoder(w).Encode(position)
	}))
	defer server.Close()
	flow, prepared, _ := outgoingTextFlow(t, server.URL, "boss")
	flow.Responder = &reliabilityResponder{decision: ReplyDecision{ManualReason: "具体工作内容需要HR确认", ReasonKey: "missing_recruitment_info", KnownAnswer: "工作地点是成都。\r\n具体地址待确认。"}}
	anchor := model.ConversationMessage{PlatformMessageID: "source", Direction: "candidate", MessageType: "text", TextContent: "请问工作地点和内容？"}
	page := model.AutoReplyConversationSnapshot{CandidateName: "合成人选", PlatformThreadID: "thread", PlatformAccountID: prepared.Request.PlatformAccountID, CommunicationPosition: "工程师", HistoryComplete: true, Messages: []model.ConversationMessage{anchor}}
	runtime := &outgoingTextRuntime{answeringRuntime: &answeringRuntime{autoReplyRuntimeStub: &autoReplyRuntimeStub{opened: page, latestBefore: anchor}}, flow: flow, prepared: prepared}
	item := model.Conversation{Key: "thread", PlatformThreadID: "thread", Name: page.CandidateName, CommunicationPosition: "工程师"}
	if err := flow.processConversation(context.Background(), prepared, runtime, []cloud.AutoReplyPositionSnapshot{position}, item, nil, &shared.Stats{}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.replies) != 1 || runtime.replies[0] != "工作地点是成都。 具体地址待确认。 这个问题我需要确认下， 稍后再回复你。" {
		t.Fatal("已知答案与人工回执的最终正文不一致", runtime.replies)
	}
}

type outgoingGoalResponder struct {
	goalPlanningResponder
}

// Reply 在既有动态目标提议里加入真实换行形式，不改变目标编号、版本或原文。
func (r *outgoingGoalResponder) Reply(ctx context.Context, input ReplyContext) (ReplyDecision, error) {
	decision, err := r.goalPlanningResponder.Reply(ctx, input)
	for i := range decision.Messages {
		if decision.Messages[i].Type == "answer" {
			decision.Messages[i].Content = "您好。\n工作地点是成都。"
		} else {
			decision.Messages[i].Content = "安排沟通前，\r\n想确认你方便哪个时间段？"
		}
	}
	return decision, err
}

// TestOutgoingBOSSAnswerThenGoalUsesFinalText 验证普通答复、同轮追问和答复后新增目标均以最终正文完成权威校验，目标只记一次已问。
func TestOutgoingBOSSAnswerThenGoalUsesFinalText(t *testing.T) {
	for _, laterReview := range []bool{false, true} {
		t.Run(fmt.Sprintf("later_review=%t", laterReview), func(t *testing.T) {
			position := cloud.AutoReplyPositionSnapshot{OK: true, Position: cloud.AutoReplyPosition{ID: "position", Name: "工程师"}, Config: cloud.PositionAutoReplyConfig{PositionID: "position", Version: 1, Enabled: true, Phrases: []cloud.AutoReplyPhrase{}, PositionDescription: "工作地点成都。安排沟通前了解候选人方便通话的时间段。"}, CompanyProfile: cloud.CompanyProfile{ID: "company"}, Subscription: cloud.AutoReplySubscription{Active: true, AllowAutoReply: true}}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(position) }))
			defer server.Close()
			flow, prepared, authority := outgoingTextFlow(t, server.URL, "boss")
			flow.Responder = &outgoingGoalResponder{goalPlanningResponder: goalPlanningResponder{afterAnswer: laterReview}}
			anchor := model.ConversationMessage{PlatformMessageID: "source", Direction: "candidate", MessageType: "text", TextContent: "有兴趣，请问工作地点？"}
			page := model.AutoReplyConversationSnapshot{CandidateName: "合成人选", PlatformThreadID: "thread", PlatformAccountID: prepared.Request.PlatformAccountID, CommunicationPosition: "工程师", HistoryComplete: true, Messages: []model.ConversationMessage{anchor}}
			runtime := &outgoingTextRuntime{answeringRuntime: &answeringRuntime{autoReplyRuntimeStub: &autoReplyRuntimeStub{opened: page, latestBefore: anchor}}, flow: flow, prepared: prepared}
			item := model.Conversation{Key: "thread", PlatformThreadID: "thread", Name: page.CandidateName, CommunicationPosition: "工程师"}
			if err := flow.processConversation(context.Background(), prepared, runtime, []cloud.AutoReplyPositionSnapshot{position}, item, nil, &shared.Stats{}); err != nil {
				t.Fatal(err)
			}
			if len(runtime.replies) != 2 || runtime.replies[0] != "您好。 工作地点是成都。" || runtime.replies[1] != "安排沟通前， 想确认你方便哪个时间段？" {
				t.Fatal("普通答复或目标追问未使用同一最终正文", runtime.replies)
			}
			goals, sends := 0, 0
			for _, row := range authority.rows {
				if row.Kind == "sends" {
					var intent struct{ Content, Status string }
					if err := json.Unmarshal(row.Payload, &intent); err != nil || strings.ContainsAny(intent.Content, "\r\n") || intent.Status != "sent" {
						t.Fatal("权威意图仍保存换行或没有确认送达", intent, err)
					}
					sends++
				}
				if row.Kind == "confirmations" {
					var goal cloud.CandidateConfirmationItem
					if err := json.Unmarshal(row.Payload, &goal); err != nil || goal.AskCount != 1 || goal.Content != "方便通话的时间" {
						t.Fatal("正文规范化改坏目标原文或重复记问", goal, err)
					}
					goals++
				}
			}
			if sends != 2 || goals != 1 {
				t.Fatal("发送账本或目标数量错误", sends, goals)
			}
		})
	}
}
