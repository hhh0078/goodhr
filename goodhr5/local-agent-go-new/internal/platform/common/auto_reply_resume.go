// Package common 文件作用：统一自动回复中的简历索要、附件下载、在线简历读取和联系方式清洗。
package common

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

var configuredEmailPattern = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)
var configuredPhonePattern = regexp.MustCompile(`(?i)(?:手机|电话|联系方式|mobile|phone)\s*[:：]?\s*([+＋]?[0-9][0-9 ()\-]{5,24}[0-9])`)
var configuredWechatPattern = regexp.MustCompile(`(?i)(?:微信|wechat)\s*[:：]?\s*([a-z0-9_\-]{5,32})`)
var configuredAgePattern = regexp.MustCompile(`([1-9][0-9]?)\s*岁`)

type configuredDownloadBrowser interface {
	Downloads(context.Context) (contract.DownloadListResult, error)
	ClearDownloads(context.Context) error
}

// RequestConfiguredAutoReplyResume 在已核对的当前候选人聊天框中索要简历。
func RequestConfiguredAutoReplyResume(ctx context.Context, browser model.Browser, cfg model.Config, adapter ConfiguredAutoReplyAdapter, snapshot model.AutoReplyConversationSnapshot) error {
	if err := EnsureConfiguredAutoReplyConversation(ctx, browser, cfg, adapter, snapshot); err != nil {
		return err
	}
	return RequestCandidateInfo(ctx, browser, cfg, model.CandidateInfoRequest{RequestResume: true})
}

// CollectConfiguredAutoReplyResume 下载真实附件，并按平台能力可选补充在线简历正文。
func CollectConfiguredAutoReplyResume(ctx context.Context, browser model.Browser, cfg model.Config, adapter ConfiguredAutoReplyAdapter, snapshot model.AutoReplyConversationSnapshot, includeOnline bool) (model.AutoReplyResumeBundle, error) {
	if !snapshot.ResumeCardAvailable {
		return model.AutoReplyResumeBundle{}, fmt.Errorf("%s当前会话没有候选人简历卡片", cfg.Name)
	}
	if err := EnsureConfiguredAutoReplyConversation(ctx, browser, cfg, adapter, snapshot); err != nil {
		return model.AutoReplyResumeBundle{}, err
	}
	attachmentText, attachmentPaths, err := collectConfiguredAttachment(ctx, browser, cfg)
	if err != nil {
		return model.AutoReplyResumeBundle{}, err
	}
	onlineText := ""
	if includeOnline {
		onlineText, err = collectConfiguredOnlineResume(ctx, browser, cfg)
		if err != nil {
			return model.AutoReplyResumeBundle{}, err
		}
	}
	if err = EnsureConfiguredAutoReplyConversation(ctx, browser, cfg, adapter, snapshot); err != nil {
		return model.AutoReplyResumeBundle{}, fmt.Errorf("恢复%s候选人聊天框失败：%w", cfg.Name, err)
	}
	resumeText := joinConfiguredResumeText(onlineText, attachmentText)
	if strings.TrimSpace(resumeText) == "" && len(attachmentPaths) == 0 {
		return model.AutoReplyResumeBundle{}, fmt.Errorf("%s简历卡片存在，但没有读到正文或下载文件", cfg.Name)
	}
	phone, email, wechat := ParseConfiguredResumeContacts(resumeText)
	birthYM, precision := ParseConfiguredResumeBirthYM(resumeText, time.Now())
	return model.AutoReplyResumeBundle{
		CandidateName: snapshot.CandidateName, Gender: snapshot.Gender,
		Phone: phone, Email: email, Wechat: wechat, BirthYM: birthYM, BirthYMPrecision: precision,
		OnlineResumeText: resumeText, AttachmentPaths: attachmentPaths,
		ResumeSourceMessageID: snapshot.ResumeSourceMessageID,
	}, nil
}

// collectConfiguredOnlineResume 打开在线简历，读取正文并可靠关闭详情。
func collectConfiguredOnlineResume(ctx context.Context, browser model.Browser, cfg model.Config) (text string, returnErr error) {
	if _, configured := cfg.Selectors["message.resume_online_entry"]; !configured {
		return "", nil
	}
	if err := ClickRequired(ctx, browser, cfg, "message.resume_online_entry"); err != nil {
		return "", fmt.Errorf("打开%s在线简历失败：%w", cfg.Name, err)
	}
	defer func() {
		var closeErr error
		if _, configured := cfg.Selectors["message.online_resume_panel"]; configured {
			closeErr = CloseOptionalPanel(context.WithoutCancel(ctx), browser, cfg, "message.online_resume_panel", "message.online_resume_close", cfg.Name+"在线简历")
		} else if _, configured := cfg.Selectors["candidate.detail"]; configured {
			closeErr = CloseCandidateDetail(context.WithoutCancel(ctx), browser, cfg)
		}
		if closeErr != nil && returnErr == nil {
			returnErr = fmt.Errorf("关闭%s在线简历失败：%w", cfg.Name, closeErr)
		}
	}()
	if _, configured := cfg.Selectors["message.online_resume_body"]; configured {
		value, found, err := ReadOptional(ctx, browser, cfg, "message.online_resume_body")
		if err != nil {
			return "", fmt.Errorf("读取%s在线简历失败：%w", cfg.Name, err)
		}
		if found && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value), nil
		}
	}
	detail, err := ExtractCandidateDetail(ctx, browser, cfg)
	if err != nil {
		return "", fmt.Errorf("读取%s在线简历失败：%w", cfg.Name, err)
	}
	return strings.TrimSpace(detail.Text), nil
}

// collectConfiguredAttachment 打开简历附件预览并通过下载监听取得真实本地文件。
func collectConfiguredAttachment(ctx context.Context, browser model.Browser, cfg model.Config) (string, []string, error) {
	if _, configured := cfg.Selectors["message.resume_attachment_entry"]; !configured {
		return "", nil, nil
	}
	downloader, ok := browser.(configuredDownloadBrowser)
	if !ok {
		return "", nil, fmt.Errorf("%s简历下载监听没有准备好", cfg.Name)
	}
	if err := downloader.ClearDownloads(ctx); err != nil {
		return "", nil, fmt.Errorf("清理旧下载记录失败：%w", err)
	}
	defer downloader.ClearDownloads(context.WithoutCancel(ctx))
	if err := openConfiguredAttachment(ctx, browser, cfg); err != nil {
		return "", nil, err
	}
	defer CloseOptionalPanel(context.WithoutCancel(ctx), browser, cfg, "message.attachment_preview", "message.attachment_preview_close", cfg.Name+"附件预览")
	attachmentText := ""
	if body, found, err := ReadOptional(ctx, browser, cfg, "message.attachment_body"); err != nil {
		return "", nil, err
	} else if found {
		attachmentText = strings.TrimSpace(body)
	}
	if _, configured := cfg.Selectors["message.attachment_download"]; configured {
		if err := ClickRequired(ctx, browser, cfg, "message.attachment_download"); err != nil {
			return "", nil, fmt.Errorf("下载%s附件简历失败：%w", cfg.Name, err)
		}
	}
	paths, err := waitConfiguredDownloads(ctx, downloader)
	if err != nil {
		return "", nil, err
	}
	return attachmentText, paths, nil
}

// openConfiguredAttachment 点击附件入口并最多重试三次确认预览已出现。
func openConfiguredAttachment(ctx context.Context, browser model.Browser, cfg model.Config) error {
	entry, err := RequiredSelector(cfg, "message.resume_attachment_entry")
	if err != nil {
		return err
	}
	var verify *contract.ClickVerification
	if preview, configured := cfg.Selectors["message.attachment_preview"]; configured && len(preview.Target.Selectors) > 0 {
		verify = &contract.ClickVerification{TargetVisible: &preview, TimeoutMS: 2500}
	}
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		_, lastErr = browser.Click(ctx, contract.ElementClickRequest{Selector: entry, ViewportMargin: 0, Verify: verify})
		if lastErr == nil {
			return nil
		}
		if attempt < 3 {
			if err = waitConversationPoll(ctx); err != nil {
				return err
			}
		}
	}
	return fmt.Errorf("打开%s附件简历连续尝试3次仍然失败：%w", cfg.Name, lastErr)
}

// waitConfiguredDownloads 等待 Worker 下载记录落盘并过滤空文件。
func waitConfiguredDownloads(ctx context.Context, downloader configuredDownloadBrowser) ([]string, error) {
	for attempt := 1; attempt <= 30; attempt++ {
		result, err := downloader.Downloads(ctx)
		if err != nil {
			return nil, fmt.Errorf("读取简历下载记录失败：%w", err)
		}
		if result.Pending == 0 && len(result.Downloads) > 0 {
			paths := make([]string, 0, len(result.Downloads))
			for _, item := range result.Downloads {
				if item.Status != "saved" || strings.TrimSpace(item.FilePath) == "" {
					continue
				}
				info, statErr := os.Stat(item.FilePath)
				if statErr == nil && !info.IsDir() && info.Size() > 0 {
					paths = append(paths, item.FilePath)
				}
			}
			if len(paths) > 0 {
				return paths, nil
			}
		}
		if attempt < 30 {
			if err = waitConversationPoll(ctx); err != nil {
				return nil, err
			}
		}
	}
	return nil, fmt.Errorf("附件简历已经点了下载，但 9 秒内没有拿到本地文件")
}

// joinConfiguredResumeText 合并在线简历和附件预览正文并保留来源。
func joinConfiguredResumeText(onlineText string, attachmentText string) string {
	parts := make([]string, 0, 2)
	if onlineText = strings.TrimSpace(onlineText); onlineText != "" {
		parts = append(parts, "在线简历\n"+onlineText)
	}
	if attachmentText = strings.TrimSpace(attachmentText); attachmentText != "" {
		parts = append(parts, "附件简历\n"+attachmentText)
	}
	return strings.Join(parts, "\n\n")
}

// ParseConfiguredResumeContacts 从有明确标签的简历正文提取手机号、邮箱和微信。
func ParseConfiguredResumeContacts(value string) (string, string, string) {
	phone := ""
	if match := configuredPhonePattern.FindStringSubmatch(value); len(match) == 2 {
		phone = normalizeConfiguredPhone(match[1])
	}
	email := strings.ToLower(configuredEmailPattern.FindString(value))
	wechat := ""
	if match := configuredWechatPattern.FindStringSubmatch(value); len(match) == 2 {
		wechat = strings.TrimSpace(match[1])
	}
	return phone, email, wechat
}

// normalizeConfiguredPhone 保留国际区号并删除空格、括号和横线。
func normalizeConfiguredPhone(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "＋", "+"))
	prefix := ""
	if strings.HasPrefix(value, "+") {
		prefix = "+"
	}
	var digits strings.Builder
	for _, char := range value {
		if char >= '0' && char <= '9' {
			digits.WriteRune(char)
		}
	}
	if digits.Len() < 6 || digits.Len() > 20 {
		return ""
	}
	return prefix + digits.String()
}

// ParseConfiguredResumeBirthYM 按年龄估算出生年份，月份未知时保存年份精度。
func ParseConfiguredResumeBirthYM(value string, now time.Time) (string, string) {
	match := configuredAgePattern.FindStringSubmatch(value)
	if len(match) != 2 {
		return "", ""
	}
	age := 0
	for _, char := range match[1] {
		age = age*10 + int(char-'0')
	}
	if age < 16 || age > 80 {
		return "", ""
	}
	return fmt.Sprintf("%04d", now.Year()-age), "year_estimated"
}
