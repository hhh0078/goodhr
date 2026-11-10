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
	}, ConversationFields: map[string]contract.SelectorSpec{
		"message_meta": testLiepinSelector("消息编号"), "body_class": testLiepinSelector("消息方向"), "message_text": testLiepinSelector("消息正文"),
	}}
}

// countingHistoryBrowser 记录加载期间是否重复完整读取正文，并可模拟最终读取时插入新消息。
type countingHistoryBrowser struct {
	liepinReplyBrowserStub
	fullReads         int
	edgeReads         int
	changeOnFirstFull bool
}

// FindAll 验证轻读只含消息编号，完整读取时可触发一次异步列表变化。
func (b *countingHistoryBrowser) FindAll(ctx context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if !request.ProbeOnly && request.MaxItems > 1 {
		b.fullReads++
		if b.changeOnFirstFull && b.fullReads == 1 {
			items := append([]contract.FindAllItem(nil), b.historyBatches[min(b.historyBatch, len(b.historyBatches)-1)]...)
			b.historyBatches = [][]contract.FindAllItem{append(items, historyAuditMessage("concurrent-latest"))}
			b.historyBatch = 0
		}
	} else if !request.ProbeOnly {
		b.edgeReads++
		if len(request.Fields) != 1 {
			return nil, fmt.Errorf("轻读请求了 %d 个字段", len(request.Fields))
		}
		if _, found := request.Fields["message_meta"]; !found {
			return nil, fmt.Errorf("轻读缺少消息编号")
		}
	}
	return b.liepinReplyBrowserStub.FindAll(ctx, request)
}

// TestLiepinHistoryRechecksEdgesAfterFinalRead 验证完整读取期间新增消息时重新核实边界，不能返回旧首尾对应的结果。
func TestLiepinHistoryRechecksEdgesAfterFinalRead(t *testing.T) {
	browser := &countingHistoryBrowser{liepinReplyBrowserStub: liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{{historyAuditMessage("original")}}}, changeOnFirstFull: true}
	result, err := readLiepinConversationHistoryDetail(context.Background(), browser, historyAuditConfig(), nil, 5000)
	if err != nil || !result.HistoryComplete || len(result.Messages) != 2 || result.Messages[1].PlatformMessageID != "concurrent-latest" || browser.fullReads != 2 {
		t.Fatalf("最终读取期间的新消息没有被重新核实：messages=%+v full=%d err=%v", result.Messages, browser.fullReads, err)
	}
}

// TestLiepinHistoryLoadsEightyMessagesWithoutRepeatingFullReads 验证20→40→60→80累计加载只完整读一次，并保留真正最新消息。
func TestLiepinHistoryLoadsEightyMessagesWithoutRepeatingFullReads(t *testing.T) {
	all := make([]contract.FindAllItem, 80)
	for i := range all {
		all[i] = historyAuditMessage(fmt.Sprintf("message-%d", i))
	}
	browser := &countingHistoryBrowser{liepinReplyBrowserStub: liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{all[60:], all[40:], all[20:], all}}}
	result, err := readLiepinConversationHistoryDetail(context.Background(), browser, historyAuditConfig(), nil, 5000)
	if err != nil || !result.HistoryComplete || len(result.Messages) != 80 || result.Messages[79].PlatformMessageID != "message-79" || browser.fullReads != 1 || browser.edgeReads < 8 {
		t.Fatalf("历史加载重复读取或丢失最新消息：count=%d full=%d edge=%d complete=%v err=%v", len(result.Messages), browser.fullReads, browser.edgeReads, result.HistoryComplete, err)
	}
}

// TestLiepinHistoryLargerWheelsKeepAllMessagesAndTopProof 验证加快真实滚轮后仍完整读取89条，顶部等待和最新端核验不减省。
func TestLiepinHistoryLargerWheelsKeepAllMessagesAndTopProof(t *testing.T) {
	all := make([]contract.FindAllItem, 89)
	for i := range all {
		all[i] = historyAuditMessage(fmt.Sprintf("message-%d", i))
	}
	browser := &countingHistoryBrowser{liepinReplyBrowserStub: liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{all[69:], all[49:], all[29:], all}}}
	result, err := readLiepinConversationHistoryDetail(context.Background(), browser, historyAuditConfig(), nil, 5000)
	if err != nil || !result.HistoryComplete || len(result.Messages) != 89 || result.Messages[88].PlatformMessageID != "message-88" || browser.fullReads != 1 {
		t.Fatalf("增大滚轮距离后漏读记录：count=%d full=%d complete=%v err=%v", len(result.Messages), browser.fullReads, result.HistoryComplete, err)
	}
	travel, edge := 0, 0
	for _, request := range browser.historyScrolls {
		if request.Distance >= 0 {
			continue
		}
		if request.Distance != -1600 || request.ProgressTarget == nil || request.WheelAnchor == nil || request.MaxAttempts != 1 {
			t.Fatalf("向上加载绕过了逐轮真实进度：%+v", request)
		}
		switch request.WaitMS {
		case 300:
			travel++
		case 800:
			edge++
		default:
			t.Fatalf("历史等待时间不正确：%d", request.WaitMS)
		}
	}
	if travel == 0 || edge < 2 {
		t.Fatalf("没有保留顶部异步加载等待：travel=%d edge=%d", travel, edge)
	}
	last := browser.historyScrolls[len(browser.historyScrolls)-1]
	if last.Distance != 2400 || !last.StopWhenProgressVisible || last.ProgressTarget == nil || *last.ProgressTarget.Target.Index != 88 {
		t.Fatalf("没有按真正末条恢复最新端：%+v", last)
	}
}

// TestLiepinHistoryChecksKnownBoundaryOnlyWhenWindowChanges 验证纯滚动不重复核对全量字段，已知消息加载出来后立即停止。
func TestLiepinHistoryChecksKnownBoundaryOnlyWhenWindowChanges(t *testing.T) {
	recent := []contract.FindAllItem{historyAuditMessage("new")}
	older := []contract.FindAllItem{historyAuditMessage("older"), historyAuditMessage("known"), historyAuditMessage("new")}
	browser := &countingHistoryBrowser{liepinReplyBrowserStub: liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{recent, recent, recent, recent, older}}}
	result, err := readLiepinConversationHistoryDetail(context.Background(), browser, historyAuditConfig(), []string{"known"}, 5000)
	if err != nil || result.HistoryComplete || len(result.Messages) != 1 || result.Messages[0].PlatformMessageID != "new" || browser.fullReads != 2 {
		t.Fatalf("差量边界没有缓存或停止不正确：messages=%+v full=%d err=%v", result.Messages, browser.fullReads, err)
	}
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
