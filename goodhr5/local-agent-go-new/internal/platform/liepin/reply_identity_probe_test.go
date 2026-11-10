// Package liepin 本文件验证聊天姓名缺失时只用短探测，原页、读取故障和真实候选人编号仍须严格核实，不访问真实平台。
package liepin

import (
	"context"
	"errors"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// identityProbeBrowserStub 记录当前页、短探测和字段读取预算，防止缺失姓名重新落入三秒读取。
type identityProbeBrowserStub struct {
	identityBrowserStub
	page                  string
	visible               bool
	probeError, nameError error
	probes                []contract.ElementFindAllRequest
	nameReads, cidReads   int
	nameReadTimeout       int
}

// ListPages 返回当前真实测试页，换页不能当成原聊天已关闭。
func (b *identityProbeBrowserStub) ListPages(context.Context) (contract.PageListResult, error) {
	return contract.PageListResult{Pages: []contract.PageInfo{{URL: b.page, Current: true}}}, nil
}

// FindAll 仅模拟当前姓名的可见性，保留Worker缺失与故障的区别。
func (b *identityProbeBrowserStub) FindAll(ctx context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if request.Selector.Description != "测试当前候选人" {
		return b.identityBrowserStub.FindAll(ctx, request)
	}
	b.probes = append(b.probes, request)
	if b.probeError != nil {
		return nil, b.probeError
	}
	if b.visible {
		return []contract.FindAllItem{{Index: 0, TotalMatches: 1}}, nil
	}
	return nil, nil
}

// Read 记录姓名和cid的实际读取，存在性探测成功后节点消失也必须保持短预算。
func (b *identityProbeBrowserStub) Read(ctx context.Context, request contract.ElementReadRequest) (contract.ReadResult, error) {
	if request.Attribute == "data-tlg-scm" {
		b.cidReads++
	} else {
		b.nameReads++
		b.nameReadTimeout = request.Selector.TimeoutMS
		if b.nameError != nil {
			return contract.ReadResult{}, b.nameError
		}
	}
	return b.identityBrowserStub.Read(ctx, request)
}

// TestLiepinIdentityMissingChatUsesShortProbe 验证缺失姓名零读取，存在姓名才读短预算字段并严格核实时cid，配置缺失与浏览器故障不能当成关闭。
func TestLiepinIdentityMissingChatUsesShortProbe(t *testing.T) {
	for _, scenario := range []string{"missing", "worker-missing", "wrong-page", "missing-name-config", "missing-id-config", "probe-fault", "present", "empty-name", "wrong-name", "empty-id", "wrong-id", "vanished", "read-fault"} {
		t.Run(scenario, func(t *testing.T) {
			nameSelector := testLiepinSelector("测试当前候选人")
			nameSelector.TimeoutMS = 3000
			cfg := model.Config{MessagesURL: "https://example.com/recruit", Selectors: map[string]contract.SelectorSpec{
				"message.current_name": nameSelector, "message.candidate_id_source": testLiepinSelector("测试候选人编号"),
			}}
			b := &identityProbeBrowserStub{identityBrowserStub: identityBrowserStub{name: "目标候选人", candidateID: "target-cid"}, page: cfg.MessagesURL, visible: true}
			missing := &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
			fault := &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "WORKER_UNRESPONSIVE"}}
			wantNameReads, wantIDReads := 0, 0
			wantMissing := false
			switch scenario {
			case "missing":
				b.visible, wantMissing = false, true
			case "worker-missing":
				b.probeError, wantMissing = missing, true
			case "wrong-page":
				b.page = "https://other.example.com/recruit"
			case "missing-name-config":
				delete(cfg.Selectors, "message.current_name")
			case "missing-id-config":
				delete(cfg.Selectors, "message.candidate_id_source")
			case "probe-fault":
				b.probeError = fault
			case "present":
				wantNameReads, wantIDReads = 1, 1
			case "empty-name":
				b.name, wantNameReads = "", 1
			case "wrong-name":
				b.name, wantNameReads = "别的候选人", 1
			case "empty-id":
				b.candidateID, wantNameReads, wantIDReads = "", 1, 1
			case "wrong-id":
				b.candidateID, wantNameReads, wantIDReads = "other-cid", 1, 1
			case "vanished":
				b.nameError, wantNameReads, wantMissing = missing, 1, true
			case "read-fault":
				b.nameError, wantNameReads = fault, 1
			}
			err := (&Runtime{}).VerifyAutoReplyConversationIdentity(context.Background(), b, cfg, model.AutoReplyConversationSnapshot{CandidateName: "目标候选人", PlatformCandidateID: "target-cid"})
			if (err == nil) != (scenario == "present") || errors.Is(err, model.ErrAutoReplyConversationNotOpen) != wantMissing {
				t.Fatalf("缺失与故障或身份错误被混淆：%v", err)
			}
			if b.nameReads != wantNameReads || b.cidReads != wantIDReads || len(b.clicks) != 0 {
				t.Fatalf("缺失时仍慢读或略过cid：name=%d cid=%d clicks=%v", b.nameReads, b.cidReads, b.clicks)
			}
			for _, probe := range b.probes {
				if probe.Selector.TimeoutMS != 600 || !probe.ProbeOnly || !probe.ExpectedMissing || probe.MaxItems != 1 {
					t.Fatalf("存在性判断仍使用长等待或全量读取：%+v", probe)
				}
			}
			if b.nameReads > 0 && b.nameReadTimeout != 600 {
				t.Fatalf("探测后消失竞态仍可能等待三秒：%d", b.nameReadTimeout)
			}
			if selector, ok := cfg.Selectors["message.current_name"]; ok && selector.TimeoutMS != 3000 {
				t.Fatal("短预算污染共享配置")
			}
		})
	}
}

// TestLiepinConversationRestoreMatchUsesShortProbe 验证恢复路径也先短探测姓名，聊天不存在时不读姓名或岗位。
func TestLiepinConversationRestoreMatchUsesShortProbe(t *testing.T) {
	cfg := model.Config{MessagesURL: "https://example.com/recruit", Selectors: map[string]contract.SelectorSpec{
		"message.current_name": testLiepinSelector("测试当前候选人"), "message.current_position": testLiepinSelector("测试当前岗位"),
	}}
	b := &identityProbeBrowserStub{page: cfg.MessagesURL}
	matched, err := liepinAutoReplyConversationMatches(context.Background(), b, cfg, model.AutoReplyConversationSnapshot{CandidateName: "目标候选人", CommunicationPosition: "测试岗位"})
	if err != nil || matched || len(b.probes) != 1 || b.nameReads != 0 || b.cidReads != 0 || len(b.clicks) != 0 {
		t.Fatalf("恢复仍为缺失聊天执行长读取：matched=%v err=%v probes=%d reads=%d", matched, err, len(b.probes), b.nameReads)
	}
}
