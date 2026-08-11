// Package zhaopin 文件作用：验证智联自动回复配置、真实未读过滤和消息方向解析。
package zhaopin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

const zhaopinAttachmentTestReturnURL = "https://rd6.zhaopin.com/app/recommend#sortType=recommend"
const zhaopinAttachmentTestDocumentURL = "https://attachment.zhaopin.com/resumeapi/parsev2/downloadFileTemporary?token=test"

type zhaopinAttachmentBrowserStub struct {
	model.Browser
	pages       contract.PageListResult
	clickResult contract.ClickResult
	clickErr    error
	saveRecord  contract.DownloadRecord
	saveErr     error
	clicks      []contract.ElementClickRequest
	saves       []contract.SaveCurrentDocumentRequest
	usedPages   []string
	closeCount  int
}

// ListPages 返回测试维护的智联标签页快照。
func (s *zhaopinAttachmentBrowserStub) ListPages(context.Context) (contract.PageListResult, error) {
	return s.pages, nil
}

// Click 模拟附件入口打开新的 PDF 标签页。
func (s *zhaopinAttachmentBrowserStub) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	s.clicks = append(s.clicks, request)
	if s.clickErr != nil {
		return contract.ClickResult{}, s.clickErr
	}
	if s.clickResult.NewPageOpened {
		for index := range s.pages.Pages {
			s.pages.Pages[index].Current = false
		}
		s.pages.Pages = append(s.pages.Pages, contract.PageInfo{
			PageID: "1", URL: s.clickResult.NewPageURL, Current: true,
		})
		s.pages.Count = len(s.pages.Pages)
	}
	return s.clickResult, nil
}

// UsePage 模拟按页面编号切换当前标签页。
func (s *zhaopinAttachmentBrowserStub) UsePage(_ context.Context, request contract.PageUseRequest) (contract.PageInfo, error) {
	s.usedPages = append(s.usedPages, request.PageID)
	for index := range s.pages.Pages {
		s.pages.Pages[index].Current = s.pages.Pages[index].PageID == request.PageID
		if s.pages.Pages[index].Current {
			return s.pages.Pages[index], nil
		}
	}
	return contract.PageInfo{}, errors.New("测试标签页不存在")
}

// ClosePage 模拟关闭当前附件标签页。
func (s *zhaopinAttachmentBrowserStub) ClosePage(context.Context) error {
	s.closeCount++
	remaining := make([]contract.PageInfo, 0, len(s.pages.Pages))
	for _, page := range s.pages.Pages {
		if !page.Current {
			remaining = append(remaining, page)
		}
	}
	s.pages.Pages = remaining
	s.pages.Count = len(remaining)
	return nil
}

// SaveCurrentDocument 模拟 Worker 返回统一下载记录。
func (s *zhaopinAttachmentBrowserStub) SaveCurrentDocument(_ context.Context, request contract.SaveCurrentDocumentRequest) (contract.DownloadRecord, error) {
	s.saves = append(s.saves, request)
	return s.saveRecord, s.saveErr
}

// TestZhaopinAutoReplyConfigIncludesConfirmedMessageSelectors 验证智联官方页面节点已经映射到自动回复必需键。
func TestZhaopinAutoReplyConfigIncludesConfirmedMessageSelectors(t *testing.T) {
	cfg := loadZhaopinAutoReplyTestConfig(t)
	if cfg.MessagesURL != "https://rd6.zhaopin.com/app/recommend" {
		t.Fatalf("智联自动回复必须留在推荐页：%s", cfg.MessagesURL)
	}
	expected := map[string]string{
		"message.drawer_scroll":           ".im-session-list__virtual",
		"message.contact_item":            ".im-session-item",
		"message.contact_click_target":    ".im-session-item__name-title",
		"message.current_name":            ".im-candidate__name",
		"message.current_position":        ".im-candidate__job",
		"message.current_avatar":          ".im-candidate__avatar",
		"message.item":                    ".im-message",
		"message.history_scroll":          ".im-timeline__wrapper",
		"message.input":                   ".im-sender__input textarea",
		"message.send":                    ".im-sender__input-tip--active .km-button",
		"message.resume_attachment_entry": ".im-attachment-card__button",
	}
	for key, value := range expected {
		selector, exists := cfg.Selectors[key]
		if !exists || len(selector.Target.Selectors) == 0 || selector.Target.Selectors[0].Value != value {
			t.Errorf("智联自动回复选择器不正确：key=%s selector=%+v", key, selector)
		}
	}
	attachment := cfg.Selectors["message.resume_attachment_entry"]
	if attachment.Target.Text != "查看附件简历" || attachment.Target.ExactText == nil || !*attachment.Target.ExactText {
		t.Fatalf("智联附件入口必须精确限定查看附件简历：%+v", attachment.Target)
	}
	if cfg.Behavior.AttachmentMode != "new_page_document" {
		t.Fatalf("智联附件必须使用新标签页文档模式：%s", cfg.Behavior.AttachmentMode)
	}
	if cfg.Behavior.AttachmentAllowedScheme != "https" ||
		cfg.Behavior.AttachmentAllowedHost != "attachment.zhaopin.com" ||
		cfg.Behavior.AttachmentAllowedPath != "/resumeapi/parsev2/downloadFileTemporary" {
		t.Fatalf("智联附件地址白名单配置不正确：%+v", cfg.Behavior)
	}
	for _, key := range []string{"name", "last_message", "unread_count", "candidate_marker", "self_marker", "message_text", "message_time", "resume_card"} {
		selector, exists := cfg.ConversationFields[key]
		if !exists || len(selector.Target.Selectors) == 0 {
			t.Errorf("智联聊天字段缺少选择器：%s", key)
		}
	}
}

// TestZhaopinUnreadSelectorRequiresVisibleNumberBadge 验证未读会话选择器必须同时依赖未读容器和数字徽标。
func TestZhaopinUnreadSelectorRequiresVisibleNumberBadge(t *testing.T) {
	cfg := loadZhaopinAutoReplyTestConfig(t)
	selector := cfg.Selectors["message.unread_item"]
	if len(selector.Target.Selectors) != 1 {
		t.Fatalf("智联未读联系人必须只有一个严格入口：%+v", selector.Target.Selectors)
	}
	value := selector.Target.Selectors[0].Value
	if !strings.Contains(value, ".im-session-item__unread") || !strings.Contains(value, ".km-badge__item") {
		t.Fatalf("智联未读联系人没有绑定真实数字徽标：%s", value)
	}
	unreadField := cfg.ConversationFields["unread_count"]
	if len(unreadField.Target.Selectors) != 1 || unreadField.Target.Selectors[0].Value != ".im-session-item__unread .km-badge__item" {
		t.Fatalf("智联联系人未读数字字段不正确：%+v", unreadField.Target.Selectors)
	}
}

// TestZhaopinUnreadConversationsIgnoreEmptyAndZeroBadges 验证空徽标和零未读不会进入自动回复循环。
func TestZhaopinUnreadConversationsIgnoreEmptyAndZeroBadges(t *testing.T) {
	items := []contract.FindAllItem{
		{Index: 0, Fields: map[string]string{"name": "空徽标"}},
		{Index: 1, Fields: map[string]string{"name": "零未读", "unread_count": "0"}},
		{Index: 2, Fields: map[string]string{"name": "有新消息", "last_message": "你好", "unread_count": "3+"}},
	}
	conversations, err := common.ConfiguredConversations(items, zhaopinAutoReplyAdapter.PlatformID, true)
	if err != nil {
		t.Fatalf("整理智联未读联系人失败：%v", err)
	}
	if len(conversations) != 1 || conversations[0].Name != "有新消息" {
		t.Fatalf("没有只保留真实带数字的智联联系人：%+v", conversations)
	}
}

// TestZhaopinMessageMarkersSeparateCandidateAndHR 验证智联非本人气泡和本人气泡会解析为不同消息方向。
func TestZhaopinMessageMarkersSeparateCandidateAndHR(t *testing.T) {
	items := []contract.FindAllItem{
		{Index: 0, Fields: map[string]string{"candidate_marker": "存在", "message_text": "候选人消息"}},
		{Index: 1, Fields: map[string]string{"self_marker": "存在", "message_text": "HR 消息"}},
	}
	messages, err := common.ParseConfiguredAutoReplyMessages(items, zhaopinAutoReplyAdapter.MessageRules, time.Date(2026, 8, 12, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60)))
	if err != nil {
		t.Fatalf("解析智联消息方向失败：%v", err)
	}
	if len(messages) != 2 || messages[0].Direction != "candidate" || messages[1].Direction != "self" {
		t.Fatalf("智联候选人和 HR 消息方向不正确：%+v", messages)
	}
}

// TestZhaopinAttachmentDocumentSavesAndRestoresPage 验证智联 PDF 保存成功后关闭附件并恢复原推荐页。
func TestZhaopinAttachmentDocumentSavesAndRestoresPage(t *testing.T) {
	cfg := loadZhaopinAutoReplyTestConfig(t)
	file, err := os.CreateTemp(t.TempDir(), "zhaopin-resume-*.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteString("%PDF-1.7\nGoodHR"); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	browser := newZhaopinAttachmentBrowserStub()
	browser.saveRecord = contract.DownloadRecord{
		Status: "saved", FilePath: file.Name(), Size: 16,
	}
	paths, err := collectZhaopinAttachmentDocument(
		context.Background(), browser, cfg,
		model.AutoReplyConversationSnapshot{CandidateName: "黄明超"},
	)
	if err != nil {
		t.Fatalf("保存智联附件简历失败：%v", err)
	}
	if len(paths) != 1 || paths[0] != file.Name() {
		t.Fatalf("智联附件路径不正确：%v", paths)
	}
	if len(browser.clicks) != 1 || !browser.clicks[0].WaitForNewPage || browser.clicks[0].NewPageTimeoutMS != 10000 {
		t.Fatalf("智联附件入口没有等待新标签页：%+v", browser.clicks)
	}
	if len(browser.saves) != 1 || browser.saves[0].MaxBytes != zhaopinAutoReplyAttachmentMaxBytes ||
		len(browser.saves[0].AllowedContentTypes) != 1 || browser.saves[0].AllowedContentTypes[0] != "application/pdf" ||
		browser.saves[0].SuggestedFilename != "黄明超-附件简历.pdf" {
		t.Fatalf("智联 PDF 保存参数不正确：%+v", browser.saves)
	}
	if browser.closeCount != 1 || len(browser.usedPages) == 0 || browser.usedPages[len(browser.usedPages)-1] != "0" {
		t.Fatalf("智联附件保存后没有关闭并恢复推荐页：close=%d use=%v", browser.closeCount, browser.usedPages)
	}
}

// TestZhaopinAttachmentDocumentRestoresPageAfterSaveFailure 验证保存失败也会关闭附件并恢复推荐页。
func TestZhaopinAttachmentDocumentRestoresPageAfterSaveFailure(t *testing.T) {
	browser := newZhaopinAttachmentBrowserStub()
	browser.saveErr = errors.New("模拟保存失败")
	_, err := collectZhaopinAttachmentDocument(
		context.Background(), browser, loadZhaopinAutoReplyTestConfig(t),
		model.AutoReplyConversationSnapshot{CandidateName: "黄明超"},
	)
	if err == nil || !strings.Contains(err.Error(), "模拟保存失败") {
		t.Fatalf("智联附件保存失败应保留原错误：%v", err)
	}
	if browser.closeCount != 1 || len(browser.usedPages) == 0 || browser.usedPages[len(browser.usedPages)-1] != "0" {
		t.Fatalf("智联附件失败后没有恢复推荐页：close=%d use=%v", browser.closeCount, browser.usedPages)
	}
}

// TestZhaopinAttachmentRejectsDuplicateReturnURL 验证原完整地址重复时不会猜测使用哪个标签页。
func TestZhaopinAttachmentRejectsDuplicateReturnURL(t *testing.T) {
	browser := newZhaopinAttachmentBrowserStub()
	browser.pages.Pages = append(browser.pages.Pages, contract.PageInfo{
		PageID: "2", URL: zhaopinAttachmentTestReturnURL,
	})
	browser.pages.Count = len(browser.pages.Pages)
	_, err := collectZhaopinAttachmentDocument(
		context.Background(), browser, loadZhaopinAutoReplyTestConfig(t),
		model.AutoReplyConversationSnapshot{CandidateName: "黄明超"},
	)
	if err == nil || !strings.Contains(err.Error(), "地址相同") {
		t.Fatalf("智联原地址重复时应明确报错：%v", err)
	}
	if len(browser.clicks) != 0 {
		t.Fatalf("智联原地址重复时不应该点击附件：%d", len(browser.clicks))
	}
}

// TestValidateZhaopinAttachmentURL 验证只接受智联官方临时附件页。
func TestValidateZhaopinAttachmentURL(t *testing.T) {
	behavior := loadZhaopinAutoReplyTestConfig(t).Behavior
	if err := validateZhaopinAttachmentURL(zhaopinAttachmentTestDocumentURL, behavior); err != nil {
		t.Fatalf("智联官方附件地址应通过：%v", err)
	}
	if err := validateZhaopinAttachmentURL("https://example.com/resume.pdf", behavior); err == nil {
		t.Fatal("非智联附件地址不应通过")
	}
	if err := validateZhaopinAttachmentURL("http://attachment.zhaopin.com/resumeapi/parsev2/downloadFileTemporary", behavior); err == nil {
		t.Fatal("非配置协议的智联附件地址不应通过")
	}
	if err := validateZhaopinAttachmentURL("https://attachment.zhaopin.com/resumeapi/other", behavior); err == nil {
		t.Fatal("非配置路径的智联附件地址不应通过")
	}
	if err := validateZhaopinAttachmentURL(zhaopinAttachmentTestDocumentURL, model.Behavior{}); err == nil {
		t.Fatal("附件地址白名单为空时必须拒绝页面")
	}
}

// newZhaopinAttachmentBrowserStub 创建只含一个当前推荐页的附件测试浏览器。
func newZhaopinAttachmentBrowserStub() *zhaopinAttachmentBrowserStub {
	return &zhaopinAttachmentBrowserStub{
		pages: contract.PageListResult{
			Pages: []contract.PageInfo{{
				PageID: "0", URL: zhaopinAttachmentTestReturnURL, Current: true,
			}},
			Count: 1,
		},
		clickResult: contract.ClickResult{
			Clicked: true, NewPageOpened: true, NewPageURL: zhaopinAttachmentTestDocumentURL,
		},
	}
}

// loadZhaopinAutoReplyTestConfig 读取并解析当前目录中的智联本地配置。
func loadZhaopinAutoReplyTestConfig(t *testing.T) model.Config {
	t.Helper()
	content, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatalf("读取智联配置失败：%v", err)
	}
	var cfg model.Config
	if err = json.Unmarshal(content, &cfg); err != nil {
		t.Fatalf("解析智联配置失败：%v", err)
	}
	return cfg
}
