// Package liepin 本文件验证限制模式只相信当前聊天框实时cid，姓名相同或历史简历编号不能替代页面身份证据。
package liepin

import (
	"context"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// identityBrowserStub 仅模拟标准读取，复用既有浏览器桩记录是否发生点击。
type identityBrowserStub struct {
	liepinReplyBrowserStub
	name, candidateID string
}

// Read 返回当前聊天姓名与按钮cid，不提供历史消息后备。
func (b *identityBrowserStub) Read(_ context.Context, request contract.ElementReadRequest) (contract.ReadResult, error) {
	if request.Attribute == "data-tlg-scm" {
		return contract.ReadResult{Value: "cid=" + b.candidateID}, nil
	}
	return contract.ReadResult{Value: b.name}, nil
}

// TestVerifyAutoReplyConversationIdentityRequiresCurrentCID 验证错名、同名错cid和缺失cid均拒绝且校验只读。
func TestVerifyAutoReplyConversationIdentityRequiresCurrentCID(t *testing.T) {
	cfg := model.Config{Selectors: map[string]contract.SelectorSpec{
		"message.current_name":        testLiepinSelector("测试当前候选人"),
		"message.candidate_id_source": testLiepinSelector("测试候选人编号"),
	}}
	snapshot := model.AutoReplyConversationSnapshot{CandidateName: "目标候选人", PlatformCandidateID: "target-cid"}
	for _, item := range []struct {
		name, cid string
		allowed   bool
	}{
		{"目标候选人", "target-cid", true},
		{"目标候选人", "other-cid", false},
		{"目标候选人", "", false},
		{"其他候选人", "target-cid", false},
	} {
		browser := &identityBrowserStub{name: item.name, candidateID: item.cid}
		err := (&Runtime{}).VerifyAutoReplyConversationIdentity(context.Background(), browser, cfg, snapshot)
		if (err == nil) != item.allowed || len(browser.clicks) != 0 {
			t.Fatalf("实时身份核对不正确：name=%s cid=%s err=%v clicks=%v", item.name, item.cid, err, browser.clicks)
		}
	}
}

// targetContactBrowserStub 记录限制模式最终交给Worker的标准点击选择器。
type targetContactBrowserStub struct {
	liepinReplyBrowserStub
	selector   contract.SelectorSpec
	clickCount int
}

// Click 仅记录合成点击，验证其联系人父级不再依赖列表序号。
func (b *targetContactBrowserStub) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	b.selector = request.Selector
	b.clickCount++
	return contract.ClickResult{Clicked: true}, nil
}

// TestOpenTargetConversationUsesStableAttribute 验证限制模式按当前读取的稳定属性点击，列表序号变化不扩大目标。
func TestOpenTargetConversationUsesStableAttribute(t *testing.T) {
	metadata := `{"to_imid":"target-thread"}`
	cfg := model.Config{Selectors: map[string]contract.SelectorSpec{
		"message.contact_item":         testLiepinSelector("测试联系人"),
		"message.contact_click_target": testLiepinSelector("测试点击联系人"),
		"message.drawer_scroll":        testLiepinSelector("测试联系人滚动区域"),
	}, ConversationFields: map[string]contract.SelectorSpec{"thread_meta": {ReadAttribute: "data-tlg-ext"}}}
	browser := &targetContactBrowserStub{liepinReplyBrowserStub: liepinReplyBrowserStub{items: []contract.FindAllItem{
		{Index: 0, Fields: map[string]string{"name": "同名候选人", "thread_meta": `{"to_imid":"other-thread"}`}},
		{Index: 3, Fields: map[string]string{"name": "同名候选人", "thread_meta": metadata}},
	}}}
	expected := model.Conversation{Name: "同名候选人", PlatformThreadID: "target-thread", PlatformCandidateID: "target-cid"}
	if err := openLiepinConversation(context.Background(), browser, cfg, expected); err != nil {
		t.Fatal(err)
	}
	if browser.clickCount != 1 || len(browser.selector.Parents) != 1 || browser.selector.Parents[0].Index != nil || browser.selector.Parents[0].Attributes["data-tlg-ext"] != metadata {
		t.Fatalf("限制点击仍依赖列表序号：%+v", browser.selector)
	}
	if len(cfg.Selectors["message.contact_item"].Target.Attributes) != 0 {
		t.Fatal("运行限制改写了共享平台配置")
	}
	browser.items = append(browser.items, browser.items[1])
	if err := openLiepinConversation(context.Background(), browser, cfg, expected); err == nil || browser.clickCount != 1 {
		t.Fatal("重复身份仍触发点击", err)
	}
}
