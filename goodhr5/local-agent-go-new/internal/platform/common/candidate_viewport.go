// Package common 文件作用：按平台配置只读定位当前屏幕起点，支持无完整卡片时以首行为备用，不追旧断点或预先滚动。
package common

import (
	"context"
	"fmt"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// StartCandidateViewport 在一次Worker只读请求中定位首张完整卡片；明确开启备用且无完整卡片时使用首行，动作时仍在原行核对身份。
func StartCandidateViewport(ctx context.Context, browser model.Browser, cfg model.Config, cursor *model.CandidateListCursor) error {
	if cursor == nil {
		return fmt.Errorf("候选人读取位置还没准备好")
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	selector, err := RequiredSelector(cfg, "candidate.item")
	if err != nil {
		return err
	}
	selector.TimeoutMS = 1500
	items, err := browser.FindAll(ctx, contract.ElementFindAllRequest{
		Selector: selector, MaxItems: 1, ExpectedMissing: true, ProbeOnly: true,
		ViewportStart: &contract.ViewportStartOptions{TopInset: cfg.Behavior.CandidateViewportTopInset, BottomInset: cfg.Behavior.CandidateViewportBottomInset},
	})
	if err != nil {
		return fmt.Errorf("读取当前候选人位置失败：%w", err)
	}
	if len(items) == 0 {
		if cfg.Behavior.CandidateViewportFallbackFirst {
			*cursor = model.CandidateListCursor{IdentityByIndex: true}
			return nil
		}
		return fmt.Errorf("当前没有完整显示的候选人卡片，请拉宽或拉高窗口、调整列表位置后再开始")
	}
	item := items[0]
	if item.View == nil {
		return fmt.Errorf("本地运行组件还没同步好，请完整退出并更新本地程序后再开始")
	}
	if item.Index < 0 || item.Index >= item.TotalMatches || !CandidateFullyVisible(*item.View, cfg) {
		return fmt.Errorf("当前候选人位置还没读完整，请等页面稳定后再开始")
	}
	*cursor = model.CandidateListCursor{StartIndex: item.Index, IdentityByIndex: true}
	return nil
}

// CandidateViewport 复用既有Worker单元素查找获取真实边界，不移动鼠标、焦点或滚动条。
func CandidateViewport(ctx context.Context, browser model.Browser, cfg model.Config, index int) (contract.ElementView, error) {
	selector, err := IndexedSelector(cfg, "candidate.item", index)
	if err != nil {
		return contract.ElementView{}, err
	}
	selector.TimeoutMS = 500
	result, err := browser.Find(ctx, contract.ElementFindRequest{Selector: selector})
	if err != nil {
		return contract.ElementView{}, fmt.Errorf("读取当前候选人位置失败：%w", err)
	}
	view := result.View
	if !view.Visible || view.Box.Width <= 0 || view.Box.Height <= 0 || view.Viewport.Width <= 0 || view.Viewport.Height <= 0 {
		return contract.ElementView{}, fmt.Errorf("当前候选人位置还没读完整，请等列表稳定后再开始")
	}
	return view, nil
}

// CandidateFullyVisible 按平台配置页头和底栏避让范围核对卡片四边完整位于可用视口，排除仅露出边缘及被固定页头遮挡的卡片。
func CandidateFullyVisible(view contract.ElementView, cfg model.Config) bool {
	return view.Visible && view.Box.Width > 0 && view.Box.Height > 0 &&
		view.Box.X >= 0 && view.Box.X+view.Box.Width <= view.Viewport.Width &&
		view.Box.Y >= float64(cfg.Behavior.CandidateViewportTopInset) && view.Box.Y+view.Box.Height <= view.Viewport.Height-float64(cfg.Behavior.CandidateViewportBottomInset)
}

// CandidatePageUsesViewport 按平台配置选择推荐、分页或搜索起点，仅在任务启动一次，后续批次与翻页不重新定位。
func CandidatePageUsesViewport(cfg model.Config, pageURL string) bool {
	if !cfg.Behavior.CandidateStartFromViewport {
		return false
	}
	if cfg.ManualSearchURL != "" && PageURLMatches(pageURL, cfg.ManualSearchURL) {
		return cfg.Behavior.CandidateSearchFromViewport
	}
	return (cfg.Behavior.CandidateListMode == "infinite_scroll" || cfg.Behavior.SupportsPaging) && PageURLMatches(pageURL, cfg.EntryURL)
}
