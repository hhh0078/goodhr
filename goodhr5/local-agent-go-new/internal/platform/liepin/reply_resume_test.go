// Package liepin 文件作用：验证真实附件优先返回，以及滚动、取消、关闭和身份错误不会被采集成功掩盖。
package liepin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

type attachmentCompletionBrowser struct {
	liepinReplyBrowserStub
	closeErr         error
	identityErr      error
	downloadErr      error
	cancelOnDownload context.CancelFunc
	downloaded       bool
}

// Read 模拟附件正文不可读，以及附件下载后的最终身份核对失败。
func (b *attachmentCompletionBrowser) Read(ctx context.Context, request contract.ElementReadRequest) (contract.ReadResult, error) {
	if request.Selector.Description == "测试附件正文" {
		return contract.ReadResult{}, nil
	}
	if b.downloaded && request.Selector.Description == "测试当前候选人" && b.identityErr != nil {
		return contract.ReadResult{}, b.identityErr
	}
	return b.liepinReplyBrowserStub.Read(ctx, request)
}

// Click 模拟附件关闭动作无法确认，不允许以已下载文件掩盖页面错误。
func (b *attachmentCompletionBrowser) Click(ctx context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	if request.Selector.Description == "测试附件关闭" && b.closeErr != nil {
		return contract.ClickResult{}, b.closeErr
	}
	return b.liepinReplyBrowserStub.Click(ctx, request)
}

// Downloads 在返回本地文件记录时注入取消或下载错误，验证采集完成边界。
func (b *attachmentCompletionBrowser) Downloads(ctx context.Context) (contract.DownloadListResult, error) {
	if b.downloadErr != nil {
		return contract.DownloadListResult{}, b.downloadErr
	}
	b.downloaded = true
	if b.cancelOnDownload != nil {
		b.cancelOnDownload()
	}
	return b.liepinReplyBrowserStub.Downloads(ctx)
}

// TestCollectAttachmentFileOnlyAndUnsafeCompletion 验证空页面正文仍可返回真实文件，取消、关闭和身份错误则必须保留。
func TestCollectAttachmentFileOnlyAndUnsafeCompletion(t *testing.T) {
	workerErr := &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "WORKER_UNRESPONSIVE", Message: "浏览器动作无法确认"}}
	for _, scenario := range []string{"file_only", "cancelled", "close_failed", "identity_failed", "download_failed"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "resume.pdf")
			if err := os.WriteFile(path, []byte("test resume file"), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			browser := &attachmentCompletionBrowser{liepinReplyBrowserStub: liepinReplyBrowserStub{
				conversationOpen: true, downloadPath: path,
			}}
			var expectedErr error
			switch scenario {
			case "cancelled":
				browser.cancelOnDownload, expectedErr = cancel, context.Canceled
			case "close_failed":
				browser.closeErr, expectedErr = workerErr, workerErr
			case "identity_failed":
				browser.identityErr, expectedErr = workerErr, workerErr
			case "download_failed":
				browser.downloadErr, expectedErr = workerErr, workerErr
			}
			cfg := model.Config{Name: "猎聘企业端", Selectors: map[string]contract.SelectorSpec{
				"message.current_name":             testLiepinSelector("测试当前候选人"),
				"message.current_position":         testLiepinSelector("测试当前岗位"),
				"message.resume_attachment_entry":  testLiepinSelector("测试附件入口"),
				"message.attachment_preview":       testLiepinSelector("测试附件预览"),
				"message.attachment_body":          testLiepinSelector("测试附件正文"),
				"message.attachment_download":      testLiepinSelector("测试附件下载"),
				"message.attachment_preview_close": testLiepinSelector("测试附件关闭"),
				"message.history_scroll":           testLiepinSelector("测试聊天滚动区域"),
				"message.resume_online_entry":      testLiepinSelector("测试在线简历入口"),
			}}
			bundle, err := (&Runtime{}).CollectAutoReplyResume(ctx, browser, cfg, model.AutoReplyConversationSnapshot{
				CandidateName: "邓云川", CommunicationPosition: "测试岗位", ResumeCardAvailable: true,
				ResumeSourceMessageID: "attachment-source",
			})
			if expectedErr == nil {
				if err != nil || len(bundle.AttachmentPaths) != 1 || bundle.AttachmentPaths[0] != path || bundle.OnlineResumeText != "" || bundle.ResumeSourceMessageID != "attachment-source" {
					t.Fatalf("真实附件不能独立返回：bundle=%+v err=%v", bundle, err)
				}
			} else if !errors.Is(err, expectedErr) || len(bundle.AttachmentPaths) != 0 {
				t.Fatalf("采集错误被附件成功掩盖：bundle=%+v err=%v expected=%v", bundle, err, expectedErr)
			}
			for _, click := range browser.clicks {
				if click == "测试在线简历入口" {
					t.Fatalf("附件采集路径不应额外打开在线简历：%v", browser.clicks)
				}
			}
		})
	}
}

type blockedAttachmentBrowser struct {
	liepinReplyBrowserStub
	scrollErr error
}

// Scroll 模拟已找到附件但真实滚轮无法抵达，供点击前停止边界验证。
func (b *blockedAttachmentBrowser) Scroll(context.Context, contract.ScrollRequest) (contract.ScrollResult, error) {
	return contract.ScrollResult{}, b.scrollErr
}

// TestAttachmentScrollFailureNeverClicks 验证长历史滚动受阻和取消不会落入三次点击重试。
func TestAttachmentScrollFailureNeverClicks(t *testing.T) {
	for _, failure := range []error{errors.New("滚轮无法到达附件"), context.Canceled} {
		browser := &blockedAttachmentBrowser{scrollErr: failure}
		cfg := model.Config{Name: "猎聘企业端", Selectors: map[string]contract.SelectorSpec{
			"message.resume_attachment_entry": testLiepinSelector("测试附件入口"),
			"message.attachment_preview":      testLiepinSelector("测试附件预览"),
			"message.history_scroll":          testLiepinSelector("测试聊天滚动区域"),
		}}
		err := openLiepinAttachmentPreview(context.Background(), browser, cfg)
		if !errors.Is(err, failure) || len(browser.clicks) != 0 {
			t.Fatalf("滚动失败仍继续点击：err=%v clicks=%v", err, browser.clicks)
		}
	}
}
