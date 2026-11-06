// Package liepin 本文件验证简历来源按卡片出现时机读取，避免为每页旧消息重复读取正文或无谓返回聊天底部。
package liepin

import (
	"context"
	"fmt"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
)

// resumeHistoryBrowser 只模拟已加载DOM与标准卡片探测，记录完整字段读取次数。
type resumeHistoryBrowser struct {
	liepinReplyBrowserStub
	fullReads int
	probes    int
}

// FindAll 根据当前已加载批次返回附件入口存在性，不在测试中执行真实浏览器操作。
func (b *resumeHistoryBrowser) FindAll(ctx context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if request.Selector.Description == "测试附件入口" {
		b.probes++
		if !request.ProbeOnly || !request.ExpectedMissing || request.Selector.State != "attached" {
			return nil, fmt.Errorf("简历探测没有使用只读attached契约")
		}
		for _, item := range b.historyBatches[min(b.historyBatch, len(b.historyBatches)-1)] {
			if item.Fields["resume_card"] != "" {
				return []contract.FindAllItem{{Index: 0}}, nil
			}
		}
		return nil, nil
	}
	if !request.ProbeOnly && request.MaxItems > 1 {
		b.fullReads++
	}
	return b.liepinReplyBrowserStub.FindAll(ctx, request)
}

// TestPrepareLiepinResumeReadsOnlyAfterCardAppears 验证20→40→60→80只完整读一次，且不再回到底部后重复上翻。
func TestPrepareLiepinResumeReadsOnlyAfterCardAppears(t *testing.T) {
	items := make([]contract.FindAllItem, 80)
	for i := range items {
		items[i] = identityTestMessage(fmt.Sprintf("合成消息%d", i), fmt.Sprintf("8月6日 %02d:%02d", 10+i/60, i%60))
	}
	items[10].Fields["resume_card"] = "在线简历 附件简历"
	parsed, err := liepinMessages(items)
	if err != nil {
		t.Fatal(err)
	}
	browser := &resumeHistoryBrowser{liepinReplyBrowserStub: liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{items[60:], items[40:], items[20:], items}}}
	cfg := historyAuditConfig()
	cfg.Selectors["message.resume_attachment_entry"] = testLiepinSelector("测试附件入口")
	if err = prepareLiepinResumeSource(context.Background(), browser, cfg, parsed[10].Key); err != nil {
		t.Fatal(err)
	}
	if browser.fullReads != 1 || browser.probes != 4 || len(browser.historyScrolls) != 3 {
		t.Fatalf("重复读正文或没有逐页探测: full=%d probes=%d scrolls=%d", browser.fullReads, browser.probes, len(browser.historyScrolls))
	}
	for _, scroll := range browser.historyScrolls {
		if scroll.Distance != -1600 || scroll.ProgressTarget == nil || scroll.MaxAttempts != 1 {
			t.Fatalf("来源准备改变了真实滚轮约束或先回底部: %+v", scroll)
		}
	}
}

// TestPrepareLiepinResumeRejectsWrongSourceAndCanceledTask 验证错源与取消都不会继续滚动或进入附件点击。
func TestPrepareLiepinResumeRejectsWrongSourceAndCanceledTask(t *testing.T) {
	items := []contract.FindAllItem{identityTestMessage("较早锚点", "10:00"), identityTestMessage("合成简历", "10:01")}
	items[1].Fields["resume_card"] = "在线简历 附件简历"
	cfg := historyAuditConfig()
	cfg.Selectors["message.resume_attachment_entry"] = testLiepinSelector("测试附件入口")
	for _, cancelled := range []bool{false, true} {
		browser := &resumeHistoryBrowser{liepinReplyBrowserStub: liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{items}}}
		ctx, cancel := context.WithCancel(context.Background())
		if cancelled {
			cancel()
		}
		err := prepareLiepinResumeSource(ctx, browser, cfg, "wrong-source")
		cancel()
		if err == nil || len(browser.clicks) != 0 || len(browser.historyScrolls) != 0 {
			t.Fatal("不可靠来源仍继续操作", err)
		}
		if cancelled && (browser.fullReads != 0 || browser.probes != 0) {
			t.Fatal("取消后仍读取页面")
		}
	}
}

// TestPrepareLiepinResumeStopsAtProvenTopWithoutCard 验证没有附件时只读取首尾并保留三次真实顶部判断。
func TestPrepareLiepinResumeStopsAtProvenTopWithoutCard(t *testing.T) {
	browser := &resumeHistoryBrowser{liepinReplyBrowserStub: liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{{identityTestMessage("合成消息", "10:00")}}}}
	cfg := historyAuditConfig()
	cfg.Selectors["message.resume_attachment_entry"] = testLiepinSelector("测试附件入口")
	if err := prepareLiepinResumeSource(context.Background(), browser, cfg, "missing-source"); err == nil {
		t.Fatal("未找到附件却成功")
	}
	if browser.fullReads != 0 || len(browser.historyScrolls) < 3 {
		t.Fatalf("缺卡片重复读全文或顶部未确认: full=%d scrolls=%d", browser.fullReads, len(browser.historyScrolls))
	}
}
