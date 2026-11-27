// Package boss 本文件回归真实BOSS小窗口裁切聊天容器上沿导致历史采集反复失败的问题，禁止把容器外消息误判为到顶。
package boss

import (
	"context"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"testing"
)

// clippedHistoryBrowser 用真实故障的负坐标模拟视口裁切，保留原消息读取和真实滚轮计数协议。
type clippedHistoryBrowser struct {
	*bossReplyTestBrowser
	container, head contract.ElementView
}

// Find 只返回当前消息区和首条的几何，不读取隐藏滚动属性。
func (b *clippedHistoryBrowser) Find(_ context.Context, r contract.ElementFindRequest) (contract.FindResult, error) {
	v := b.head
	if r.Selector.Description == "history" {
		v = b.container
	}
	return contract.FindResult{View: v}, nil
}

// Scroll 保留真实故障的边缘不可见、位置未动回执，防止测试先假设已经到顶。
func (b *clippedHistoryBrowser) Scroll(context.Context, contract.ScrollRequest) (contract.ScrollResult, error) {
	b.scrolls++
	visible, moved := false, false
	return contract.ScrollResult{ProgressTargetVisible: &visible, ProgressTargetMoved: &moved}, nil
}

// TestBossClippedHistoryTop 验证首条在容器内且部分可见才可经过两轮稳定滚动结束；尚在容器上方或完全离屏必须拒绝。
func TestBossClippedHistoryTop(t *testing.T) {
	for _, mode := range []string{"clipped", "above_container", "outside_viewport", "hidden"} {
		t.Run(mode, func(t *testing.T) {
			base, cfg := bossMessagesFixture([]contract.FindAllItem{bossTestMessage("10:35", "item-friend", "合成来信")})
			box := contract.ElementView{Visible: true, Box: contract.ElementBox{X: 521, Y: -5.5, Width: 458, Height: 496}, Viewport: contract.ViewportSize{Width: 1280, Height: 779}}
			head := box
			head.Box = contract.ElementBox{X: 551, Y: -5.5, Width: 398, Height: 142}
			switch mode {
			case "above_container":
				head.Box.Y = -20
			case "outside_viewport":
				head.Box.Y = -200
			case "hidden":
				head.Visible = false
			}
			browser := &clippedHistoryBrowser{bossReplyTestBrowser: base, container: box, head: head}
			result, err := readBossHistoryWindow(context.Background(), browser, cfg, nil, 5000, nil)
			if mode == "clipped" {
				if err != nil || !result.HistoryComplete || len(result.Messages) != 1 || browser.scrolls != 2 {
					t.Fatalf("真实裁切未完成稳定采集：%+v %v scrolls=%d", result, err, browser.scrolls)
				}
			} else if err == nil || result.HistoryComplete {
				t.Fatal("容器外或完全离屏的消息被猜测为已到顶")
			}
		})
	}
}
