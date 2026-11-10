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

// weeklyPanelBrowser 模拟周报遮层，确保 Esc 无效时只点周报自己的关闭按钮。
type weeklyPanelBrowser struct {
	*replyPanelBrowser
	weeklyOpen   bool
	escapeWorks  bool
	weeklyCloses int
}

// FindAll 只补充周报的可见性，其余会话读取复用已有测试浏览器。
func (b *weeklyPanelBrowser) FindAll(ctx context.Context, req contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if req.Selector.Description == "测试周报" {
		if b.weeklyOpen {
			return []contract.FindAllItem{{Index: 0}}, nil
		}
		return nil, nil
	}
	return b.replyPanelBrowser.FindAll(ctx, req)
}

// PressKey 记录周报 Esc，并根据平台行为决定是否确实关闭。
func (b *weeklyPanelBrowser) PressKey(ctx context.Context, req contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	if req.Target != nil && req.Target.Description == "测试周报" {
		if req.Key != "Escape" || req.ExpectedPageURL != "https://example.com/recruit" {
			return contract.KeyboardPressResult{}, fmt.Errorf("未核实原页")
		}
		b.pressed = append(b.pressed, "测试周报")
		if b.escapeWorks {
			b.weeklyOpen = false
		}
		return contract.KeyboardPressResult{Pressed: true}, nil
	}
	return b.replyPanelBrowser.PressKey(ctx, req)
}

// Click 只允许周报专属备用按钮清除周报，不触碰已打开的候选人。
func (b *weeklyPanelBrowser) Click(ctx context.Context, req contract.ElementClickRequest) (contract.ClickResult, error) {
	if req.Selector.Description == "测试周报关闭" {
		b.weeklyCloses++
		b.weeklyOpen = false
		return contract.ClickResult{}, nil
	}
	return b.replyPanelBrowser.Click(ctx, req)
}

// TestLiepinInitializeClosesOnlyWeeklyReport 验证无周报不按键，有周报优先 Esc，未关闭才点击备用按钮。
func TestLiepinInitializeClosesOnlyWeeklyReport(t *testing.T) {
	for _, mode := range []string{"absent", "escape", "fallback"} {
		t.Run(mode, func(t *testing.T) {
			r, base, cfg := allContactFixture()
			cfg.MessagesURL = "https://example.com/recruit"
			cfg.Selectors["message.weekly_report"] = testLiepinSelector("测试周报")
			cfg.Selectors["message.weekly_report_close"] = testLiepinSelector("测试周报关闭")
			cfg.Selectors["message.attachment_preview"] = testLiepinSelector("测试附件预览")
			base.drawerOpen, base.conversationOpen = true, true
			b := &weeklyPanelBrowser{replyPanelBrowser: &replyPanelBrowser{liepinReplyBrowserStub: base}, weeklyOpen: mode != "absent", escapeWorks: mode == "escape"}
			if err := r.InitializeAutoReplyPage(context.Background(), b, cfg, nil); err != nil {
				t.Fatal(err)
			}
			wantKeys, wantCloses := 0, 0
			if mode != "absent" {
				wantKeys = 1
			}
			if mode == "fallback" {
				wantCloses = 1
			}
			if b.weeklyOpen || len(b.pressed) != wantKeys || b.weeklyCloses != wantCloses || !b.drawerOpen || !b.conversationOpen || len(b.clicks) != 0 {
				t.Fatalf("关闭范围错误：mode=%s keys=%v clicks=%d weekly=%t", mode, b.pressed, b.weeklyCloses, b.weeklyOpen)
			}
		})
	}
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
