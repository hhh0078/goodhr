// Package auto_reply 本文件验证候选人主动简历邀约只同意一次，旧位置的新文件仍被登记，停止联系与未知结果不会补点。
package auto_reply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// resumeOfferFlowFixture 在已有HR回复之前补入旧版已处理邀请，返回同意后替换该行的新文件窗口。
func resumeOfferFlowFixture(t *testing.T) (*Flow, shared.PreparedTask, cloud.AutoReplyPositionSnapshot, cloud.AutoReplyConversation, model.AutoReplyConversationSnapshot, *nativeRequestRuntime) {
	t.Helper()
	f, p, pos, conv, snapshot, _, r := nativeRequestFixture(t)
	ctx := context.Background()
	at := time.Date(2026, 11, 10, 1, 0, 0, 0, time.UTC)
	offer := model.ConversationMessage{Key: "offer", Direction: "candidate", MessageType: "card", TextContent: "对方想发送附件简历给您，您是否同意", CardContent: json.RawMessage(`{"info_type":"resume","info_status":"offered"}`), SentAt: &at}
	snapshot.Messages = []model.ConversationMessage{offer, r.latestBefore}
	messages, _, _ := convertMessages(snapshot.Messages)
	if _, err := f.Cloud.SyncAutoReplyMessages(ctx, credentials(p), cloud.AutoReplyMessageSyncRequest{ConversationID: conv.ID, Messages: messages}); err != nil {
		t.Fatal(err)
	}
	conv, _, _ = f.Cloud.AutoReplyAnswerConversation(ctx, credentials(p), conv.ID)
	file := model.ConversationMessage{Key: "new-pdf", Direction: "candidate", MessageType: "resume", TextContent: "候选人附件.pdf", CardContent: json.RawMessage(`{"info_type":"resume","info_status":"received"}`), SentAt: &at}
	r.result = model.AutoReplyInfoRequestResult{Status: model.AutoReplyInfoReceived, EvidenceMessageID: "new-pdf", EvidenceMessage: &file, EvidenceMessages: []model.ConversationMessage{file, r.latestBefore}}
	return f, p, pos, conv, snapshot, r
}

// TestResumeOfferAcceptsBackfillAndNeverRollsBackNewSource 验证先同步真实文件、只执行一次同意，旧邀请不覆盖后来的文件B待办。
func TestResumeOfferAcceptsBackfillAndNeverRollsBackNewSource(t *testing.T) {
	f, p, pos, conv, snapshot, r := resumeOfferFlowFixture(t)
	ctx := context.Background()
	if err := f.acceptResumeOffer(ctx, p, r, pos, &conv, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 1 || r.calls[0] != "resume" || r.anchors[0] != "answer-1" || r.sent || r.resumeCollects != 0 || conv.ResumeRetry == nil || conv.ResumeRetry.SourceMessageID != "new-pdf" || snapshot.ResumeSourceMessageID != "new-pdf" {
		t.Fatal("接收与下载来源不一致", r.calls, conv.ResumeRetry)
	}
	if _, err := f.Cloud.EnqueueAutoReplyResumeRetry(ctx, credentials(p), conv.ID, "file-B"); err != nil {
		t.Fatal(err)
	}
	conv, _, _ = f.Cloud.AutoReplyAnswerConversation(ctx, credentials(p), conv.ID)
	snapshot.Messages = []model.ConversationMessage{{Key: "offer", Direction: "candidate", MessageType: "card", CardContent: json.RawMessage(`{"info_type":"resume","info_status":"offered"}`)}, r.latestBefore}
	snapshot.ResumeSourceMessageID = "file-B"
	if err := f.acceptResumeOffer(ctx, p, r, pos, &conv, &snapshot); err != nil {
		t.Fatal(err)
	}
	current, _, _ := f.Cloud.AutoReplyAnswerConversation(ctx, credentials(p), conv.ID)
	if len(r.calls) != 1 || current.ResumeRetry == nil || current.ResumeRetry.SourceMessageID != "file-B" || snapshot.ResumeSourceMessageID != "file-B" {
		t.Fatal("旧邀约使新附件退回旧来源")
	}
}

// TestResumeOfferUnknownAndSuperseded 验证未知同意结果永不补点，明确未执行才释放账本允许重新读取。
func TestResumeOfferUnknownAndSuperseded(t *testing.T) {
	for _, mode := range []string{"unknown", "superseded", "deferred"} {
		t.Run(mode, func(t *testing.T) {
			f, p, pos, conv, snapshot, r := resumeOfferFlowFixture(t)
			if mode == "unknown" {
				r.requestErr = errors.New("点击后回执超时")
			}
			if mode == "superseded" {
				r.requestErr = model.ErrAutoReplySupersededBeforeSend
			}
			if mode == "deferred" {
				r.result.Status = model.AutoReplyInfoDeferred
			}
			err := f.acceptResumeOffer(context.Background(), p, r, pos, &conv, &snapshot)
			status, _, _ := f.Cloud.AutoReplySendReceipt(context.Background(), credentials(p), conv.ID, cloud.AutoReplyResumeOfferSendKey("offer"), 0)
			if mode == "unknown" {
				if err != nil || status != "unknown" {
					t.Fatal("未知动作未阻止补点", status, err)
				}
				reads := r.openReadCount
				r.openReadErr = errors.New("已经暂停的动作不应再次读取页面")
				if err = f.acceptResumeOffer(context.Background(), p, r, pos, &conv, &snapshot); err != nil {
					t.Fatal(err)
				}
				if len(r.calls) != 1 || r.openReadCount != reads {
					t.Fatal("未知结果重复同意")
				}
			} else if status != "" || mode == "superseded" && !errors.Is(err, model.ErrAutoReplySupersededBeforeSend) || mode == "deferred" && err != nil {
				t.Fatal("未执行动作没有释放", status, err)
			}
			if conv.ResumeRetry != nil {
				t.Fatal("未接收就登记附件或收到状态")
			}
		})
	}
}

// TestUnknownResumeOfferStillDownloadsObservedNewPDF 验证同意账本未知时，后来确实出现的新文件仍进入原有下载，不需要补点同意。
func TestUnknownResumeOfferStillDownloadsObservedNewPDF(t *testing.T) {
	f, p, pos, conv, snapshot, r := resumeOfferFlowFixture(t)
	ctx := context.Background()
	if err := f.Cloud.CompleteAutoReplyMessage(ctx, credentials(p), conv.ID, "in-1", "sent"); err != nil {
		t.Fatal(err)
	}
	key := cloud.AutoReplyResumeOfferSendKey("offer")
	if _, _, err := f.Cloud.ClaimAutoReplySend(ctx, credentials(p), conv.ID, key, cloud.AutoReplyResumeOfferContent, 0); err != nil {
		t.Fatal(err)
	}
	if err := f.Cloud.FinishAutoReplySend(ctx, credentials(p), conv.ID, key, 0, "unknown"); err != nil {
		t.Fatal(err)
	}
	snapshot.Messages = r.result.EvidenceMessages
	snapshot.ResumeCardAvailable = true
	snapshot.ResumeSourceMessageID = "new-pdf"
	snapshot.CommunicationPosition = "工程师"
	pos.Position.Name = "工程师"
	r.opened = snapshot
	r.resumeErr = errors.New("下载测试暂停点")
	f.Responder = &reliabilityResponder{}
	item := model.Conversation{Key: "thread", PlatformThreadID: "thread", PlatformCandidateID: "page-id", PlatformAccountID: p.Request.PlatformAccountID, Name: snapshot.CandidateName, CommunicationPosition: "工程师"}
	if err := f.processConversation(ctx, p, r, []cloud.AutoReplyPositionSnapshot{pos}, item, &snapshot, &shared.Stats{}); err != nil {
		t.Fatal(err)
	}
	current, _, err := f.Cloud.AutoReplyAnswerConversation(ctx, credentials(p), conv.ID)
	if err != nil || r.resumeCollects != 1 || len(r.calls) != 0 || current.ResumeRetry == nil || current.ResumeRetry.SourceMessageID != "new-pdf" {
		t.Fatal("未知同意阻挡真实附件采集", r.resumeCollects, r.calls, current.ResumeRetry, err)
	}
}

type resumeOfferToolResponder struct {
	calls   int
	pending []cloud.AutoReplyMessage
	anchor  string
	control bool
}

// Reply 走真实提示词和发送工具准备，确保原生控制锚点存在但不会当作普通候选问题。
func (r *resumeOfferToolResponder) Reply(_ context.Context, input ReplyContext) (ReplyDecision, error) {
	if input.MessageFollowup || input.ConditionsOnly {
		return ReplyDecision{}, nil
	}
	r.calls++
	messages, err := initialToolMessages(input)
	if err != nil {
		return ReplyDecision{}, err
	}
	var dynamic replyPromptInput
	if err = json.Unmarshal([]byte(messages[len(messages)-1].Content), &dynamic); err != nil {
		return ReplyDecision{}, err
	}
	r.pending, r.anchor = input.PendingMessages, input.BasedOnMessageKey
	for _, message := range dynamic.Messages {
		if message.SourceRef == input.BasedOnMessageKey {
			r.control = message.NativeControlEvent
		}
	}
	state := toolExecutionState{input: input}
	if _, err = state.prepareMessages(ai.ToolCall{Function: ai.ToolCallFunction{Arguments: `{"messages":[{"type":"answer","content":"工作地点在成都。"}]}`}}); err != nil {
		return ReplyDecision{}, err
	}
	return ReplyDecision{Messages: state.pendingMessages}, nil
}

// TestUnknownResumeOfferDoesNotBlockPreparedReply 验证普通问题在控制卡之前或之后，均实际走提示词、工具准备、服务器Claim和严格送达，不会再点同意。
func TestUnknownResumeOfferDoesNotBlockPreparedReply(t *testing.T) {
	for _, before := range []bool{true, false} {
		for _, status := range []string{"unknown", "sending"} {
			t.Run(fmt.Sprintf("question_before=%v/status=%s", before, status), func(t *testing.T) {
				f, p, pos, conv, snapshot, native := resumeOfferFlowFixture(t)
				ctx := context.Background()
				question := model.ConversationMessage{Key: "new-question", Direction: "candidate", MessageType: "text", TextContent: "请问工作地点在哪里？"}
				offer := snapshot.Messages[0]
				offer.Key = "pending-offer"
				snapshot.Messages = []model.ConversationMessage{native.latestBefore, offer, question}
				if before {
					snapshot.Messages = []model.ConversationMessage{native.latestBefore, question, offer}
				}
				snapshot.CommunicationPosition = "工程师"
				pos.Position.Name = "工程师"
				messages, _, _ := convertMessages(snapshot.Messages)
				if _, err := f.Cloud.SyncAutoReplyMessages(ctx, credentials(p), cloud.AutoReplyMessageSyncRequest{ConversationID: conv.ID, Messages: messages}); err != nil {
					t.Fatal(err)
				}
				key := cloud.AutoReplyResumeOfferSendKey("pending-offer")
				if _, _, err := f.Cloud.ClaimAutoReplySend(ctx, credentials(p), conv.ID, key, cloud.AutoReplyResumeOfferContent, 0); err != nil {
					t.Fatal(err)
				}
				if status == "unknown" {
					if err := f.Cloud.FinishAutoReplySend(ctx, credentials(p), conv.ID, key, 0, status); err != nil {
						t.Fatal(err)
					}
				}
				responder := &resumeOfferToolResponder{}
				f.Responder = responder
				runtime := &outgoingTextRuntime{answeringRuntime: &answeringRuntime{autoReplyRuntimeStub: &autoReplyRuntimeStub{opened: snapshot, latestBefore: snapshot.Messages[len(snapshot.Messages)-1]}}, flow: f, prepared: p}
				item := model.Conversation{Key: "thread", PlatformThreadID: "thread", PlatformCandidateID: "page-id", PlatformAccountID: p.Request.PlatformAccountID, Name: snapshot.CandidateName, CommunicationPosition: "工程师"}
				if err := f.processConversation(ctx, p, runtime, []cloud.AutoReplyPositionSnapshot{pos}, item, &snapshot, &shared.Stats{}); err != nil {
					t.Fatal(err)
				}
				anchor := "new-question"
				if before {
					anchor = "pending-offer"
				}
				if responder.calls != 1 || len(responder.pending) != 1 || responder.pending[0].TextContent != question.TextContent || responder.anchor != anchor || responder.control != before || len(runtime.replies) != 1 {
					t.Fatal("普通问题未完成真实准备和送达", responder, runtime.replies)
				}
				if sent, source, err := f.Cloud.AutoReplySendReceipt(ctx, credentials(p), conv.ID, anchor, 1); err != nil || sent != "sent" || source != "out-1" {
					t.Fatal("普通答复缺少独立严格送达账本", sent, source, err)
				}
				if saved, _, err := f.Cloud.AutoReplySendReceipt(ctx, credentials(p), conv.ID, key, 0); err != nil || saved != status {
					t.Fatal("同意的未知账本被改写", saved, err)
				}
				if pending, err := f.Cloud.PendingAutoReplyMessages(ctx, credentials(p), conv.ID); err != nil || len(pending) != 0 {
					t.Fatal("问题没有完成", pending, err)
				}
			})
		}
	}
}

type resumeOfferHistoryRuntime struct {
	*nativeRequestRuntime
	opens, reads int
}

// OpenAutoReplyConversation 记录是否又为已暂停的同意动作重开聊天。
func (r *resumeOfferHistoryRuntime) OpenAutoReplyConversation(ctx context.Context, b model.Browser, cfg model.Config, c model.Conversation, known []string, maxHistory int) (model.AutoReplyConversationSnapshot, error) {
	r.opens++
	return r.nativeRequestRuntime.OpenAutoReplyConversation(ctx, b, cfg, c, known, maxHistory)
}

// ReadAutoReplyMessages 记录历史采集次数，第二轮稳定列表只能轻读最新消息。
func (r *resumeOfferHistoryRuntime) ReadAutoReplyMessages(ctx context.Context, b model.Browser, cfg model.Config, s model.AutoReplyConversationSnapshot, known []string, maxHistory int) ([]model.ConversationMessage, bool, error) {
	r.reads++
	return r.nativeRequestRuntime.ReadAutoReplyMessages(ctx, b, cfg, s, known, maxHistory)
}

// TestUnknownResumeOfferConvergesListAndCapture 验证只有旧控制卡时清除采集待办、确认列表水位，下一轮不再重开或读取历史。
func TestUnknownResumeOfferConvergesListAndCapture(t *testing.T) {
	f, p, pos, conv, snapshot, native := resumeOfferFlowFixture(t)
	ctx := context.Background()
	p.Platform.Behavior.AutoReplyAllContactInbox = true
	pos.Position.Name = "工程师"
	snapshot.CommunicationPosition = "工程师"
	offer := snapshot.Messages[0]
	offer.Key = "pending-offer"
	snapshot.Messages = []model.ConversationMessage{native.latestBefore, offer}
	native.latestBefore = offer
	native.opened = snapshot
	key := cloud.AutoReplyResumeOfferSendKey(offer.Key)
	if _, _, err := f.Cloud.ClaimAutoReplySend(ctx, credentials(p), conv.ID, key, cloud.AutoReplyResumeOfferContent, 0); err != nil {
		t.Fatal(err)
	}
	if err := f.Cloud.FinishAutoReplySend(ctx, credentials(p), conv.ID, key, 0, "unknown"); err != nil {
		t.Fatal(err)
	}
	item := model.Conversation{Key: "thread", PlatformThreadID: "thread", PlatformCandidateID: "page-id", PlatformAccountID: p.Request.PlatformAccountID, Name: snapshot.CandidateName, CommunicationPosition: "工程师", Fields: map[string]string{"contact_list_signature": "baseline"}}
	if _, _, err := f.reviewAllContacts(ctx, p, []model.Conversation{item}); err != nil {
		t.Fatal(err)
	}
	item.Fields["contact_list_signature"] = "changed"
	r := &resumeOfferHistoryRuntime{nativeRequestRuntime: native}
	policy := &shared.ConsecutiveErrorPolicy{}
	processed, opened, err := f.processCheckpoint(ctx, p, captureUnreadScanner{items: []model.Conversation{item}}, r, []cloud.AutoReplyPositionSnapshot{pos}, 1, &shared.Stats{}, policy, nil)
	if err != nil || processed != 1 || len(native.calls) != 0 {
		t.Fatal("旧未知卡未安全收敛", processed, err)
	}
	if pending, err := f.Cloud.ListPendingAutoReplyConversations(ctx, credentials(p), "boss", p.Request.PlatformAccountID); err != nil || len(pending) != 0 {
		t.Fatal("旧卡留下循环采集待办", pending, err)
	}
	opens, reads := r.opens, r.reads
	processed, _, err = f.processCheckpoint(ctx, p, captureUnreadScanner{items: []model.Conversation{item}}, r, []cloud.AutoReplyPositionSnapshot{pos}, 1, &shared.Stats{}, policy, opened)
	if err != nil || processed != 0 || r.opens != opens || r.reads != reads || len(native.calls) != 0 {
		t.Fatal("稳定水位仍重开或读历史", processed, r.opens, r.reads, err)
	}
}

// TestResumeOfferCandidateStopRemainsForNormalProcessing 验证邀请后明确停止联系时不点同意，也不吞掉候选人停止原文。
func TestResumeOfferCandidateStopRemainsForNormalProcessing(t *testing.T) {
	f, p, pos, conv, snapshot, r := resumeOfferFlowFixture(t)
	stop := model.ConversationMessage{Key: "stop", Direction: "candidate", MessageType: "text", TextContent: "不要联系了，我不考虑了"}
	snapshot.Messages = append(snapshot.Messages, stop)
	messages, _, _ := convertMessages(snapshot.Messages)
	if _, err := f.Cloud.SyncAutoReplyMessages(context.Background(), credentials(p), cloud.AutoReplyMessageSyncRequest{ConversationID: conv.ID, Messages: messages}); err != nil {
		t.Fatal(err)
	}
	r.latestBefore = stop
	if err := f.acceptResumeOffer(context.Background(), p, r, pos, &conv, &snapshot); err != nil {
		t.Fatal("停止消息被错误挡在普通处理前", err)
	}
	pending, err := f.Cloud.PendingAutoReplyMessages(context.Background(), credentials(p), conv.ID)
	pending = withoutResumeOffers(pending)
	if err != nil || len(r.calls) != 0 || len(pending) == 0 || pending[len(pending)-1].TextContent != stop.TextContent {
		t.Fatal("停止后仍点击或原文丢失", r.calls, pending, err)
	}
}

// TestResumeOfferExactPDFWindowEvidence 验证窗口必须含当前锚点，且真实附件的正文、时间和typed元数据逐项一致。
func TestResumeOfferExactPDFWindowEvidence(t *testing.T) {
	at := time.Now().UTC()
	file := model.ConversationMessage{Key: "pdf", Direction: "candidate", MessageType: "resume", TextContent: "附件.pdf", CardContent: json.RawMessage(`{"info_type":"resume","info_status":"received"}`), SentAt: &at}
	result := model.AutoReplyInfoRequestResult{Status: model.AutoReplyInfoReceived, EvidenceMessageID: "pdf", EvidenceMessage: &file}
	for _, mode := range []string{"valid", "old_anchor_only", "missing_anchor", "generic_metadata", "different_time", "different_body", "duplicate", "old_pdf", "offer_as_pdf"} {
		t.Run(mode, func(t *testing.T) {
			value := result
			window := []model.ConversationMessage{file, {Key: "current-hr", Direction: "self", MessageType: "text"}}
			known := map[string]bool{"old-hr": true, "current-hr": true}
			switch mode {
			case "old_anchor_only":
				window[1].Key = "old-hr"
			case "missing_anchor":
				window = window[:1]
			case "generic_metadata":
				window[0].CardContent = json.RawMessage(`{"summary":"附件"}`)
			case "different_time":
				later := at.Add(time.Second)
				window[0].SentAt = &later
			case "different_body":
				window[0].TextContent += "变化"
			case "duplicate":
				window = append(window, file)
			case "old_pdf":
				known["pdf"] = true
			case "offer_as_pdf":
				value.EvidenceMessageID = "offer"
			}
			got := resumeOfferPDFEvidence(value, "offer", known) && resumeOfferWindowMatches(window, value, "current-hr", "offer", known)
			if got != (mode == "valid") {
				t.Fatal("附件证据核验不符", mode, got)
			}
		})
	}
	if !resumeOfferWindowMatches([]model.ConversationMessage{file}, result, "offer", "offer", nil) {
		t.Fatal("唯一原邀约被文件替换时应保留兼容")
	}
}

// TestResumeOfferDerivedFromReadAndDeletionBoundary 验证优化纯读也能派生邀请，普通文字和删除前卡片不进入接收动作。
func TestResumeOfferDerivedFromReadAndDeletionBoundary(t *testing.T) {
	at := time.Now().UTC()
	offer := model.ConversationMessage{Key: "offer", Direction: "candidate", MessageType: "card", SentAt: &at, CardContent: json.RawMessage(`{"info_type":"resume","info_status":"offered"}`)}
	page := model.AutoReplyConversationSnapshot{ResumeOfferMessageID: "stale", Messages: []model.ConversationMessage{offer}}
	if visibleResumeOffer(page) != "offer" {
		t.Fatal("读取路径依赖了Open字段")
	}
	page.HistoryAfter = &at
	if visibleResumeOffer(page) != "" {
		t.Fatal("删除前卡片可同意")
	}
	messages := []cloud.AutoReplyMessage{{Direction: "candidate", MessageType: "text", TextContent: "我可以发简历吗"}, {Direction: offer.Direction, MessageType: offer.MessageType, CardContent: offer.CardContent}}
	if got := withoutResumeOffers(messages); len(got) != 1 || got[0].TextContent != messages[0].TextContent {
		t.Fatal("普通简历问题被吞掉")
	}
}

// TestResumeOfferProcessDownloadsWithoutPendingQuestion 验证旧已处理邀请在优化读取后仍会被接收，新PDF回填时没有待回复问题也能进入原有下载。
func TestResumeOfferProcessDownloadsWithoutPendingQuestion(t *testing.T) {
	f, p, pos, conv, snapshot, r := resumeOfferFlowFixture(t)
	ctx := context.Background()
	if err := f.Cloud.CompleteAutoReplyMessage(ctx, credentials(p), conv.ID, "in-1", "sent"); err != nil {
		t.Fatal(err)
	}
	pos.Position.Name = "工程师"
	snapshot.CommunicationPosition = "工程师"
	r.opened = snapshot
	r.resumeErr = errors.New("下载测试暂停点")
	responder := &reliabilityResponder{}
	f.Responder = responder
	conversation := model.Conversation{Key: "thread", PlatformThreadID: "thread", PlatformCandidateID: "page-id", PlatformAccountID: p.Request.PlatformAccountID, Name: snapshot.CandidateName, CommunicationPosition: "工程师"}
	err := f.processConversation(ctx, p, r, []cloud.AutoReplyPositionSnapshot{pos}, conversation, &snapshot, &shared.Stats{})
	if err != nil || len(r.calls) != 1 || r.resumeCollects != 1 || responder.calls != 0 || r.sent {
		t.Fatal("无新问题的回填附件没有进入下载，或错误调用问答/发送", err, r.calls, r.resumeCollects, responder.calls, r.sent)
	}
	current, _, readErr := f.Cloud.AutoReplyAnswerConversation(ctx, credentials(p), conv.ID)
	if readErr != nil || current.ResumeRetry == nil || current.ResumeRetry.SourceMessageID != "new-pdf" || current.ResumeRetry.Attempts != 1 {
		t.Fatal("下载重试来源或次数错误", current.ResumeRetry, readErr)
	}
}
