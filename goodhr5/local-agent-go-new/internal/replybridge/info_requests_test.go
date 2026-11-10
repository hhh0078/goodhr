// Package replybridge 本文件验证原生三类请求通过同一白名单传输，明确暂缓、单种限制与参数锚点不丢失。
package replybridge

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/platform/model"
)

type nativeInfoBridgeRuntime struct {
	model.Runtime
	model.AutoReplyRuntime
	calls   int
	request model.CandidateInfoRequest
	anchor  string
}

// RequestAutoReplyCandidateInfo 记录已校验的单种请求并明确返回未执行，不操作任何真实平台。
func (r *nativeInfoBridgeRuntime) RequestAutoReplyCandidateInfo(_ context.Context, _ model.Browser, _ model.Config, snapshot model.AutoReplyConversationSnapshot, request model.CandidateInfoRequest) (model.AutoReplyInfoRequestResult, error) {
	r.calls++
	r.request = request
	r.anchor = snapshot.ExpectedSendMessageKey
	return model.AutoReplyInfoRequestResult{Status: model.AutoReplyInfoDeferred}, nil
}

// TestNativeInfoBridgePreservesDeferredAndRejectsMultiple 验证暂缓不会变成错误或文字备用，混合请求在执行前拒绝。
func TestNativeInfoBridgePreservesDeferredAndRejectsMultiple(t *testing.T) {
	runtime := &nativeInfoBridgeRuntime{}
	executor := &Executor{PlatformID: "boss", Runtime: runtime, Config: model.Config{Behavior: model.Behavior{AutoReplyNativeInfoRequests: true}}}
	request := Request{Snapshot: model.AutoReplyConversationSnapshot{ExpectedSendMessageKey: "actual-answer"}, InfoRequest: model.CandidateInfoRequest{RequestPhone: true}}
	payload, _ := json.Marshal(request)
	command := Command{ID: "one", RunID: "run", PlatformID: "boss", Action: "request_info", Payload: payload, Deadline: time.Now().Add(time.Second)}
	response := executor.Execute(context.Background(), command)
	if response.Error != "" || runtime.calls != 1 || runtime.anchor != "actual-answer" {
		t.Fatal("原生请求或锚点未传输", response.Error)
	}
	var decoded Response
	if json.Unmarshal(response.Value, &decoded) != nil || decoded.InfoResult.Status != model.AutoReplyInfoDeferred {
		t.Fatal("明确暂缓状态丢失")
	}
	request.InfoRequest.RequestWechat = true
	command.ID = "two"
	command.Payload, _ = json.Marshal(request)
	if result := executor.Execute(context.Background(), command); result.Error == "" || runtime.calls != 1 {
		t.Fatal("混合请求在平台层执行了")
	}
}
