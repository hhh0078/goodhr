// Package liepin 文件作用：验证猎聘自动回复的会话防串、消息解析和简历联系方式清洗规则。
package liepin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
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
	conversationOpen bool
	downloadPath     string
	unreadCount      int
	unreadSelected   bool
	clicks           []string
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
	}
	if request.Selector.Description == "测试在线简历入口" {
		b.detailOpen = true
	}
	if request.Selector.Description == "测试在线简历关闭" {
		b.detailOpen = false
		b.conversationOpen = false
	}
	if request.Selector.Description == "测试附件入口" {
		if !b.conversationOpen {
			return contract.ClickResult{}, fmt.Errorf("聊天框已经关闭，附件入口不存在")
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
			{Index: 0, Fields: map[string]string{"name": "邓云川", "thread_meta": url.QueryEscape(`{"unread":true,"to_imid":"thread-new"}`)}},
			{Index: 1, Fields: map[string]string{"name": "旧候选人", "thread_meta": url.QueryEscape(`{"unread":true,"to_imid":"thread-old"}`)}},
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
	if err != nil || len(conversations) != 1 || conversations[0].Name != "邓云川" {
		t.Fatalf("没有按入口数字只保留最新会话：conversations=%+v err=%v", conversations, err)
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
	browser := &liepinReplyBrowserStub{conversationOpen: true, downloadPath: downloadPath}
	cfg := model.Config{
		ID: "liepin", Name: "猎聘企业端",
		Selectors: map[string]contract.SelectorSpec{
			"message.current_name":             testLiepinSelector("测试当前候选人"),
			"message.resume_attachment_entry":  testLiepinSelector("测试附件入口"),
			"message.attachment_preview":       testLiepinSelector("测试附件预览"),
			"message.attachment_body":          testLiepinSelector("测试附件正文"),
			"message.attachment_download":      testLiepinSelector("测试附件下载"),
			"message.attachment_preview_close": testLiepinSelector("测试附件关闭"),
			"message.resume_online_entry":      testLiepinSelector("测试在线简历入口"),
			"candidate.detail":                 testLiepinSelector("测试在线简历浮层"),
			"candidate.detail_close":           testLiepinSelector("测试在线简历关闭"),
		},
	}
	bundle, err := (&Runtime{}).CollectAutoReplyResume(context.Background(), browser, cfg, model.AutoReplyConversationSnapshot{
		CandidateName: "邓云川", ResumeCardAvailable: true,
	})
	if err != nil {
		t.Fatalf("收集猎聘简历失败：%v", err)
	}
	if len(bundle.AttachmentPaths) != 1 || bundle.AttachmentPaths[0] != downloadPath {
		t.Fatalf("附件没有完成落盘：%v", bundle.AttachmentPaths)
	}
	attachmentIndex, onlineIndex := -1, -1
	for index, click := range browser.clicks {
		if click == "测试附件入口" {
			attachmentIndex = index
		}
		if click == "测试在线简历入口" {
			onlineIndex = index
		}
	}
	if attachmentIndex < 0 || onlineIndex < 0 || attachmentIndex >= onlineIndex {
		t.Fatalf("简历处理顺序不正确：clicks=%v", browser.clicks)
	}
}

// TestLiepinPositionMatchesFullAndTruncated 验证完整岗位和带省略号岗位只按前缀安全匹配。
func TestLiepinPositionMatchesFullAndTruncated(t *testing.T) {
	if !liepinPositionMatches("AI应用开发工程师初级可以实习", "AI应用开发工程师初...") {
		t.Fatal("页面截断岗位应该匹配完整岗位")
	}
	if liepinPositionMatches("高中数学老师", "AI应用开发工程师初...") {
		t.Fatal("不同岗位不能误匹配")
	}
}

// testLiepinSelector 创建只用于单元测试的最小有效选择器。
func testLiepinSelector(description string) contract.SelectorSpec {
	return contract.SelectorSpec{
		Target:      contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".test"}}},
		Description: description,
	}
}
