// Package liepin 文件作用：实现猎聘企业端自动回复中的简历索要、在线简历读取和附件下载。
package liepin

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/system/failurediag"
)

var liepinEmailPattern = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)
var liepinPhoneLabelPattern = regexp.MustCompile(`(?i)(?:手机|电话|联系方式|mobile|phone)\s*[:：]?\s*([+＋]?[0-9][0-9 ()\-]{5,24}[0-9])`)
var liepinWechatPattern = regexp.MustCompile(`(?i)(?:微信|wechat)\s*[:：]?\s*([a-z0-9_\-]{5,32})`)
var liepinAgePattern = regexp.MustCompile(`([1-9][0-9]?)\s*岁`)

type liepinDownloadBrowser interface {
	WatchDownload(page, file string, ignoredQuery ...string) (<-chan contract.DownloadReceipt, func(), error)
}

// RequestAutoReplyResume 在当前已核对的候选人聊天框中索要简历。
func (r *Runtime) RequestAutoReplyResume(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) error {
	if _, _, err := waitLiepinConversation(ctx, browser, cfg, snapshot.CandidateName, snapshot.CommunicationPosition); err != nil {
		return err
	}
	available, err := common.ProbeSelectorExists(ctx, browser, cfg, "candidate.request_resume")
	if err != nil {
		return err
	}
	if !available {
		return model.ErrAutoReplyResumeRequestUnavailable
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
		pinned, confirmed, err := liepinRecentResumeSource(ctx, browser, cfg, snapshot)
		if err != nil {
			return model.AutoReplyResumeBundle{}, err
		}
		if confirmed {
			cfg = pinned
		} else if snapshot.HistoryAfter != nil {
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
	pagesBefore, err := browser.ListPages(ctx)
	if err != nil {
		return "", nil, err
	}
	returnURL, err := common.UniqueCurrentPageURLFromList(pagesBefore)
	if err != nil {
		return "", nil, err
	}
	var receiptRecord contract.DownloadRecord
	previewAt := time.Now()
	if err := openLiepinAttachmentPreview(ctx, browser, cfg); err != nil {
		return "", nil, failurediag.AttachmentFailure(err, "打开附件预览", previewAt, 0, 0, "")
	}
	defer func() {
		if ctx.Err() != nil && !errors.Is(returnErr, ctx.Err()) {
			returnErr = errors.Join(returnErr, ctx.Err())
		}
		cleanupCtx := context.WithoutCancel(ctx)
		// 下载可能打开 PDF 并改变 Worker 当前页；必须先回原页核对身份，不能在 PDF 上误判预览已关闭。
		if restoreErr := restoreLiepinAttachmentPage(cleanupCtx, browser, cfg, snapshot, returnURL, pagesBefore, receiptRecord); restoreErr != nil {
			returnErr = errors.Join(returnErr, restoreErr)
			return
		}
		if closeErr := common.ClosePanelEscapeFirst(cleanupCtx, browser, cfg, "message.attachment_preview", "message.attachment_preview_close", returnURL, func(ctx context.Context) error {
			return (&Runtime{}).VerifyAutoReplyConversationIdentity(ctx, browser, cfg, snapshot)
		}); closeErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("关闭%s附件预览失败：%w", cfg.Name, closeErr))
		}
	}()
	attachmentText := ""
	if body, selectorErr := common.RequiredSelector(cfg, "message.attachment_body"); selectorErr == nil {
		body.TimeoutMS = 250 // 预览正文是辅助信息；文件正文由本机解析。
		if read, readErr := browser.Read(ctx, contract.ElementReadRequest{Selector: body, Property: "text"}); readErr == nil {
			attachmentText = strings.TrimSpace(read.Value)
		}
	}
	download, err := common.RequiredSelector(cfg, "message.attachment_download")
	if err != nil {
		return "", nil, err
	}
	prepareAt := time.Now()
	link, err := browser.Read(ctx, contract.ElementReadRequest{Selector: download, Attribute: "href"})
	if err != nil {
		return "", nil, failurediag.AttachmentFailure(fmt.Errorf("读取附件下载链接失败：%w", err), "准备下载监听", prepareAt, 0, 0, "")
	}
	file, ignored, err := liepinAttachmentDownloadSource(ctx, browser, cfg, returnURL, link.Value)
	if err != nil {
		return "", nil, failurediag.AttachmentFailure(err, "准备下载监听", prepareAt, 0, 0, "")
	}
	receipt, stop, err := downloader.WatchDownload(returnURL, file, ignored...)
	if err != nil {
		return "", nil, failurediag.AttachmentFailure(err, "准备下载监听", prepareAt, 0, 0, "")
	}
	defer stop()
	clickAt := time.Now()
	if _, err = browser.Click(ctx, contract.ElementClickRequest{Selector: download, WatchDownloadViewer: true}); err != nil {
		return "", nil, failurediag.AttachmentFailure(fmt.Errorf("下载%s附件简历失败：%w", cfg.Name, err), "触发附件下载", clickAt, 0, 0, "")
	}
	// 没有起下载才报短超时；原生下载已经开始时等它完成，避免大文件被重复点击。
	downloadAt := time.Now()
	startTimer := time.NewTimer(12 * time.Second)
	defer startTimer.Stop()
	startDeadline := startTimer.C
	receiptDeadline := errors.New("附件下载回执等待时限已到")
	waitCtx, cancel := context.WithTimeoutCause(ctx, 90*time.Second, receiptDeadline)
	defer cancel()
	for {
		select {
		case <-startDeadline:
			return "", nil, failurediag.AttachmentFailure(fmt.Errorf("还没看到这份附件开始下载"), "等待下载开始", downloadAt, 12*time.Second, 0, "尚未观察到下载开始")
		case <-waitCtx.Done():
			reason := ""
			if errors.Is(context.Cause(waitCtx), receiptDeadline) {
				reason = "下载回执等待时限已到"
			} else if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				reason = "上层请求时限已到"
			}
			return "", nil, failurediag.AttachmentFailure(fmt.Errorf("还没收到这份附件的下载回执：%w", waitCtx.Err()), "等待下载回执", downloadAt, 90*time.Second, 0, reason)
		case result, ok := <-receipt:
			if !ok {
				return "", nil, failurediag.AttachmentFailure(fmt.Errorf("附件下载监听已结束"), "等待下载回执", downloadAt, 90*time.Second, 0, "")
			}
			if result.Err != nil {
				return "", nil, failurediag.AttachmentFailure(result.Err, "等待下载回执", downloadAt, 90*time.Second, 0, "")
			}
			if result.Record.Status == "pending" {
				startDeadline = nil
				continue
			}
			info, err := os.Stat(result.Record.FilePath)
			if result.Record.Status != "saved" || err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
				return "", nil, failurediag.AttachmentFailure(fmt.Errorf("下载记录已收到，但本地附件还不可用"), "核对下载文件", downloadAt, 0, 0, "")
			}
			receiptRecord = result.Record
			return attachmentText, []string{result.Record.FilePath}, nil
		}
	}
}

// liepinAttachmentDownloadSource 优先使用下载链接；按钮没有链接时，用已核实预览内展示的文件 URL 认领原生下载。
func liepinAttachmentDownloadSource(ctx context.Context, browser model.Browser, cfg model.Config, pageURL, href string) (string, []string, error) {
	page, err := url.Parse(pageURL)
	if err != nil {
		return "", nil, fmt.Errorf("附件原页面地址无效")
	}
	if value := strings.TrimSpace(href); value != "" && !strings.HasPrefix(value, "#") {
		if target, parseErr := page.Parse(value); parseErr == nil && (target.Scheme == "http" || target.Scheme == "https") && target.Host != "" && target.Path != "" && target.String() != pageURL {
			return target.String(), nil, nil
		}
	}
	body, err := common.RequiredSelector(cfg, "message.attachment_body")
	if err != nil || len(body.Frames) == 0 {
		return "", nil, fmt.Errorf("未找到可核实的附件预览文件")
	}
	last := len(body.Frames) - 1
	frame := contract.SelectorSpec{Frames: body.Frames[:last], Target: body.Frames[last], State: "visible", TimeoutMS: 1200, Description: cfg.Name + "附件预览文档"}
	src, err := browser.Read(ctx, contract.ElementReadRequest{Selector: frame, Attribute: "src"})
	if err != nil {
		return "", nil, fmt.Errorf("读取预览文件地址失败：%w", err)
	}
	viewer, err := url.Parse(src.Value)
	if err != nil || viewer.Scheme != "https" || viewer.Host == "" {
		return "", nil, fmt.Errorf("附件预览没有提供可核实的文件地址")
	}
	files := viewer.Query()["file"]
	if len(files) != 1 {
		return "", nil, fmt.Errorf("附件预览没有展示唯一的文件")
	}
	file, err := url.Parse(files[0])
	if err != nil || file.Scheme != "https" || file.Host == "" || file.User != nil || file.Path == "" || file.Path == "/" ||
		file.Query().Get("download") != "0" || file.Query().Get("dlFileName") == "" || file.Query().Get("signParams") == "" {
		return "", nil, fmt.Errorf("附件预览没有提供可验证的文件标识")
	}
	// 签名和预览/下载标记随点击改变；文件路径、文件名、其余参数及招聘标签页保持精确核对。
	return file.String(), []string{"tk", "download"}, nil
}

// restoreLiepinAttachmentPage 用实时标签列表恢复唯一原页，核对返回地址与候选人编号后才允许关闭预览。
func restoreLiepinAttachmentPage(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, returnURL string, before contract.PageListResult, record contract.DownloadRecord) error {
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
	restore, err := newLiepinDownloadViewer(before, pages, record)
	isSavedPDF := record.Status == "saved" && record.ID != "" && strings.EqualFold(filepath.Ext(record.FilePath), ".pdf")
	log.Printf("event=attachment_viewer_restore pages_before=%d pages_after=%d has_receipt=%t saved_pdf=%t matched_first_list=%t", len(before.Pages), len(pages.Pages), record.ID != "", isSavedPDF, restore != nil)
	if err != nil {
		return err
	}
	if isSavedPDF {
		if restore == nil {
			restore = &contract.DownloadViewerRestore{DownloadID: record.ID}
		}
		restore.AwaitViewer = true
	}
	if restore != nil {
		restore.OriginalURL = returnURL
		target, err = browser.UsePage(ctx, contract.PageUseRequest{PageID: target.PageID, DismissDownload: restore})
		if err != nil {
			return fmt.Errorf("收起本次下载的文件页失败：%w", err)
		}
		if (!target.DownloadViewerClosed && !target.DownloadViewerPreserved && !target.DownloadViewerAbsent) || target.URL != returnURL || !target.Current {
			return fmt.Errorf("未确认原招聘页已恢复，暂不关闭预览")
		}
	} else if !target.Current {
		return fmt.Errorf("当前标签与本次下载文件无法对应，暂不切换或关闭页面")
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
