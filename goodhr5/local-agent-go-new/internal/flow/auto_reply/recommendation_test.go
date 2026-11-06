// Package auto_reply 本文件使用本机替身验证持久推荐恢复与生成途中版本改变的安全丢弃。
package auto_reply

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/storage"
)

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
				_, err = client.SaveAutoReplyConfirmationItem(ctx, creds, cloud.CandidateConfirmationItem{ID: "condition", ConversationID: conv.ID, CandidateID: conv.CandidateID, PositionID: "position", ItemType: "required", Content: "接受出差", Status: "matched", StatusReason: "候选人已明确答复", SourceRef: "message", EvidenceText: "我接受出差"})
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
				source, qualified := recommendationSource(snapshot, *state.Candidate, items)
				if !qualified {
					t.Fatal("合成条件应满足推荐资格")
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
