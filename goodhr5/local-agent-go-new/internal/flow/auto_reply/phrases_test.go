// Package auto_reply 本文件验证实际模型 HTTP 请求读取岗位快照、完整话术与提示词，及固定签收和人工登记保护。
package auto_reply

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/storage"
)

// TestRuntimePromptsReachFourModelRequests 从两版服务器快照运行四种真实模型调用，确认改配置后下轮立即生效。
func TestRuntimePromptsReachFourModelRequests(t *testing.T) {
	ctx := context.Background()
	current := 1
	stage := ""
	total := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		marker := fmt.Sprint("岗位配置版本", current)
		if strings.HasSuffix(r.URL.Path, "/snapshot") {
			json.NewEncoder(w).Encode(cloud.AutoReplyPositionSnapshot{Position: cloud.AutoReplyPosition{ID: "position-1"}, CompanyProfile: cloud.CompanyProfile{ID: "co"}, Config: cloud.PositionAutoReplyConfig{PositionID: "position-1", Enabled: true, PhrasesVersion: marker, Phrases: []cloud.AutoReplyPhrase{{ID: "custom", Content: "完整话术" + marker}}, Prompts: cloud.AutoReplyPrompts{Dialogue: "对话" + marker, Review: "内部复核" + marker, Resume: "简历提取" + marker, Recommendation: "推荐报告" + marker}}})
			return
		}
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		total++
		var req struct {
			Messages []ai.ToolMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		var systems, user string
		for _, m := range req.Messages {
			if m.Role == "system" {
				systems += m.Content
			}
			if m.Role == "user" {
				user += m.Content
			}
		}
		if !strings.Contains(systems, stage+marker) || strings.Contains(user, stage+marker) {
			t.Errorf("提示词未进入正确角色: %s %s", stage, marker)
		}
		w.Header().Set("Content-Type", "application/json")
		switch stage {
		case "对话", "内部复核":
			if !strings.Contains(user, "完整话术"+marker) || !strings.Contains(systems, "手机号已有") {
				t.Error("本岗位话术或固定安全边界缺失")
			}
			if stage == "内部复核" {
				fmt.Fprint(w, toolCallResponse("rev", toolFinishReview, `{}`, 4))
			} else {
				fmt.Fprint(w, toolCallResponse("msg", toolSendMessages, `{"messages":[{"type":"answer","content":"方便聊聊。"}]}`, 4))
			}
		case "简历提取":
			if !strings.Contains(systems, "is_resume") || !strings.Contains(systems, "work_experiences") {
				t.Error("缺少固定简历字段")
			}
			content, _ := json.Marshal(`{"is_resume":true,"candidate_name":"甲","educations":[{"school_name":"示例学校"}]}`)
			fmt.Fprintf(w, `{"choices":[{"message":{"content":%s}}]}`, content)
		case "推荐报告":
			if !strings.Contains(systems, "match_score") {
				t.Error("缺少报告字段")
			}
			content, _ := json.Marshal(`{"match_score":80,"recommendation_level":"建议推进","executive_summary":"事实摘要"}`)
			fmt.Fprintf(w, `{"choices":[{"message":{"content":%s}}]}`, content)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	client := cloud.New(server.URL)
	client.EnableLocalAutoReply(store, dir)
	input := responderInput(server.URL)
	input.PendingMessages = input.Messages
	path := filepath.Join(dir, "示例简历.txt")
	if err = os.WriteFile(path, []byte("候选人甲，毕业于示例学校。"), 0600); err != nil {
		t.Fatal(err)
	}
	attachment, err := client.UploadAutoReplyAttachment(ctx, input.Credentials, cloud.AutoReplyAttachmentUpload{FilePath: path, ConversationID: input.Conversation.ID})
	if err != nil {
		t.Fatal(err)
	}
	responder := &AIResponder{AI: ai.New(), Cloud: client}
	for current = 1; current <= 2; current++ {
		input.Position, err = client.AutoReplySnapshot(ctx, input.Credentials, "position-1")
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range []string{"对话", "内部复核", "简历提取", "推荐报告"} {
			stage = s
			switch stage {
			case "对话", "内部复核":
				input.ConditionsOnly = stage == "内部复核"
				_, err = responder.Reply(ctx, input)
			case "简历提取":
				_, err = responder.StructureResume(ctx, ResumeStructureContext{Credentials: input.Credentials, AIConfig: input.AIConfig, Position: input.Position, Conversation: input.Conversation, Attachments: []cloud.StoredResumeAttachment{attachment}})
			case "推荐报告":
				_, err = responder.AI.GenerateAutoReplyRecommendation(ctx, input.AIConfig, cloud.AutoReplyRecommendationReport{Candidate: cloud.AutoReplyReportCandidate{ID: "c"}, Position: cloud.AutoReplyReportPosition{ID: "p"}}, input.Position.Config.Prompts.Recommendation)
			}
			if err != nil {
				t.Fatalf("version %d stage %s: %v", current, stage, err)
			}
		}
	}
	if total != 8 {
		t.Fatalf("模型请求%d次", total)
	}
}

// TestPhraseDisablingPhoneAndHandoff 验证禁用、历史拒绝、同号重复、语义归并及原话核验。
func TestPhraseDisablingPhoneAndHandoff(t *testing.T) {
	input := responderInput("")
	config := &input.Position.Config
	if fixedReplyPhrase(*config, "receive_resume") == "" {
		t.Fatal("旧配置缺签收")
	}
	config.PhrasesVersion = "v1"
	config.Phrases = []cloud.AutoReplyPhrase{}
	if fixedReplyPhrase(*config, "receive_resume") != "" || fixedReplyPhrase(*config, "request_resume") != "" {
		t.Fatal("删除后的话术被恢复")
	}
	config.Phrases = []cloud.AutoReplyPhrase{{ID: "receive_phone", Content: "联系方式收到了。"}, {ID: "request_phone", Content: "方便留个电话吗？"}}
	msg := cloud.AutoReplyMessage{PlatformMessageID: "new", Direction: "candidate", MessageType: "text", TextContent: "我的电话是 138 0013 8000"}
	input.PendingMessages = []cloud.AutoReplyMessage{msg}
	input.Messages = input.PendingMessages
	if fixedReceiptID(input) != "receive_phone" || previouslyReceivedPhone(input) {
		t.Fatal("没有签收到首次发来的纯号码")
	}
	if err := checkPhoneReply(inspectPhoneContact(input), *config, "好的，您的号码13800138000。"); err == nil {
		t.Fatal("回显了号码")
	}
	old := msg
	old.PlatformMessageID = "old"
	input.Messages = append([]cloud.AutoReplyMessage{old}, input.Messages...)
	if !previouslyReceivedPhone(input) {
		t.Fatal("同号重复签收")
	}
	input.Messages = []cloud.AutoReplyMessage{{Direction: "self", TextContent: "方便留个电话吗？"}, {Direction: "candidate", TextContent: "先在这里聊吧"}}
	if err := checkPhoneReply(inspectPhoneContact(input), *config, "方便留个电话吗？"); err == nil {
		t.Fatal("拒绝后再次索号")
	}
	msg.TextContent = "我想问餐补怎么算？另外几点下班？"
	input.PendingMessages = []cloud.AutoReplyMessage{msg}
	input.KnownQuestions = []knownRecruitmentQuestion{{ID: "food", Status: "pending", Question: "餐补标准？"}}
	questions, err := verifiedHandoffQuestions(input, notifyHRToolArgs{ReasonKey: "missing_recruitment_info", Questions: []handoffQuestion{{Question: "餐补怎么算", ExistingQuestionID: "food"}, {Question: "几点下班"}}})
	if err != nil || len(questions) != 2 || questions[0].sourceKey != "new" {
		t.Fatal("同批多题丢失", err)
	}
	if _, err = verifiedHandoffQuestions(input, notifyHRToolArgs{ReasonKey: "missing_recruitment_info", Question: "福利好不好"}); err == nil {
		t.Fatal("接受了非原话的问题")
	}
	if len(handoffReplyMessages("回答一", "")) != 1 || len(handoffReplyMessages("", "")) != 0 {
		t.Fatal("禁用签收不能影响已证实问题的回复")
	}
}
