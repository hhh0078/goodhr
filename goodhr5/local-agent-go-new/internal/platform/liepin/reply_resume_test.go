// Package liepin 文件作用：验证真实附件优先返回，以及滚动、取消、关闭和身份错误不会被采集成功掩盖。
package liepin

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"slices"
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

// WatchDownload 在订阅时注入取消或监听错误，验证归属核对与清理仍能执行。
func (b *attachmentCompletionBrowser) WatchDownload(page, file string, ignoredQuery ...string) (<-chan contract.DownloadReceipt, func(), error) {
	if b.downloadErr != nil {
		return nil, nil, b.downloadErr
	}
	b.downloaded = true
	if b.cancelOnDownload != nil {
		b.cancelOnDownload()
	}
	return b.liepinReplyBrowserStub.WatchDownload(page, file)
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
				"message.candidate_id_source":      testLiepinSelector("测试候选人编号"),
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
				CandidateName: "邓云川", PlatformCandidateID: "target-cid", CommunicationPosition: "测试岗位", ResumeCardAvailable: true,
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

// previewSourceBrowser 只返回当前可见附件预览的地址，验证没有 href 时仍能识别来源。
type previewSourceBrowser struct {
	liepinReplyBrowserStub
	src string
}

// Read 模拟标准 Locator 读取可见 PDF 预览框的 src 属性。
func (b *previewSourceBrowser) Read(_ context.Context, request contract.ElementReadRequest) (contract.ReadResult, error) {
	if request.Attribute != "src" || request.Selector.State != "visible" {
		return contract.ReadResult{}, errors.New("只允许读取可见预览文件")
	}
	return contract.ReadResult{Value: b.src}, nil
}

// TestAttachmentSourceFromVisibleViewer 验证直链、文档预览、缺失及存在多个文件地址的情况。
func TestAttachmentSourceFromVisibleViewer(t *testing.T) {
	body := testLiepinSelector("附件预览正文")
	body.Frames = []contract.SelectorGroup{{Selectors: []contract.SelectorCandidate{{Type: "css", Value: "iframe"}}}}
	cfg := model.Config{Selectors: map[string]contract.SelectorSpec{"message.attachment_body": body}}
	page := "https://recruit.example/jobs"
	file := "https://files.example/path/resume.pdf?dlFileName=a.pdf&signParams=s&tk=one&download=0"
	viewer := "https://preview.example/show?file=" + url.QueryEscape(file)
	b := &previewSourceBrowser{src: viewer}
	for _, href := range []string{"", "javascript:void(0)", "#"} {
		got, ignored, err := liepinAttachmentDownloadSource(context.Background(), b, cfg, page, href)
		if err != nil || got != file || !slices.Equal(ignored, []string{"tk", "download"}) {
			t.Fatalf("无法核对预览来源: %v %v", ignored, err)
		}
	}
	got, ignored, err := liepinAttachmentDownloadSource(context.Background(), b, cfg, page, "/direct.pdf")
	if err != nil || got != "https://recruit.example/direct.pdf" || len(ignored) != 0 {
		t.Fatal("直链应精确匹配")
	}
	for _, bad := range []string{"", viewer + "&file=" + url.QueryEscape(file), "https://preview.example/show?file=" + url.QueryEscape("https://files.example/")} {
		b.src = bad
		if _, _, err := liepinAttachmentDownloadSource(context.Background(), b, cfg, page, ""); err == nil {
			t.Fatal("无法唯一核对的文件不应下载")
		}
	}
}
