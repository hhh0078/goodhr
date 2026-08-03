// Package auto_reply 验证多岗位归属、页面选择、消息同步和安全发送核心规则。
package auto_reply

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// autoReplyBrowserStub 提供自动回复流程测试所需的浏览器最小实现。
type autoReplyBrowserStub struct {
	pages      []contract.PageInfo
	usedPageID string
}

// StartBrowser 模拟复用已经打开的浏览器。
func (b *autoReplyBrowserStub) StartBrowser(context.Context, contract.BrowserStartRequest) (contract.BrowserStatus, error) {
	return contract.BrowserStatus{Running: true, Reused: true}, nil
}

// OpenPage 模拟打开页面。
func (b *autoReplyBrowserStub) OpenPage(context.Context, contract.PageOpenRequest) (contract.PageInfo, error) {
	return contract.PageInfo{}, nil
}

// ListPages 返回测试配置的标签页。
func (b *autoReplyBrowserStub) ListPages(context.Context) (contract.PageListResult, error) {
	return contract.PageListResult{Pages: b.pages, Count: len(b.pages)}, nil
}

// UsePage 记录流程选择的标签页。
func (b *autoReplyBrowserStub) UsePage(_ context.Context, request contract.PageUseRequest) (contract.PageInfo, error) {
	b.usedPageID = request.PageID
	return contract.PageInfo{PageID: request.PageID, Current: true}, nil
}

// FindAll 返回空列表。
func (b *autoReplyBrowserStub) FindAll(context.Context, contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	return nil, nil
}

// Read 返回空读取结果。
func (b *autoReplyBrowserStub) Read(context.Context, contract.ElementReadRequest) (contract.ReadResult, error) {
	return contract.ReadResult{}, nil
}

// Click 返回成功点击结果。
func (b *autoReplyBrowserStub) Click(context.Context, contract.ElementClickRequest) (contract.ClickResult, error) {
	return contract.ClickResult{}, nil
}

// Input 返回成功输入结果。
func (b *autoReplyBrowserStub) Input(context.Context, contract.ElementInputRequest) (contract.InputResult, error) {
	return contract.InputResult{}, nil
}

// Scroll 返回成功滚动结果。
func (b *autoReplyBrowserStub) Scroll(context.Context, contract.ScrollRequest) (contract.ScrollResult, error) {
	return contract.ScrollResult{}, nil
}

// PressKey 返回成功按键结果。
func (b *autoReplyBrowserStub) PressKey(context.Context, contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	return contract.KeyboardPressResult{}, nil
}

// ClosePage 模拟关闭页面。
func (b *autoReplyBrowserStub) ClosePage(context.Context) error {
	return nil
}

// autoReplyRuntimeStub 模拟平台自动回复能力和发送前后的最新消息。
type autoReplyRuntimeStub struct {
	latestBefore model.ConversationMessage
	latestAfter  model.ConversationMessage
	sent         bool
	sentText     string
}

// InitializeAutoReplyPage 模拟页面初始化。
func (r *autoReplyRuntimeStub) InitializeAutoReplyPage(context.Context, model.Browser, model.Config) error {
	return nil
}

// OpenAutoReplyConversation 返回空会话快照。
func (r *autoReplyRuntimeStub) OpenAutoReplyConversation(context.Context, model.Browser, model.Config, model.Conversation, string, int) (model.AutoReplyConversationSnapshot, error) {
	return model.AutoReplyConversationSnapshot{}, nil
}

// RequestAutoReplyResume 模拟索要简历。
func (r *autoReplyRuntimeStub) RequestAutoReplyResume(context.Context, model.Browser, model.Config, model.AutoReplyConversationSnapshot) error {
	return nil
}

// CollectAutoReplyResume 返回空简历结果。
func (r *autoReplyRuntimeStub) CollectAutoReplyResume(context.Context, model.Browser, model.Config, model.AutoReplyConversationSnapshot) (model.AutoReplyResumeBundle, error) {
	return model.AutoReplyResumeBundle{}, nil
}

// SendAutoReplyMessage 记录实际发送的消息。
func (r *autoReplyRuntimeStub) SendAutoReplyMessage(_ context.Context, _ model.Browser, _ model.Config, _ model.AutoReplyConversationSnapshot, message string) error {
	r.sent = true
	r.sentText = message
	return nil
}

// ReadLatestAutoReplyMessage 按发送前后返回不同的页面最新消息。
func (r *autoReplyRuntimeStub) ReadLatestAutoReplyMessage(context.Context, model.Browser, model.Config, model.AutoReplyConversationSnapshot) (model.ConversationMessage, error) {
	if r.sent {
		return r.latestAfter, nil
	}
	return r.latestBefore, nil
}

// TestResolvePositionRequiresUniqueMatch 验证完整岗位、截断岗位和同前缀冲突的安全规则。
func TestResolvePositionRequiresUniqueMatch(t *testing.T) {
	items := []cloud.AutoReplyPositionSnapshot{
		{Position: cloud.AutoReplyPosition{ID: "p1", Name: "AI应用开发工程师初级"}},
		{Position: cloud.AutoReplyPosition{ID: "p2", Name: "高中数学老师"}},
	}
	resolved, err := resolvePosition(items, "高中数学老师")
	if err != nil || resolved.Position.ID != "p2" {
		t.Fatalf("完整岗位没有唯一匹配：result=%+v err=%v", resolved, err)
	}
	resolved, err = resolvePosition(items, "AI应用开发工程师初...")
	if err != nil || resolved.Position.ID != "p1" {
		t.Fatalf("截断岗位没有唯一匹配：result=%+v err=%v", resolved, err)
	}
	items = append(items, cloud.AutoReplyPositionSnapshot{Position: cloud.AutoReplyPosition{ID: "p3", Name: "AI应用开发工程师初中级"}})
	if _, err = resolvePosition(items, "AI应用开发工程师初..."); err == nil || !strings.Contains(err.Error(), "匹配到2个岗位") {
		t.Fatalf("同前缀岗位应该转人工：%v", err)
	}
}

// TestSelectCurrentPlatformPagePrefersCurrentAndRejectsAmbiguous 验证当前页优先以及多个后台标签页不猜测。
func TestSelectCurrentPlatformPagePrefersCurrentAndRejectsAmbiguous(t *testing.T) {
	config := model.Config{Name: "猎聘企业端", EntryURL: "https://lpt.liepin.com/recommend", LoginURL: "https://lpt.liepin.com/login"}
	browser := &autoReplyBrowserStub{pages: []contract.PageInfo{
		{PageID: "other", URL: "https://example.com"},
		{PageID: "liepin-current", URL: "https://lpt.liepin.com/recommend", Current: true},
		{PageID: "liepin-other", URL: "https://lpt.liepin.com/chat"},
	}}
	flow := &Flow{Browser: browser}
	if err := flow.selectCurrentPlatformPage(context.Background(), config); err != nil {
		t.Fatalf("当前猎聘页应该直接使用：%v", err)
	}
	if browser.usedPageID != "" {
		t.Fatalf("当前页已匹配时不应切换标签：%s", browser.usedPageID)
	}

	browser.pages[1].Current = false
	if err := flow.selectCurrentPlatformPage(context.Background(), config); err == nil || !strings.Contains(err.Error(), "同时打开了2个") {
		t.Fatalf("多个非当前猎聘页应该报错：%v", err)
	}
}

// TestConvertMessagesKeepsLatestCandidateKey 验证消息方向校验和候选人最新消息键。
func TestConvertMessagesKeepsLatestCandidateKey(t *testing.T) {
	messages, key, err := convertMessages([]model.ConversationMessage{
		{PlatformMessageID: "in-1", Direction: "candidate", MessageType: "text", TextContent: "你好"},
		{PlatformMessageID: "out-1", Direction: "self", MessageType: "text", TextContent: "你好"},
		{Key: "in-2-fingerprint", Direction: "candidate", MessageType: "text", TextContent: "薪资是多少"},
	})
	if err != nil || len(messages) != 3 || key != "in-2-fingerprint" {
		t.Fatalf("消息转换不正确：messages=%+v key=%s err=%v", messages, key, err)
	}
	if _, _, err = convertMessages([]model.ConversationMessage{{Direction: "unknown"}}); err == nil {
		t.Fatal("无法确认消息方向时不应继续")
	}
}

// TestSendVerifiedMessageChecksLatestAndAvoidsDuplicate 验证发送前消息复核、发送后回读和重复保护。
func TestSendVerifiedMessageChecksLatestAndAvoidsDuplicate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auto-reply/agent/messages/sync" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var payload cloud.AutoReplyMessageSyncRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Messages) != 1 || payload.Messages[0].Direction != "self" || payload.Messages[0].TextContent != "薪资面议" {
			t.Fatalf("sent message sync=%+v", payload.Messages)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"sync":{"inserted":1,"last_synced_message_key":"out-1"}}`))
	}))
	defer server.Close()
	store, err := storage.Open(filepath.Join(t.TempDir(), "auto-reply.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	flow := &Flow{Browser: &autoReplyBrowserStub{}, Cloud: cloud.New(server.URL), Store: store}
	prepared := shared.PreparedTask{
		Request: shared.StartRequest{TaskID: "task-1", Token: "token"}, MachineID: "goodhr-device-v1-test",
		Platform: model.Config{ID: "liepin", Name: "猎聘企业端"},
	}
	runtime := &autoReplyRuntimeStub{
		latestBefore: model.ConversationMessage{PlatformMessageID: "in-1", Direction: "candidate", TextContent: "薪资是多少"},
		latestAfter:  model.ConversationMessage{PlatformMessageID: "out-1", Direction: "self", TextContent: "薪资面议"},
	}
	conversation := cloud.AutoReplyConversation{ID: "conversation-1", PlatformThreadID: "thread-1"}
	snapshot := model.AutoReplyConversationSnapshot{CandidateName: "李女士"}
	sent, err := flow.sendVerifiedMessage(context.Background(), prepared, runtime, conversation, snapshot, "in-1", "薪资面议", true)
	if err != nil || !sent || runtime.sentText != "薪资面议" {
		t.Fatalf("安全发送失败：sent=%t text=%q err=%v", sent, runtime.sentText, err)
	}

	runtime.sent = false
	sent, err = flow.sendVerifiedMessage(context.Background(), prepared, runtime, conversation, snapshot, "in-1", "薪资面议", true)
	if err != nil || sent || runtime.sent {
		t.Fatalf("重复回复应该在点击前跳过：sent=%t runtime.sent=%t err=%v", sent, runtime.sent, err)
	}

	runtime.latestBefore = model.ConversationMessage{PlatformMessageID: "in-2", Direction: "candidate", TextContent: "又发了一条"}
	sent, err = flow.sendVerifiedMessage(context.Background(), prepared, runtime, conversation, snapshot, "in-1", "另一条回复", true)
	if err != nil || sent || runtime.sent {
		t.Fatalf("候选人新消息出现后旧回复应该废弃：sent=%t runtime.sent=%t err=%v", sent, runtime.sent, err)
	}
}

// TestCheckpointSettingsCapsThree 验证单轮配置永远不会超过三个候选人。
func TestCheckpointSettingsCapsThree(t *testing.T) {
	items := []cloud.AutoReplyPositionSnapshot{{
		Position: cloud.AutoReplyPosition{ID: "p1"},
		Config:   cloud.PositionAutoReplyConfig{MaxThreadsPerCheckpoint: 99, PollIntervalSeconds: 5},
	}}
	limit, wait := checkpointSettings(items, "p1")
	if limit != 3 || wait != 5 {
		t.Fatalf("checkpoint settings limit=%d wait=%d", limit, wait)
	}
}

// TestMessageFingerprintIsStable 验证没有平台消息编号时同一消息会生成相同指纹。
func TestMessageFingerprintIsStable(t *testing.T) {
	sentAt := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	message := model.ConversationMessage{Direction: "candidate", MessageType: "text", TextContent: "你好", SentAt: &sentAt}
	if messageFingerprint(message, 0) != messageFingerprint(message, 0) {
		t.Fatal("同一条消息的指纹必须稳定")
	}
}
