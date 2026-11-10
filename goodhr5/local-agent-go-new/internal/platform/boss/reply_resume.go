// Package boss 本文件只采集已核实来源的真实简历附件，复用原生下载监听而不清理全局下载记录。
package boss

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/system/failurediag"
)

// bossDownloadBrowser 复用本机已有监听，只认领同页同文件且登记后开始的原生下载。
type bossDownloadBrowser interface {
	WatchDownload(page, file string, ignoredQuery ...string) (<-chan contract.DownloadReceipt, func(), error)
}

// collectBossAutoReplyResume 按服务器保存的来源编号定位附件，文件正文交给公共流程解析。
func collectBossAutoReplyResume(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (model.AutoReplyResumeBundle, error) {
	if !snapshot.ResumeCardAvailable || snapshot.ResumeSourceMessageID == "" {
		return model.AutoReplyResumeBundle{}, fmt.Errorf("当前聊天还没有可核实来源的简历附件")
	}
	// 来源定位入口首先核身；这里不在没有任何页面操作的情况下重复核对同一会话。
	pinned, err := pinBossResumeSource(ctx, browser, cfg, snapshot)
	if err != nil {
		return model.AutoReplyResumeBundle{}, err
	}
	paths, err := collectBossAttachment(ctx, browser, pinned, snapshot)
	if err != nil {
		return model.AutoReplyResumeBundle{}, err
	}
	return model.AutoReplyResumeBundle{
		CandidateName: snapshot.CandidateName, Gender: snapshot.Gender,
		AttachmentPaths: paths, ResumeSourceMessageID: snapshot.ResumeSourceMessageID,
	}, nil
}

// collectBossAttachment 先核对文件地址并登记监听，再点击一次下载；保存和原页身份都确认后才返回。
func collectBossAttachment(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (paths []string, returnErr error) {
	downloader, ok := browser.(bossDownloadBrowser)
	if !ok {
		return nil, fmt.Errorf("简历下载监听还没准备好")
	}
	page, err := bossAutoReplyPageURL(ctx, browser, cfg)
	if err != nil {
		return nil, err
	}
	if err = verifyBossOpenAction(ctx, browser, cfg, snapshot, page); err != nil {
		return nil, err
	}
	if cfg, err = prepareBossResumeSourceVisibility(ctx, browser, cfg, snapshot, page); err != nil {
		return nil, err
	}
	entry, err := common.RequiredSelector(cfg, "message.resume_attachment_entry")
	if err != nil {
		return nil, err
	}
	preview, err := common.RequiredSelector(cfg, "message.attachment_preview")
	if err != nil {
		return nil, err
	}
	openedAt := time.Now()
	_, clickErr := browser.Click(ctx, contract.ElementClickRequest{Selector: entry, Verify: &contract.ClickVerification{TargetVisible: &preview, TimeoutMS: 2000}})
	if contract.IsSessionUnavailable(clickErr) {
		return nil, clickErr
	}
	opened, probeErr := common.ProbeSelectorExists(ctx, browser, cfg, "message.attachment_preview")
	if probeErr != nil || !opened {
		return nil, failurediag.AttachmentFailure(errors.Join(clickErr, probeErr, fmt.Errorf("附件预览尚未确认打开")), "打开附件预览", openedAt, 2*time.Second, 1, "")
	}
	var record contract.DownloadRecord
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := finishBossAttachment(cleanup, browser, cfg, snapshot, page, record); err != nil {
			returnErr = errors.Join(returnErr, err)
		}
		returnErr = errors.Join(returnErr, ctx.Err())
	}()
	if err = verifyBossOpenAction(ctx, browser, cfg, snapshot, page); err != nil {
		return nil, err
	}
	download, err := common.RequiredSelector(cfg, "message.attachment_download")
	if err != nil {
		return nil, err
	}
	preparedAt := time.Now()
	file, err := readBossAttachmentFileSource(ctx, browser, cfg, page, download)
	if err != nil {
		return nil, failurediag.AttachmentFailure(err, "准备下载监听", preparedAt, 0, 0, "")
	}
	receipts, stop, err := downloader.WatchDownload(page, file)
	if err != nil {
		return nil, failurediag.AttachmentFailure(err, "准备下载监听", preparedAt, 0, 0, "")
	}
	defer stop()
	if err = verifyBossOpenAction(ctx, browser, cfg, snapshot, page); err != nil {
		return nil, err
	}
	clickedAt := time.Now()
	if result, err := browser.Click(ctx, contract.ElementClickRequest{Selector: download, WatchDownloadViewer: true}); err != nil || !result.Clicked {
		return nil, failurediag.AttachmentFailure(errors.Join(err, fmt.Errorf("附件下载点击尚未确认")), "触发附件下载", clickedAt, 0, 1, "")
	}
	record, err = awaitBossAttachment(ctx, receipts)
	if err != nil {
		return nil, err
	}
	if record.PageURL != page || record.URL != file {
		return nil, fmt.Errorf("下载回执与本次附件来源不一致，暂不使用")
	}
	return []string{record.FilePath}, nil
}

// readBossAttachmentFileSource 优先复用已配置的可见iframe来源；只有该入口不存在才读取下载链接，不掩盖已读来源的校验错误。
func readBossAttachmentFileSource(ctx context.Context, browser model.Browser, cfg model.Config, page string, download contract.SelectorSpec) (string, error) {
	raw, found, err := common.ReadOptional(ctx, browser, cfg, "message.attachment_file_source")
	if err != nil {
		return "", err
	}
	if found {
		return bossAttachmentDownloadSource(cfg, page, raw)
	}
	href, err := browser.Read(ctx, contract.ElementReadRequest{Selector: download, Attribute: "href"})
	if err != nil {
		return "", err
	}
	return bossAttachmentDownloadSource(cfg, page, href.Value)
}

// bossAttachmentDownloadSource 只接受可见链接或预览中明确的唯一文件参数，不推测接口或签名。
func bossAttachmentDownloadSource(cfg model.Config, page, raw string) (string, error) {
	base, err := url.Parse(page)
	if err != nil || base.Host == "" {
		return "", fmt.Errorf("附件所属页面无法核对")
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") {
		return "", fmt.Errorf("附件没有提供可核实的文件地址")
	}
	value, err := base.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("附件文件地址不完整")
	}
	values := value.Query()
	files, embedded := values["file"]
	if urls, found := values["url"]; found {
		if embedded {
			return "", fmt.Errorf("附件预览文件来源不唯一")
		}
		files, embedded = urls, true
	}
	if embedded {
		if len(files) != 1 || files[0] == "" {
			return "", fmt.Errorf("附件预览未指向唯一文件")
		}
		value, err = value.Parse(files[0])
		if err != nil {
			return "", fmt.Errorf("附件预览文件地址不完整")
		}
	}
	if (value.Scheme != "http" && value.Scheme != "https") || value.Hostname() == "" || value.User != nil || value.Fragment != "" || value.Path == "" || value.Path == "/" || value.String() == page {
		return "", fmt.Errorf("附件文件地址无法安全核对")
	}
	if cfg.Behavior.AttachmentPreviewPath != "" {
		return bossDerivedAttachmentSource(cfg, base, value)
	}
	return value.String(), nil
}

// bossDerivedAttachmentSource 仅应用实测并写入平台配置的预览与下载规则，逐值保留文件id和鉴权类型。
func bossDerivedAttachmentSource(cfg model.Config, page, preview *url.URL) (string, error) {
	behavior := cfg.Behavior
	prefix := behavior.AttachmentPreviewPath
	if page.Scheme != "https" || preview.Scheme != page.Scheme || !strings.EqualFold(preview.Host, page.Host) || !strings.HasPrefix(preview.Path, prefix) {
		return "", fmt.Errorf("附件预览地址与已验证的平台规则不符")
	}
	document := strings.TrimPrefix(preview.Path, prefix)
	if document == "" || strings.ContainsAny(document, "/\\ ") {
		return "", fmt.Errorf("附件预览没有唯一文档标识")
	}
	for _, character := range document {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '-' && character != '_' {
			return "", fmt.Errorf("附件预览文档标识格式未核实")
		}
	}
	values, err := url.ParseQuery(preview.RawQuery)
	if err != nil {
		return "", fmt.Errorf("附件预览参数无法核对")
	}
	for key, entries := range values {
		if len(entries) != 1 || (key != "d" && key != "id" && key != "authType" && key != "previewType" && key != "geekId") {
			return "", fmt.Errorf("附件预览包含未验证的文件参数")
		}
	}
	if values.Get("id") == "" || values.Get("authType") == "" || values.Get("previewType") != "1" || values.Get("geekId") != document || values.Get("d") == "" {
		return "", fmt.Errorf("附件预览的文档归属或鉴权参数不完整")
	}
	if behavior.AttachmentAllowedScheme != "https" || behavior.AttachmentAllowedHost == "" || !strings.HasPrefix(behavior.AttachmentAllowedPath, "/") || !strings.HasSuffix(behavior.AttachmentAllowedPath, "/") {
		return "", fmt.Errorf("平台附件下载规则尚未配置完整")
	}
	target, err := url.Parse(behavior.AttachmentAllowedScheme + "://" + behavior.AttachmentAllowedHost + behavior.AttachmentAllowedPath)
	if err != nil || target.User != nil || target.Host != behavior.AttachmentAllowedHost || target.RawQuery != "" || target.Fragment != "" || target.Path != behavior.AttachmentAllowedPath {
		return "", fmt.Errorf("平台附件下载规则无法安全核对")
	}
	target.Path += document
	// 原生下载已实测固定按id、authType排列；不改变值，也不删除未核实的参数后冒认其它文件。
	target.RawQuery = "id=" + url.QueryEscape(values.Get("id")) + "&authType=" + url.QueryEscape(values.Get("authType"))
	return target.String(), nil
}

// awaitBossAttachment 正常回执立即返回，只有下载未开始或在途时才等待，不再轮询全局下载列表。
func awaitBossAttachment(ctx context.Context, receipts <-chan contract.DownloadReceipt) (contract.DownloadRecord, error) {
	started := time.Now()
	startTimer := time.NewTimer(12 * time.Second)
	defer startTimer.Stop()
	startDeadline := startTimer.C
	waitCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	for {
		select {
		case <-startDeadline:
			return contract.DownloadRecord{}, failurediag.AttachmentFailure(fmt.Errorf("还没看到这份附件开始下载"), "等待下载开始", started, 12*time.Second, 0, "尚未观察到下载开始")
		case <-waitCtx.Done():
			return contract.DownloadRecord{}, failurediag.AttachmentFailure(waitCtx.Err(), "等待下载回执", started, 90*time.Second, 0, "")
		case receipt, ok := <-receipts:
			if !ok || receipt.Err != nil {
				return contract.DownloadRecord{}, errors.Join(receipt.Err, fmt.Errorf("这份附件的下载监听已结束"))
			}
			if receipt.Record.Status == "pending" {
				startDeadline = nil
				continue
			}
			record := receipt.Record
			info, err := os.Stat(record.FilePath)
			if record.Status != "saved" || record.ID == "" || err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
				return contract.DownloadRecord{}, fmt.Errorf("下载有回执，但这份附件还不能使用")
			}
			return record, nil
		}
	}
}

// finishBossAttachment 先证明回到原页，再由关闭回调就地核身；没有预览时也必须完成最终身份核验。
func finishBossAttachment(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, page string, record contract.DownloadRecord) error {
	if err := restoreBossAttachmentPage(ctx, browser, page, record); err != nil {
		return err
	}
	verified := false
	if err := common.ClosePanelEscapeFirst(ctx, browser, cfg, "message.attachment_preview", "message.attachment_preview_close", page, func(ctx context.Context) error {
		verified = true
		return verifyBossOpenAction(ctx, browser, cfg, snapshot, page)
	}); err != nil {
		return err
	}
	if !verified {
		return verifyBossOpenAction(ctx, browser, cfg, snapshot, page)
	}
	return nil
}

// restoreBossAttachmentPage 只证明唯一原页及本次原生viewer复位，完整核身交给紧邻的关闭动作前回调，不重复读取。
func restoreBossAttachmentPage(ctx context.Context, browser model.Browser, page string, record contract.DownloadRecord) error {
	current, err := common.UniqueCurrentPageURL(ctx, browser)
	if err == nil && current == page {
		return nil
	}
	if record.ID == "" || record.Status != "saved" || record.PageURL != page {
		return fmt.Errorf("下载后页面已变化，暂不切页或关闭预览")
	}
	pages, err := browser.ListPages(ctx)
	if err != nil {
		return err
	}
	var target contract.PageInfo
	count := 0
	for _, candidate := range pages.Pages {
		if candidate.URL == page {
			target, count = candidate, count+1
		}
	}
	if count != 1 || target.PageID == "" {
		return fmt.Errorf("下载后没能唯一找到原招聘页面")
	}
	restored, err := browser.UsePage(ctx, contract.PageUseRequest{PageID: target.PageID, DismissDownload: &contract.DownloadViewerRestore{OriginalURL: page, DownloadID: record.ID, AwaitViewer: true}})
	if err != nil || restored.URL != page || !restored.Current || (!restored.DownloadViewerClosed && !restored.DownloadViewerPreserved && !restored.DownloadViewerAbsent) {
		return errors.Join(err, fmt.Errorf("尚未确认回到原招聘页面"))
	}
	return nil
}
