// Package zhaopin 文件作用：把智联招聘完整自动回复能力接入公共配置驱动流程。
package zhaopin

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

var _ model.AutoReplyRuntime = (*Runtime)(nil)

// zhaopinAutoReplyAttachmentMaxBytes 与云端简历附件 20MB 上限保持一致。
const zhaopinAutoReplyAttachmentMaxBytes int64 = 20 * 1024 * 1024

var zhaopinAutoReplyAdapter = common.ConfiguredAutoReplyAdapter{
	PlatformID: "zhaopin",
	MessageRules: common.AutoReplyMessageRules{
		CandidateTokens: []string{"receive", "other", "left"},
		SelfTokens:      []string{"send", "self", "mine", "right"},
	},
}

type zhaopinCurrentDocumentBrowser interface {
	SaveCurrentDocument(context.Context, contract.SaveCurrentDocumentRequest) (contract.DownloadRecord, error)
}

// InitializeAutoReplyPage 清理智联遗留弹层。
func (r *Runtime) InitializeAutoReplyPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return common.InitializeConfiguredAutoReplyPage(ctx, browser, cfg)
}

// ScanUnreadConversations 返回智联真实未读会话。
func (r *Runtime) ScanUnreadConversations(ctx context.Context, browser model.Browser, cfg model.Config) ([]model.Conversation, error) {
	return common.ScanConfiguredAutoReplyConversations(ctx, browser, cfg, zhaopinAutoReplyAdapter)
}

// OpenAutoReplyConversation 打开智联会话并读取身份、岗位和差量历史。
func (r *Runtime) OpenAutoReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, conversation model.Conversation, knownMessageKeys []string, maxHistory int) (model.AutoReplyConversationSnapshot, error) {
	return common.OpenConfiguredAutoReplyConversation(ctx, browser, cfg, zhaopinAutoReplyAdapter, conversation, knownMessageKeys, maxHistory)
}

// ReadAutoReplyMessages 读取智联当前聊天框的差量消息。
func (r *Runtime) ReadAutoReplyMessages(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, knownMessageKeys []string, maxHistory int) ([]model.ConversationMessage, bool, error) {
	return common.ReadConfiguredAutoReplyMessages(ctx, browser, cfg, zhaopinAutoReplyAdapter, snapshot, knownMessageKeys, maxHistory)
}

// RequestAutoReplyResume 在智联当前聊天框索要简历。
func (r *Runtime) RequestAutoReplyResume(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) error {
	return common.RequestConfiguredAutoReplyResume(ctx, browser, cfg, zhaopinAutoReplyAdapter, snapshot)
}

// CollectAutoReplyResume 下载智联真实 PDF 附件并恢复原候选人聊天框。
func (r *Runtime) CollectAutoReplyResume(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (model.AutoReplyResumeBundle, error) {
	if !snapshot.ResumeCardAvailable {
		return model.AutoReplyResumeBundle{}, fmt.Errorf("%s当前会话没有候选人简历卡片", cfg.Name)
	}
	if err := common.EnsureConfiguredAutoReplyConversation(ctx, browser, cfg, zhaopinAutoReplyAdapter, snapshot); err != nil {
		return model.AutoReplyResumeBundle{}, err
	}
	attachmentPaths, err := collectZhaopinAttachmentDocument(ctx, browser, cfg, snapshot)
	if err != nil {
		return model.AutoReplyResumeBundle{}, err
	}
	if err = common.EnsureConfiguredAutoReplyConversation(ctx, browser, cfg, zhaopinAutoReplyAdapter, snapshot); err != nil {
		return model.AutoReplyResumeBundle{}, fmt.Errorf("恢复%s候选人聊天框失败：%w", cfg.Name, err)
	}
	return model.AutoReplyResumeBundle{
		CandidateName:         snapshot.CandidateName,
		Gender:                snapshot.Gender,
		AttachmentPaths:       attachmentPaths,
		ResumeSourceMessageID: snapshot.ResumeSourceMessageID,
	}, nil
}

// collectZhaopinAttachmentDocument 打开智联 PDF 标签页，保存真实附件后关闭并恢复原推荐页。
func collectZhaopinAttachmentDocument(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (paths []string, returnErr error) {
	saver, ok := browser.(zhaopinCurrentDocumentBrowser)
	if !ok {
		return nil, fmt.Errorf("%s当前文档保存能力没有准备好", cfg.Name)
	}
	pages, err := browser.ListPages(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取%s推荐页标签失败：%w", cfg.Name, err)
	}
	returnURL, err := uniqueZhaopinCurrentPageURL(pages)
	if err != nil {
		return nil, err
	}
	entry, err := common.RequiredSelector(cfg, "message.resume_attachment_entry")
	if err != nil {
		return nil, err
	}
	clicked, err := browser.Click(ctx, contract.ElementClickRequest{
		Selector: entry, ViewportMargin: 0,
		WaitForNewPage: true, NewPageTimeoutMS: 10000,
	})
	if err != nil {
		cleanupErr := restoreZhaopinAttachmentPage(context.WithoutCancel(ctx), browser, cfg.Behavior, returnURL, "")
		return nil, errors.Join(fmt.Errorf("打开%s附件简历失败：%w", cfg.Name, err), cleanupErr)
	}
	if !clicked.NewPageOpened {
		return nil, fmt.Errorf("%s附件简历没有打开新标签页", cfg.Name)
	}
	documentURL := strings.TrimSpace(clicked.NewPageURL)
	defer func() {
		cleanupErr := restoreZhaopinAttachmentPage(context.WithoutCancel(ctx), browser, cfg.Behavior, returnURL, documentURL)
		if cleanupErr != nil {
			returnErr = errors.Join(returnErr, cleanupErr)
		}
	}()
	currentPages, err := browser.ListPages(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取%s附件标签失败：%w", cfg.Name, err)
	}
	currentURL, err := uniqueZhaopinCurrentPageURL(currentPages)
	if err != nil {
		return nil, fmt.Errorf("暂时不能确定%s附件标签：%w", cfg.Name, err)
	}
	documentURL = currentURL
	if err = validateZhaopinAttachmentURL(documentURL, cfg.Behavior); err != nil {
		return nil, err
	}
	filename := strings.TrimSpace(snapshot.CandidateName)
	if filename == "" {
		filename = "智联候选人"
	}
	record, err := saver.SaveCurrentDocument(ctx, contract.SaveCurrentDocumentRequest{
		MaxBytes:            zhaopinAutoReplyAttachmentMaxBytes,
		AllowedContentTypes: []string{"application/pdf"},
		SuggestedFilename:   filename + "-附件简历.pdf",
		TimeoutMS:           30000,
	})
	if err != nil {
		return nil, fmt.Errorf("保存%s附件简历失败：%w", cfg.Name, err)
	}
	if record.Status != "saved" || strings.TrimSpace(record.FilePath) == "" || record.Size <= 0 {
		return nil, fmt.Errorf("%s附件简历没有生成有效的本地下载记录", cfg.Name)
	}
	info, err := os.Stat(record.FilePath)
	if err != nil || info.IsDir() || info.Size() <= 0 {
		return nil, fmt.Errorf("%s附件简历没有保存成有效文件", cfg.Name)
	}
	return []string{record.FilePath}, nil
}

// uniqueZhaopinCurrentPageURL 返回唯一活动页地址，并拒绝存在相同地址的重复标签页。
func uniqueZhaopinCurrentPageURL(pages contract.PageListResult) (string, error) {
	currentURL := ""
	currentCount := 0
	for _, page := range pages.Pages {
		if page.Current {
			currentCount++
			currentURL = strings.TrimSpace(page.URL)
		}
	}
	if currentCount == 0 || currentURL == "" {
		return "", fmt.Errorf("暂时没有读到智联活动标签页地址")
	}
	if currentCount > 1 {
		return "", fmt.Errorf("当前同时存在 %d 个智联活动标签页", currentCount)
	}
	matches := 0
	for _, page := range pages.Pages {
		if strings.TrimSpace(page.URL) == currentURL {
			matches++
		}
	}
	if matches > 1 {
		return "", fmt.Errorf("发现 %d 个地址相同的智联标签页，无法确定该使用哪一个", matches)
	}
	return currentURL, nil
}

// validateZhaopinAttachmentURL 按本地平台配置校验智联临时 PDF 下载页，不记录带签名的查询参数。
func validateZhaopinAttachmentURL(rawURL string, behavior model.Behavior) error {
	allowedScheme := strings.TrimSpace(behavior.AttachmentAllowedScheme)
	allowedHost := strings.TrimSpace(behavior.AttachmentAllowedHost)
	allowedPath := strings.TrimSpace(behavior.AttachmentAllowedPath)
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if allowedScheme == "" || allowedHost == "" || allowedPath == "" || err != nil ||
		!strings.EqualFold(parsed.Scheme, allowedScheme) ||
		!strings.EqualFold(parsed.Hostname(), allowedHost) || parsed.Path != allowedPath {
		return fmt.Errorf("智联附件打开到了不正确的页面，我先停下来避免保存错文件")
	}
	return nil
}

// restoreZhaopinAttachmentPage 关闭唯一附件标签，并按完整地址恢复唯一原推荐页。
func restoreZhaopinAttachmentPage(ctx context.Context, browser model.Browser, behavior model.Behavior, returnURL string, documentURL string) error {
	pages, err := browser.ListPages(ctx)
	if err != nil {
		return fmt.Errorf("读取智联附件和推荐页标签失败：%w", err)
	}
	documentMatches := make([]contract.PageInfo, 0, 1)
	for _, page := range pages.Pages {
		if documentURL != "" && strings.TrimSpace(page.URL) == documentURL {
			documentMatches = append(documentMatches, page)
			continue
		}
		if documentURL == "" && page.Current && validateZhaopinAttachmentURL(page.URL, behavior) == nil {
			documentMatches = append(documentMatches, page)
		}
	}
	if len(documentMatches) > 1 {
		return fmt.Errorf("发现 %d 个地址相同的智联附件标签页，暂时不能安全关闭", len(documentMatches))
	}
	if len(documentMatches) == 1 {
		if !documentMatches[0].Current {
			if _, err = browser.UsePage(ctx, contract.PageUseRequest{PageID: documentMatches[0].PageID}); err != nil {
				return fmt.Errorf("切换到智联附件标签失败：%w", err)
			}
		}
		if err = browser.ClosePage(ctx); err != nil {
			return fmt.Errorf("关闭智联附件标签失败：%w", err)
		}
	}
	pages, err = browser.ListPages(ctx)
	if err != nil {
		return fmt.Errorf("关闭附件后读取智联推荐页失败：%w", err)
	}
	returnMatches := make([]contract.PageInfo, 0, 1)
	for _, page := range pages.Pages {
		if strings.TrimSpace(page.URL) == returnURL {
			returnMatches = append(returnMatches, page)
		}
	}
	if len(returnMatches) == 0 {
		return fmt.Errorf("原智联推荐页已经不在了")
	}
	if len(returnMatches) > 1 {
		return fmt.Errorf("发现 %d 个地址相同的智联推荐页，无法确定该返回哪一个", len(returnMatches))
	}
	if _, err = browser.UsePage(ctx, contract.PageUseRequest{PageID: returnMatches[0].PageID}); err != nil {
		return fmt.Errorf("切回原智联推荐页失败：%w", err)
	}
	return nil
}

// SendAutoReplyMessage 核对智联候选人和岗位后发送消息。
func (r *Runtime) SendAutoReplyMessage(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, message string) error {
	return common.SendConfiguredAutoReplyMessage(ctx, browser, cfg, zhaopinAutoReplyAdapter, snapshot, message)
}

// ReadLatestAutoReplyMessage 返回智联当前聊天框最新双方消息。
func (r *Runtime) ReadLatestAutoReplyMessage(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (model.ConversationMessage, error) {
	return common.ReadLatestConfiguredAutoReplyMessage(ctx, browser, cfg, zhaopinAutoReplyAdapter, snapshot)
}

// ReadOpenAutoReplyLatestMessage 仅在智联目标聊天框仍打开时读取最新消息。
func (r *Runtime) ReadOpenAutoReplyLatestMessage(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (model.ConversationMessage, bool, error) {
	return common.ReadOpenLatestConfiguredAutoReplyMessage(ctx, browser, cfg, zhaopinAutoReplyAdapter, snapshot)
}

// CloseAutoReplyConversation 关闭智联附件、聊天框和联系人列表。
func (r *Runtime) CloseAutoReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, _ model.AutoReplyConversationSnapshot) error {
	return common.CloseConfiguredAutoReplyConversation(ctx, browser, cfg)
}

// ReadConversation 兼容平台公共接口并返回可读聊天正文。
func (r *Runtime) ReadConversation(ctx context.Context, browser model.Browser, cfg model.Config, conversation model.Conversation) (string, error) {
	snapshot, err := r.OpenAutoReplyConversation(ctx, browser, cfg, conversation, nil, 5000)
	if err != nil {
		return "", err
	}
	lines := make([]string, 0, len(snapshot.Messages))
	for _, message := range snapshot.Messages {
		if text := strings.TrimSpace(message.TextContent); text != "" {
			lines = append(lines, text)
		}
	}
	return strings.Join(lines, "\n"), nil
}

// ReplyConversation 兼容平台公共接口并在发送前继续核对候选人。
func (r *Runtime) ReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, conversation model.Conversation, reply string) error {
	return r.SendAutoReplyMessage(ctx, browser, cfg, model.AutoReplyConversationSnapshot{
		Conversation: conversation, CandidateName: conversation.Name,
		CommunicationPosition: conversation.CommunicationPosition,
	}, reply)
}
