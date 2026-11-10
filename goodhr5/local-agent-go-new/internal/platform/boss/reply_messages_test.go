// Package boss 本文件用合成聊天验证可见时间组、防重内部键、增量边界和末条小窗口读取。
package boss

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// bossReplyTestBrowser 记录读取窗口与滚轮次数，不连接真实账号或服务器。
type bossReplyTestBrowser struct {
	model.Browser
	messages, contacts []contract.FindAllItem
	requests           []contract.ElementFindAllRequest
	values             map[string]string
	scrolls, clicks    int
	onScroll           func(*bossReplyTestBrowser)
}

// FindAll 按原始位置返回首尾窗口，字段只回传本次明确请求的项。
func (b *bossReplyTestBrowser) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	b.requests = append(b.requests, request)
	rows := b.messages
	if request.Selector.Description == "contacts" {
		rows = b.contacts
	}
	start := request.StartIndex
	if request.FromEnd {
		start = max(0, len(rows)-request.MaxItems)
	}
	var result []contract.FindAllItem
	for i := start; i < min(len(rows), start+request.MaxItems); i++ {
		row := contract.FindAllItem{Index: i, TotalMatches: len(rows), Text: rows[i].Text, Fields: map[string]string{}}
		for key := range request.Fields {
			row.Fields[key] = rows[i].Fields[key]
		}
		result = append(result, row)
	}
	return result, nil
}

// Scroll 回传明确的顶部位置证据，可在测试中模拟历史延后追加。
func (b *bossReplyTestBrowser) Scroll(_ context.Context, _ contract.ScrollRequest) (contract.ScrollResult, error) {
	b.scrolls++
	if b.onScroll != nil {
		b.onScroll(b)
	}
	visible, moved := true, false
	return contract.ScrollResult{ProgressTargetVisible: &visible, ProgressTargetMoved: &moved}, nil
}

// Read 返回测试明确配置的当前头部字段，缺失元素使用真实Worker错误码。
func (b *bossReplyTestBrowser) Read(_ context.Context, request contract.ElementReadRequest) (contract.ReadResult, error) {
	value, found := b.values[request.Selector.Description]
	if !found {
		return contract.ReadResult{}, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
	}
	return contract.ReadResult{Value: value}, nil
}

// ListPages 返回唯一活动测试页，保证核验没有隐式切换页面。
func (b *bossReplyTestBrowser) ListPages(context.Context) (contract.PageListResult, error) {
	return contract.PageListResult{Pages: []contract.PageInfo{{URL: "https://example.test/chat", Current: true}}}, nil
}

// Click 只记录测试中实际请求的点击数量。
func (b *bossReplyTestBrowser) Click(context.Context, contract.ElementClickRequest) (contract.ClickResult, error) {
	b.clicks++
	return contract.ClickResult{Clicked: true}, nil
}

// bossReplySelector 创建无平台CSS的合成逻辑选择器。
func bossReplySelector(description string) contract.SelectorSpec {
	return contract.SelectorSpec{Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: "test"}}}, Description: description}
}

// bossMessagesFixture 构造仅有已知逻辑字段的聊天测试环境。
func bossMessagesFixture(rows []contract.FindAllItem) (*bossReplyTestBrowser, model.Config) {
	cfg := model.Config{MessagesURL: "https://example.test/chat", Selectors: map[string]contract.SelectorSpec{"message.item": bossReplySelector("messages"), "message.history_scroll": bossReplySelector("history")}, ConversationFields: map[string]contract.SelectorSpec{}}
	for _, key := range []string{"message_time", "message_text", "body_class", "resume_card", "common_card"} {
		cfg.ConversationFields[key] = bossReplySelector(key)
	}
	return &bossReplyTestBrowser{messages: rows}, cfg
}

// bossTestMessage 创建带可见时间或继承上组时间的合成气泡。
func bossTestMessage(clock, direction, text string) contract.FindAllItem {
	return contract.FindAllItem{Text: clock + "|" + direction + "|" + text, Fields: map[string]string{"message_time": clock, "message_text": text, "body_class": direction}}
}

// TestBossVisibleGroupsKeepRepeatedMessages 验证同文同分钟每条都有独立内部键，向前补历史不改变已完整组的编号。
func TestBossVisibleGroupsKeepRepeatedMessages(t *testing.T) {
	now := time.Date(2026, 11, 10, 3, 0, 0, 0, time.FixedZone("CST", 8*3600))
	rows := []contract.FindAllItem{bossTestMessage("昨天 22:26", "item-system", "沟通职位"), bossTestMessage("00:57", "item-friend", "你好"), bossTestMessage("", "item-friend", "你好"), bossTestMessage("", "item-myself", "您好")}
	before, err := bossMessages(rows, now)
	if err != nil || before[1].Key == before[2].Key || before[1].PlatformMessageID != "" || before[2].SentAt == nil || !before[2].SentAt.Equal(*before[1].SentAt) {
		t.Fatalf("同组重复消息被合并或虚构原生编号：%+v %v", before, err)
	}
	after, err := bossMessages(append([]contract.FindAllItem{bossTestMessage("昨天 20:00", "item-myself", "更早消息")}, rows...), now)
	if err != nil || before[1].Key != after[2].Key || before[2].Key != after[3].Key {
		t.Fatal("补读更早的时间组改变了新组重复编号", err)
	}
}

// TestBossHistoryStopsAtCompleteKnownGroup 验证已知锚点所在时间组完整时不滚动、不重复读取整段正文。
func TestBossHistoryStopsAtCompleteKnownGroup(t *testing.T) {
	rows := []contract.FindAllItem{bossTestMessage("昨天 22:26", "item-system", "沟通职位"), bossTestMessage("00:57", "item-friend", "旧问题"), bossTestMessage("", "item-friend", "新问题")}
	b, cfg := bossMessagesFixture(rows)
	messages, _ := bossMessages(rows, time.Now())
	history, err := readBossHistoryWindow(context.Background(), b, cfg, []string{messages[1].Key}, 5000, nil)
	if err != nil || b.scrolls != 0 || len(b.requests) != 1 || len(history.Messages) != 2 || history.Messages[1].Key != messages[2].Key {
		t.Fatalf("已知完整组仍多翻或漏消息：scroll=%d reads=%d history=%+v err=%v", b.scrolls, len(b.requests), history, err)
	}
}

// TestBossTopProofDoesNotRereadBodies 验证顶部稳定检查只读首尾，延后追加历史时会重新核对新增正文。
func TestBossTopProofDoesNotRereadBodies(t *testing.T) {
	rows := []contract.FindAllItem{bossTestMessage("01:00", "item-friend", "你好"), bossTestMessage("", "item-myself", "您好")}
	b, cfg := bossMessagesFixture(rows)
	b.onScroll = func(browser *bossReplyTestBrowser) {
		if browser.scrolls == 2 {
			browser.messages = append([]contract.FindAllItem{bossTestMessage("昨天 20:00", "item-system", "更早记录")}, browser.messages...)
		}
	}
	history, err := readBossHistoryWindow(context.Background(), b, cfg, nil, 5000, nil)
	bodyReads := 0
	for _, request := range b.requests {
		if request.MaxItems == 5000 {
			bodyReads++
		}
	}
	if err != nil || !history.HistoryComplete || len(history.Messages) != 3 || bodyReads != 2 || b.scrolls != 4 {
		t.Fatalf("顶部延后追加没有完整核实或重复读正文：history=%+v reads=%d scroll=%d err=%v", history, bodyReads, b.scrolls, err)
	}
}

// TestBossLatestExpandsOnlyItsTimeGroup 验证长聊天取末条时逐步扩窗，同分钟重复消息键与整段解析一致。
func TestBossLatestExpandsOnlyItsTimeGroup(t *testing.T) {
	rows := []contract.FindAllItem{bossTestMessage("昨天 20:00", "item-system", "早期记录")}
	for i := 0; i < 20; i++ {
		rows = append(rows, bossTestMessage("", "item-friend", fmt.Sprintf("旧消息%d", i)))
	}
	rows = append(rows, bossTestMessage("01:00", "item-myself", "你好"), bossTestMessage("", "item-myself", "你好"))
	b, cfg := bossMessagesFixture(rows)
	latest, err := readBossLatestAutoReplyMessage(context.Background(), b, cfg)
	all, _ := bossMessages(rows, time.Now())
	if err != nil || latest.Key != all[len(all)-1].Key || b.scrolls != 0 {
		t.Fatalf("末条时间组编号与完整历史不符：%+v %v", latest, err)
	}
	for _, request := range b.requests {
		if request.MaxItems == 5000 {
			t.Fatal("小时间组的末条读取不应读取5000条窗口")
		}
	}
}

// TestBossDeletedContentCannotBecomeNewResume 验证删除同分钟及更早卡片不参与新资料，系统索要回执保持system方向。
func TestBossDeletedContentCannotBecomeNewResume(t *testing.T) {
	now := time.Date(2026, 11, 10, 3, 0, 0, 0, time.FixedZone("CST", 8*3600))
	old := bossTestMessage("01:00", "item-friend", "旧简历")
	old.Fields["resume_card"] = "旧简历"
	request := bossTestMessage("01:02", "item-system", "简历请求已发送")
	newer := bossTestMessage("01:03", "item-friend", "新问题")
	messages, err := bossMessages([]contract.FindAllItem{old, request, newer}, now)
	if err != nil {
		t.Fatal(err)
	}
	after := *messages[0].SentAt
	history, stop, err := bossHistoryBoundary(messages, nil, 5000, &after, 1, false)
	var card bossInfoCard
	_ = json.Unmarshal(messages[1].CardContent, &card)
	if err != nil || !stop || !history.HistoryComplete || history.ResumeCardAvailable || len(history.Messages) != 2 || card.InfoType != "resume" || card.InfoStatus != "requested" || messages[1].Direction != "system" {
		t.Fatalf("旧简历复活或系统回执被当候选人来信：%+v %+v %v", history, card, err)
	}
}
