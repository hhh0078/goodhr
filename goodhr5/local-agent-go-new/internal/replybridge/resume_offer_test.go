// Package replybridge 本文件验证原生简历邀请沿现有白名单传输，旧协议设备无法把新同意语义当成普通索要执行。
package replybridge

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestResumeOfferBridgeRejectsWrongKindAndOldWire 验证新设备拒绝混类动作，旧类型严格解码在任何资料操作前拒绝新增来源字段。
func TestResumeOfferBridgeRejectsWrongKindAndOldWire(t *testing.T) {
	runtime := &nativeInfoBridgeRuntime{}
	executor := &Executor{PlatformID: "boss", Runtime: runtime, Config: model.Config{Behavior: model.Behavior{AutoReplyNativeInfoRequests: true}}}
	request := Request{Snapshot: model.AutoReplyConversationSnapshot{ResumeOfferMessageID: "offer", ExpectedSendMessageKey: "latest"}, InfoRequest: model.CandidateInfoRequest{RequestPhone: true}}
	payload, _ := json.Marshal(request)
	command := Command{ID: "offer", RunID: "run", PlatformID: "boss", Action: "request_info", Payload: payload, Deadline: time.Now().Add(time.Second)}
	if result := executor.Execute(context.Background(), command); result.Error == "" || runtime.calls != 0 {
		t.Fatal("非简历同意进入平台")
	}
	request.InfoRequest = model.CandidateInfoRequest{RequestResume: true}
	command.ID = "accept-offer"
	command.Payload, _ = json.Marshal(request)
	if result := executor.Execute(context.Background(), command); result.Error != "" || runtime.calls != 1 {
		t.Fatal("现有资料接口无法承载明确同意", result.Error)
	}
	// 旧设备采用同一严格Decoder，但它的snapshot没有新增字段；递归校验必须在平台方法调用前报错。
	var old struct {
		Snapshot struct {
			Expected              string                      `json:"expected_send_message_key,omitempty"`
			Conversation          model.Conversation          `json:"conversation"`
			CandidateName         string                      `json:"candidate_name"`
			AvatarURL             string                      `json:"avatar_url"`
			Gender                string                      `json:"gender"`
			Phone                 string                      `json:"phone"`
			PlatformThreadID      string                      `json:"platform_thread_id"`
			PlatformCandidateID   string                      `json:"platform_candidate_id"`
			PlatformAccountID     string                      `json:"platform_account_id"`
			CommunicationPosition string                      `json:"communication_position"`
			Messages              []model.ConversationMessage `json:"messages"`
			HistoryComplete       bool                        `json:"history_complete"`
			ResumeCardAvailable   bool                        `json:"resume_card_available"`
			ResumeSourceMessageID string                      `json:"resume_source_message_id"`
		} `json:"snapshot"`
		Conversation model.Conversation         `json:"conversation"`
		InfoRequest  model.CandidateInfoRequest `json:"info_request"`
	}
	decoder := json.NewDecoder(bytes.NewReader(command.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&old); err == nil || !strings.Contains(err.Error(), "resume_offer_message_id") {
		t.Fatal("旧设备可能把同意误执行成索要", err)
	}
}
