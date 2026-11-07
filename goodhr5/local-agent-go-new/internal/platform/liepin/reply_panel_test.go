// Package liepin 本文件验证自动回复启动与退出在猎聘真实页面入口上只操作当前任务允许的弹层。
package liepin

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// replyPanelBrowser 模拟可同时打开的预览、聊天和抽屉，并记录 Esc 准确落在哪一层。
type replyPanelBrowser struct {
	*liepinReplyBrowserStub
	pressed []string
}

// PressKey 对已经打开的对应目标执行 Esc；没有指定原页或正确目标的动作一律失败。
func (b *replyPanelBrowser) PressKey(_ context.Context, req contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	if req.Key != "Escape" || req.Target == nil || req.ExpectedPageURL != "https://example.com/recruit" {
		return contract.KeyboardPressResult{}, fmt.Errorf("按键没有限定当前页和目标")
	}
	key := req.Target.Description
	b.pressed = append(b.pressed, key)
	switch key {
	case "测试附件预览":
		b.attachmentOpen = false
	case "测试当前候选人":
		b.conversationOpen = false
	case "测试联系人抽屉":
		b.drawerOpen = false
	default:
		return contract.KeyboardPressResult{}, fmt.Errorf("未识别的动作")
	}
	return contract.KeyboardPressResult{Pressed: true}, nil
}

// TestLiepinInitializeRetainsDrawerAndChat 验证正常启动复用原抽屉，只有确实阻挡视线且属于目标的预览才被关闭。
func TestLiepinInitializeRetainsDrawerAndChat(t *testing.T) {
	r, base, cfg := allContactFixture()
	cfg.MessagesURL = "https://example.com/recruit"
	for key, name := range map[string]string{"message.attachment_preview": "测试附件预览", "message.attachment_preview_close": "测试附件关闭", "message.current_name": "测试当前候选人", "message.candidate_id_source": "测试候选人编号", "candidate.chat_modal": "测试当前候选人", "candidate.chat_close": "测试聊天关闭", "candidate.contact_drawer": "测试联系人抽屉", "candidate.contact_drawer_close": "测试抽屉关闭"} {
		cfg.Selectors[key] = testLiepinSelector(name)
	}
	target := &model.AutoReplyConversationSnapshot{CandidateName: "邓云川", PlatformCandidateID: "target-cid"}
	b := &replyPanelBrowser{liepinReplyBrowserStub: base}
	b.drawerOpen, b.allSelected, b.conversationOpen = true, true, true
	if err := r.InitializeAutoReplyPage(context.Background(), b, cfg, target); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ScanUnreadConversations(context.Background(), b, cfg); err != nil {
		t.Fatal(err)
	}
	if len(b.clicks)+len(b.pressed) != 0 || !b.drawerOpen || !b.conversationOpen {
		t.Fatalf("启动不应关闭再重开联系人或聊天：%v %v", b.clicks, b.pressed)
	}
	b.attachmentOpen = true
	wrongTarget := *target
	wrongTarget.PlatformCandidateID = "other-cid"
	if err := r.InitializeAutoReplyPage(context.Background(), b, cfg, &wrongTarget); err == nil || len(b.clicks)+len(b.pressed) != 0 || !b.attachmentOpen {
		t.Fatalf("不应操作其他候选人的预览：%v %v %v", err, b.clicks, b.pressed)
	}
	if err := r.InitializeAutoReplyPage(context.Background(), b, cfg, target); err != nil || !slices.Equal(b.pressed, []string{"测试附件预览"}) || !b.drawerOpen || !b.conversationOpen {
		t.Fatalf("启动应只关闭目标预览：%v %v", err, b.pressed)
	}
	b.pressed = nil
	b.attachmentOpen = true
	if err := r.CloseAutoReplyConversation(context.Background(), b, cfg, *target); err != nil || !slices.Equal(b.pressed, []string{"测试附件预览", "测试当前候选人", "测试联系人抽屉"}) || len(b.clicks) != 0 {
		t.Fatalf("显式收尾没有按层优先用 Esc 关闭：%v %v %v", err, b.pressed, b.clicks)
	}
}

// TestLiepinDeliveryTailStaysOnOriginalChat 验证抢先来信的送达复核只读原人原页最后数行，既不翻页，也不重新打开聊天。
func TestLiepinDeliveryTailStaysOnOriginalChat(t *testing.T) {
	cfg := historyAuditConfig()
	cfg.Selectors["message.current_name"] = testLiepinSelector("测试当前候选人")
	cfg.Selectors["message.current_position"] = testLiepinSelector("测试当前岗位")
	cfg.Selectors["message.candidate_id_source"] = testLiepinSelector("测试候选人编号")
	b := &liepinReplyBrowserStub{conversationOpen: true, historyBatches: [][]contract.FindAllItem{{
		{Fields: map[string]string{"message_meta": `{"message_id":"in"}`, "message_text": "第一条", "body_class": "message-item-receive"}},
		{Fields: map[string]string{"message_meta": `{"message_id":"out"}`, "message_text": "收到", "body_class": "message-item-send"}},
		{Fields: map[string]string{"message_meta": `{"message_id":"next"}`, "message_text": "还有一个问题", "body_class": "message-item-receive"}},
	}}}
	s := model.AutoReplyConversationSnapshot{DeliveryPageURL: "https://example.com/recruit", CandidateName: "邓云川", PlatformCandidateID: "target-cid", CommunicationPosition: "测试岗位", ExpectedSendMessageKey: "in"}
	r := NewRuntime()
	rows, _, err := r.ReadAutoReplyMessages(context.Background(), b, cfg, s, nil, 8)
	if err != nil || len(rows) != 3 || rows[1].PlatformMessageID != "out" || len(b.historyScrolls) != 0 || len(b.clicks) != 0 {
		t.Fatalf("送达窗口复核异常：消息=%+v 轮滚=%d 点击=%v 错误=%v", rows, len(b.historyScrolls), b.clicks, err)
	}
	for _, wrong := range []string{"page", "cid"} {
		bad := s
		if wrong == "page" {
			bad.DeliveryPageURL = "https://example.com/other"
		} else {
			bad.PlatformCandidateID = "other-cid"
		}
		rows, _, err = r.ReadAutoReplyMessages(context.Background(), b, cfg, bad, nil, 8)
		if err == nil || len(rows) != 0 || len(b.clicks) != 0 {
			t.Fatalf("错页或错人时不能认领气泡或切页：%v %v", wrong, err)
		}
	}
}
