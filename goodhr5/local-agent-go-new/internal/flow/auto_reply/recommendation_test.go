// Package auto_reply 本文件使用本机替身验证持久推荐恢复与生成途中版本改变的安全丢弃。
package auto_reply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/storage"
)

// TestRecommendationFailureSummary 验证待办可记录服务类别和格式出错位置，同时隔离模型正文与凭据。
func TestRecommendationFailureSummary(t *testing.T) {
	for _, sample := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("生成：%w", &ai.ServiceError{StatusCode: 401, Message: "private-provider-body"}), "HTTP 401"},
		{fmt.Errorf("生成：%w", context.DeadlineExceeded), "超时"},
		{context.Canceled, "中断"},
		{errors.New("private-provider-body"), "推荐暂时没完成"},
	} {
		message := recommendationFailureSummary(sample.err)
		if !strings.Contains(message, sample.want) || strings.Contains(message, "private-provider-body") {
			t.Fatalf("推荐错误分类不安全，期望%s，得到%s", sample.want, message)
		}
	}
}

// TestRecommendationRequiresEveryEnabledHRGate 验证有证据的旧 AI 条目不能单独触发推荐，也不能替代缺失或变更的有效条件。
func TestRecommendationRequiresEveryEnabledHRGate(t *testing.T) {
	input := proposalState().input
	input.CandidateState.Candidate = &cloud.AutoReplyStoredCandidate{ID: "candidate", ResumeStructured: true}
	configured := cloud.PositionReplyCondition{ID: "travel", Type: "confirm", Content: "接受出差", Enabled: true}
	legitimate := cloud.CandidateConfirmationItem{ID: "configured", SourceType: "position", PositionConditionID: "travel", PositionID: "position", ItemType: "confirm", Content: "接受出差", Status: "matched", SourceRef: "message-1", EvidenceText: "我可以接受出差"}
	old := legitimate
	old.ID, old.SourceType, old.PositionConditionID = "legacy", "ai", ""
	for _, scenario := range []struct {
		name   string
		config []cloud.PositionReplyCondition
		items  []cloud.CandidateConfirmationItem
	}{
		{"零配置", nil, []cloud.CandidateConfirmationItem{old}},
		{"必要项缺失", []cloud.PositionReplyCondition{configured}, []cloud.CandidateConfirmationItem{old}},
		{"重复绑定不唯一", []cloud.PositionReplyCondition{configured}, []cloud.CandidateConfirmationItem{legitimate, legitimate}},
		{"正文变更", []cloud.PositionReplyCondition{{ID: "travel", Type: "confirm", Content: "接受出国出差", Enabled: true}}, []cloud.CandidateConfirmationItem{legitimate}},
		{"停用", []cloud.PositionReplyCondition{{ID: "travel", Type: "confirm", Content: "接受出差", Enabled: false}}, []cloud.CandidateConfirmationItem{legitimate}},
	} {
		input.Position.Config.Conditions = scenario.config
		if _, qualified := recommendationSource(input, scenario.items); qualified {
			t.Errorf("%s 不能生成推荐", scenario.name)
		}
	}
}

// TestRecommendationRecoveryChecksCurrentVersion 验证没有新聊天也会恢复队列，生成期间岗位变化时不保存或发布旧报告。
func TestRecommendationRecoveryChecksCurrentVersion(t *testing.T) {
	for _, changeDuringAI := range []string{"none", "config", "company", "message", "expired", "config_outage"} {
		for _, crashBeforeEnqueue := range []bool{false, true} {
			if changeDuringAI == "config_outage" && crashBeforeEnqueue {
				continue
			}
			t.Run(fmt.Sprintf("changed=%v/crash=%v", changeDuringAI, crashBeforeEnqueue), func(t *testing.T) {
				ctx := context.Background()
				dir := t.TempDir()
				store, err := storage.Open(filepath.Join(dir, "local.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				snapshot := cloud.AutoReplyPositionSnapshot{Position: cloud.AutoReplyPosition{ID: "position", Name: "合成岗位", PlatformID: "boss"}, Config: cloud.PositionAutoReplyConfig{PositionID: "position", Enabled: true, Version: 1}, CompanyProfile: cloud.CompanyProfile{ID: "company", Name: "合成公司"}, Subscription: cloud.AutoReplySubscription{Active: true, AllowAutoReply: true}}
				snapshot.Config.Conditions = []cloud.PositionReplyCondition{{ID: "configured-travel", Type: "required", Content: "接受出差", Enabled: true}}
				var mu sync.Mutex
				calls := 0
				configReads := 0
				var onModelCall func()
				serverURL := ""
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					switch r.URL.Path {
					case "/api/auto-reply/agent/positions/position/snapshot":
						_ = json.NewEncoder(w).Encode(snapshot)
					case "/api/config/effective-ai":
						configReads++
						if changeDuringAI == "config_outage" && configReads <= 4 {
							http.Error(w, "temporary unavailable", http.StatusServiceUnavailable)
							return
						}
						_ = json.NewEncoder(w).Encode(struct {
							Config cloud.AIConfig `json:"config"`
						}{cloud.AIConfig{BaseURL: serverURL, APIKey: "synthetic", Model: "synthetic"}})
					case "/v1/chat/completions":
						calls++
						if changeDuringAI == "config" {
							snapshot.Config.Version++
						}
						if changeDuringAI == "company" {
							snapshot.CompanyProfile.Overview = "公司资料已改变"
						}
						if changeDuringAI == "expired" {
							snapshot.Subscription.Active = false
						}
						if onModelCall != nil {
							onModelCall()
						}
						text, _ := json.Marshal(`{"match_score":90,"recommendation_level":"建议推进","executive_summary":"合成分析"}`)
						_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":%s}}]}`, text)
					default:
						t.Errorf("不应访问接口：%s", r.URL.Path)
						http.NotFound(w, r)
					}
				}))
				defer server.Close()
				serverURL = server.URL
				client := cloud.New(server.URL)
				client.EnableLocalAutoReply(store, filepath.Join(dir, "private"))
				creds := cloud.AgentCredentials{Token: "synthetic", MachineID: "machine"}
				candidateResult, err := client.SaveAutoReplyCandidate(ctx, creds, cloud.AutoReplyCandidateInput{StructuredCandidate: cloud.StructuredCandidate{CandidateName: "合成人选", Phone: "13800138000"}, PlatformID: "boss", PlatformAccountID: "account", PlatformCandidateID: "person", PositionID: "position"})
				if err != nil {
					t.Fatal(err)
				}
				conv, err := client.SaveAutoReplyConversation(ctx, creds, cloud.AutoReplyConversation{PlatformID: "boss", PlatformAccountID: "account", PlatformThreadID: "thread", CandidateID: candidateResult.CandidateID, PlatformIdentityID: candidateResult.PlatformIdentity.ID, PositionID: "position"})
				if err != nil {
					t.Fatal(err)
				}
				if changeDuringAI == "message" {
					onModelCall = func() {
						if _, err := client.SyncAutoReplyMessages(ctx, creds, cloud.AutoReplyMessageSyncRequest{ConversationID: conv.ID, Messages: []cloud.AutoReplyMessage{{Fingerprint: "newer", Direction: "candidate", TextContent: "我不能出差了"}}}); err != nil {
							t.Error(err)
						}
					}
				}
				messages := []cloud.AutoReplyMessage{{Fingerprint: "message", Direction: "candidate", TextContent: "我接受出差"}}
				if _, err = client.SyncAutoReplyMessages(ctx, creds, cloud.AutoReplyMessageSyncRequest{ConversationID: conv.ID, Messages: messages}); err != nil {
					t.Fatal(err)
				}
				_, err = client.SaveAutoReplyConfirmationItem(ctx, creds, cloud.CandidateConfirmationItem{ID: "condition", SourceType: "position", PositionConditionID: "configured-travel", ConversationID: conv.ID, CandidateID: conv.CandidateID, PositionID: "position", ItemType: "required", Content: "接受出差", Status: "matched", StatusReason: "候选人已明确答复", SourceRef: "message", EvidenceText: "我接受出差"})
				if err != nil {
					t.Fatal(err)
				}
				// 旧版会从岗位名称擅自增加门槛；即使旧证据不可用，新生成及发布也只复核已配置的出差条件。
				_, err = client.SaveAutoReplyConfirmationItem(ctx, creds, cloud.CandidateConfirmationItem{
					ID: "legacy-ai", SourceType: "ai", ConversationID: conv.ID, CandidateID: conv.CandidateID,
					PositionID: "position", ItemType: "confirm", Content: "具备高中数学教学经验", Status: "unmatched",
					SourceRef: "legacy", EvidenceText: "历史原文不可用",
				})
				if err != nil {
					t.Fatal(err)
				}
				state, err := client.AutoReplyCandidateState(ctx, creds, cloud.AutoReplyCandidateLookup{PlatformID: "boss", PlatformAccountID: "account", PlatformThreadID: "thread"})
				if err != nil || state.Candidate == nil {
					t.Fatalf("candidate %v %v", state, err)
				}
				responder := &AIResponder{AI: newAIClient(), Cloud: client}
				input := ReplyContext{Credentials: creds, Position: snapshot, Conversation: conv, CandidateState: state, Messages: messages, BasedOnMessageKey: "message"}
				if crashBeforeEnqueue {
					// 模拟业务事务已经提交、已发送自己消息，但还未来得及登记推荐就退出。
					err = client.CommitAutoReplyProposal(ctx, creds, conv.ID, "message", 1, nil, nil)
					if err == nil {
						_, err = client.SyncAutoReplyMessages(ctx, creds, cloud.AutoReplyMessageSyncRequest{ConversationID: conv.ID, Messages: []cloud.AutoReplyMessage{{Fingerprint: "self", Direction: "self", TextContent: "收到"}}})
					}
				} else {
					err = responder.QueueRecommendation(ctx, input)
				}
				if err != nil {
					t.Fatal(err)
				}
				jobs, err := client.PendingAutoReplyRecommendations(ctx, creds)
				expectedJobs := 1
				if crashBeforeEnqueue {
					expectedJobs = 0
				}
				if err != nil || len(jobs) != expectedJobs {
					t.Fatalf("jobs %d %v", len(jobs), err)
				}
				items, err := client.AutoReplyConfirmationItems(ctx, creds, conv.ID)
				if err != nil {
					t.Fatal(err)
				}
				source, qualified := recommendationSource(input, items)
				if !qualified || len(source.Conditions) != 1 || source.Conditions[0].ID != "condition" {
					t.Fatalf("推荐应只评估 HR 设置的条件：%+v", source.Conditions)
				}
				hash, err := recommendationInputHash(source)
				if err != nil {
					t.Fatal(err)
				}
				// 重新创建处理器模拟客户端重启；不再同步任何消息，直接从持久待办恢复。
				responder = &AIResponder{AI: newAIClient(), Cloud: client}
				if changeDuringAI == "config_outage" {
					// 直接模拟四次到期重试，不等待真实分钟；配置断网不能消耗模型生成额度。
					for attempt := 0; attempt < 4; attempt++ {
						if completed, err := responder.processRecommendation(ctx, creds, jobs[0]); err == nil || completed {
							t.Fatalf("断网重试结果错误：%v %v", completed, err)
						}
					}
					mu.Lock()
					callsBeforeRecovery := calls
					mu.Unlock()
					if callsBeforeRecovery != 0 {
						t.Fatalf("配置断网却调用模型%d次", callsBeforeRecovery)
					}
				}
				if err = responder.ProcessPendingRecommendations(ctx, creds); err != nil {
					t.Fatal(err)
				}
				if crashBeforeEnqueue {
					// 收据恢复只负责登记，下次后台周期优先处理已经登记的报告。
					if err = responder.ProcessPendingRecommendations(ctx, creds); err != nil {
						t.Fatal(err)
					}
				}
				mu.Lock()
				count := calls
				mu.Unlock()
				if count != 1 {
					t.Fatalf("模型调用次数=%d", count)
				}
				pending, err := client.PendingAutoReplyRecommendations(ctx, creds)
				if err != nil || len(pending) != 0 {
					t.Fatalf("任务未结束 %v %v", pending, err)
				}
				receipts, err := client.PendingAutoReplyProposals(ctx, creds)
				if err != nil || len(receipts) != 0 {
					t.Fatalf("提交收据未完成：%+v %v", receipts, err)
				}
				report, _, err := client.ClaimAutoReplyReport(ctx, creds, hash)
				if err != nil {
					t.Fatal(err)
				}
				stale := changeDuringAI == "config" || changeDuringAI == "company" || changeDuringAI == "message" || changeDuringAI == "expired"
				if stale && len(report) > 0 {
					t.Fatal("生成途中条件已改变，却保留了旧报告")
				}
				if !stale && len(report) == 0 {
					t.Fatal("有效待办未生成报告")
				}
				if changeDuringAI == "config_outage" {
					if completed, err := responder.processRecommendation(ctx, creds, jobs[0]); err != nil || !completed {
						t.Fatalf("缓存报告恢复失败：%v %v", completed, err)
					}
					mu.Lock()
					defer mu.Unlock()
					if calls != 1 {
						t.Fatalf("已生成报告重复付费%d次", calls)
					}
				}
			})
		}
	}
}
