// Package liepin 文件作用：实现猎聘企业端自动回复中的简历索要、在线简历读取和附件下载。
package liepin

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

var liepinEmailPattern = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)
var liepinPhoneLabelPattern = regexp.MustCompile(`(?i)(?:手机|电话|联系方式|mobile|phone)\s*[:：]?\s*([+＋]?[0-9][0-9 ()\-]{5,24}[0-9])`)
var liepinWechatPattern = regexp.MustCompile(`(?i)(?:微信|wechat)\s*[:：]?\s*([a-z0-9_\-]{5,32})`)
var liepinAgePattern = regexp.MustCompile(`([1-9][0-9]?)\s*岁`)

type liepinDownloadBrowser interface {
	Downloads(context.Context) (contract.DownloadListResult, error)
	ClearDownloads(context.Context) error
}

// RequestAutoReplyResume 在当前已核对的候选人聊天框中索要简历。
func (r *Runtime) RequestAutoReplyResume(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) error {
	if _, _, err := waitLiepinConversation(ctx, browser, cfg, snapshot.CandidateName); err != nil {
		return err
	}
	return common.RequestCandidateInfo(ctx, browser, cfg, model.CandidateInfoRequest{RequestResume: true})
}

// CollectAutoReplyResume 读取在线简历和附件预览正文，下载附件并整理联系方式。
func (r *Runtime) CollectAutoReplyResume(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (model.AutoReplyResumeBundle, error) {
	if !snapshot.ResumeCardAvailable {
		return model.AutoReplyResumeBundle{}, fmt.Errorf("%s当前会话没有候选人简历卡片", cfg.Name)
	}
	if _, _, err := waitLiepinConversation(ctx, browser, cfg, snapshot.CandidateName); err != nil {
		return model.AutoReplyResumeBundle{}, err
	}
	onlineText, err := collectLiepinOnlineResume(ctx, browser, cfg)
	if err != nil {
		return model.AutoReplyResumeBundle{}, err
	}
	attachmentText, attachmentPaths, err := collectLiepinAttachment(ctx, browser, cfg)
	if err != nil {
		return model.AutoReplyResumeBundle{}, err
	}
	resumeText := joinLiepinResumeText(onlineText, attachmentText)
	phone, email, wechat := parseLiepinContacts(resumeText)
	birthYM, precision := parseLiepinBirthYM(resumeText, time.Now())
	return model.AutoReplyResumeBundle{
		CandidateName: snapshot.CandidateName, Gender: snapshot.Gender,
		Phone: phone, Email: email, Wechat: wechat, BirthYM: birthYM,
		BirthYMPrecision: precision, OnlineResumeText: resumeText,
		AttachmentPaths: attachmentPaths, ResumeSourceMessageID: snapshot.ResumeSourceMessageID,
	}, nil
}

// collectLiepinOnlineResume 打开候选人在线简历新标签页，读取正文后关闭该标签页。
func collectLiepinOnlineResume(ctx context.Context, browser model.Browser, cfg model.Config) (text string, returnErr error) {
	selector, err := common.RequiredSelector(cfg, "message.resume_online_entry")
	if err != nil {
		return "", err
	}
	result, err := browser.Click(ctx, contract.ElementClickRequest{
		Selector: selector, ViewportMargin: 0, WaitForNewPage: true, NewPageTimeoutMS: 10000,
	})
	if err != nil {
		return "", fmt.Errorf("打开%s在线简历失败：%w", cfg.Name, err)
	}
	if !result.NewPageOpened {
		return "", fmt.Errorf("%s在线简历没有打开新标签页", cfg.Name)
	}
	defer func() {
		if closeErr := browser.ClosePage(context.WithoutCancel(ctx)); closeErr != nil && returnErr == nil {
			returnErr = fmt.Errorf("关闭%s在线简历标签页失败：%w", cfg.Name, closeErr)
		}
	}()
	body, err := common.RequiredSelector(cfg, "message.online_resume_body")
	if err != nil {
		return "", err
	}
	read, err := browser.Read(ctx, contract.ElementReadRequest{Selector: body, Property: "text"})
	if err != nil {
		return "", fmt.Errorf("读取%s在线简历失败：%w", cfg.Name, err)
	}
	if strings.TrimSpace(read.Value) == "" {
		return "", fmt.Errorf("%s在线简历正文是空的", cfg.Name)
	}
	return strings.TrimSpace(read.Value), nil
}

// collectLiepinAttachment 打开附件预览，读取 iframe 正文并通过下载监听取得本地文件。
func collectLiepinAttachment(ctx context.Context, browser model.Browser, cfg model.Config) (string, []string, error) {
	downloader, ok := browser.(liepinDownloadBrowser)
	if !ok {
		return "", nil, fmt.Errorf("%s简历下载监听没有准备好", cfg.Name)
	}
	if err := downloader.ClearDownloads(ctx); err != nil {
		return "", nil, fmt.Errorf("清理旧下载记录失败：%w", err)
	}
	defer downloader.ClearDownloads(context.WithoutCancel(ctx))
	selector, err := common.RequiredSelector(cfg, "message.resume_attachment_entry")
	if err != nil {
		return "", nil, err
	}
	if _, err = browser.Click(ctx, contract.ElementClickRequest{Selector: selector, ViewportMargin: 0}); err != nil {
		return "", nil, fmt.Errorf("打开%s附件简历失败：%w", cfg.Name, err)
	}
	if err = waitLiepinAttachmentPreview(ctx, browser, cfg); err != nil {
		return "", nil, err
	}
	defer common.CloseOptionalPanel(
		context.WithoutCancel(ctx), browser, cfg,
		"message.attachment_preview", "message.attachment_preview_close", cfg.Name+"附件预览",
	)
	attachmentText := ""
	if body, selectorErr := common.RequiredSelector(cfg, "message.attachment_body"); selectorErr == nil {
		if read, readErr := browser.Read(ctx, contract.ElementReadRequest{Selector: body, Property: "text"}); readErr == nil {
			attachmentText = strings.TrimSpace(read.Value)
		}
	}
	if err = common.ClickRequired(ctx, browser, cfg, "message.attachment_download"); err != nil {
		return "", nil, fmt.Errorf("下载%s附件简历失败：%w", cfg.Name, err)
	}
	paths, err := waitLiepinDownloads(ctx, downloader)
	if err != nil {
		return "", nil, err
	}
	return attachmentText, paths, nil
}

// waitLiepinAttachmentPreview 轮询确认附件预览弹框已经打开。
func waitLiepinAttachmentPreview(ctx context.Context, browser model.Browser, cfg model.Config) error {
	for attempt := 1; attempt <= liepinConversationPollAttempts; attempt++ {
		opened, err := common.ProbeSelectorExists(ctx, browser, cfg, "message.attachment_preview")
		if err != nil {
			return err
		}
		if opened {
			return nil
		}
		if attempt < liepinConversationPollAttempts {
			if err = waitLiepinReplyPoll(ctx); err != nil {
				return err
			}
		}
	}
	return fmt.Errorf("猎聘附件预览没有在 6 秒内打开")
}

// waitLiepinDownloads 等待 Worker 下载记录全部落盘，并确认文件真实存在。
func waitLiepinDownloads(ctx context.Context, downloader liepinDownloadBrowser) ([]string, error) {
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
				if statErr != nil || info.IsDir() || info.Size() == 0 {
					continue
				}
				paths = append(paths, item.FilePath)
			}
			if len(paths) > 0 {
				return paths, nil
			}
		}
		if attempt < 30 {
			if err = waitLiepinReplyPoll(ctx); err != nil {
				return nil, err
			}
		}
	}
	return nil, fmt.Errorf("附件简历已经点了下载，但 9 秒内没有拿到本地文件")
}

// joinLiepinResumeText 合并在线简历和附件文本，并保留来源标题。
func joinLiepinResumeText(onlineText string, attachmentText string) string {
	parts := make([]string, 0, 2)
	if onlineText = strings.TrimSpace(onlineText); onlineText != "" {
		parts = append(parts, "在线简历\n"+onlineText)
	}
	if attachmentText = strings.TrimSpace(attachmentText); attachmentText != "" {
		parts = append(parts, "附件简历\n"+attachmentText)
	}
	return strings.Join(parts, "\n\n")
}

// parseLiepinContacts 从简历正文提取有明确标签的手机号、邮箱和微信。
func parseLiepinContacts(value string) (string, string, string) {
	phone := ""
	if match := liepinPhoneLabelPattern.FindStringSubmatch(value); len(match) == 2 {
		phone = normalizeLiepinPhone(match[1])
	}
	email := strings.ToLower(liepinEmailPattern.FindString(value))
	wechat := ""
	if match := liepinWechatPattern.FindStringSubmatch(value); len(match) == 2 {
		wechat = strings.TrimSpace(match[1])
	}
	return phone, email, wechat
}

// normalizeLiepinPhone 保留可选国际区号并删除空格、括号和横线。
func normalizeLiepinPhone(value string) string {
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

// parseLiepinBirthYM 按页面年龄估算出生年份，月份未知时只保存年份精度。
func parseLiepinBirthYM(value string, now time.Time) (string, string) {
	match := liepinAgePattern.FindStringSubmatch(value)
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
