// Package liepin 本文件用模拟页面验证聊天顶部证据、尾部截限、恢复最新端与取消边界，不操作真实招聘页面。
package liepin

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// historyAuditConfig 返回仅包含消息列表与滚动容器的测试配置。
func historyAuditConfig() model.Config {
	return model.Config{ID: "liepin", Name: "猎聘企业端", Selectors: map[string]contract.SelectorSpec{
		"message.item":           testLiepinSelector("测试聊天消息"),
		"message.history_scroll": testLiepinSelector("测试聊天滚动区域"),
	}}
}

// historyAuditMessage 构造带稳定编号的候选人消息，避免依赖真实候选人资料。
func historyAuditMessage(id string) contract.FindAllItem {
	return contract.FindAllItem{Fields: map[string]string{
		"body_class": "im-ui-message-item-receive", "message_text": id,
		"message_meta": fmt.Sprintf(`{"message_id":%q}`, id),
	}}
}

// TestLiepinHistoryKeepsScrollingWhileLongDOMMoves 验证DOM连续三轮不变但仍在移动时不会提前宣告读完。
func TestLiepinHistoryKeepsScrollingWhileLongDOMMoves(t *testing.T) {
	recent := []contract.FindAllItem{historyAuditMessage("recent")}
	all := append([]contract.FindAllItem{historyAuditMessage("old")}, recent...)
	browser := &liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{recent, recent, recent, recent, recent, all}}
	result, err := readLiepinConversationHistoryDetail(context.Background(), browser, historyAuditConfig(), nil, 5000)
	if err != nil || !result.HistoryComplete || len(result.Messages) != 2 || browser.historyBatch < 8 {
		t.Fatalf("提前把滚动当作结束: messages=%d complete=%v scrolls=%d err=%v", len(result.Messages), result.HistoryComplete, browser.historyBatch, err)
	}
	last := browser.historyScrolls[len(browser.historyScrolls)-1]
	if last.Distance <= 0 || !last.StopWhenProgressVisible || last.ProgressTarget == nil || *last.ProgressTarget.Target.Index != 1 {
		t.Fatalf("完成历史读取后没有恢复最新端: %+v", last)
	}
}

// TestLiepinHistoryCapPreservesNewestAndDoesNotClaimComplete 验证超过上限时仍包含真正末条消息且不虚报全量历史。
func TestLiepinHistoryCapPreservesNewestAndDoesNotClaimComplete(t *testing.T) {
	items := make([]contract.FindAllItem, 5003)
	for i := range items {
		items[i] = historyAuditMessage(fmt.Sprintf("message-%d", i))
	}
	browser := &liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{items}}
	result, err := readLiepinConversationHistoryDetail(context.Background(), browser, historyAuditConfig(), nil, 5000)
	if err != nil || result.HistoryComplete || len(result.Messages) != 5000 || result.Messages[0].PlatformMessageID != "message-3" || result.Messages[4999].PlatformMessageID != "message-5002" {
		t.Fatalf("截限没有保留尾部: count=%d complete=%v err=%v", len(result.Messages), result.HistoryComplete, err)
	}
	latest, err := readLiepinLatestAutoReplyMessage(context.Background(), browser, historyAuditConfig())
	if err != nil || latest.PlatformMessageID != "message-5002" {
		t.Fatalf("最新消息读取失真: %s %v", latest.PlatformMessageID, err)
	}
}

// TestLiepinHistoryStopsAtKnownBoundaryWithoutClimbing 验证已知消息命中后只同步新消息并直接检查末端。
func TestLiepinHistoryStopsAtKnownBoundaryWithoutClimbing(t *testing.T) {
	browser := &liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{{historyAuditMessage("known"), historyAuditMessage("new")}}}
	result, err := readLiepinConversationHistoryDetail(context.Background(), browser, historyAuditConfig(), []string{"known"}, 5000)
	if err != nil || result.HistoryComplete || len(result.Messages) != 1 || result.Messages[0].PlatformMessageID != "new" || browser.historyBatch != 0 {
		t.Fatalf("已知边界仍翻历史: %+v %v", result, err)
	}
}

// TestLiepinHistoryRejectsMissingProgressAndBlockedScroll 验证缺少几何证据、卡住、轮数耗尽时均不报告完整。
func TestLiepinHistoryRejectsMissingProgressAndBlockedScroll(t *testing.T) {
	for _, kind := range []string{"missing", "blocked", "limit"} {
		t.Run(kind, func(t *testing.T) {
			browser := &liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{{historyAuditMessage("recent")}}, historyMissingProgress: kind == "missing", historyBlocked: kind == "blocked", historyAlwaysMoving: kind == "limit"}
			result, err := readLiepinConversationHistoryDetail(context.Background(), browser, historyAuditConfig(), nil, 5000)
			if err == nil || result.HistoryComplete {
				t.Fatalf("不完整历史被当作成功: %+v %v", result, err)
			}
		})
	}
}

// TestLiepinHistoryEmptyAndCancelledDoNotScroll 验证没有消息或任务取消时不继续操作页面。
func TestLiepinHistoryEmptyAndCancelledDoNotScroll(t *testing.T) {
	browser := &liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{{}}}
	if _, err := readLiepinConversationHistoryDetail(context.Background(), browser, historyAuditConfig(), nil, 5000); err == nil {
		t.Fatal("空消息应明确返回错误")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readLiepinConversationHistoryDetail(ctx, browser, historyAuditConfig(), nil, 5000); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消未传递: %v", err)
	}
	if len(browser.historyScrolls) != 0 {
		t.Fatal("无消息或取消后仍操作页面")
	}
}

// changingHistoryTailBrowser 模拟恢复尾部期间异步加载旧消息，导致末条DOM序号发生变化。
type changingHistoryTailBrowser struct{ liepinReplyBrowserStub }

// Scroll 在首次恢复时模拟新节点插入，仍使用基础替身返回几何证据。
func (b *changingHistoryTailBrowser) Scroll(ctx context.Context, request contract.ScrollRequest) (contract.ScrollResult, error) {
	if request.Distance > 0 && b.historyBatch == 0 {
		b.historyBatch++
	}
	return b.liepinReplyBrowserStub.Scroll(ctx, request)
}

// TestLiepinHistoryRestoresActualTailAfterAsyncInsert 验证恢复过程中序号变化后重新定位真正末条。
func TestLiepinHistoryRestoresActualTailAfterAsyncInsert(t *testing.T) {
	browser := &changingHistoryTailBrowser{liepinReplyBrowserStub: liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{
		{historyAuditMessage("recent")}, {historyAuditMessage("older"), historyAuditMessage("recent")},
	}}}
	if err := restoreLiepinLatestMessages(context.Background(), browser, historyAuditConfig()); err != nil {
		t.Fatal(err)
	}
	if len(browser.historyScrolls) != 2 || *browser.historyScrolls[0].ProgressTarget.Target.Index != 0 || *browser.historyScrolls[1].ProgressTarget.Target.Index != 1 {
		t.Fatalf("异步插入后没有重新定位末条: %+v", browser.historyScrolls)
	}
}
