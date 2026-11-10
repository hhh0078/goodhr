// Package boss 本文件验证自动回复的收起范围、三类索要判定及准确下载回执，不操作真实招聘账号。
package boss

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// bossActionTestBrowser 模拟单个原页和目标会话，只记录真实按键或按钮动作。
type bossActionTestBrowser struct {
	model.Browser
	page                  string
	drawer, chat, preview bool
	collapseNoop          bool
	candidate             string
	clicks, keys          []string
	rows                  []contract.FindAllItem
	draft                 string
	keyErr                error
}

// ListPages 只返回唯一当前原页，便于覆盖切页拒绝。
func (b *bossActionTestBrowser) ListPages(context.Context) (contract.PageListResult, error) {
	return contract.PageListResult{Pages: []contract.PageInfo{{PageID: "original", URL: b.page, Current: true}}}, nil
}

// FindAll 根据面板状态返回可见性，不引入固定等待。
func (b *bossActionTestBrowser) FindAll(_ context.Context, req contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if req.Selector.Description == "messages" {
		rows := append([]contract.FindAllItem(nil), b.rows...)
		for index := range rows {
			rows[index].Index, rows[index].TotalMatches = index, len(rows)
		}
		count := min(len(rows), req.MaxItems)
		if req.FromEnd {
			return rows[len(rows)-count:], nil
		}
		return rows[:count], nil
	}
	visible := false
	switch req.Selector.Description {
	case "drawer":
		visible = b.drawer
	case "chat":
		visible = b.chat
	case "preview":
		visible = b.preview
	}
	if visible {
		return []contract.FindAllItem{{Index: 0, TotalMatches: 1}}, nil
	}
	return nil, nil
}

// Read 提供当前目标的公开头部字段，聊天关闭后不伪造候选人仍在。
func (b *bossActionTestBrowser) Read(_ context.Context, req contract.ElementReadRequest) (contract.ReadResult, error) {
	if !b.chat {
		return contract.ReadResult{}, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
	}
	if req.Selector.Description == "input" {
		return contract.ReadResult{Value: b.draft, Property: "value"}, nil
	}
	values := map[string]string{"name": "目标候选人", "candidate": "get_resume_" + b.candidate, "phone-candidate": "get_phone_" + b.candidate, "wechat-candidate": "get_wechat_" + b.candidate, "thread": "101-0", "position": "测试岗位"}
	return contract.ReadResult{Value: values[req.Selector.Description]}, nil
}

// Click 只允许一键收起，模拟它同时关闭列表和聊天。
func (b *bossActionTestBrowser) Click(_ context.Context, req contract.ElementClickRequest) (contract.ClickResult, error) {
	b.clicks = append(b.clicks, req.Selector.Description)
	if req.Selector.Description == "collapse" && !b.collapseNoop {
		b.drawer, b.chat = false, false
	}
	if req.Selector.Description == "send" {
		b.draft = ""
	}
	return contract.ClickResult{Clicked: true}, nil
}

// Input 只录入草稿，不在换行或输入过程中自动发送。
func (b *bossActionTestBrowser) Input(_ context.Context, req contract.ElementInputRequest) (contract.InputResult, error) {
	b.draft = req.Text
	return contract.InputResult{Typed: true, Verified: true}, nil
}

// PressKey 模拟Esc关闭指定最上层目标，不影响其他面板。
func (b *bossActionTestBrowser) PressKey(_ context.Context, req contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	if req.Key == "Enter" {
		if !req.RequireFocus || req.Target == nil || req.Target.Description != "input" || req.ExpectedPageURL != b.page || req.ExpectedValue == nil || *req.ExpectedValue != b.draft {
			return contract.KeyboardPressResult{}, errors.New("发送按键没有核对原页焦点和草稿")
		}
		b.keys = append(b.keys, "Enter")
		if b.keyErr != nil {
			return contract.KeyboardPressResult{}, b.keyErr
		}
		b.draft = ""
		return contract.KeyboardPressResult{Pressed: true}, nil
	}
	if req.Key != "Escape" || req.Target == nil || req.ExpectedPageURL != b.page {
		return contract.KeyboardPressResult{}, errors.New("按键范围不完整")
	}
	b.keys = append(b.keys, req.Target.Description)
	switch req.Target.Description {
	case "preview":
		b.preview = false
	case "chat":
		b.chat = false
	case "drawer":
		b.drawer = false
	}
	return contract.KeyboardPressResult{Pressed: true}, nil
}

// TestBossSendEnterAndSafeFallback 验证BOSS实际入口走回车，未知结果不补发，明确未按键才点备用发送。
func TestBossSendEnterAndSafeFallback(t *testing.T) {
	for _, mode := range []string{"enter", "unknown", "not_started"} {
		t.Run(mode, func(t *testing.T) {
			b, cfg, snapshot := bossActionFixture()
			for key, description := range map[string]string{"message.item": "messages", "message.input": "input", "message.send": "send"} {
				cfg.Selectors[key] = contract.SelectorSpec{Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: "test"}}}, Description: description}
			}
			b.rows = []contract.FindAllItem{
				{Text: "之前的消息", Fields: map[string]string{"message_time": "昨天 10:00", "message_text": "之前的消息", "body_class": "item-myself"}},
				{Text: "请问工作时间？", Fields: map[string]string{"message_time": "10:00", "message_text": "请问工作时间？", "body_class": "item-friend"}},
			}
			messages, err := bossMessages(b.rows, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			snapshot.ExpectedSendMessageKey = messages[1].Key
			if mode == "unknown" {
				b.keyErr = &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ACTION_TIMEOUT"}}
			}
			if mode == "not_started" {
				b.keyErr = &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND", Details: []byte(`{"key_started":false}`)}}
			}
			err = sendBossAutoReplyMessage(context.Background(), b, cfg, snapshot, "上午九点到下午六点。")
			wantClicks := 0
			if mode == "not_started" {
				wantClicks = 1
			}
			if (err != nil) != (mode == "unknown") || !reflect.DeepEqual(b.keys, []string{"Enter"}) || len(b.clicks) != wantClicks {
				t.Fatalf("发送路径错误：mode=%s keys=%v clicks=%v err=%v", mode, b.keys, b.clicks, err)
			}
		})
	}
}

// bossActionFixture 使用逻辑键构造面板验证环境，不依赖真实平台CSS。
func bossActionFixture() (*bossActionTestBrowser, model.Config, model.AutoReplyConversationSnapshot) {
	cfg := model.Config{MessagesURL: "https://example.test/recommend", Selectors: map[string]contract.SelectorSpec{}}
	for key, description := range map[string]string{
		"message.drawer": "drawer", "message.drawer_collapse": "collapse", "candidate.chat_modal": "chat", "candidate.chat_close": "close-chat",
		"message.attachment_preview": "preview", "message.attachment_preview_close": "close-preview", "message.current_name": "name",
		"message.current_candidate_id": "candidate", "message.current_thread_id": "thread", "message.current_position": "position",
		"message.current_phone_candidate_id": "phone-candidate", "message.current_wechat_candidate_id": "wechat-candidate",
	} {
		cfg.Selectors[key] = contract.SelectorSpec{Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: "test"}}}, Description: description}
	}
	b := &bossActionTestBrowser{page: cfg.MessagesURL, drawer: true, chat: true, candidate: "101"}
	snapshot := model.AutoReplyConversationSnapshot{CandidateName: "目标候选人", PlatformCandidateID: "101", PlatformThreadID: "101-0", CommunicationPosition: "测试岗位"}
	return b, cfg, snapshot
}

// TestBossPanelsKeepNormalChatAndCollapseTogether 验证初始化不关聊天，退出只点一次收起；附件先Esc。
func TestBossPanelsKeepNormalChatAndCollapseTogether(t *testing.T) {
	for _, preview := range []bool{false, true} {
		b, cfg, snapshot := bossActionFixture()
		b.preview = preview
		if err := initializeBossAutoReplyPage(context.Background(), b, cfg, &snapshot); err != nil {
			t.Fatal(err)
		}
		if !b.drawer || !b.chat || len(b.clicks) != 0 || b.preview {
			t.Fatalf("初始化改变了正常会话：%+v", b)
		}
		if err := closeBossAutoReplyConversation(context.Background(), b, cfg, snapshot); err != nil {
			t.Fatal(err)
		}
		if b.drawer || b.chat || !reflect.DeepEqual(b.clicks, []string{"collapse"}) || len(b.keys) != map[bool]int{false: 0, true: 1}[preview] {
			t.Fatalf("面板没有一次收好：clicks=%v keys=%v", b.clicks, b.keys)
		}
	}
}

// TestBossPanelFallbackAndWrongIdentity 验证一键收起未生效才用Esc备用，错人时不触碰任何面板。
func TestBossPanelFallbackAndWrongIdentity(t *testing.T) {
	b, cfg, snapshot := bossActionFixture()
	b.collapseNoop = true
	if err := closeBossAutoReplyConversation(context.Background(), b, cfg, snapshot); err != nil || b.chat || b.drawer || !reflect.DeepEqual(b.keys, []string{"chat", "drawer"}) {
		t.Fatalf("备用收尾错误：%v %v", b.keys, err)
	}
	b, cfg, snapshot = bossActionFixture()
	b.candidate = "102"
	if err := closeBossAutoReplyConversation(context.Background(), b, cfg, snapshot); err == nil || len(b.clicks)+len(b.keys) != 0 {
		t.Fatalf("错人仍然收起：%v", err)
	}
	b.page = "https://example.test/other"
	if _, _, err := bossConversationPanels(context.Background(), b, cfg, cfg.MessagesURL); err == nil {
		t.Fatal("切页被当作已关闭")
	}
}

// TestBossInfoMutualAndReceiptScope 验证系统卡片不能补足双方资格，旧回执和其它类资料不能串用。
func TestBossInfoMutualAndReceiptScope(t *testing.T) {
	candidate := model.ConversationMessage{Direction: "candidate", MessageType: "text", TextContent: "你好"}
	self := model.ConversationMessage{Direction: "self", MessageType: "text", TextContent: "你好"}
	if bossMutualCommunication([]model.ConversationMessage{candidate}) || bossMutualCommunication([]model.ConversationMessage{candidate, {Direction: "self", MessageType: "card", TextContent: "系统索要卡片"}}) || !bossMutualCommunication([]model.ConversationMessage{candidate, self}) {
		t.Fatal("双方沟通判断错误")
	}
	rows := []model.ConversationMessage{{Key: "old", CardContent: []byte(`{"info_type":"phone","info_status":"requested"}`)}, {Key: "other", CardContent: []byte(`{"info_type":"wechat","info_status":"received"}`)}}
	if result, found := bossInfoMessageResult(rows, "phone", nil); !found || result.Status != model.AutoReplyInfoRequested || result.EvidenceMessageID != "old" {
		t.Fatalf("请求卡片识别错误：%+v", result)
	}
	if _, found := bossInfoMessageResult(rows, "phone", map[string]bool{"old": true}); found {
		t.Fatal("旧卡片被重复当作新回执")
	}
	if model.CandidateInfoRequestKind(model.CandidateInfoRequest{RequestPhone: true, RequestResume: true}) != "" {
		t.Fatal("多类索要未被拒绝")
	}
	if result, err := requestBossAutoReplyCandidateInfo(context.Background(), nil, model.Config{}, model.AutoReplyConversationSnapshot{}, model.CandidateInfoRequest{RequestResume: true}); err != nil || result.Status != model.AutoReplyInfoDeferred {
		t.Fatalf("无沟通锚点仍开始操作：%+v %v", result, err)
	}
	boundary := time.Now()
	old, fresh := boundary.Add(-time.Minute), boundary.Add(time.Minute)
	rows[0].SentAt, rows[1].SentAt = &old, &fresh
	if got := bossInfoMessagesAfter(rows, &boundary); len(got) != 1 || got[0].Key != "other" {
		t.Fatal("删除前交换记录进入新资料状态")
	}
}

// TestBossNativeInfoDefersOneSidedChat 验证只有单方正文时真实原生入口明确暂缓，不尝试点任何资料按钮。
func TestBossNativeInfoDefersOneSidedChat(t *testing.T) {
	for _, side := range []string{"item-friend", "item-myself"} {
		b, cfg, snapshot := bossActionFixture()
		cfg.Selectors["message.item"] = contract.SelectorSpec{Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: "test"}}}, Description: "messages"}
		b.rows = []contract.FindAllItem{
			{Text: "系统提示", Fields: map[string]string{"message_time": "昨天 10:00", "message_text": "系统提示", "body_class": "item-system"}},
			{Text: "你好", Fields: map[string]string{"message_time": "10:00", "message_text": "你好", "body_class": side}},
		}
		messages, err := bossMessages(b.rows, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		snapshot.ExpectedSendMessageKey = messages[1].Key
		result, err := requestBossAutoReplyCandidateInfo(context.Background(), b, cfg, snapshot, model.CandidateInfoRequest{RequestResume: true})
		if err != nil || result.Status != model.AutoReplyInfoDeferred || len(b.clicks)+len(b.keys) != 0 {
			t.Fatalf("单方沟通仍索要了资料：%+v %v", result, err)
		}
	}
}

// TestBossAttachmentSourceAndReceipt 验证地址来源、已保存文件和取消；正常回执无需固定等待。
func TestBossAttachmentSourceAndReceipt(t *testing.T) {
	page := "https://example.test/recommend"
	for _, raw := range []string{"", "#", "javascript:void(0)", "https://user:pass@example.test/file", "https://example.test/file#fragment", "https://example.test/viewer?file=a&file=b"} {
		if _, err := bossAttachmentDownloadSource(model.Config{}, page, raw); err == nil {
			t.Fatalf("不安全来源被接受：%q", raw)
		}
	}
	if got, err := bossAttachmentDownloadSource(model.Config{}, page, "https://example.test/viewer?file=https%3A%2F%2Fcdn.example.test%2Fresume.pdf"); err != nil || got != "https://cdn.example.test/resume.pdf" {
		t.Fatalf("未正确读取明确文件：%s %v", got, err)
	}
	if got, err := bossAttachmentDownloadSource(model.Config{}, page, "/bzl-office/pdf-viewer-b?url=%2Fdownload%2Fpreview%3Fid%3Dsynthetic"); err != nil || got != "https://example.test/download/preview?id=synthetic" {
		t.Fatalf("未正确读取预览中明确的相对文件地址：%s %v", got, err)
	}
	file := filepath.Join(t.TempDir(), "resume.pdf")
	if err := os.WriteFile(file, []byte("synthetic-file"), 0o600); err != nil {
		t.Fatal(err)
	}
	ch := make(chan contract.DownloadReceipt, 2)
	ch <- contract.DownloadReceipt{Record: contract.DownloadRecord{ID: "download-1", Status: "pending"}}
	ch <- contract.DownloadReceipt{Record: contract.DownloadRecord{ID: "download-1", Status: "saved", FilePath: file}}
	start := time.Now()
	if result, err := awaitBossAttachment(context.Background(), ch); err != nil || result.FilePath != file || time.Since(start) > time.Second {
		t.Fatalf("正常下载回执多等了：%+v %v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := awaitBossAttachment(ctx, make(chan contract.DownloadReceipt)); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消没有及时停止：%v", err)
	}
}

// TestBossAttachmentDerivedRule 只允许平台配置中实测的路径和参数关系，未知形态不猜测下载接口。
func TestBossAttachmentDerivedRule(t *testing.T) {
	cfg := model.Config{Behavior: model.Behavior{AttachmentPreviewPath: "/verified-preview/", AttachmentAllowedScheme: "https", AttachmentAllowedHost: "download.example.test", AttachmentAllowedPath: "/verified-download/"}}
	page := "https://example.test/recommend"
	preview := "https://example.test/verified-preview/document-A?d=1000&id=file-A&authType=0&previewType=1&geekId=document-A"
	wrap := func(value string) string { return "https://example.test/viewer?url=" + url.QueryEscape(value) }
	want := "https://download.example.test/verified-download/document-A?id=file-A&authType=0"
	if got, err := bossAttachmentDownloadSource(cfg, page, wrap(preview)); err != nil || got != want {
		t.Fatalf("已验证规则没有按原值派生：%s %v", got, err)
	}
	for _, raw := range []string{
		"https://other.example.test/verified-preview/document-A?d=1000&id=file-A&authType=0&previewType=1&geekId=document-A",
		"https://example.test/unknown/document-A?d=1000&id=file-A&authType=0&previewType=1&geekId=document-A",
		preview + "&id=other", preview + "&unknown=1",
		"https://example.test/verified-preview/document-A?d=1000&id=file-A&authType=0&previewType=1&geekId=different",
	} {
		if _, err := bossAttachmentDownloadSource(cfg, page, wrap(raw)); err == nil {
			t.Fatalf("未知附件结构被接受：%q", raw)
		}
	}
}
