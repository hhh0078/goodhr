// Package lifecycle 本文件用正式猎聘运行时和日志包装验证下载切到 PDF 后的原页恢复、身份检查及失败清理。
package lifecycle

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

type attachmentPageBrowser struct {
	model.Browser
	cfg         model.Config
	scenario    string
	filePath    string
	originURL   string
	downloaded  bool
	pdfCurrent  bool
	previewOpen bool
	usedPageID  string
	closeCount  int
	closePages  int
	cancel      context.CancelFunc
	downloadErr error
	receipt     chan contract.DownloadReceipt
	viewerWatch bool
	unwatched   bool
}

const syntheticAttachmentURL = "https://example.invalid/synthetic-resume.pdf"

// ListPages 模拟下载打开 PDF，同时其他标签关闭导致原页序号变化。
func (b *attachmentPageBrowser) ListPages(context.Context) (contract.PageListResult, error) {
	viewer := (&url.URL{Scheme: "file", Path: filepath.ToSlash(b.filePath)}).String()
	if !b.downloaded {
		pages := []contract.PageInfo{{PageID: "0", URL: b.originURL, Current: true}}
		switch b.scenario {
		case "existing_viewer":
			pages = append(pages, contract.PageInfo{PageID: "1", URL: viewer})
		case "initial_duplicate_origin":
			pages = append(pages, contract.PageInfo{PageID: "1", URL: b.originURL})
		case "initial_multiple_current":
			pages = append(pages, contract.PageInfo{PageID: "1", URL: "https://example.invalid/other", Current: true})
		case "initial_no_current":
			pages[0].Current = false
		}
		return contract.PageListResult{Pages: pages, Count: len(pages)}, nil
	}
	pages := []contract.PageInfo{{PageID: "7", URL: b.originURL, Current: !b.pdfCurrent}, {PageID: "8", URL: viewer, Current: b.pdfCurrent}}
	switch b.scenario {
	case "delayed_viewer":
		pages[1].URL = "about:blank"
	case "duplicate_file":
		pages = append(pages, contract.PageInfo{PageID: "10", URL: viewer})
	case "missing_origin", "download_and_restore_error", "cancel_and_restore_error":
		pages = pages[1:]
	case "duplicate_origin":
		pages = append(pages, contract.PageInfo{PageID: "9", URL: b.originURL})
	}
	return contract.PageListResult{Pages: pages, Count: len(pages)}, nil
}

// UsePage 只允许使用重新枚举得到的原页序号，并可模拟枚举后地址发生变化。
func (b *attachmentPageBrowser) UsePage(ctx context.Context, request contract.PageUseRequest) (contract.PageInfo, error) {
	if err := ctx.Err(); err != nil {
		return contract.PageInfo{}, err
	}
	b.usedPageID = request.PageID
	if request.PageID != "7" || request.DismissDownload == nil || request.DismissDownload.OriginalURL != b.originURL || request.DismissDownload.DownloadID != "native_download_proof" ||
		(!b.viewerWatch || !request.DismissDownload.AwaitViewer || (b.scenario != "delayed_viewer" && request.DismissDownload.ViewerURL != (&url.URL{Scheme: "file", Path: filepath.ToSlash(b.filePath)}).String())) {
		return contract.PageInfo{}, errors.New("误用下载前的旧页面序号")
	}
	b.pdfCurrent = false
	if request.DismissDownload.KeepViewer != (b.scenario == "existing_viewer") {
		return contract.PageInfo{}, errors.New("错误尝试关闭预先存在的文件页")
	}
	page := contract.PageInfo{PageID: "7", URL: b.originURL, Current: true, DownloadViewerClosed: !request.DismissDownload.KeepViewer, DownloadViewerPreserved: request.DismissDownload.KeepViewer}
	if b.scenario == "returned_url_changed" {
		page.URL = "https://example.invalid/other"
	}
	if b.scenario == "returned_not_current" {
		page.Current = false
	}
	return page, nil
}

// Read 仅在原页提供合成身份；读取 PDF 的招聘元素必须直接失败。
func (b *attachmentPageBrowser) Read(_ context.Context, request contract.ElementReadRequest) (contract.ReadResult, error) {
	if b.pdfCurrent {
		return contract.ReadResult{}, errors.New("错误地在 PDF 页读取招聘元素")
	}
	if request.Attribute == "data-tlg-scm" {
		cid := "target-cid"
		if b.downloaded && b.scenario == "wrong_cid" {
			cid = "other-cid"
		}
		return contract.ReadResult{Value: "cid=" + cid}, nil
	}
	if request.Selector.Description == b.cfg.Selectors["message.current_name"].Description {
		return contract.ReadResult{Value: "合成候选人"}, nil
	}
	if request.Selector.Description == b.cfg.Selectors["message.current_position"].Description {
		return contract.ReadResult{Value: "合成岗位"}, nil
	}
	if request.Selector.Description == b.cfg.Selectors["message.attachment_download"].Description && request.Attribute == "href" {
		return contract.ReadResult{Value: syntheticAttachmentURL}, nil
	}
	return contract.ReadResult{}, nil
}

// FindAll 在原招聘页分别探测聊天姓名和附件预览，避免把“尚未打开预览”误当成“聊天已关闭”。
func (b *attachmentPageBrowser) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if b.pdfCurrent {
		return nil, errors.New("错误地在 PDF 页判断附件已关闭")
	}
	if request.Selector.Description == b.cfg.Selectors["message.current_name"].Description {
		return []contract.FindAllItem{{Index: 0, TotalMatches: 1}}, nil
	}
	if b.previewOpen {
		return []contract.FindAllItem{{Index: 0}}, nil
	}
	return nil, nil
}

// Click 模拟附件预览、下载和关闭，不允许点击在线简历或在 PDF 上关闭预览。
func (b *attachmentPageBrowser) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	if b.pdfCurrent {
		return contract.ClickResult{}, errors.New("错误地在 PDF 页点击招聘元素")
	}
	switch request.Selector.Description {
	case b.cfg.Selectors["message.resume_attachment_entry"].Description:
		b.previewOpen = true
	case b.cfg.Selectors["message.attachment_download"].Description:
		b.viewerWatch = request.WatchDownloadViewer
		if b.receipt == nil || b.unwatched {
			return contract.ClickResult{}, errors.New("没有在真实点击前订阅下载")
		}
		b.downloaded, b.pdfCurrent = true, b.scenario != "logical_original"
		if b.cancel != nil {
			b.cancel()
		} else {
			b.receipt <- contract.DownloadReceipt{Err: b.downloadErr, Record: contract.DownloadRecord{ID: "native_download_proof", Status: "saved", FilePath: b.filePath}}
		}
	case b.cfg.Selectors["message.attachment_preview_close"].Description:
		b.closeCount++
		b.previewOpen = false
	default:
		return contract.ClickResult{}, errors.New("出现预期外点击")
	}
	return contract.ClickResult{Clicked: true}, nil
}

// Scroll 模拟已经抵达已加载的附件入口，不操作真实浏览器。
func (b *attachmentPageBrowser) Scroll(context.Context, contract.ScrollRequest) (contract.ScrollResult, error) {
	return contract.ScrollResult{}, nil
}

// WatchDownload 只允许预览当前页附件时按实际链接订阅，直到下载点击后才投递回执。
func (b *attachmentPageBrowser) WatchDownload(page, file string, ignoredQuery ...string) (<-chan contract.DownloadReceipt, func(), error) {
	if !b.previewOpen || b.downloaded || b.receipt != nil || page != b.originURL || file != syntheticAttachmentURL {
		return nil, nil, errors.New("下载订阅时机或附件归属不正确")
	}
	b.receipt = make(chan contract.DownloadReceipt, 1)
	return b.receipt, func() { b.unwatched = true }, nil
}

// PressKey 验证快捷键绑定原页及弹层；本替身保留预览，以覆盖随后按钮关闭的备用路径。
func (b *attachmentPageBrowser) PressKey(_ context.Context, r contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	if !b.previewOpen || b.pdfCurrent || r.Key != "Escape" || r.ExpectedPageURL != b.originURL || r.Target == nil {
		return contract.KeyboardPressResult{}, errors.New("快捷键没有绑定正确的预览")
	}
	return contract.KeyboardPressResult{Pressed: true}, nil
}

// ClosePage 记录不应发生的标签关闭，保护下载后新开的 PDF。
func (b *attachmentPageBrowser) ClosePage(context.Context) error {
	b.closePages++
	return errors.New("本流程不得关闭 PDF 或招聘标签页")
}

// TestLoggedLiepinAttachmentRestoresOriginalPage 验证正式包装链保留切页能力，并阻止歧义、身份变化及清理失败掩盖原错误。
func TestLoggedLiepinAttachmentRestoresOriginalPage(t *testing.T) {
	for _, scenario := range []string{"success", "logical_original", "delayed_viewer", "duplicate_file", "existing_viewer", "initial_duplicate_origin", "initial_multiple_current", "initial_no_current", "missing_origin", "duplicate_origin", "returned_url_changed", "returned_not_current", "wrong_cid", "download_error", "cancelled", "download_and_restore_error", "cancel_and_restore_error"} {
		t.Run(scenario, func(t *testing.T) {
			cfg, err := platform.LoadConfig("liepin")
			if err != nil {
				t.Fatal(err)
			}
			// 历史加载已有独立回归，本用例从已核实的附件卡片开始。
			cfg.Behavior.AutoReplyResumeLoadsHistory = false
			runtime, err := platform.RuntimeFor("liepin")
			if err != nil {
				t.Fatal(err)
			}
			wrapped := newLoggedRuntime(runtime, "synthetic-attachment-task", &runtimeLogCollector{}).(model.AutoReplyRuntime)
			filePath := filepath.Join(t.TempDir(), "resume.pdf")
			if err = os.WriteFile(filePath, []byte("synthetic resume bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			browser := &attachmentPageBrowser{cfg: cfg, scenario: scenario, filePath: filePath, originURL: cfg.EntryURL}
			var primaryErr error
			if scenario == "download_error" || scenario == "download_and_restore_error" {
				primaryErr = errors.New("合成下载失败")
				browser.downloadErr = primaryErr
			}
			if scenario == "cancelled" || scenario == "cancel_and_restore_error" {
				primaryErr, browser.cancel = context.Canceled, cancel
			}
			bundle, err := wrapped.CollectAutoReplyResume(ctx, browser, cfg, model.AutoReplyConversationSnapshot{
				CandidateName: "合成候选人", PlatformCandidateID: "target-cid", CommunicationPosition: "合成岗位",
				ResumeCardAvailable: true, ResumeSourceMessageID: "source-message",
			})
			if scenario == "success" || scenario == "logical_original" || scenario == "existing_viewer" || scenario == "delayed_viewer" {
				if err != nil || len(bundle.AttachmentPaths) != 1 || bundle.AttachmentPaths[0] != filePath {
					t.Fatalf("正式包装未返回原附件：bundle=%+v err=%v", bundle, err)
				}
			} else if err == nil || len(bundle.AttachmentPaths) != 0 {
				t.Fatalf("页面不确定仍返回采集成功：bundle=%+v err=%v", bundle, err)
			}
			if primaryErr != nil && !errors.Is(err, primaryErr) {
				t.Fatalf("清理掩盖了下载或取消主错误：err=%v primary=%v", err, primaryErr)
			}
			if (browser.receipt != nil) != browser.downloaded || browser.unwatched != browser.downloaded {
				t.Fatalf("订阅与实际下载不对应：downloaded=%t watched=%t stopped=%t", browser.downloaded, browser.receipt != nil, browser.unwatched)
			}
			wantClose := scenario == "success" || scenario == "logical_original" || scenario == "existing_viewer" || scenario == "delayed_viewer"
			if (browser.closeCount == 1) != wantClose || browser.closePages != 0 {
				t.Fatalf("未核实原页身份即关闭，或误关PDF：preview_closes=%d page_closes=%d", browser.closeCount, browser.closePages)
			}
			if wantClose && (browser.usedPageID != "7" || browser.pdfCurrent || browser.previewOpen) {
				t.Fatalf("未使用最新序号恢复并关闭原页预览：used=%q pdf=%t preview=%t", browser.usedPageID, browser.pdfCurrent, browser.previewOpen)
			}
		})
	}
}
