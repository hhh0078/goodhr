// Package lifecycle 本文件验证正式平台注册、日志包装和限制任务入口不会丢失实时身份核验，全程只使用隔离替身。
package lifecycle

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/auto_reply"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// identityAssemblyBrowser 只返回合成页面身份，在首个浏览器启动步骤主动终止，不接触真实平台。
type identityAssemblyBrowser struct {
	model.Browser
	candidateID string
	pageURL     string
	startCalls  int
	startError  error
}

// ListPages 为正式核身提供唯一当前招聘页，避免嵌入的空Browser接口被误调用。
func (b *identityAssemblyBrowser) ListPages(context.Context) (contract.PageListResult, error) {
	return contract.PageListResult{Count: 1, Pages: []contract.PageInfo{{PageID: "identity-original", URL: b.pageURL, Current: true}}}, nil
}

// FindAll 模拟聊天姓名的标准短探测，本夹具不负责真实DOM定位或页面等待。
func (*identityAssemblyBrowser) FindAll(context.Context, contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	return []contract.FindAllItem{{Index: 0, TotalMatches: 1}}, nil
}

// Read 提供正式猎聘核验方法所需的姓名和当前 cid，其他浏览器动作均不可用。
func (b *identityAssemblyBrowser) Read(_ context.Context, request contract.ElementReadRequest) (contract.ReadResult, error) {
	if request.Attribute == "data-tlg-scm" {
		return contract.ReadResult{Value: "cid=" + b.candidateID}, nil
	}
	return contract.ReadResult{Value: "合成目标"}, nil
}

// StartBrowser 记录流程已通过守卫组装并立即返回隔离终止信号。
func (b *identityAssemblyBrowser) StartBrowser(context.Context, contract.BrowserStartRequest) (contract.BrowserStatus, error) {
	b.startCalls++
	return contract.BrowserStatus{}, b.startError
}

// identityAssemblyResponder 保证组装回归不会调用任何模型。
type identityAssemblyResponder struct{}

// Reply 拒绝意外进入模型步骤，测试只覆盖启动和只读身份核验。
func (identityAssemblyResponder) Reply(context.Context, auto_reply.ReplyContext) (auto_reply.ReplyDecision, error) {
	return auto_reply.ReplyDecision{}, errors.New("组装测试禁止调用模型")
}

// TestLoggedLiepinTargetAssembly 验证 Runner 使用的真实注册和日志包装链路可进入限制流程，并原样保留正确及错误 cid 的校验。
func TestLoggedLiepinTargetAssembly(t *testing.T) {
	t.Setenv(shared.AutoReplyTargetEnvironment, "")
	runtime, err := platform.RuntimeFor("liepin")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := platform.LoadConfig("liepin")
	if err != nil {
		t.Fatal(err)
	}
	wrapped := newLoggedRuntime(runtime, "synthetic-identity-task", nil)
	if _, ok := wrapped.(model.AutoReplyContactInspector); !ok {
		t.Fatal("正式猎聘日志包装丢失删除后只核实新联系的能力")
	}
	identity, ok := wrapped.(model.AutoReplyIdentityVerifier)
	if !ok {
		t.Fatal("正式猎聘日志包装丢失实时身份核验能力")
	}
	browser := &identityAssemblyBrowser{candidateID: "synthetic-cid", pageURL: cfg.MessagesURL, startError: errors.New("已进入隔离启动步骤")}
	snapshot := model.AutoReplyConversationSnapshot{CandidateName: "合成目标", PlatformCandidateID: "synthetic-cid"}
	if err = identity.VerifyAutoReplyConversationIdentity(context.Background(), browser, cfg, snapshot); err != nil {
		t.Fatal(err)
	}
	browser.candidateID = "other-cid"
	if err = identity.VerifyAutoReplyConversationIdentity(context.Background(), browser, cfg, snapshot); err == nil {
		t.Fatal("包装后同名不同 cid 被放行")
	}
	root := t.TempDir()
	store, err := storage.Open(filepath.Join(root, "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cloudClient := cloud.New("http://example.invalid")
	cloudClient.EnableLocalAutoReply(store, filepath.Join(root, "auto-reply"))
	flow := &auto_reply.Flow{Browser: browser, Store: store, Cloud: cloudClient, Responder: identityAssemblyResponder{}}
	prepared := shared.PreparedTask{Platform: cfg, ProfilePath: filepath.Join(root, "profile"), Request: shared.StartRequest{
		TaskID: "synthetic-identity-task", TaskType: "auto_reply", PlatformAccountID: "synthetic-account",
		AutoReplyTarget: &shared.AutoReplyTarget{CandidateName: "合成目标", PlatformThreadID: "synthetic-thread", PlatformCandidateID: "synthetic-cid"},
	}}
	_, err = flow.Run(context.Background(), prepared, wrapped)
	if !errors.Is(err, browser.startError) || browser.startCalls != 1 {
		t.Fatalf("正式包装链路未通过限制任务组装：calls=%d err=%v", browser.startCalls, err)
	}
}
