// Package common 文件作用：验证公共自动回复只处理真实未读、恢复会话会切全部标签并保持跨年指纹稳定。
package common

import (
	"context"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

type autoReplyTabBrowser struct {
	model.Browser
	allSelected bool
	clicks      []string
}

type autoReplyLocateBrowser struct {
	model.Browser
	items []contract.FindAllItem
}

// FindAll 返回公共会话重新定位测试准备的联系人列表。
func (b *autoReplyLocateBrowser) FindAll(_ context.Context, _ contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	return b.items, nil
}

// FindAll 模拟已经打开的联系人抽屉和全部标签选中状态。
func (b *autoReplyTabBrowser) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	switch request.Selector.Description {
	case "联系人抽屉":
		return []contract.FindAllItem{{Index: 0}}, nil
	case "全部标签已选中":
		if b.allSelected {
			return []contract.FindAllItem{{Index: 0}}, nil
		}
		return nil, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
	default:
		return []contract.FindAllItem{{Index: 0}}, nil
	}
}

// Click 记录切换全部联系人并更新选中状态。
func (b *autoReplyTabBrowser) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	b.clicks = append(b.clicks, request.Selector.Description)
	if request.Selector.Description == "全部标签" {
		b.allSelected = true
	}
	return contract.ClickResult{Clicked: true}, nil
}

// TestConfiguredConversationsRequiresVisibleUnreadCount 验证未读数字为空或零的联系人不会进入自动回复循环。
func TestConfiguredConversationsRequiresVisibleUnreadCount(t *testing.T) {
	items := []contract.FindAllItem{
		{Index: 0, Fields: map[string]string{"name": "空数字"}},
		{Index: 1, Fields: map[string]string{"name": "零", "unread_count": "0"}},
		{Index: 2, Fields: map[string]string{"name": "新消息", "unread_count": "2"}},
	}
	conversations, err := ConfiguredConversations(items, "test", true)
	if err != nil {
		t.Fatalf("整理未读联系人失败：%v", err)
	}
	if len(conversations) != 1 || conversations[0].Name != "新消息" {
		t.Fatalf("没有只保留头像带数字的联系人：%+v", conversations)
	}
}

// TestConfiguredConversationKeyIgnoresChangedSummary 验证无原生会话编号时消息摘要变化不会重复创建会话。
func TestConfiguredConversationKeyIgnoresChangedSummary(t *testing.T) {
	first, err := ConfiguredConversations([]contract.FindAllItem{{
		Index: 0, Fields: map[string]string{
			"name": "张女士", "avatar_url": "https://example.com/avatar-1.png", "last_message": "第一条消息",
		},
	}}, "boss", false)
	if err != nil {
		t.Fatalf("整理第一条会话失败：%v", err)
	}
	second, err := ConfiguredConversations([]contract.FindAllItem{{
		Index: 0, Fields: map[string]string{
			"name": "张女士", "avatar_url": "https://example.com/avatar-1.png?sign=changed", "last_message": "摘要已经变化",
		},
	}}, "boss", false)
	if err != nil {
		t.Fatalf("整理第二条会话失败：%v", err)
	}
	if len(first) != 1 || len(second) != 1 || first[0].Key != second[0].Key {
		t.Fatalf("同一候选人的会话键不稳定：first=%+v second=%+v", first, second)
	}
	if !strings.HasPrefix(first[0].Key, "local:") || first[0].PlatformThreadID != "" || second[0].PlatformThreadID != "" {
		t.Fatalf("本地会话键冒充了页面会话编号：first=%+v second=%+v", first[0], second[0])
	}
}

// TestConfiguredConversationWithoutNameRejectsFallbackKey 验证缺少平台会话编号和姓名时不会生成冲突本地键。
func TestConfiguredConversationWithoutNameRejectsFallbackKey(t *testing.T) {
	_, err := ConfiguredConversations([]contract.FindAllItem{{
		Index: 0, Fields: map[string]string{"last_message": "你好"},
	}}, "boss", false)
	if err == nil || !strings.Contains(err.Error(), "缺少候选人姓名") {
		t.Fatalf("缺少候选人姓名时应该明确报错：%v", err)
	}
}

// TestConfiguredMessageKeysSurvivePrependedHistory 验证向顶部加载更早消息不会改变原有无编号消息的指纹。
func TestConfiguredMessageKeysSurvivePrependedHistory(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	recentItems := []contract.FindAllItem{
		{Index: 0, Fields: map[string]string{"direction": "candidate", "message_text": "你好", "message_time": "10:00"}},
		{Index: 1, Fields: map[string]string{"direction": "self", "message_text": "你好，请问有什么想了解的？", "message_time": "10:01"}},
	}
	recent, err := ParseConfiguredAutoReplyMessages(recentItems, AutoReplyMessageRules{}, now)
	if err != nil {
		t.Fatalf("解析当前消息失败：%v", err)
	}
	withHistoryItems := append([]contract.FindAllItem{{
		Index: 0, Fields: map[string]string{"direction": "candidate", "message_text": "你好", "message_time": "10:00"},
	}}, recentItems...)
	withHistory, err := ParseConfiguredAutoReplyMessages(withHistoryItems, AutoReplyMessageRules{}, now)
	if err != nil {
		t.Fatalf("解析补齐历史后的消息失败：%v", err)
	}
	if recent[0].Key == "" || recent[0].Key != withHistory[1].Key || recent[1].Key != withHistory[2].Key {
		t.Fatalf("加载顶部历史后原消息指纹变化：recent=%+v history=%+v", recent, withHistory)
	}
	withNewItems := append(append([]contract.FindAllItem(nil), recentItems...), contract.FindAllItem{
		Index: 2, Fields: map[string]string{"direction": "candidate", "message_text": "还有一个问题", "message_time": "10:02"},
	})
	withNew, err := ParseConfiguredAutoReplyMessages(withNewItems, AutoReplyMessageRules{}, now)
	if err != nil {
		t.Fatalf("解析新增消息后的记录失败：%v", err)
	}
	if recent[0].Key != withNew[0].Key || recent[1].Key != withNew[1].Key {
		t.Fatalf("收到更新消息后旧消息指纹变化：recent=%+v new=%+v", recent, withNew)
	}
}

// TestConfiguredHistoryKeepsResumeCardBeforeKnownBoundary 验证旧简历卡片位于云端游标前时仍保留卡片状态。
func TestConfiguredHistoryKeepsResumeCardBeforeKnownBoundary(t *testing.T) {
	browser := &autoReplyLocateBrowser{items: []contract.FindAllItem{
		{Index: 0, Fields: map[string]string{
			"direction": "candidate", "message_type": "resume", "resume_card": "附件简历", "message_time": "09:58",
		}},
		{Index: 1, Fields: map[string]string{
			"direction": "self", "message_text": "收到", "message_time": "10:00", "message_id": "known-1",
		}},
		{Index: 2, Fields: map[string]string{
			"direction": "candidate", "message_text": "工作地点在哪里？", "message_time": "10:01", "message_id": "new-1",
		}},
	}}
	cfg := model.Config{Name: "测试平台", Selectors: map[string]contract.SelectorSpec{
		"message.item": autoReplyTestSelector("聊天消息"),
	}}
	history, err := readConfiguredAutoReplyHistory(
		context.Background(), browser, cfg, ConfiguredAutoReplyAdapter{PlatformID: "test"}, []string{"known-1"}, 5000,
	)
	if err != nil {
		t.Fatalf("读取差量消息失败：%v", err)
	}
	if len(history.Messages) != 1 || history.Messages[0].PlatformMessageID != "new-1" {
		t.Fatalf("差量边界不正确：%+v", history.Messages)
	}
	if !history.ResumeCardAvailable || history.ResumeSourceMessageID == "" {
		t.Fatalf("游标前的旧简历卡片状态丢失：%+v", history)
	}
}

// TestEnsureConfiguredConversationDrawerSelectsAllTab 验证恢复已读会话前会从未读标签切到全部联系人。
func TestEnsureConfiguredConversationDrawerSelectsAllTab(t *testing.T) {
	browser := &autoReplyTabBrowser{}
	cfg := model.Config{Selectors: map[string]contract.SelectorSpec{
		"message.drawer":           autoReplyTestSelector("联系人抽屉"),
		"message.all_tab":          autoReplyTestSelector("全部标签"),
		"message.all_tab_selected": autoReplyTestSelector("全部标签已选中"),
	}}
	if err := ensureConfiguredConversationDrawer(context.Background(), browser, cfg); err != nil {
		t.Fatalf("恢复全部联系人失败：%v", err)
	}
	if len(browser.clicks) != 1 || browser.clicks[0] != "全部标签" {
		t.Fatalf("没有只切换一次全部标签：%v", browser.clicks)
	}
}

// TestParseConfiguredMessageTimeUsesPreviousYear 验证元旦附近的十二月消息不会被解析到未来。
func TestParseConfiguredMessageTimeUsesPreviousYear(t *testing.T) {
	now := time.Date(2027, 1, 1, 8, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	parsed := parseConfiguredMessageTime("12月31日 23:00", now)
	if parsed == nil || parsed.In(now.Location()).Year() != 2026 {
		t.Fatalf("跨年消息时间不正确：%v", parsed)
	}
}

// TestLocateConfiguredConversationUsesUniqueNameAfterSummaryChanges 验证没有平台编号时，唯一姓名仍可在消息摘要变化后恢复会话。
func TestLocateConfiguredConversationUsesUniqueNameAfterSummaryChanges(t *testing.T) {
	browser := &autoReplyLocateBrowser{items: []contract.FindAllItem{
		{Index: 7, Fields: map[string]string{"name": "张女士", "last_message": "候选人刚发来的新消息"}},
	}}
	cfg := model.Config{Name: "测试平台", Selectors: map[string]contract.SelectorSpec{
		"message.contact_item": autoReplyTestSelector("联系人项目"),
	}}
	index, err := locateConfiguredAutoReplyConversation(context.Background(), browser, cfg, ConfiguredAutoReplyAdapter{PlatformID: "test"}, model.Conversation{
		Name: "张女士", Summary: "数据库中的旧摘要",
	})
	if err != nil || index != 7 {
		t.Fatalf("唯一姓名没有在摘要变化后恢复会话：index=%d err=%v", index, err)
	}
}

// TestLocateConfiguredConversationRejectsDuplicateNames 验证没有稳定编号且姓名重复时不会猜测联系人。
func TestLocateConfiguredConversationRejectsDuplicateNames(t *testing.T) {
	browser := &autoReplyLocateBrowser{items: []contract.FindAllItem{
		{Index: 1, Fields: map[string]string{"name": "张女士", "last_message": "消息一"}},
		{Index: 2, Fields: map[string]string{"name": "张女士", "last_message": "消息二"}},
	}}
	cfg := model.Config{Name: "测试平台", Selectors: map[string]contract.SelectorSpec{
		"message.contact_item": autoReplyTestSelector("联系人项目"),
	}}
	_, err := locateConfiguredAutoReplyConversation(context.Background(), browser, cfg, ConfiguredAutoReplyAdapter{PlatformID: "test"}, model.Conversation{
		Name: "张女士", Summary: "消息一",
	})
	if err == nil {
		t.Fatalf("同名联系人即使摘要碰巧相同也不应该猜测")
	}
}

// TestLocateConfiguredConversationUsesAvatarKeyForDuplicateNames 验证同名联系人可按稳定头像路径唯一恢复。
func TestLocateConfiguredConversationUsesAvatarKeyForDuplicateNames(t *testing.T) {
	browser := &autoReplyLocateBrowser{items: []contract.FindAllItem{
		{Index: 1, Fields: map[string]string{"name": "张女士", "avatar_url": "https://img.example.com/a.png", "last_message": "消息一"}},
		{Index: 2, Fields: map[string]string{"name": "张女士", "avatar_url": "https://img.example.com/b.png?token=new", "last_message": "消息二"}},
	}}
	cfg := model.Config{Name: "测试平台", Selectors: map[string]contract.SelectorSpec{
		"message.contact_item": autoReplyTestSelector("联系人项目"),
	}}
	key, err := LocalConversationKey("test", "张女士", "https://img.example.com/b.png?token=old")
	if err != nil {
		t.Fatal(err)
	}
	index, err := locateConfiguredAutoReplyConversation(context.Background(), browser, cfg, ConfiguredAutoReplyAdapter{PlatformID: "test"}, model.Conversation{
		Key: key, Name: "张女士", AvatarURL: "https://img.example.com/b.png?token=old",
	})
	if err != nil || index != 2 {
		t.Fatalf("同名联系人没有按头像稳定键恢复：index=%d err=%v", index, err)
	}
}

// TestConfiguredPositionMatchesOnlyAllowsExplicitEllipsis 验证普通短文本不能冒充岗位前缀，只有明确省略号才允许。
func TestConfiguredPositionMatchesOnlyAllowsExplicitEllipsis(t *testing.T) {
	adapter := ConfiguredAutoReplyAdapter{}
	if configuredPositionMatches(adapter, "高级软件工程师", "高级软件") {
		t.Fatalf("没有省略号的短岗位文字不应该匹配完整岗位")
	}
	if !configuredPositionMatches(adapter, "高级软件工程师", "高级软件...") {
		t.Fatalf("带三个点的岗位前缀应该匹配")
	}
	if !configuredPositionMatches(adapter, "高级软件工程师", "高级软件…") {
		t.Fatalf("带中文省略号的岗位前缀应该匹配")
	}
}

// autoReplyTestSelector 返回公共自动回复测试使用的最小选择器。
func autoReplyTestSelector(description string) contract.SelectorSpec {
	return contract.SelectorSpec{
		Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".test"}}},
		State:  "visible", TimeoutMS: 1, Description: description,
	}
}
