// Package lifecycle 本文件验证日志包装保留原生资料索要与既有身份、详情、联系人能力，不将旧平台伪装成支持原生索要。
package lifecycle

import (
	"context"
	"testing"

	"goodhr5/local-agent-go-new/internal/platform/model"
)

type nativeInfoLifecycleRuntime struct {
	model.Runtime
	model.AutoReplyRuntime
	model.DetailBrowser
	model.AutoReplyIdentityVerifier
	model.AutoReplyContactInspector
	calls int
}

// RequestAutoReplyCandidateInfo 模拟单类原生请求并返回明确暂缓，不连接招聘平台。
func (r *nativeInfoLifecycleRuntime) RequestAutoReplyCandidateInfo(context.Context, model.Browser, model.Config, model.AutoReplyConversationSnapshot, model.CandidateInfoRequest) (model.AutoReplyInfoRequestResult, error) {
	r.calls++
	return model.AutoReplyInfoRequestResult{Status: model.AutoReplyInfoDeferred}, nil
}

// TestLoggedNativeInfoPreservesCapabilities 验证新能力和全部已有可选能力均经过真实任务日志包装保留。
func TestLoggedNativeInfoPreservesCapabilities(t *testing.T) {
	runtime := &nativeInfoLifecycleRuntime{}
	wrapped := newLoggedRuntime(runtime, "test", nil)
	requester, ok := wrapped.(model.AutoReplyInfoRequester)
	if !ok {
		t.Fatal("原生资料索要在日志包装后丢失")
	}
	if _, ok = wrapped.(model.DetailBrowser); !ok {
		t.Fatal("详情能力丢失")
	}
	if _, ok = wrapped.(model.AutoReplyIdentityVerifier); !ok {
		t.Fatal("身份能力丢失")
	}
	if _, ok = wrapped.(model.AutoReplyContactInspector); !ok {
		t.Fatal("联系人能力丢失")
	}
	result, err := requester.RequestAutoReplyCandidateInfo(context.Background(), nil, model.Config{}, model.AutoReplyConversationSnapshot{}, model.CandidateInfoRequest{RequestPhone: true})
	if err != nil || result.Status != model.AutoReplyInfoDeferred || runtime.calls != 1 {
		t.Fatal("原生请求回执未保留", err)
	}
	legacy := newLoggedRuntime(struct {
		model.Runtime
		model.AutoReplyRuntime
	}{}, "legacy", nil)
	if _, ok = legacy.(model.AutoReplyInfoRequester); ok {
		t.Fatal("旧平台被误判支持原生资料索要")
	}
}
