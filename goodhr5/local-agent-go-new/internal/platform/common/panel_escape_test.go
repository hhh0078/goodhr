// Package common 本文件验证关闭动作必须确认原页、目标身份与弹层实际消失。
package common

import (
	"context"
	"errors"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// escapePanelBrowser 按测试指定的按键和点击结果改变模拟弹层，记录实际执行的每种操作。
type escapePanelBrowser struct {
	model.Browser
	url                     string
	shown, rightPerson      bool
	presses, clicks, probes int
	onPress, onClick        func(*escapePanelBrowser)
}

// ListPages 返回此刻活动页，复现操作期间外部切换标签页。
func (b *escapePanelBrowser) ListPages(context.Context) (contract.PageListResult, error) {
	return contract.PageListResult{Pages: []contract.PageInfo{{URL: b.url, Current: true}}}, nil
}

// FindAll 只返回当前弹层实际可见状态；隐藏检测不依赖按钮成功回执。
func (b *escapePanelBrowser) FindAll(_ context.Context, req contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	b.probes++
	if req.Selector.Description != "聊天框" || !req.ProbeOnly {
		return nil, errors.New("没有按原弹层核实状态")
	}
	if b.shown {
		return []contract.FindAllItem{{}}, nil
	}
	return nil, nil
}

// PressKey 只允许带有原页与弹层约束的单次 Esc；按键的后果由当前用例决定。
func (b *escapePanelBrowser) PressKey(_ context.Context, req contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	b.presses++
	if req.Key != "Escape" || req.Target == nil || req.Target.Description != "聊天框" || req.ExpectedPageURL != b.url {
		return contract.KeyboardPressResult{}, errors.New("没有核实页面和弹层就按键")
	}
	if b.onPress != nil {
		b.onPress(b)
	}
	return contract.KeyboardPressResult{Pressed: true}, nil
}

// Click 要求在原人、原弹层可见且 Esc 尝试过后，才允许带消失验证的按钮回退。
func (b *escapePanelBrowser) Click(_ context.Context, req contract.ElementClickRequest) (contract.ClickResult, error) {
	b.clicks++
	if !b.rightPerson || !b.shown || b.presses != 1 || req.Selector.Description != "关闭聊天框" || req.Verify == nil || req.Verify.TargetHidden == nil || req.Verify.TargetHidden.Description != "聊天框" {
		return contract.ClickResult{}, errors.New("备用点击缺少前置或关闭验证")
	}
	if b.onClick != nil {
		b.onClick(b)
	}
	return contract.ClickResult{Clicked: true, Verified: true}, nil
}

// TestClosePanelEscapeFirst 验证按键或按钮后切页、切人、弹层仍可见时不会误报成功或追加关闭。
func TestClosePanelEscapeFirst(t *testing.T) {
	const original = "https://example.test/chat"
	hide := func(b *escapePanelBrowser) { b.shown = false }
	changePage := func(b *escapePanelBrowser) { b.url = original + "?other"; b.shown = false }
	changePerson := func(b *escapePanelBrowser) { b.rightPerson = false }
	for _, c := range []struct {
		name               string
		shown, rightPerson bool
		url                string
		onPress, onClick   func(*escapePanelBrowser)
		wantErr            bool
		presses, clicks    int
	}{
		{"Esc关闭即结束", true, true, original, hide, nil, false, 1, 0},
		{"原页没有打开弹层", false, false, original, nil, nil, false, 0, 0},
		{"其他页上弹层不存在", false, true, original + "?other", nil, nil, true, 0, 0},
		{"其他页上弹层也可见", true, true, original + "?other", nil, nil, true, 0, 0},
		{"开始就是别的候选人", true, false, original, nil, nil, true, 0, 0},
		{"Esc后切页且看似已关", true, true, original, changePage, nil, true, 1, 0},
		{"Esc后候选人被切换", true, true, original, changePerson, nil, true, 1, 0},
		{"Esc静默失败后点一次关闭", true, true, original, nil, hide, false, 1, 1},
		{"按钮报告成功但仍可见", true, true, original, nil, nil, true, 1, 1},
		{"按钮后切页不能当作已关", true, true, original, nil, changePage, true, 1, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := &escapePanelBrowser{url: c.url, shown: c.shown, rightPerson: c.rightPerson, onPress: c.onPress, onClick: c.onClick}
			err := ClosePanelEscapeFirst(context.Background(), b, candidateChatTestConfig(), "candidate.chat_modal", "candidate.chat_close", original, func(context.Context) error {
				if !b.rightPerson {
					return errors.New("当前候选人有变化")
				}
				return nil
			})
			if (err != nil) != c.wantErr || b.presses != c.presses || b.clicks != c.clicks || (c.url != original && b.probes != 0) {
				t.Fatalf("按键=%d 点击=%d 探测=%d 错误=%v", b.presses, b.clicks, b.probes, err)
			}
		})
	}
}
