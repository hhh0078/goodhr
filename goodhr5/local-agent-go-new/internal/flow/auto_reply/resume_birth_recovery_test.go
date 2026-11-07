// Package auto_reply 本文件从真实会话处理入口验证已入库简历的生日补全与附件来源隔离。
package auto_reply

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// birthRecoveryRuntime 在模拟发送时读取本机资料，检验旧简历补全发生在回复之后。
type birthRecoveryRuntime struct {
	*answeringRuntime
	beforeSend func()
}

// SendAutoReplyMessage 在测试中记录发送时点，再交给已有页面替身产生可回读的消息。
func (r *birthRecoveryRuntime) SendAutoReplyMessage(ctx context.Context, browser model.Browser, config model.Config, snapshot model.AutoReplyConversationSnapshot, message string) error {
	r.beforeSend()
	return r.answeringRuntime.SendAutoReplyMessage(ctx, browser, config, snapshot, message)
}

// TestExistingResumeBirthRecoveryThroughConversation 覆盖普通新问题和已读复查，同时保持无来源、错附件及人工已有值。
func TestExistingResumeBirthRecoveryThroughConversation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auto-reply/agent/positions/position/snapshot" {
			t.Errorf("出现非配置请求：%s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true,"position":{"id":"position"},"config":{"position_id":"position","enabled":true,"version":1},"company_profile":{"id":"company"},"subscription":{"active":true,"allow_auto_reply":true}}`))
	}))
	defer server.Close()
	for _, tc := range []struct {
		name, candidateSource, attachmentSource, before, want string
		idle                                                  bool
	}{
		{"同份附件新问题", "resume-key", "同份", "", "1990-05", false},
		{"旧问题已回复", "resume-key", "同份", "", "1990-05", true},
		{"候选人无来源", "", "", "", "", false},
		{"附件无来源", "resume-key", "", "", "", false},
		{"附件是其他消息", "resume-key", "其他", "", "", false},
		{"已手工填写", "resume-key", "同份", "1988-10", "1988-10", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flow, prepared := newReliabilityFlow(t, server.URL)
			ctx := context.Background()
			cred := credentials(prepared)
			account := prepared.Request.PlatformAccountID
			lookup := cloud.AutoReplyCandidateLookup{PlatformID: prepared.Platform.ID, PlatformAccountID: account, PlatformThreadID: "thread"}
			conversation, err := flow.Cloud.SaveAutoReplyConversation(ctx, cred, cloud.AutoReplyConversation{PositionID: "position", PlatformID: lookup.PlatformID, PlatformAccountID: account, PlatformThreadID: lookup.PlatformThreadID, CandidateName: "合成候选人"})
			if err != nil {
				t.Fatal(err)
			}
			candidate, err := flow.Cloud.EnsureLocalAutoReplyCandidate(ctx, cred, conversation)
			if err != nil {
				t.Fatal(err)
			}
			original := "出 生：1990-05\n工作经历：2016-06至今"
			input := cloud.AutoReplyCandidateInput{LocalCandidateID: candidate.ID, PositionID: conversation.PositionID, PlatformID: lookup.PlatformID, PlatformAccountID: account, ResumeSourceMessageID: tc.candidateSource, StructuredCandidate: cloud.StructuredCandidate{CandidateName: conversation.CandidateName, WorkYears: "8", RawText: original, BirthYM: tc.before}}
			if tc.before != "" {
				input.BirthYMPrecision = "month"
			}
			if _, err := flow.Cloud.SaveAutoReplyCandidate(ctx, cred, input); err != nil {
				t.Fatal(err)
			}
			history := []cloud.AutoReplyMessage{}
			if tc.candidateSource != "" {
				history = append(history, cloud.AutoReplyMessage{Direction: "candidate", MessageType: "resume", PlatformMessageID: tc.candidateSource})
			}
			history = append(history, cloud.AutoReplyMessage{Direction: "self", MessageType: "text", PlatformMessageID: "old-answer"})
			if _, err := flow.Cloud.SyncAutoReplyMessages(ctx, cred, cloud.AutoReplyMessageSyncRequest{ConversationID: conversation.ID, Messages: history, HistoryComplete: true}); err != nil {
				t.Fatal(err)
			}
			stored, err := flow.Cloud.AutoReplyMessages(ctx, cred, conversation.ID)
			if err != nil {
				t.Fatal(err)
			}
			attachmentSource := ""
			switch tc.attachmentSource {
			case "同份":
				attachmentSource = stored[0].ID
			case "其他":
				attachmentSource = stored[len(stored)-1].ID
			}
			path := filepath.Join(t.TempDir(), "附件.txt")
			if err := os.WriteFile(path, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := flow.Cloud.UploadAutoReplyAttachment(ctx, cred, cloud.AutoReplyAttachmentUpload{ConversationID: conversation.ID, SourceMessageID: attachmentSource, FilePath: path}); err != nil {
				t.Fatal(err)
			}
			responder := &resumeRetryResponder{}
			flow.Responder = responder
			question := model.ConversationMessage{Direction: "candidate", MessageType: "text", PlatformMessageID: "new-question", TextContent: "工作内容是什么？"}
			snapshot := model.AutoReplyConversationSnapshot{CandidateName: conversation.CandidateName, PlatformThreadID: "thread", CommunicationPosition: "工程师"}
			wantAnswers := 0
			if !tc.idle {
				snapshot.Messages = []model.ConversationMessage{question}
				wantAnswers = 1
			}
			runtime := &birthRecoveryRuntime{answeringRuntime: &answeringRuntime{autoReplyRuntimeStub: &autoReplyRuntimeStub{opened: snapshot, latestBefore: question}}, beforeSend: func() {
				state, err := flow.Cloud.AutoReplyCandidateState(ctx, cred, lookup)
				if err != nil || state.Candidate == nil || state.Candidate.BirthYM != tc.before {
					t.Errorf("发送前改动了候选人资料：%v", err)
				}
			}}
			position := cloud.AutoReplyPositionSnapshot{Position: cloud.AutoReplyPosition{ID: "position", Name: "工程师"}, Config: cloud.PositionAutoReplyConfig{Version: 1}}
			row := model.Conversation{Key: "thread", PlatformThreadID: "thread", Name: snapshot.CandidateName, CommunicationPosition: snapshot.CommunicationPosition}
			if err := flow.processConversation(ctx, prepared, runtime, []cloud.AutoReplyPositionSnapshot{position}, row, nil, &shared.Stats{}); err != nil {
				t.Fatal(err)
			}
			state, err := flow.Cloud.AutoReplyCandidateState(ctx, cred, lookup)
			if err != nil || state.Candidate == nil {
				t.Fatalf("读取结果失败：%v", err)
			}
			got := state.Candidate
			if got.BirthYM != tc.want || (tc.want != "" && got.BirthYMPrecision != "month") || (tc.want == "" && got.BirthYMPrecision != "") || got.ID != candidate.ID || got.RawText != original || got.WorkYears != "8" || got.ResumeSourceMessageID != tc.candidateSource {
				t.Fatalf("补全后的年月=%q 精度=%q，来源=%q，原有资料未完整保留或补全结果错误", got.BirthYM, got.BirthYMPrecision, got.ResumeSourceMessageID)
			}
			if len(runtime.replies) != wantAnswers || responder.answers != wantAnswers || responder.structures != 0 || responder.reviews != 0 || runtime.resumeCollects != 0 || len(state.Attachments) != 1 {
				t.Fatalf("发送=%d 回答=%d 补审=%d 结构化=%d 采集=%d 附件=%d", len(runtime.replies), responder.answers, responder.reviews, responder.structures, runtime.resumeCollects, len(state.Attachments))
			}
		})
	}
}
