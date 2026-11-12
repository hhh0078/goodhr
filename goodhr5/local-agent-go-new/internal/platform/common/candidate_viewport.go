// Package common 文件作用：推荐名单从当前屏幕首张完整卡片起跑，标准只读几何定位，不回读屏幕上方人选，不追旧断点或预先滚动。
package common

import (
	"context"
	"fmt"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// StartCandidateViewport 用标准元素边界二分定位纵向名单的首个完整显示的卡片，仅保存当前行号；动作时在该行内核对身份，不读取前方同名者。
func StartCandidateViewport(ctx context.Context, browser model.Browser, cfg model.Config, cursor *model.CandidateListCursor) error {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	selector, err := RequiredSelector(cfg, "candidate.item")
	if err != nil {
		return err
	}
	selector.TimeoutMS = 1500
	items, err := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: selector, MaxItems: 1, ExpectedMissing: true, ProbeOnly: true})
	if err != nil {
		return fmt.Errorf("检查当前候选人列表失败：%w", err)
	}
	if len(items) == 0 || items[0].TotalMatches <= 0 {
		return fmt.Errorf("当前屏幕还没读到候选人，请等列表加载好后再开始")
	}
	count := items[0].TotalMatches
	first, last := 0, count
	for first < last {
		middle := first + (last-first)/2
		view, err := CandidateViewport(ctx, browser, cfg, middle)
		if err != nil {
			return err
		}
		if view.Box.Y < float64(cfg.Behavior.CandidateViewportTopInset) {
			first = middle + 1
		} else {
			last = middle
		}
	}
	var view contract.ElementView
	for first < count {
		view, err = CandidateViewport(ctx, browser, cfg, first)
		if err != nil {
			return err
		}
		if CandidateFullyVisible(view, cfg) {
			break
		}
		// 同一行网格卡片可能高度不同，只在当前屏幕内继续找完整卡片，不向下滚动或扫描屏幕外名单。
		if view.Box.Y >= view.Viewport.Height-float64(cfg.Behavior.CandidateViewportBottomInset) {
			first = count
			break
		}
		first++
	}
	if first == count {
		return fmt.Errorf("当前没有完整显示的候选人卡片，请拉宽或拉高窗口、调整列表位置后再开始")
	}
	// 名单异步刷新或用户移动滚动条时，不把过期的屏幕位置当作新起点。
	after, err := CandidateViewport(ctx, browser, cfg, first)
	if err != nil {
		return err
	}
	if !CandidateFullyVisible(after, cfg) || after.Box.Y != view.Box.Y || after.Box.Height != view.Box.Height {
		return fmt.Errorf("当前候选人位置刚发生变化，请等页面稳定后再开始")
	}
	*cursor = model.CandidateListCursor{StartIndex: first, IdentityByIndex: true}
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

// CandidatePageUsesViewport 按平台配置选择推荐或搜索起点，仅在任务启动一次，后续批次与翻页不重新定位。
func CandidatePageUsesViewport(cfg model.Config, pageURL string) bool {
	if !cfg.Behavior.CandidateStartFromViewport {
		return false
	}
	if cfg.ManualSearchURL != "" && PageURLMatches(pageURL, cfg.ManualSearchURL) {
		return cfg.Behavior.CandidateSearchFromViewport
	}
	return cfg.Behavior.CandidateListMode == "infinite_scroll" && PageURLMatches(pageURL, cfg.EntryURL)
}
