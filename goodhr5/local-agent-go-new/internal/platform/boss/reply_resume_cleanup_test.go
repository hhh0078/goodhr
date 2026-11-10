// Package boss 本文件验证附件收尾仅去掉相邻重复核身，Esc、备用关闭按钮和无预览返回仍保留实时身份及原页守卫。
package boss

import (
	"context"
	"errors"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// bossAttachmentCleanupBrowser 模拟原页、原生下载viewer和附件预览，不连接真实账号、下载器或文件。
type bossAttachmentCleanupBrowser struct {
	*bossReplyTestBrowser
	page                         string
	shown, escapeWorks, restored bool
	names, presses, closes, uses int
	onProbe, onPress, onUse      func()
}

// Read 统计完整核身中的姓名读取，避免收尾恢复与关闭前连续重复整套身份检查。
func (b *bossAttachmentCleanupBrowser) Read(ctx context.Context, request contract.ElementReadRequest) (contract.ReadResult, error) {
	if request.Selector.Description == "name" {
		b.names++
	}
	return b.bossReplyTestBrowser.Read(ctx, request)
}

// ListPages 返回唯一原页与当前viewer，供生产代码证明只恢复本次原页。
func (b *bossAttachmentCleanupBrowser) ListPages(context.Context) (contract.PageListResult, error) {
	pages := []contract.PageInfo{{PageID: "original", URL: "https://example.test/chat", Current: b.page == "https://example.test/chat"}}
	if b.page != "https://example.test/chat" {
		pages = append(pages, contract.PageInfo{PageID: "viewer", URL: b.page, Current: true})
	}
	return contract.PageListResult{Pages: pages}, nil
}

// UsePage 仅认可本次下载编号和原页，缺原生viewer证明时不伪造恢复成功。
func (b *bossAttachmentCleanupBrowser) UsePage(_ context.Context, request contract.PageUseRequest) (contract.PageInfo, error) {
	b.uses++
	if request.PageID != "original" || request.DismissDownload == nil || request.DismissDownload.OriginalURL != "https://example.test/chat" || request.DismissDownload.DownloadID != "download-1" || !request.DismissDownload.AwaitViewer {
		return contract.PageInfo{}, errors.New("原生下载回执不匹配")
	}
	b.page = "https://example.test/chat"
	if b.onUse != nil {
		b.onUse()
	}
	return contract.PageInfo{PageID: "original", URL: b.page, Current: true, DownloadViewerClosed: b.restored}, nil
}

// FindAll 只探测已配置附件预览，探测期间允许模拟外部换页以验证按键前守卫。
func (b *bossAttachmentCleanupBrowser) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if request.Selector.Description != "preview" || !request.ProbeOnly {
		return nil, errors.New("收尾不应读取聊天正文或其它弹层")
	}
	if b.onProbe != nil {
		change := b.onProbe
		b.onProbe = nil
		change()
	}
	if b.shown {
		return []contract.FindAllItem{{}}, nil
	}
	return nil, nil
}

// PressKey 仅允许原页、原预览的一次Esc；快捷键不适用时保留弹层供备用关闭验证。
func (b *bossAttachmentCleanupBrowser) PressKey(_ context.Context, request contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	if request.Key != "Escape" || request.Target == nil || request.Target.Description != "preview" || request.ExpectedPageURL != b.page || b.names == 0 {
		return contract.KeyboardPressResult{}, errors.New("Esc缺少原页或身份核验")
	}
	b.presses++
	if b.onPress != nil {
		b.onPress()
	}
	if !b.escapeWorks {
		return contract.KeyboardPressResult{}, errors.New("合成平台不支持Esc")
	}
	b.shown = false
	return contract.KeyboardPressResult{Pressed: true}, nil
}

// Click 仅在Esc后再次核身成功且预览仍在时接受一次带消失验证的备用关闭按钮。
func (b *bossAttachmentCleanupBrowser) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	if b.presses != 1 || b.names != 2 || !b.shown || request.Selector.Description != "close-preview" || request.Verify == nil || request.Verify.TargetHidden == nil || request.Verify.TargetHidden.Description != "preview" {
		return contract.ClickResult{}, errors.New("备用关闭缺少再次核身或原弹层证明")
	}
	b.closes++
	b.shown = false
	return contract.ClickResult{Clicked: true, Verified: true}, nil
}

// bossAttachmentCleanupFixture 构造身份和岗位可核实的原聊天，不提供真实文件或原生输入能力。
func bossAttachmentCleanupFixture() (*bossAttachmentCleanupBrowser, model.Config, model.AutoReplyConversationSnapshot) {
	base, cfg, snapshot := bossIdentityFixture()
	cfg.Selectors["message.attachment_preview"] = bossReplySelector("preview")
	cfg.Selectors["message.attachment_preview_close"] = bossReplySelector("close-preview")
	return &bossAttachmentCleanupBrowser{bossReplyTestBrowser: base, page: cfg.MessagesURL, shown: true, escapeWorks: true, restored: true}, cfg, snapshot
}

// TestBossAttachmentCleanupKeepsOneImmediateIdentity 验证正常Esc和预览已关都仅核身一次，备用按钮则在动作间隔后再次完整核身。
func TestBossAttachmentCleanupKeepsOneImmediateIdentity(t *testing.T) {
	for _, mode := range []string{"escape", "already_closed", "fallback", "restore_viewer"} {
		t.Run(mode, func(t *testing.T) {
			b, cfg, snapshot := bossAttachmentCleanupFixture()
			record := contract.DownloadRecord{}
			wantPresses, wantCloses, wantNames, wantUses := 1, 0, 1, 0
			switch mode {
			case "already_closed":
				b.shown, wantPresses = false, 0
			case "fallback":
				b.escapeWorks, wantCloses, wantNames = false, 1, 2
			case "restore_viewer":
				b.page, wantUses = "https://files.example.test/native-viewer", 1
				record = contract.DownloadRecord{ID: "download-1", Status: "saved", PageURL: cfg.MessagesURL}
			}
			err := finishBossAttachment(context.Background(), b, cfg, snapshot, cfg.MessagesURL, record)
			if err != nil || b.presses != wantPresses || b.closes != wantCloses || b.names != wantNames || b.uses != wantUses {
				t.Fatalf("相邻核身去重破坏必要检查：names=%d presses=%d closes=%d uses=%d err=%v", b.names, b.presses, b.closes, b.uses, err)
			}
		})
	}
}

// TestBossAttachmentCleanupRejectsChangedEvidence 验证换人、同名错号、换岗、切页和无效viewer回执均不会误关其它页面。
func TestBossAttachmentCleanupRejectsChangedEvidence(t *testing.T) {
	for _, mode := range []string{"other_name", "same_name_wrong_id", "job", "no_preview_wrong_person", "page_during_probe", "missing_receipt", "wrong_receipt_page", "wrong_receipt_id", "viewer_not_proven", "restore_other_person", "person_after_escape"} {
		t.Run(mode, func(t *testing.T) {
			b, cfg, snapshot := bossAttachmentCleanupFixture()
			record := contract.DownloadRecord{}
			wantPresses := 0
			switch mode {
			case "other_name":
				b.values["name"] = "其他候选人"
			case "same_name_wrong_id":
				b.values["candidate"] = "get_resume_999999999"
			case "job":
				b.values["position"] = "其他岗位"
			case "no_preview_wrong_person":
				b.shown, b.values["name"] = false, "其他候选人"
			case "page_during_probe":
				b.onProbe = func() { b.page = "https://example.test/other" }
			case "missing_receipt", "wrong_receipt_page", "wrong_receipt_id", "viewer_not_proven", "restore_other_person":
				b.page = "https://files.example.test/native-viewer"
				record = contract.DownloadRecord{ID: "download-1", Status: "saved", PageURL: cfg.MessagesURL}
				switch mode {
				case "missing_receipt":
					record = contract.DownloadRecord{}
				case "wrong_receipt_page":
					record.PageURL = "https://example.test/other"
				case "wrong_receipt_id":
					record.ID = "not-this-download"
				case "viewer_not_proven":
					b.restored = false
				case "restore_other_person":
					b.onUse = func() { b.values["name"] = "其他候选人" }
				}
			case "person_after_escape":
				b.escapeWorks, wantPresses = false, 1
				b.onPress = func() { b.values["name"] = "其他候选人" }
			}
			err := finishBossAttachment(context.Background(), b, cfg, snapshot, cfg.MessagesURL, record)
			if err == nil || b.presses != wantPresses || b.closes != 0 {
				t.Fatalf("页面或身份变化仍关闭：presses=%d closes=%d err=%v", b.presses, b.closes, err)
			}
		})
	}
}
