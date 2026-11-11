// Package boss 文件作用：BOSS每次任务从屏幕内第一张候选人卡片开始，只读定位，不查历史断点或预先滚动；三个系统共用此实现。
package boss

import (
	"context"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// startCandidateViewport 用标准元素边界二分定位纵向名单的首个可见卡片，还原前方同名同龄计数，避免恢复时点到屏幕上方的同名者。
func startCandidateViewport(ctx context.Context, browser model.Browser, cfg model.Config, cursor *model.CandidateListCursor) error {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	selector, err := common.RequiredSelector(cfg, "candidate.item")
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
		view, err := candidateViewport(ctx, browser, cfg, middle)
		if err != nil {
			return err
		}
		if view.Box.Y+view.Box.Height <= float64(cfg.Behavior.CandidateTopInset) {
			first = middle + 1
		} else {
			last = middle
		}
	}
	if first == count {
		return fmt.Errorf("当前屏幕没有候选人卡片，请先把列表停在要开始的位置")
	}
	view, err := candidateViewport(ctx, browser, cfg, first)
	if err != nil {
		return err
	}
	if !candidateIntersectsViewport(view, cfg) {
		return fmt.Errorf("没确认当前屏幕内的候选人，请等页面稳定后再开始")
	}
	// 只读前方最小身份字段，不处理、不滚回旧人选；原定位规则需要保留同名同龄者的真实出现序号。
	occurrences := make(map[string]int)
	fields := make(map[string]contract.SelectorSpec)
	for _, key := range []string{"name", "age", "candidate_age"} {
		if field, exists := cfg.CandidateFields[key]; exists {
			fields[key] = field
		}
	}
	for start := 0; start < first; start += 20 {
		size := min(20, first-start)
		rows, err := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: selector, StartIndex: start, MaxItems: size, Fields: fields, ReadConcurrency: 5})
		if err != nil {
			return fmt.Errorf("核对同名候选人位置失败：%w", err)
		}
		if len(rows) != size {
			return fmt.Errorf("候选人列表正在刷新，请等稳定后再开始")
		}
		for offset, row := range rows {
			if row.Index != start+offset || strings.TrimSpace(row.Fields["name"]) == "" {
				return fmt.Errorf("候选人位置尚未读完整，请等列表稳定后再开始")
			}
			identity := strings.Join(common.CandidateIdentityTexts(row.Fields["name"], row.Fields, row.Text), "\x00")
			occurrences[identity]++
		}
	}
	// 名单异步刷新或用户移动滚动条时，不把过期的屏幕位置当作新起点。
	after, err := candidateViewport(ctx, browser, cfg, first)
	if err != nil {
		return err
	}
	if !candidateIntersectsViewport(after, cfg) || after.Box.Y != view.Box.Y || after.Box.Height != view.Box.Height {
		return fmt.Errorf("当前候选人位置刚发生变化，请等页面稳定后再开始")
	}
	*cursor = model.CandidateListCursor{StartIndex: first, IdentityOccurrences: occurrences}
	return nil
}

// candidateViewport 复用既有Worker单元素查找获取真实边界，不移动鼠标、焦点或滚动条。
func candidateViewport(ctx context.Context, browser model.Browser, cfg model.Config, index int) (contract.ElementView, error) {
	selector, err := common.IndexedSelector(cfg, "candidate.item", index)
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

// candidateIntersectsViewport 按BOSS已有页头和底栏避让范围判断卡片是否真正出现在屏幕里。
func candidateIntersectsViewport(view contract.ElementView, cfg model.Config) bool {
	return view.Box.X+view.Box.Width > 0 && view.Box.X < view.Viewport.Width &&
		view.Box.Y+view.Box.Height > float64(cfg.Behavior.CandidateTopInset) && view.Box.Y < view.Viewport.Height-float64(cfg.Behavior.CandidateBottomInset)
}
