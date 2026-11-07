// Package liepin 文件作用：实现猎聘企业端自动回复中的简历索要、在线简历读取和附件下载。
package liepin

import (
	"context"
	"errors"
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
	if _, _, err := waitLiepinConversation(ctx, browser, cfg, snapshot.CandidateName, snapshot.CommunicationPosition); err != nil {
		return err
	}
	return common.RequestCandidateInfo(ctx, browser, cfg, model.CandidateInfoRequest{RequestResume: true})
}

// CollectAutoReplyResume 优先返回已核实落盘的真实附件，由公共流程解析正文和联系方式。
// 在线简历是补充来源，不能在附件下载成功后再占用采集预算、导致整个结果丢失。
func (r *Runtime) CollectAutoReplyResume(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (model.AutoReplyResumeBundle, error) {
	if !snapshot.ResumeCardAvailable {
		return model.AutoReplyResumeBundle{}, fmt.Errorf("%s当前会话没有候选人简历卡片", cfg.Name)
	}
	if _, _, err := waitLiepinConversation(ctx, browser, cfg, snapshot.CandidateName, snapshot.CommunicationPosition); err != nil {
		return model.AutoReplyResumeBundle{}, err
	}
	if cfg.Behavior.AutoReplyResumeLoadsHistory {
		if snapshot.HistoryAfter != nil {
			h, err := readLiepinHistoryWindow(ctx, browser, cfg, nil, 5000, snapshot.HistoryAfter)
			if err != nil {
				return model.AutoReplyResumeBundle{}, err
			}
			if !h.ResumeCardAvailable || snapshot.ResumeSourceMessageID == "" || h.ResumeSourceMessageID != snapshot.ResumeSourceMessageID {
				return model.AutoReplyResumeBundle{}, fmt.Errorf("没找到本次重新联系后发来的那份附件，暂不下载")
			}
		} else if err := prepareLiepinResumeSource(ctx, browser, cfg, snapshot.ResumeSourceMessageID); err != nil {
			return model.AutoReplyResumeBundle{}, err
		}
		if err := r.VerifyAutoReplyConversationIdentity(ctx, browser, cfg, snapshot); err != nil {
			return model.AutoReplyResumeBundle{}, err
		}
	}
	attachmentText, attachmentPaths, err := collectLiepinAttachment(ctx, browser, cfg, snapshot)
	if err != nil {
		return model.AutoReplyResumeBundle{}, err
	}
	if err = ctx.Err(); err != nil {
		return model.AutoReplyResumeBundle{}, err
	}
	if err = ensureLiepinAutoReplyConversation(ctx, browser, cfg, snapshot); err != nil {
		return model.AutoReplyResumeBundle{}, fmt.Errorf("恢复%s候选人聊天框失败：%w", cfg.Name, err)
	}
	if err = ctx.Err(); err != nil {
		return model.AutoReplyResumeBundle{}, err
	}
	resumeText := joinLiepinResumeText("", attachmentText)
	phone, email, wechat := parseLiepinContacts(resumeText)
	birthYM, precision := parseLiepinBirthYM(resumeText, time.Now())
	return model.AutoReplyResumeBundle{
		CandidateName: snapshot.CandidateName, Gender: snapshot.Gender,
		Phone: phone, Email: email, Wechat: wechat, BirthYM: birthYM,
		BirthYMPrecision: precision, OnlineResumeText: resumeText,
		AttachmentPaths: attachmentPaths, ResumeSourceMessageID: snapshot.ResumeSourceMessageID,
	}, nil
}

// collectLiepinOnlineResume 打开候选人在线简历浮层，读取正文后确认关闭浮层。
func collectLiepinOnlineResume(ctx context.Context, browser model.Browser, cfg model.Config) (text string, returnErr error) {
	selector, err := common.RequiredSelector(cfg, "message.resume_online_entry")
	if err != nil {
		return "", err
	}
	_, err = browser.Click(ctx, contract.ElementClickRequest{Selector: selector, ViewportMargin: 0})
	if err != nil {
		return "", fmt.Errorf("打开%s在线简历失败：%w", cfg.Name, err)
	}
	defer func() {
		if closeErr := common.CloseCandidateDetail(context.WithoutCancel(ctx), browser, cfg); closeErr != nil && returnErr == nil {
			returnErr = fmt.Errorf("关闭%s在线简历失败：%w", cfg.Name, closeErr)
		}
	}()
	detail, err := common.ExtractCandidateDetail(ctx, browser, cfg)
	if err != nil {
		return "", fmt.Errorf("读取%s在线简历失败：%w", cfg.Name, err)
	}
	if strings.TrimSpace(detail.Text) == "" {
		return "", fmt.Errorf("%s在线简历正文是空的", cfg.Name)
	}
	return strings.TrimSpace(detail.Text), nil
}

// collectLiepinAttachment 打开附件预览，读取 iframe 正文并通过下载监听取得本地文件。
func collectLiepinAttachment(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (text string, paths []string, returnErr error) {
	downloader, ok := browser.(liepinDownloadBrowser)
	if !ok {
		return "", nil, fmt.Errorf("%s简历下载监听没有准备好", cfg.Name)
	}
	returnURL, err := currentLiepinAttachmentPageURL(ctx, browser)
	if err != nil {
		return "", nil, err
	}
	if err := downloader.ClearDownloads(ctx); err != nil {
		return "", nil, fmt.Errorf("清理旧下载记录失败：%w", err)
	}
	defer downloader.ClearDownloads(context.WithoutCancel(ctx))
	if err := openLiepinAttachmentPreview(ctx, browser, cfg); err != nil {
		return "", nil, err
	}
	defer func() {
		if ctx.Err() != nil && !errors.Is(returnErr, ctx.Err()) {
			returnErr = errors.Join(returnErr, ctx.Err())
		}
		cleanupCtx := context.WithoutCancel(ctx)
		// 下载可能打开 PDF 并改变 Worker 当前页；必须先回原页核对身份，不能在 PDF 上误判预览已关闭。
		if restoreErr := restoreLiepinAttachmentPage(cleanupCtx, browser, cfg, snapshot, returnURL); restoreErr != nil {
			returnErr = errors.Join(returnErr, restoreErr)
			return
		}
		if closeErr := common.CloseOptionalPanel(
			cleanupCtx, browser, cfg,
			"message.attachment_preview", "message.attachment_preview_close", cfg.Name+"附件预览",
		); closeErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("关闭%s附件预览失败：%w", cfg.Name, closeErr))
		}
	}()
	attachmentText := ""
	if body, selectorErr := common.RequiredSelector(cfg, "message.attachment_body"); selectorErr == nil {
		if read, readErr := browser.Read(ctx, contract.ElementReadRequest{Selector: body, Property: "text"}); readErr == nil {
			attachmentText = strings.TrimSpace(read.Value)
		}
	}
	if err := common.ClickRequired(ctx, browser, cfg, "message.attachment_download"); err != nil {
		return "", nil, fmt.Errorf("下载%s附件简历失败：%w", cfg.Name, err)
	}
	paths, err = waitLiepinDownloads(ctx, downloader)
	if err != nil {
		return "", nil, err
	}
	return attachmentText, paths, nil
}

// currentLiepinAttachmentPageURL 保存下载前唯一活动页的完整地址，不缓存可能随标签关闭而变化的页面序号。
func currentLiepinAttachmentPageURL(ctx context.Context, browser model.Browser) (string, error) {
	pages, err := browser.ListPages(ctx)
	if err != nil {
		return "", fmt.Errorf("读取附件所属标签页失败：%w", err)
	}
	currentURL, currentCount := "", 0
	for _, page := range pages.Pages {
		if page.Current {
			currentURL = strings.TrimSpace(page.URL)
			currentCount++
		}
	}
	if currentCount != 1 || currentURL == "" {
		return "", fmt.Errorf("附件所属标签页无法唯一确认，暂不开始下载")
	}
	matches := 0
	for _, page := range pages.Pages {
		if strings.TrimSpace(page.URL) == currentURL {
			matches++
		}
	}
	if matches != 1 {
		return "", fmt.Errorf("发现地址相同的招聘标签页，暂不猜测附件归属")
	}
	return currentURL, nil
}

// restoreLiepinAttachmentPage 用实时标签列表恢复唯一原页，核对返回地址与候选人编号后才允许关闭预览。
func restoreLiepinAttachmentPage(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, returnURL string) error {
	pages, err := browser.ListPages(ctx)
	if err != nil {
		return fmt.Errorf("下载后读取招聘标签页失败：%w", err)
	}
	var target contract.PageInfo
	matches := 0
	for _, page := range pages.Pages {
		if strings.TrimSpace(page.URL) == returnURL {
			target = page
			matches++
		}
	}
	if matches != 1 || target.PageID == "" {
		return fmt.Errorf("下载后无法唯一找到原招聘标签页，暂不关闭任何页面或预览")
	}
	if !target.Current {
		target, err = browser.UsePage(ctx, contract.PageUseRequest{PageID: target.PageID})
		if err != nil {
			return fmt.Errorf("切回附件所属招聘页失败：%w", err)
		}
		if strings.TrimSpace(target.URL) != returnURL || !target.Current {
			return fmt.Errorf("切回后的页面地址不一致，暂不关闭预览")
		}
	}
	if err = (&Runtime{}).VerifyAutoReplyConversationIdentity(ctx, browser, cfg, snapshot); err != nil {
		return fmt.Errorf("下载后原聊天身份核对失败：%w", err)
	}
	return nil
}

// openLiepinAttachmentPreview 点击附件入口并验证弹框，第一次没打开时最多重试两次。
func openLiepinAttachmentPreview(ctx context.Context, browser model.Browser, cfg model.Config) error {
	entry, err := common.RequiredSelector(cfg, "message.resume_attachment_entry")
	if err != nil {
		return err
	}
	preview, err := common.RequiredSelector(cfg, "message.attachment_preview")
	if err != nil {
		return err
	}
	historyScroll, err := common.RequiredSelector(cfg, "message.history_scroll")
	if err != nil {
		return err
	}
	// 附件可能位于数十条旧消息之前；先用真实滚轮抵达已识别的卡片，避免点击内部的小步滚动反复耗尽。
	// Worker 会按目标位置决定方向、进入可操作区域即停止，停滞和取消仍直接返回错误。
	// 入口只有一行文字，必须完整露出聊天滚动区，避免半行被顶部栏裁剪后误点。
	requireFull := true
	if _, err = browser.Scroll(ctx, contract.ScrollRequest{
		Target: &entry, WheelAnchor: &historyScroll, Distance: 1600,
		MaxAttempts: 40, WaitMS: 150, RequireFull: &requireFull,
	}); err != nil {
		return fmt.Errorf("定位%s附件简历失败：%w", cfg.Name, err)
	}
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		_, lastErr = browser.Click(ctx, contract.ElementClickRequest{
			Selector: entry, WheelAnchor: &historyScroll, ViewportMargin: 0,
			Diagnostic: true, DiagnosticAttempt: attempt,
			Verify: &contract.ClickVerification{TargetVisible: &preview, TimeoutMS: 2500},
		})
		if lastErr == nil {
			return nil
		}
		opened, probeErr := common.ProbeSelectorExists(ctx, browser, cfg, "message.attachment_preview")
		if probeErr != nil {
			return probeErr
		}
		if opened {
			return nil
		}
		if attempt < 3 {
			if err = waitLiepinReplyPoll(ctx); err != nil {
				return err
			}
		}
	}
	return fmt.Errorf("打开%s附件简历连续尝试3次仍然失败：%w", cfg.Name, lastErr)
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
