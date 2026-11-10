// Package cloud 本文件验证在线基础资料只进入服务器临时实体、简历库ID不混淆、开关关闭可缓存且图片不会入接口或权威缓存。
package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestOnlineProfileCachesWithoutPromotingAttachment 验证匹配API关闭入库或返回正式编号时都保留本轮实体ID，不把在线资料冒充已解析附件。
func TestOnlineProfileCachesWithoutPromotingAttachment(t *testing.T) {
	for _, canonical := range []string{"", "canonical-library-id"} {
		t.Run(canonical, func(t *testing.T) {
			var transmitted string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				transmitted = string(body)
				w.Header().Set("Content-Type", "application/json")
				result := AutoReplyOnlineProfileResult{CandidateID: canonical, MatchStatus: "temporary", ProfileKey: "server-source", Candidate: StructuredCandidate{CandidateName: "测试候选人", EducationLevel: "本科"}}
				if canonical != "" {
					result.MatchStatus = "matched"
				}
				_ = json.NewEncoder(w).Encode(result)
			}))
			defer server.Close()
			c, cred, _ := entityTestClient(t)
			c.baseURL = server.URL
			ctx := context.Background()
			conversation := entityTestConversation(t, c, cred)
			before, err := c.EnsureLocalAutoReplyCandidate(ctx, cred, conversation)
			if err != nil {
				t.Fatal(err)
			}
			input := AutoReplyOnlineProfileInput{PositionID: "position", ConversationID: conversation.ID, PlatformID: "boss", PlatformCandidateID: "page-id", PlatformAccountID: "account-one", Profile: model.AutoReplyOnlineProfile{CandidateName: "测试候选人", Mode: "ai", RawText: "测试候选人 本科", SourceKey: "image-source", Screenshots: [][]byte{[]byte("MUST-NOT-PERSIST")}}}
			if _, err = c.SaveAutoReplyOnlineProfile(ctx, cred, input); err != nil {
				t.Fatal(err)
			}
			state, err := c.AutoReplyCandidateState(ctx, cred, AutoReplyCandidateLookup{PlatformID: "boss", PlatformAccountID: "account-one", PlatformThreadID: "thread"})
			if err != nil {
				t.Fatal(err)
			}
			if state.Candidate == nil || state.Candidate.ID != before.ID || state.Candidate.OnlineProfile == nil || state.Candidate.OnlineProfile.CandidateID != canonical || state.Candidate.ResumeStructured || state.HasResumeAttachment {
				t.Fatal("在线资料混淆正式编号或附件状态")
			}
			if len(state.Candidate.OnlineProfile.Profile.Screenshots) != 0 || strings.Contains(transmitted, "screenshots") || strings.Contains(transmitted, "MUST-NOT-PERSIST") {
				t.Fatal("截图进入匹配API或权威缓存")
			}
			if state.Candidate.BasicInfo != "测试候选人 本科" || state.Candidate.EducationLevel != "本科" {
				t.Fatal("在线基础资料未用于候选人上下文")
			}
		})
	}
}

// TestOnlineProfileRejectsUnconfirmedServerResult 验证没有资料摘要的API响应不能伪造成成功缓存。
func TestOnlineProfileRejectsUnconfirmedServerResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"candidate_id":"x","match_status":"matched"}`)
	}))
	defer server.Close()
	c, cred, _ := entityTestClient(t)
	c.baseURL = server.URL
	conversation := entityTestConversation(t, c, cred)
	_, err := c.SaveAutoReplyOnlineProfile(context.Background(), cred, AutoReplyOnlineProfileInput{PositionID: "position", ConversationID: conversation.ID, PlatformID: "boss", PlatformCandidateID: "id", PlatformAccountID: "account-one", Profile: model.AutoReplyOnlineProfile{SourceKey: "source", RawText: "原文"}})
	if err == nil {
		t.Fatal("没有服务器摘要也被缓存成功")
	}
}
