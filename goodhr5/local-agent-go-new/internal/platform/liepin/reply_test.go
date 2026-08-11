// Package liepin 文件作用：验证猎聘自动回复的会话防串、消息解析和简历联系方式清洗规则。
package liepin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

type liepinReplyBrowserStub struct {
	items            []contract.FindAllItem
	drawerOpen       bool
	detailOpen       bool
	attachmentOpen   bool
	attachmentVerify bool
	attachmentAnchor string
	attachmentFails  int
	conversationOpen bool
	downloadPath     string
	unreadCount      int
	unreadSelected   bool
	allSelected      bool
	staleUnreadItems bool
	refreshStarted   bool
	clicks           []string
	historyBatches   [][]contract.FindAllItem
	historyBatch     int
}

// OpenPage 返回空页面，满足平台 Browser 测试契约。
func (b *liepinReplyBrowserStub) OpenPage(context.Context, contract.PageOpenRequest) (contract.PageInfo, error) {
	return contract.PageInfo{}, nil
}

// ListPages 返回空标签页列表，满足平台 Browser 测试契约。
func (b *liepinReplyBrowserStub) ListPages(context.Context) (contract.PageListResult, error) {
	return contract.PageListResult{}, nil
}

// UsePage 返回空页面，满足平台 Browser 测试契约。
func (b *liepinReplyBrowserStub) UsePage(context.Context, contract.PageUseRequest) (contract.PageInfo, error) {
	return contract.PageInfo{}, nil
}

// FindAll 返回测试准备的最新联系人顺序。
func (b *liepinReplyBrowserStub) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	switch request.Selector.Description {
	case "测试聊天消息":
		if len(b.historyBatches) > 0 {
			index := min(b.historyBatch, len(b.historyBatches)-1)
			return b.historyBatches[index], nil
		}
	case "测试联系人抽屉":
		if b.drawerOpen {
			return []contract.FindAllItem{{Index: 0}}, nil
		}
		return nil, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
	case "测试未读数字":
		if b.unreadCount > 0 {
			return []contract.FindAllItem{{Index: 0}}, nil
		}
		return nil, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
	case "测试未读标签已选中":
		if b.unreadSelected {
			return []contract.FindAllItem{{Index: 0}}, nil
		}
		return nil, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
	case "测试全部标签已选中":
		if b.allSelected {
			return []contract.FindAllItem{{Index: 0}}, nil
		}
		return nil, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
	case "测试未读联系人":
		if b.staleUnreadItems {
			return nil, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
		}
	case "测试在线简历浮层":
		if b.detailOpen {
			return []contract.FindAllItem{{Index: 0}}, nil
		}
		return nil, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
	case "测试附件预览":
		if b.attachmentOpen {
			return []contract.FindAllItem{{Index: 0}}, nil
		}
		return nil, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
	case "测试当前候选人":
		if b.conversationOpen {
			return []contract.FindAllItem{{Index: 0}}, nil
		}
		return nil, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
	}
	return b.items, nil
}

// Read 返回空读取结果，满足平台 Browser 测试契约。
func (b *liepinReplyBrowserStub) Read(_ context.Context, request contract.ElementReadRequest) (contract.ReadResult, error) {
	if request.Selector.Description == "测试未读数字" {
		return contract.ReadResult{Value: fmt.Sprintf("%d", b.unreadCount)}, nil
	}
	if request.Selector.Description == "测试在线简历浮层" && b.detailOpen {
		return contract.ReadResult{Value: "邓云川\n本科\n手机：17607080935"}, nil
	}
	if request.Selector.Description == "测试当前候选人" && b.conversationOpen {
		return contract.ReadResult{Value: "邓云川"}, nil
	}
	if request.Selector.Description == "测试当前岗位" && b.conversationOpen {
		return contract.ReadResult{Value: "测试岗位"}, nil
	}
	if request.Selector.Description == "测试附件正文" && b.attachmentOpen {
		return contract.ReadResult{Value: "邮箱：test@example.com"}, nil
	}
	return contract.ReadResult{}, nil
}

// Click 返回成功点击结果，满足平台 Browser 测试契约。
func (b *liepinReplyBrowserStub) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	b.clicks = append(b.clicks, request.Selector.Description)
	if request.Selector.Description == "测试联系人入口" {
		b.drawerOpen = true
	}
	if request.Selector.Description == "测试未读标签" {
		b.unreadSelected = true
		b.allSelected = false
		if b.refreshStarted {
			b.staleUnreadItems = false
		}
	}
	if request.Selector.Description == "测试全部标签" {
		b.allSelected = true
		b.unreadSelected = false
		b.refreshStarted = true
	}
	if request.Selector.Description == "message.contact_click_target" {
		b.conversationOpen = true
	}
	if request.Selector.Description == "测试在线简历入口" {
		b.detailOpen = true
	}
	if request.Selector.Description == "测试在线简历关闭" {
		b.detailOpen = false
		b.conversationOpen = false
		b.drawerOpen = false
	}
	if request.Selector.Description == "测试附件入口" {
		b.attachmentVerify = request.Verify != nil && request.Verify.TargetVisible != nil
		if request.WheelAnchor != nil {
			b.attachmentAnchor = request.WheelAnchor.Description
		}
		if !b.conversationOpen {
			return contract.ClickResult{}, fmt.Errorf("聊天框已经关闭，附件入口不存在")
		}
		if b.attachmentFails > 0 {
			b.attachmentFails--
			return contract.ClickResult{}, fmt.Errorf("附件预览第一次没有打开")
		}
		b.attachmentOpen = true
	}
	if request.Selector.Description == "测试附件关闭" {
		b.attachmentOpen = false
	}
	return contract.ClickResult{Clicked: true}, nil
}

// Input 返回成功输入结果，满足平台 Browser 测试契约。
func (b *liepinReplyBrowserStub) Input(context.Context, contract.ElementInputRequest) (contract.InputResult, error) {
	return contract.InputResult{Typed: true}, nil
}

// Scroll 返回成功滚动结果，满足平台 Browser 测试契约。
func (b *liepinReplyBrowserStub) Scroll(context.Context, contract.ScrollRequest) (contract.ScrollResult, error) {
	if len(b.historyBatches) > 0 {
		b.historyBatch++
	}
	return contract.ScrollResult{Scrolled: true}, nil
}

// PressKey 返回成功按键结果，满足平台 Browser 测试契约。
func (b *liepinReplyBrowserStub) PressKey(context.Context, contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	b.detailOpen = false
	return contract.KeyboardPressResult{Pressed: true}, nil
}

// ClosePage 模拟成功关闭当前标签页。
func (b *liepinReplyBrowserStub) ClosePage(context.Context) error {
	return nil
}

// Downloads 返回测试准备的已落盘附件记录。
func (b *liepinReplyBrowserStub) Downloads(context.Context) (contract.DownloadListResult, error) {
	return contract.DownloadListResult{Downloads: []contract.DownloadRecord{{Status: "saved", FilePath: b.downloadPath}}}, nil
}

// ClearDownloads 模拟清空 Worker 下载记录，不删除测试附件。
func (b *liepinReplyBrowserStub) ClearDownloads(context.Context) error {
	return nil
}

// TestEnsureLiepinUnreadConversationDrawerSelectsUnreadTab 验证联系人抽屉会切到未读标签后再扫描会话。
func TestEnsureLiepinUnreadConversationDrawerSelectsUnreadTab(t *testing.T) {
	browser := &liepinReplyBrowserStub{drawerOpen: true}
	cfg := model.Config{
		ID: "liepin", Name: "猎聘企业端",
		Selectors: map[string]contract.SelectorSpec{
			"message.drawer":              testLiepinSelector("测试联系人抽屉"),
			"message.unread_tab":          testLiepinSelector("测试未读标签"),
			"message.unread_tab_selected": testLiepinSelector("测试未读标签已选中"),
		},
	}
	ready, _, err := ensureLiepinUnreadConversationDrawer(context.Background(), browser, cfg)
	if err != nil || !ready {
		t.Fatalf("联系人未读列表没有准备好：ready=%t err=%v", ready, err)
	}
	if !browser.unreadSelected || len(browser.clicks) != 1 || browser.clicks[0] != "测试未读标签" {
		t.Fatalf("没有只点击一次未读标签：selected=%t clicks=%v", browser.unreadSelected, browser.clicks)
	}
}

// TestScanUnreadConversationsCapsPlatformHistoryByEntryCount 验证猎聘长期保留的旧未读不会超过悬浮入口本次新消息数。
func TestScanUnreadConversationsCapsPlatformHistoryByEntryCount(t *testing.T) {
	browser := &liepinReplyBrowserStub{
		unreadCount: 1,
		items: []contract.FindAllItem{
			{Index: 0, Fields: map[string]string{"name": "邓云川", "avatar_url": "https://image.example.com/avatar.png", "unread_count": "1", "thread_meta": url.QueryEscape(`{"unread":true,"to_imid":"thread-new"}`)}},
			{Index: 1, Fields: map[string]string{"name": "旧候选人", "unread_count": "1", "thread_meta": url.QueryEscape(`{"unread":true,"to_imid":"thread-old"}`)}},
		},
	}
	cfg := model.Config{
		ID: "liepin", Name: "猎聘企业端", MaxItems: 100,
		Selectors: map[string]contract.SelectorSpec{
			"message.drawer":              testLiepinSelector("测试联系人抽屉"),
			"message.entry":               testLiepinSelector("测试联系人入口"),
			"message.entry_unread_count":  testLiepinSelector("测试未读数字"),
			"message.unread_tab":          testLiepinSelector("测试未读标签"),
			"message.unread_tab_selected": testLiepinSelector("测试未读标签已选中"),
			"message.unread_item":         testLiepinSelector("测试未读联系人"),
		},
	}
	conversations, err := (&Runtime{}).ScanUnreadConversations(context.Background(), browser, cfg)
	if err != nil || len(conversations) != 1 || conversations[0].Name != "邓云川" || conversations[0].AvatarURL != "https://image.example.com/avatar.png" {
		t.Fatalf("没有按入口数字只保留最新会话：conversations=%+v err=%v", conversations, err)
	}
}

// TestScanUnreadConversationsIgnoresHistoricalUnreadWithoutBadge 验证抽屉已打开时只处理头像上仍有数字的联系人。
func TestScanUnreadConversationsIgnoresHistoricalUnreadWithoutBadge(t *testing.T) {
	browser := &liepinReplyBrowserStub{
		drawerOpen: true, unreadSelected: true,
		items: []contract.FindAllItem{
			{Index: 0, Fields: map[string]string{"name": "邓云川", "unread_count": "1", "thread_meta": url.QueryEscape(`{"unread":true,"to_imid":"thread-new"}`)}},
			{Index: 1, Fields: map[string]string{"name": "历史未读", "thread_meta": url.QueryEscape(`{"unread":true,"to_imid":"thread-old"}`)}},
		},
	}
	cfg := model.Config{
		ID: "liepin", Name: "猎聘企业端", MaxItems: 100,
		Selectors: map[string]contract.SelectorSpec{
			"message.drawer":              testLiepinSelector("测试联系人抽屉"),
			"message.unread_tab":          testLiepinSelector("测试未读标签"),
			"message.unread_tab_selected": testLiepinSelector("测试未读标签已选中"),
			"message.unread_item":         testLiepinSelector("测试未读联系人"),
		},
	}
	conversations, err := (&Runtime{}).ScanUnreadConversations(context.Background(), browser, cfg)
	if err != nil || len(conversations) != 1 || conversations[0].Name != "邓云川" {
		t.Fatalf("没有只保留头像带数字的联系人：conversations=%+v err=%v", conversations, err)
	}
}

// TestScanUnreadConversationsRefreshesStaleUnreadTab 验证入口有数字但未读标签为空时会刷新一次标签。
func TestScanUnreadConversationsRefreshesStaleUnreadTab(t *testing.T) {
	browser := &liepinReplyBrowserStub{
		drawerOpen: true, unreadSelected: true, unreadCount: 1, staleUnreadItems: true,
		items: []contract.FindAllItem{{Index: 0, Fields: map[string]string{
			"name": "邓云川", "unread_count": "1", "thread_meta": url.QueryEscape(`{"unread":true,"to_imid":"thread-new"}`),
		}}},
	}
	cfg := model.Config{
		ID: "liepin", Name: "猎聘企业端", MaxItems: 100,
		Selectors: map[string]contract.SelectorSpec{
			"message.drawer":              testLiepinSelector("测试联系人抽屉"),
			"message.entry_unread_count":  testLiepinSelector("测试未读数字"),
			"message.unread_tab":          testLiepinSelector("测试未读标签"),
			"message.unread_tab_selected": testLiepinSelector("测试未读标签已选中"),
			"message.all_tab":             testLiepinSelector("测试全部标签"),
			"message.all_tab_selected":    testLiepinSelector("测试全部标签已选中"),
			"message.unread_item":         testLiepinSelector("测试未读联系人"),
		},
	}
	conversations, err := (&Runtime{}).ScanUnreadConversations(context.Background(), browser, cfg)
	if err != nil || len(conversations) != 1 || conversations[0].Name != "邓云川" {
		t.Fatalf("刷新后仍没有读到未读联系人：conversations=%+v err=%v", conversations, err)
	}
	if !browser.refreshStarted || browser.staleUnreadItems {
		t.Fatalf("没有完成全部到未读的刷新：clicks=%v stale=%t", browser.clicks, browser.staleUnreadItems)
	}
}

// TestLiepinFallbackConversationKeyIgnoresChangedSummary 验证猎聘缺少原生编号时不会用消息摘要生成新会话。
func TestLiepinFallbackConversationKeyIgnoresChangedSummary(t *testing.T) {
	first, err := liepinConversations([]contract.FindAllItem{{
		Index: 0, Fields: map[string]string{"name": "张女士", "last_message": "第一条消息"},
	}}, false)
	if err != nil {
		t.Fatalf("整理第一条猎聘会话失败：%v", err)
	}
	second, err := liepinConversations([]contract.FindAllItem{{
		Index: 0, Fields: map[string]string{"name": "张女士", "last_message": "摘要已经变化"},
	}}, false)
	if err != nil {
		t.Fatalf("整理第二条猎聘会话失败：%v", err)
	}
	if len(first) != 1 || len(second) != 1 || first[0].Key != second[0].Key || !strings.HasPrefix(first[0].Key, "local:") {
		t.Fatalf("猎聘同一候选人的本地会话键不稳定：first=%+v second=%+v", first, second)
	}
	if first[0].PlatformThreadID != "" || second[0].PlatformThreadID != "" {
		t.Fatalf("猎聘本地会话键冒充了页面会话编号：first=%+v second=%+v", first[0], second[0])
	}
	browser := &liepinReplyBrowserStub{items: []contract.FindAllItem{{
		Index: 7, Fields: map[string]string{"name": "张女士", "last_message": "候选人刚发来的新消息"},
	}}}
	cfg := model.Config{Name: "猎聘企业端", Selectors: map[string]contract.SelectorSpec{
		"message.contact_item": testLiepinSelector("联系人项目"),
	}}
	index, err := locateLiepinConversation(context.Background(), browser, cfg, first[0])
	if err != nil || index != 7 {
		t.Fatalf("猎聘摘要变化后没有按唯一姓名恢复本地会话：index=%d err=%v", index, err)
	}
}

// TestLocateLiepinConversationUsesFreshThreadID 验证旧序号变化后仍按 to_imid 找到目标会话。
func TestLocateLiepinConversationUsesFreshThreadID(t *testing.T) {
	browser := &liepinReplyBrowserStub{items: []contract.FindAllItem{
		{Index: 0, Fields: map[string]string{
			"name": "其他候选人", "last_message": "其他消息",
			"thread_meta": url.QueryEscape(`{"unread":true,"to_imid":"thread-other"}`),
		}},
		{Index: 1, Fields: map[string]string{
			"name": "李女士", "last_message": "薪资是多少",
			"thread_meta": url.QueryEscape(`{"unread":true,"to_imid":"thread-target"}`),
		}},
	}}
	cfg := model.Config{
		ID: "liepin", Name: "猎聘企业端", MaxItems: 100,
		Selectors: map[string]contract.SelectorSpec{
			"message.contact_item": testLiepinSelector("联系人项目"),
		},
	}
	index, err := locateLiepinConversation(context.Background(), browser, cfg, model.Conversation{
		Index: 99, Name: "李女士", PlatformThreadID: "thread-target",
	})
	if err != nil || index != 1 {
		t.Fatalf("没有按最新会话编号定位：index=%d err=%v", index, err)
	}
}

// TestLiepinMessagesParsesDirectionsAndResumeCard 验证系统、候选人简历和 HR 消息能被稳定分类。
func TestLiepinMessagesParsesDirectionsAndResumeCard(t *testing.T) {
	items := []contract.FindAllItem{
		{Index: 0, Text: "沟通职位：测试岗位", Fields: map[string]string{
			"message_meta": url.QueryEscape(`{"message_id":"system-1"}`),
			"message_time": "8月2日 00:54",
		}},
		{Index: 1, Fields: map[string]string{
			"body_class":   "im-ui-message-item-body im-ui-message-item-receive",
			"message_text": "这是我的简历", "resume_card": "在线简历 附件简历",
			"candidate_meta": "cid=candidate-1&ctype=2", "message_time": "昨天 10:44",
		}},
		{Index: 2, Fields: map[string]string{
			"body_class":   "im-ui-message-item-body im-ui-message-item-send",
			"message_text": "收到", "message_time": "10:45",
		}},
	}
	messages, err := liepinMessages(items)
	if err != nil || len(messages) != 3 {
		t.Fatalf("消息解析失败：messages=%+v err=%v", messages, err)
	}
	if messages[0].Direction != "system" || messages[0].PlatformMessageID != "system-1" {
		t.Fatalf("系统消息解析不正确：%+v", messages[0])
	}
	if messages[1].Direction != "candidate" || messages[1].MessageType != "resume" {
		t.Fatalf("候选人简历消息解析不正确：%+v", messages[1])
	}
	var card struct {
		CandidateID string `json:"candidate_id"`
	}
	if err = json.Unmarshal(messages[1].CardContent, &card); err != nil || card.CandidateID != "candidate-1" {
		t.Fatalf("简历卡片候选人编号不正确：card=%+v err=%v", card, err)
	}
	if available, source := liepinResumeCard(messages); !available || source == "" {
		t.Fatalf("没有识别出候选人简历卡片：available=%t source=%q", available, source)
	}
	if messages[2].Direction != "self" || messages[2].Key == "" {
		t.Fatalf("HR 消息解析不正确：%+v", messages[2])
	}
}

// TestReadLiepinConversationHistoryRetriesUnchangedScroll 验证第一次滚轮没有加载新节点时仍会继续读取更早的简历卡片。
func TestReadLiepinConversationHistoryRetriesUnchangedScroll(t *testing.T) {
	recent := []contract.FindAllItem{{Index: 0, Fields: map[string]string{
		"body_class": "im-ui-message-item-body im-ui-message-item-receive", "message_text": "最新消息", "message_time": "17:15",
	}}}
	withResume := append([]contract.FindAllItem{{Index: 0, Fields: map[string]string{
		"body_class": "im-ui-message-item-body im-ui-message-item-receive", "message_text": "这是我的简历",
		"message_time": "11:44", "resume_card": "在线简历 附件简历",
	}}}, recent...)
	browser := &liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{recent, recent, withResume}}
	cfg := model.Config{ID: "liepin", Name: "猎聘企业端", Selectors: map[string]contract.SelectorSpec{
		"message.item":           testLiepinSelector("测试聊天消息"),
		"message.history_scroll": testLiepinSelector("测试聊天滚动区域"),
	}}
	messages, complete, err := readLiepinConversationHistory(context.Background(), browser, cfg, nil, 5000)
	if err != nil || !complete {
		t.Fatalf("聊天历史没有稳定读取完成：complete=%t err=%v", complete, err)
	}
	if available, _ := liepinResumeCard(messages); !available || browser.historyBatch < 2 {
		t.Fatalf("第一次滚轮无变化后没有继续找到简历：available=%t scrolls=%d", available, browser.historyBatch)
	}
}

// TestReadLiepinConversationHistoryStopsAtRecentTwoMessageBoundary 验证页面已包含云端最近两条消息时不滚动，只返回边界后的新消息。
func TestReadLiepinConversationHistoryStopsAtRecentTwoMessageBoundary(t *testing.T) {
	items := []contract.FindAllItem{
		{Index: 0, Fields: map[string]string{
			"body_class": "im-ui-message-item-body im-ui-message-item-receive", "message_text": "旧消息一",
			"message_meta": url.QueryEscape(`{"message_id":"old-1"}`), "message_time": "10:00",
		}},
		{Index: 1, Fields: map[string]string{
			"body_class": "im-ui-message-item-body im-ui-message-item-send", "message_text": "旧回复",
			"message_meta": url.QueryEscape(`{"message_id":"old-2"}`), "message_time": "10:01",
		}},
		{Index: 2, Fields: map[string]string{
			"body_class": "im-ui-message-item-body im-ui-message-item-receive", "message_text": "新消息",
			"message_meta": url.QueryEscape(`{"message_id":"new-1"}`), "message_time": "10:02",
		}},
	}
	browser := &liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{items}}
	cfg := model.Config{ID: "liepin", Name: "猎聘企业端", Selectors: map[string]contract.SelectorSpec{
		"message.item": testLiepinSelector("测试聊天消息"),
	}}
	messages, complete, err := readLiepinConversationHistory(context.Background(), browser, cfg, []string{"old-1", "old-2"}, 5000)
	if err != nil || complete || len(messages) != 1 || messages[0].PlatformMessageID != "new-1" || browser.historyBatch != 0 {
		t.Fatalf("messages=%+v complete=%t scrolls=%d err=%v", messages, complete, browser.historyBatch, err)
	}
}

// TestReadLiepinConversationHistoryStopsAtNewestKnownBoundary 验证两条已同步消息中间夹有同分钟消息时仍按最靠后的边界停止。
func TestReadLiepinConversationHistoryStopsAtNewestKnownBoundary(t *testing.T) {
	items := []contract.FindAllItem{
		{Index: 0, Fields: map[string]string{
			"body_class": "im-ui-message-item-body im-ui-message-item-receive", "message_text": "已同步候选人消息",
			"message_meta": url.QueryEscape(`{"message_id":"known-1"}`), "message_time": "10:00",
		}},
		{Index: 1, Fields: map[string]string{
			"body_class": "im-ui-message-item-body im-ui-message-item-receive", "message_text": "同一分钟的中间消息",
			"message_meta": url.QueryEscape(`{"message_id":"middle-1"}`), "message_time": "10:00",
		}},
		{Index: 2, Fields: map[string]string{
			"body_class": "im-ui-message-item-body im-ui-message-item-send", "message_text": "已同步回复",
			"message_meta": url.QueryEscape(`{"message_id":"known-2"}`), "message_time": "10:00",
		}},
		{Index: 3, Fields: map[string]string{
			"body_class": "im-ui-message-item-body im-ui-message-item-receive", "message_text": "真正的新消息",
			"message_meta": url.QueryEscape(`{"message_id":"new-1"}`), "message_time": "10:01",
		}},
	}
	browser := &liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{items}}
	cfg := model.Config{ID: "liepin", Name: "猎聘企业端", Selectors: map[string]contract.SelectorSpec{
		"message.item": testLiepinSelector("测试聊天消息"),
	}}
	messages, complete, err := readLiepinConversationHistory(context.Background(), browser, cfg, []string{"known-1", "known-2"}, 5000)
	if err != nil || complete || len(messages) != 1 || messages[0].PlatformMessageID != "new-1" || browser.historyBatch != 0 {
		t.Fatalf("messages=%+v complete=%t scrolls=%d err=%v", messages, complete, browser.historyBatch, err)
	}
}

// TestLiepinHistoryKeepsResumeCardBeforeKnownBoundary 验证旧简历卡片位于云端游标前时仍保留卡片状态。
func TestLiepinHistoryKeepsResumeCardBeforeKnownBoundary(t *testing.T) {
	items := []contract.FindAllItem{
		{Index: 0, Fields: map[string]string{
			"body_class": "im-ui-message-item-body im-ui-message-item-receive", "message_text": "这是我的简历",
			"resume_card": "在线简历 附件简历", "message_time": "09:58",
		}},
		{Index: 1, Fields: map[string]string{
			"body_class": "im-ui-message-item-body im-ui-message-item-send", "message_text": "收到",
			"message_meta": url.QueryEscape(`{"message_id":"known-1"}`), "message_time": "10:00",
		}},
		{Index: 2, Fields: map[string]string{
			"body_class": "im-ui-message-item-body im-ui-message-item-receive", "message_text": "工作地点在哪里？",
			"message_meta": url.QueryEscape(`{"message_id":"new-1"}`), "message_time": "10:01",
		}},
	}
	browser := &liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{items}}
	cfg := model.Config{ID: "liepin", Name: "猎聘企业端", Selectors: map[string]contract.SelectorSpec{
		"message.item": testLiepinSelector("测试聊天消息"),
	}}
	history, err := readLiepinConversationHistoryDetail(context.Background(), browser, cfg, []string{"known-1"}, 5000)
	if err != nil {
		t.Fatalf("读取猎聘差量消息失败：%v", err)
	}
	if len(history.Messages) != 1 || history.Messages[0].PlatformMessageID != "new-1" {
		t.Fatalf("猎聘差量边界不正确：%+v", history.Messages)
	}
	if !history.ResumeCardAvailable || history.ResumeSourceMessageID == "" {
		t.Fatalf("猎聘游标前的旧简历卡片状态丢失：%+v", history)
	}
}

// TestReadOpenAutoReplyLatestMessageDoesNotReopenClosedChat 验证空闲轮询只探测聊天框，不通过联系人列表重新打开旧候选人。
func TestReadOpenAutoReplyLatestMessageDoesNotReopenClosedChat(t *testing.T) {
	browser := &liepinReplyBrowserStub{}
	cfg := model.Config{ID: "liepin", Name: "猎聘企业端", Selectors: map[string]contract.SelectorSpec{
		"message.current_name": testLiepinSelector("测试当前候选人"),
	}}
	message, opened, err := (&Runtime{}).ReadOpenAutoReplyLatestMessage(
		context.Background(), browser, cfg, model.AutoReplyConversationSnapshot{CandidateName: "邓云川"},
	)
	if err != nil || opened || message.Key != "" || len(browser.clicks) != 0 {
		t.Fatalf("message=%+v opened=%t clicks=%v err=%v", message, opened, browser.clicks, err)
	}
}

// TestLiepinOpenConversationRequiresMatchingNameAndPosition 验证猎聘企业端同名不同岗位不会被当成当前待回复会话。
func TestLiepinOpenConversationRequiresMatchingNameAndPosition(t *testing.T) {
	browser := &liepinReplyBrowserStub{conversationOpen: true}
	cfg := model.Config{ID: "liepin", Name: "猎聘企业端", Selectors: map[string]contract.SelectorSpec{
		"message.current_name":     testLiepinSelector("测试当前候选人"),
		"message.current_position": testLiepinSelector("测试当前岗位"),
	}}
	matched, err := liepinAutoReplyConversationMatches(context.Background(), browser, cfg, model.AutoReplyConversationSnapshot{
		CandidateName: "邓云川", CommunicationPosition: "测试岗位",
	})
	if err != nil || !matched {
		t.Fatalf("同一候选人和岗位没有匹配：matched=%t err=%v", matched, err)
	}
	matched, err = liepinAutoReplyConversationMatches(context.Background(), browser, cfg, model.AutoReplyConversationSnapshot{
		CandidateName: "邓云川", CommunicationPosition: "另一个岗位",
	})
	if err != nil || matched {
		t.Fatalf("同名不同岗位被误判为同一会话：matched=%t err=%v", matched, err)
	}
	message, opened, err := (&Runtime{}).ReadOpenAutoReplyLatestMessage(
		context.Background(), browser, cfg, model.AutoReplyConversationSnapshot{
			CandidateName: "邓云川", CommunicationPosition: "另一个岗位",
		},
	)
	if err != nil || opened || message.Key != "" {
		t.Fatalf("轮询读取了同名不同岗位的消息：message=%+v opened=%t err=%v", message, opened, err)
	}
	_, err = liepinAutoReplyConversationMatches(context.Background(), browser, cfg, model.AutoReplyConversationSnapshot{
		CandidateName: "邓云川",
	})
	if err == nil || !strings.Contains(err.Error(), "缺少沟通岗位") {
		t.Fatalf("打开聊天框时缺少目标岗位应该拒绝复用：%v", err)
	}
}

// TestLiepinMessageFallbackKeyUsesAbsoluteTime 验证相对时间和次日月日时间会生成同一条消息指纹。
func TestLiepinMessageFallbackKeyUsesAbsoluteTime(t *testing.T) {
	yesterday := []contract.FindAllItem{{Index: 0, Fields: map[string]string{
		"body_class":   "im-ui-message-item-body im-ui-message-item-receive",
		"message_text": "薪资是多少", "message_time": "昨天 10:44",
	}}}
	monthDay := []contract.FindAllItem{{Index: 0, Fields: map[string]string{
		"body_class":   "im-ui-message-item-body im-ui-message-item-receive",
		"message_text": "薪资是多少", "message_time": "8月3日 10:44",
	}}}
	first, err := liepinMessagesAt(yesterday, time.Date(2026, 8, 4, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60)))
	if err != nil {
		t.Fatalf("解析相对时间消息失败：%v", err)
	}
	second, err := liepinMessagesAt(monthDay, time.Date(2026, 8, 5, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60)))
	if err != nil {
		t.Fatalf("解析月日时间消息失败：%v", err)
	}
	if first[0].Key == "" || first[0].Key != second[0].Key {
		t.Fatalf("同一条消息隔天指纹发生变化：first=%q second=%q", first[0].Key, second[0].Key)
	}
}

// TestLiepinMessageKeysSurvivePrependedHistory 验证猎聘向顶部加载旧消息后不会改变原有无编号消息指纹。
func TestLiepinMessageKeysSurvivePrependedHistory(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	recentItems := []contract.FindAllItem{
		{Index: 0, Fields: map[string]string{
			"body_class": "im-ui-message-item-body im-ui-message-item-receive", "message_text": "你好", "message_time": "10:00",
		}},
		{Index: 1, Fields: map[string]string{
			"body_class": "im-ui-message-item-body im-ui-message-item-send", "message_text": "你好，请问有什么想了解的？", "message_time": "10:01",
		}},
	}
	recent, err := liepinMessagesAt(recentItems, now)
	if err != nil {
		t.Fatalf("解析猎聘当前消息失败：%v", err)
	}
	withHistoryItems := append([]contract.FindAllItem{{Index: 0, Fields: map[string]string{
		"body_class": "im-ui-message-item-body im-ui-message-item-receive", "message_text": "你好", "message_time": "10:00",
	}}}, recentItems...)
	withHistory, err := liepinMessagesAt(withHistoryItems, now)
	if err != nil {
		t.Fatalf("解析猎聘补齐历史后的消息失败：%v", err)
	}
	if recent[0].Key == "" || recent[0].Key != withHistory[1].Key || recent[1].Key != withHistory[2].Key {
		t.Fatalf("猎聘加载顶部历史后原消息指纹变化：recent=%+v history=%+v", recent, withHistory)
	}
	withNewItems := append(append([]contract.FindAllItem(nil), recentItems...), contract.FindAllItem{
		Index: 2, Fields: map[string]string{
			"body_class": "im-ui-message-item-body im-ui-message-item-receive", "message_text": "还有一个问题", "message_time": "10:02",
		},
	})
	withNew, err := liepinMessagesAt(withNewItems, now)
	if err != nil {
		t.Fatalf("解析猎聘新增消息后的记录失败：%v", err)
	}
	if recent[0].Key != withNew[0].Key || recent[1].Key != withNew[1].Key {
		t.Fatalf("猎聘收到更新消息后旧消息指纹变化：recent=%+v new=%+v", recent, withNew)
	}
}

// TestParseLiepinMessageTimeUsesPreviousYear 验证一月看到十二月消息时不会误记成未来时间。
func TestParseLiepinMessageTimeUsesPreviousYear(t *testing.T) {
	parsed := parseLiepinMessageTime(
		"12月31日 23:50",
		time.Date(2026, 1, 1, 8, 0, 0, 0, time.FixedZone("CST", 8*60*60)),
	)
	if parsed == nil || parsed.In(time.FixedZone("CST", 8*60*60)).Year() != 2025 {
		t.Fatalf("跨年消息时间不正确：%v", parsed)
	}
}

// TestParseLiepinResumeFields 验证国际手机号、邮箱、微信和年龄估算年份的清洗结果。
func TestParseLiepinResumeFields(t *testing.T) {
	resume := "男 | 40岁\nMobile: +86 136 3281 3031\nEmail: Jinbin.Liang@gmail.com\n微信：jinbin_liang"
	phone, email, wechat := parseLiepinContacts(resume)
	if phone != "+8613632813031" || email != "jinbin.liang@gmail.com" || wechat != "jinbin_liang" {
		t.Fatalf("联系方式解析不正确：phone=%q email=%q wechat=%q", phone, email, wechat)
	}
	birthYM, precision := parseLiepinBirthYM(resume, time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC))
	if birthYM != "1986" || precision != "year_estimated" {
		t.Fatalf("年龄估算出生年份不正确：birth_ym=%q precision=%q", birthYM, precision)
	}
}

// TestCollectLiepinOnlineResumeUsesSamePagePanel 验证在线简历按同页浮层读取并在完成后关闭。
func TestCollectLiepinOnlineResumeUsesSamePagePanel(t *testing.T) {
	browser := &liepinReplyBrowserStub{}
	cfg := model.Config{
		ID: "liepin", Name: "猎聘企业端",
		Selectors: map[string]contract.SelectorSpec{
			"message.resume_online_entry": testLiepinSelector("测试在线简历入口"),
			"candidate.detail":            testLiepinSelector("测试在线简历浮层"),
			"candidate.detail_close":      testLiepinSelector("测试在线简历关闭"),
		},
	}
	text, err := collectLiepinOnlineResume(context.Background(), browser, cfg)
	if err != nil || text != "邓云川\n本科\n手机：17607080935" {
		t.Fatalf("同页在线简历读取失败：text=%q err=%v", text, err)
	}
	if browser.detailOpen {
		t.Fatal("在线简历读取完成后仍然遮挡页面")
	}
	if len(browser.clicks) != 2 || browser.clicks[0] != "测试在线简历入口" || browser.clicks[1] != "测试在线简历关闭" {
		t.Fatalf("在线简历打开和关闭顺序不正确：%v", browser.clicks)
	}
}

// TestCollectAutoReplyResumeDownloadsAttachmentBeforeOnlinePanel 验证附件先于会关闭聊天框的在线简历浮层处理。
func TestCollectAutoReplyResumeDownloadsAttachmentBeforeOnlinePanel(t *testing.T) {
	downloadPath := t.TempDir() + "/resume.pdf"
	if err := os.WriteFile(downloadPath, []byte("test resume"), 0o600); err != nil {
		t.Fatalf("准备测试附件失败：%v", err)
	}
	browser := &liepinReplyBrowserStub{
		conversationOpen: true, downloadPath: downloadPath, attachmentFails: 1,
		items: []contract.FindAllItem{{Index: 0, Fields: map[string]string{
			"name": "邓云川", "thread_meta": url.QueryEscape(`{"to_imid":"thread-target"}`),
		}}},
	}
	cfg := model.Config{
		ID: "liepin", Name: "猎聘企业端",
		Selectors: map[string]contract.SelectorSpec{
			"message.current_name":             testLiepinSelector("测试当前候选人"),
			"message.current_position":         testLiepinSelector("测试当前岗位"),
			"message.drawer":                   testLiepinSelector("测试联系人抽屉"),
			"message.entry":                    testLiepinSelector("测试联系人入口"),
			"message.all_tab":                  testLiepinSelector("测试全部标签"),
			"message.all_tab_selected":         testLiepinSelector("测试全部标签已选中"),
			"message.contact_item":             testLiepinSelector("测试联系人项目"),
			"message.contact_click_target":     testLiepinSelector("测试联系人点击"),
			"message.drawer_scroll":            testLiepinSelector("测试联系人滚动区域"),
			"message.resume_attachment_entry":  testLiepinSelector("测试附件入口"),
			"message.attachment_preview":       testLiepinSelector("测试附件预览"),
			"message.attachment_body":          testLiepinSelector("测试附件正文"),
			"message.attachment_download":      testLiepinSelector("测试附件下载"),
			"message.attachment_preview_close": testLiepinSelector("测试附件关闭"),
			"message.history_scroll":           testLiepinSelector("测试聊天滚动区域"),
			"message.resume_online_entry":      testLiepinSelector("测试在线简历入口"),
			"candidate.detail":                 testLiepinSelector("测试在线简历浮层"),
			"candidate.detail_close":           testLiepinSelector("测试在线简历关闭"),
		},
	}
	bundle, err := (&Runtime{}).CollectAutoReplyResume(context.Background(), browser, cfg, model.AutoReplyConversationSnapshot{
		Conversation:  model.Conversation{Name: "邓云川", PlatformThreadID: "thread-target"},
		CandidateName: "邓云川", PlatformThreadID: "thread-target", CommunicationPosition: "测试岗位",
		ResumeCardAvailable: true,
	})
	if err != nil {
		t.Fatalf("收集猎聘简历失败：%v", err)
	}
	if len(bundle.AttachmentPaths) != 1 || bundle.AttachmentPaths[0] != downloadPath {
		t.Fatalf("附件没有完成落盘：%v", bundle.AttachmentPaths)
	}
	attachmentIndex, attachmentClicks, onlineIndex := -1, 0, -1
	for index, click := range browser.clicks {
		if click == "测试附件入口" {
			attachmentIndex = index
			attachmentClicks++
		}
		if click == "测试在线简历入口" {
			onlineIndex = index
		}
	}
	if attachmentIndex < 0 || onlineIndex < 0 || attachmentIndex >= onlineIndex {
		t.Fatalf("简历处理顺序不正确：clicks=%v", browser.clicks)
	}
	if attachmentClicks != 2 || !browser.attachmentVerify || browser.attachmentAnchor != "测试聊天滚动区域" {
		t.Fatalf("附件入口没有在滚动区内重试并验证弹框：clicks=%v verified=%t anchor=%q", browser.clicks, browser.attachmentVerify, browser.attachmentAnchor)
	}
	if !browser.conversationOpen || !browser.allSelected {
		t.Fatalf("在线简历关闭后没有恢复原候选人聊天框：conversation_open=%t all_selected=%t clicks=%v", browser.conversationOpen, browser.allSelected, browser.clicks)
	}
}

// TestLiepinAttachmentDownloadTargetsClickableAnchor 验证附件下载选择器直接点击真实链接，不误点只有样式的外层容器。
func TestLiepinAttachmentDownloadTargetsClickableAnchor(t *testing.T) {
	raw, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatalf("读取猎聘配置失败：%v", err)
	}
	var cfg model.Config
	if err = json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("解析猎聘配置失败：%v", err)
	}
	selector, ok := cfg.Selectors["message.attachment_download"]
	if !ok || len(selector.Target.Selectors) != 1 {
		t.Fatalf("附件下载选择器没有准备完整：%+v", selector)
	}
	if value := selector.Target.Selectors[0].Value; value != ".ant-im-space-item a" {
		t.Fatalf("附件下载没有指向可点击链接：%q", value)
	}
}

// TestLiepinAttachmentEntryTargetsText 验证附件入口只点文字，不再随机落到整块卡片的图标或空白区域。
func TestLiepinAttachmentEntryTargetsText(t *testing.T) {
	raw, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatalf("读取猎聘配置失败：%v", err)
	}
	var cfg model.Config
	if err = json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("解析猎聘配置失败：%v", err)
	}
	selector, ok := cfg.Selectors["message.resume_attachment_entry"]
	if !ok || len(selector.Target.Selectors) != 1 {
		t.Fatalf("附件入口选择器没有准备完整：%+v", selector)
	}
	if value := selector.Target.Selectors[0].Value; value != ".im-ui-send-attachment-card-info-item-content" {
		t.Fatalf("附件入口没有缩小到文字：%q", value)
	}
	if selector.Target.Text != "附件简历" || selector.Target.ExactText == nil || !*selector.Target.ExactText {
		t.Fatalf("附件入口文字约束不正确：%+v", selector.Target)
	}
	if len(selector.Parents) != 2 || !strings.Contains(selector.Parents[1].Selectors[0].Value, ":not(:has(~") {
		t.Fatalf("附件入口没有限定最后一条简历消息：%+v", selector.Parents)
	}
}

// TestLiepinRequestResumeSelectorExcludesViewResume 验证索要简历不会误点已经变成“看简历”的同类按钮。
func TestLiepinRequestResumeSelectorExcludesViewResume(t *testing.T) {
	raw, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatalf("读取猎聘配置失败：%v", err)
	}
	var cfg model.Config
	if err = json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("解析猎聘配置失败：%v", err)
	}
	selector := cfg.Selectors["candidate.request_resume"]
	if selector.Target.Text != "索要简历" || selector.Target.ExactText == nil || !*selector.Target.ExactText {
		t.Fatalf("索要简历没有排除看简历按钮：%+v", selector.Target)
	}
}

// TestLiepinPositionMatchesFullAndTruncated 验证只有明确带省略号的岗位才允许按非空前缀匹配。
func TestLiepinPositionMatchesFullAndTruncated(t *testing.T) {
	if !liepinPositionMatches("高中数学老师", "高中数学老师") {
		t.Fatal("完整岗位应该精确匹配")
	}
	if !liepinPositionMatches("AI应用开发工程师初级可以实习", "AI应用开发工程师初...") {
		t.Fatal("三个点截断岗位应该匹配完整岗位")
	}
	if !liepinPositionMatches("AI应用开发工程师初级可以实习", "AI应用开发工程师初…") {
		t.Fatal("中文省略号截断岗位应该匹配完整岗位")
	}
	if liepinPositionMatches("高中数学老师", "AI应用开发工程师初...") {
		t.Fatal("不同岗位不能误匹配")
	}
	if liepinPositionMatches("高中数学老师助教", "高中数学老师") {
		t.Fatal("没有省略号的岗位不能按前缀误匹配")
	}
	if liepinPositionMatches("高中数学老师", "...") || liepinPositionMatches("高中数学老师", "…") {
		t.Fatal("省略号前缀为空时不能匹配任何岗位")
	}
}

// testLiepinSelector 创建只用于单元测试的最小有效选择器。
func testLiepinSelector(description string) contract.SelectorSpec {
	return contract.SelectorSpec{
		Target:      contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".test"}}},
		Description: description,
	}
}
