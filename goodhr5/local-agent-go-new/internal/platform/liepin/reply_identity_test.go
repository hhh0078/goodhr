// Package liepin 本文件验证限制模式只相信当前聊天框实时cid，姓名相同或历史简历编号不能替代页面身份证据。
package liepin

import (
	"context"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// identityBrowserStub 仅模拟标准读取，复用既有浏览器桩记录是否发生点击。
type identityBrowserStub struct {
	liepinReplyBrowserStub
	name, candidateID string
}

// FindAll 让身份桩的聊天可见性与实际姓名保持一致，避免只模拟读取却略过真实存在性探测。
func (b *identityBrowserStub) FindAll(ctx context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if request.Selector.Description == "测试当前候选人" {
		if b.name != "" {
			return []contract.FindAllItem{{Index: 0, TotalMatches: 1}}, nil
		}
		return nil, nil
	}
	return b.liepinReplyBrowserStub.FindAll(ctx, request)
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
	lookups    []contract.ElementFindAllRequest
}

// FindAll 模拟 Worker 的联系人整行文字预筛，返回筛选后的相对下标和请求中选定的字段。
func (b *targetContactBrowserStub) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	b.lookups = append(b.lookups, request)
	var items []contract.FindAllItem
	for _, item := range b.items {
		if texts := request.Selector.Target.Texts; len(texts) > 0 && !strings.Contains(item.Fields["name"]+" "+item.Text, texts[0]) {
			continue
		}
		fields := make(map[string]string)
		for key := range request.Fields {
			fields[key] = item.Fields[key]
		}
		items = append(items, contract.FindAllItem{Index: len(items), Fields: fields})
	}
	return items, nil
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
	}, ConversationFields: map[string]contract.SelectorSpec{"thread_meta": {ReadAttribute: "data-tlg-ext"}, "name": testLiepinSelector("联系人姓名")}}
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

// TestOpenLiepinContactScopesNameAndRestoresByID 验证普通与限定任务只读命中项，旧姓名和摘要同名不能误导打开动作。
func TestOpenLiepinContactScopesNameAndRestoresByID(t *testing.T) {
	const targetMeta = `{"to_imid":"target-thread"}`
	for _, tc := range []struct {
		label, name, cid string
		scans            int
		allow            bool
	}{
		{"普通会话", "目标", "", 1, true},
		{"限定会话", "目标", "target-cid", 1, true},
		{"普通旧姓名按会话属性定位", "旧姓名", "", 2, true},
		{"限定旧姓名不能点", "旧姓名", "target-cid", 2, false},
		{"无姓名按会话属性定位", "", "", 1, true},
	} {
		t.Run(tc.label, func(t *testing.T) {
			cfg := model.Config{Selectors: map[string]contract.SelectorSpec{
				"message.contact_item": testLiepinSelector("联系人行"), "message.contact_click_target": testLiepinSelector("点击姓名"), "message.drawer_scroll": testLiepinSelector("滚动区"),
			}, ConversationFields: map[string]contract.SelectorSpec{
				"thread_meta": {ReadAttribute: "data-tlg-ext"}, "name": testLiepinSelector("姓名"), "avatar_url": testLiepinSelector("头像"),
			}}
			b := &targetContactBrowserStub{liepinReplyBrowserStub: liepinReplyBrowserStub{items: []contract.FindAllItem{
				{Fields: map[string]string{"name": "无关", "thread_meta": `{"to_imid":"other"}`}},
				{Text: "请联系目标和旧姓名", Fields: map[string]string{"name": "旁人", "thread_meta": `{"to_imid":"extra"}`}},
				{Fields: map[string]string{"name": "目标", "thread_meta": `{"to_imid":"same-name"}`}},
				{Fields: map[string]string{"name": "目标", "thread_meta": targetMeta}},
			}}}
			err := openLiepinConversation(context.Background(), b, cfg, model.Conversation{Name: tc.name, PlatformThreadID: "target-thread", PlatformCandidateID: tc.cid})
			if (err == nil) != tc.allow || len(b.lookups) != tc.scans || (b.clickCount == 1) != tc.allow {
				t.Fatalf("打开结果错误：err=%v 查询=%d 点击=%d", err, len(b.lookups), b.clickCount)
			}
			if tc.allow && (b.selector.Parents[0].Index != nil || b.selector.Parents[0].Attributes["data-tlg-ext"] != targetMeta) {
				t.Fatalf("点击使用了筛选列表相对下标或错误的会话属性：%+v", b.selector)
			}
			if tc.name != "" && (len(b.lookups[0].Selector.Target.Texts) != 1 || b.lookups[0].Selector.Target.Texts[0] != tc.name || len(b.lookups[0].Fields) != 2) {
				t.Fatalf("未按姓名限制读取范围：%+v", b.lookups[0])
			}
			last := b.lookups[len(b.lookups)-1]
			if tc.cid == "" && (tc.name == "" || tc.scans == 2) && (len(last.Selector.Target.Texts) != 0 || len(last.Fields) != 1 || last.Fields["thread_meta"].ReadAttribute != "data-tlg-ext") {
				t.Fatal("普通回退应该仅查全表会话属性", last)
			}
		})
	}
}
