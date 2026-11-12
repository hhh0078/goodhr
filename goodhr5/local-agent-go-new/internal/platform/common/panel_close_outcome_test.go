// Package common 文件作用：验证聊天框和联系人侧栏关闭时先复核真实状态，回执异常不能导致重复点击或绕过任务取消。
package common

import (
	"context"
	"errors"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// panelCloseOutcomeBrowser 模拟关闭动作已经生效但回执报错，以及真正断连、取消和检查失败的边界。
type panelCloseOutcomeBrowser struct {
	model.Browser
	shown, hideOnClick bool
	clicks, presses    int
	clickErr, probeErr error
	cancelOnClick      context.CancelFunc
}

// FindAll 只返回面板当前是否可见，关闭后不读取已销毁的正文。
func (b *panelCloseOutcomeBrowser) FindAll(_ context.Context, req contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if !req.ProbeOnly || len(req.Fields) != 0 {
		return nil, errors.New("关闭检查必须只读存在状态")
	}
	if b.clicks > 0 && b.probeErr != nil {
		return nil, b.probeErr
	}
	if b.shown {
		return []contract.FindAllItem{{}}, nil
	}
	return nil, nil
}

// Click 独立模拟页面关闭和动作回执，复现“动作已生效但回执失败”。
func (b *panelCloseOutcomeBrowser) Click(context.Context, contract.ElementClickRequest) (contract.ClickResult, error) {
	b.clicks++
	if b.hideOnClick {
		b.shown = false
	}
	if b.cancelOnClick != nil {
		b.cancelOnClick()
	}
	return contract.ClickResult{}, b.clickErr
}

// PressKey 记录并执行备用 Esc，用于检查弹层已消失后是否仍有多余操作。
func (b *panelCloseOutcomeBrowser) PressKey(context.Context, contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	b.presses++
	b.shown = false
	return contract.KeyboardPressResult{}, nil
}

// TestCloseOptionalPanelConfirmsStateAfterClickError 验证聊天框、联系人侧栏的关闭回执报错时，确认隐藏即可结束且不补点。
func TestCloseOptionalPanelConfirmsStateAfterClickError(t *testing.T) {
	for _, key := range []string{"candidate.chat_modal", "candidate.contact_drawer"} {
		t.Run(key, func(t *testing.T) {
			b := &panelCloseOutcomeBrowser{shown: true, hideOnClick: true, clickErr: &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "CLICK_VERIFY_FAILED"}}}
			cfg := model.Config{Selectors: map[string]contract.SelectorSpec{key: selector("面板"), "close": selector("关闭按钮")}}
			if err := CloseOptionalPanel(context.Background(), b, cfg, key, "close", "面板"); err != nil {
				t.Fatal(err)
			}
			if b.shown || b.clicks != 1 || b.presses != 0 {
				t.Fatalf("确认关闭后禁止重复点击或追加 Esc：%+v", b)
			}
		})
	}
}

// TestCloseOptionalPanelKeepsRealFailures 验证真正的浏览器故障、状态检查失败和任务取消不会被面板消失掩盖。
func TestCloseOptionalPanelKeepsRealFailures(t *testing.T) {
	fatal := &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "WORKER_UNRESPONSIVE"}}
	for _, name := range []string{"断连", "检查失败", "点击取消", "任务取消"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			b := &panelCloseOutcomeBrowser{shown: true, hideOnClick: true}
			want := error(fatal)
			switch name {
			case "断连":
				b.clickErr = fatal
			case "检查失败":
				b.probeErr = fatal
			case "点击取消":
				want = context.Canceled
				b.clickErr = want
			case "任务取消":
				want = context.Canceled
				b.cancelOnClick = cancel
			}
			cfg := model.Config{Selectors: map[string]contract.SelectorSpec{"panel": selector("面板"), "close": selector("关闭按钮")}}
			err := CloseOptionalPanel(ctx, b, cfg, "panel", "close", "面板")
			if !errors.Is(err, want) || b.clicks != 1 || b.presses != 0 {
				t.Fatalf("真实故障或取消必须中止，不能补点或报成功：%v %+v", err, b)
			}
		})
	}
}
