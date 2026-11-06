// Package liepin 文件作用：验证历史附件滚动失败或任务取消时，不继续点击或误触其他区域。
package liepin

import (
	"context"
	"errors"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

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
