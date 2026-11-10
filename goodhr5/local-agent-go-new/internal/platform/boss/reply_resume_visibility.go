// Package boss 本文件仅在显式采集附件时避开聊天固定头，最多真实滚轮一次后重新核身和定位同一文件；不修改公共点击保护。
package boss

import (
	"context"
	"fmt"
	"math"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// bossResumeGeometryBrowser 复用设备已有的标准Find能力，不扩大其它平台的浏览器最小接口。
type bossResumeGeometryBrowser interface {
	Find(context.Context, contract.ElementFindRequest) (contract.FindResult, error)
}

// prepareBossResumeSourceVisibility 只纠正中心被裁剪的既有附件，正常位置零滚动，滚后不复用旧行或旧身份。
func prepareBossResumeSourceVisibility(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, page string) (model.Config, error) {
	entry, err := common.RequiredSelector(cfg, "message.resume_attachment_entry")
	if err != nil {
		return cfg, err
	}
	viewport, err := common.RequiredSelector(cfg, "message.history_viewport")
	if err != nil {
		return cfg, err
	}
	distance, err := bossResumeVisibilityDistance(ctx, browser, entry, viewport)
	if err != nil || distance == 0 {
		return cfg, err
	}
	if err = verifyBossOpenAction(ctx, browser, cfg, snapshot, page); err != nil {
		return cfg, err
	}
	// 不带Target，避免最小gap被平台半幅滚动后提前判失败；只在已核实视口滚一次，再由fresh pin和中心几何决定能否点击。
	if _, err = browser.Scroll(ctx, contract.ScrollRequest{WheelAnchor: &viewport, Distance: distance, MaxAttempts: 1, WaitMS: 250}); err != nil {
		return cfg, err // 单次失败不补滚、不点击；沿原命令失败边界返回。
	}
	snapshot.DeliveryPageURL = page // fresh pin只能留在原页，禁止借机重新打开已经关闭或切换的会话。
	if cfg, err = pinBossResumeSourceWindow(ctx, browser, cfg, snapshot, false); err != nil {
		return cfg, err
	}
	entry = cfg.Selectors["message.resume_attachment_entry"]
	if distance, err = bossResumeVisibilityDistance(ctx, browser, entry, viewport); err != nil {
		return cfg, err
	}
	if distance != 0 {
		return cfg, fmt.Errorf("附件仍未进入聊天可操作区域，暂不点击")
	}
	if err = verifyBossOpenAction(ctx, browser, cfg, snapshot, page); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// bossResumeVisibilityDistance 读取当前按钮与真实裁剪容器交集，只返回单次不超过240像素的纵向纠正距离。
func bossResumeVisibilityDistance(ctx context.Context, browser model.Browser, entry, viewport contract.SelectorSpec) (int, error) {
	finder, ok := browser.(bossResumeGeometryBrowser)
	if !ok {
		return 0, fmt.Errorf("附件位置核对能力尚未准备好，暂不点击")
	}
	target, err := finder.Find(ctx, contract.ElementFindRequest{Selector: entry})
	if err != nil {
		return 0, err
	}
	container, err := finder.Find(ctx, contract.ElementFindRequest{Selector: viewport})
	if err != nil {
		return 0, err
	}
	return bossResumeClippedDistance(target.View, container.View)
}

// bossResumeClippedDistance 对有限、同一窗口的可见几何求交；横向越界和无效容器直接拒绝，不尝试操作其它区域。
func bossResumeClippedDistance(target, viewport contract.ElementView) (int, error) {
	for _, view := range []contract.ElementView{target, viewport} {
		for _, value := range []float64{view.Box.X, view.Box.Y, view.Box.Width, view.Box.Height, view.Viewport.Width, view.Viewport.Height} {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return 0, fmt.Errorf("附件或聊天视口位置无法核实，暂不点击")
			}
		}
		if !view.Visible || view.Box.Width <= 0 || view.Box.Height <= 0 || view.Viewport.Width <= 0 || view.Viewport.Height <= 0 {
			return 0, fmt.Errorf("附件或聊天视口不可操作，暂不点击")
		}
	}
	if !target.Enabled || target.Viewport != viewport.Viewport {
		return 0, fmt.Errorf("附件不可用或窗口尺寸已变化，暂不点击")
	}
	box := viewport.Box
	left, right := math.Max(0, box.X)+1, math.Min(viewport.Viewport.Width, box.X+box.Width)-1
	top, bottom := math.Max(0, box.Y)+1, math.Min(viewport.Viewport.Height, box.Y+box.Height)-1
	x, y := target.Box.X+target.Box.Width/2, target.Box.Y+target.Box.Height/2
	if left >= right || top >= bottom || x < left || x > right {
		return 0, fmt.Errorf("附件不在可核实的聊天视口内，暂不点击")
	}
	if y >= top && y <= bottom {
		return 0, nil
	}
	return int(math.Round(math.Max(-240, math.Min(240, y-(top+bottom)/2)))), nil
}
