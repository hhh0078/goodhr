// Package lifecycle 本文件用正式猎聘运行时和日志包装验证下载切到 PDF 后的原页恢复、身份检查及失败清理。
package lifecycle

import (
	"context"
	"errors"
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
}

// ListPages 模拟下载打开 PDF，同时其他标签关闭导致原页序号变化。
func (b *attachmentPageBrowser) ListPages(context.Context) (contract.PageListResult, error) {
	if !b.downloaded {
		pages := []contract.PageInfo{{PageID: "0", URL: b.originURL, Current: true}}
		switch b.scenario {
		case "initial_duplicate_origin":
			pages = append(pages, contract.PageInfo{PageID: "1", URL: b.originURL})
		case "initial_multiple_current":
			pages = append(pages, contract.PageInfo{PageID: "1", URL: "https://example.invalid/other", Current: true})
		case "initial_no_current":
			pages[0].Current = false
		}
		return contract.PageListResult{Pages: pages, Count: len(pages)}, nil
	}
	pages := []contract.PageInfo{{PageID: "7", URL: b.originURL, Current: !b.pdfCurrent}, {PageID: "8", URL: "file:///synthetic-resume.pdf", Current: b.pdfCurrent}}
	switch b.scenario {
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
	if request.PageID != "7" {
		return contract.PageInfo{}, errors.New("误用下载前的旧页面序号")
	}
	b.pdfCurrent = false
	page := contract.PageInfo{PageID: "7", URL: b.originURL, Current: true}
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
	return contract.ReadResult{}, nil
}

// FindAll 只允许在已恢复的原招聘页检查附件预览是否消失。
func (b *attachmentPageBrowser) FindAll(_ context.Context, _ contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if b.pdfCurrent {
		return nil, errors.New("错误地在 PDF 页判断附件已关闭")
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
		b.downloaded, b.pdfCurrent = true, true
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

// Downloads 返回真实临时文件，并可在下载后模拟失败或任务取消。
func (b *attachmentPageBrowser) Downloads(context.Context) (contract.DownloadListResult, error) {
	if b.cancel != nil {
		b.cancel()
	}
	if b.downloadErr != nil {
		return contract.DownloadListResult{}, b.downloadErr
	}
	return contract.DownloadListResult{Downloads: []contract.DownloadRecord{{Status: "saved", FilePath: b.filePath}}}, nil
}

// ClearDownloads 仅模拟下载记录清理，不删除文件。
func (b *attachmentPageBrowser) ClearDownloads(context.Context) error { return nil }

// ClosePage 记录不应发生的标签关闭，保护下载后新开的 PDF。
func (b *attachmentPageBrowser) ClosePage(context.Context) error {
	b.closePages++
	return errors.New("本流程不得关闭 PDF 或招聘标签页")
}

// TestLoggedLiepinAttachmentRestoresOriginalPage 验证正式包装链保留切页能力，并阻止歧义、身份变化及清理失败掩盖原错误。
func TestLoggedLiepinAttachmentRestoresOriginalPage(t *testing.T) {
	for _, scenario := range []string{"success", "initial_duplicate_origin", "initial_multiple_current", "initial_no_current", "missing_origin", "duplicate_origin", "returned_url_changed", "returned_not_current", "wrong_cid", "download_error", "cancelled", "download_and_restore_error", "cancel_and_restore_error"} {
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
			if scenario == "success" {
				if err != nil || len(bundle.AttachmentPaths) != 1 || bundle.AttachmentPaths[0] != filePath {
					t.Fatalf("正式包装未返回原附件：bundle=%+v err=%v", bundle, err)
				}
			} else if err == nil || len(bundle.AttachmentPaths) != 0 {
				t.Fatalf("页面不确定仍返回采集成功：bundle=%+v err=%v", bundle, err)
			}
			if primaryErr != nil && !errors.Is(err, primaryErr) {
				t.Fatalf("清理掩盖了下载或取消主错误：err=%v primary=%v", err, primaryErr)
			}
			wantClose := scenario == "success" || scenario == "download_error" || scenario == "cancelled"
			if (browser.closeCount == 1) != wantClose || browser.closePages != 0 {
				t.Fatalf("未核实原页身份即关闭，或误关PDF：preview_closes=%d page_closes=%d", browser.closeCount, browser.closePages)
			}
			if wantClose && (browser.usedPageID != "7" || browser.pdfCurrent || browser.previewOpen) {
				t.Fatalf("未使用最新序号恢复并关闭原页预览：used=%q pdf=%t preview=%t", browser.usedPageID, browser.pdfCurrent, browser.previewOpen)
			}
		})
	}
}
