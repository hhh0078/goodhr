// Package job51 文件作用：仅为前程无忧关闭弹框的点击启用鼠标移到中央，避免顶部悬浮卡片遮挡下一步。
package job51

import (
	"context"
	"encoding/json"
	"errors"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// closeMouseBrowser 只在前程无忧关闭流程中修饰点击，其余浏览器能力原样复用。
type closeMouseBrowser struct {
	model.Browser
	moveErr          error
	closeDescription string // 混合打开/关闭流程只为配置中的关闭目标启用；纯关闭流程留空。
}

// Click 要求 Worker 松开关闭按钮后立即移动鼠标；Windows 和 macOS 双架构使用同一原生鼠标能力。
// 关闭已执行但移走失败时，保留错误供平台层返回，避免公共关闭逻辑重放已完成的点击或吞掉移动错误。
func (b *closeMouseBrowser) Click(ctx context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	if b.closeDescription != "" && request.Selector.Description != b.closeDescription {
		return b.Browser.Click(ctx, request)
	}
	if b.moveErr != nil {
		return contract.ClickResult{}, b.moveErr
	}
	request.MoveToCenterAfterClick = true
	result, err := b.Browser.Click(ctx, request)
	var workerErr *contract.WorkerError
	if errors.As(err, &workerErr) && workerErr != nil {
		var details struct {
			PostClickMoveFailed bool `json:"post_click_move_failed"`
		}
		if json.Unmarshal(workerErr.Body.Details, &details) == nil && details.PostClickMoveFailed {
			b.moveErr = err
			if !contract.IsSessionUnavailable(err) {
				return contract.ClickResult{Clicked: true}, nil
			}
		}
	}
	return result, err
}

// closeCandidatePanels 复用公共弹框关闭能力，仅在前程无忧启用点击后移走鼠标，并保留移动失败。
func closeCandidatePanels(ctx context.Context, browser model.Browser, cfg model.Config) error {
	closing := &closeMouseBrowser{Browser: browser}
	err := common.CloseCandidatePanels(ctx, closing, cfg)
	return errors.Join(err, closing.moveErr)
}
