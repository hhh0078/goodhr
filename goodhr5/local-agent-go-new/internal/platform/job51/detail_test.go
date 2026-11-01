// Package job51 文件作用：验证详情暂态关闭失败仅在弹窗仍可见时有限重试，不影响招呼发送次数。
package job51

import (
	"context"
	"errors"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

type detailCloseStub struct {
	model.Browser
	opened, failFirst, alwaysFail bool
	clicks                        int
}

// FindAll 只模拟当前详情和关闭按钮是否仍可见。
func (s *detailCloseStub) FindAll(context.Context, contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if s.opened {
		return []contract.FindAllItem{{Index: 0}}, nil
	}
	return nil, nil
}

// Click 模拟关闭按钮的一次暂态未响应，第二次可关闭；不执行发送动作。
func (s *detailCloseStub) Click(context.Context, contract.ElementClickRequest) (contract.ClickResult, error) {
	s.clicks++
	if s.alwaysFail || s.failFirst && s.clicks == 1 {
		return contract.ClickResult{}, errors.New("合成关闭未响应")
	}
	s.opened = false
	return contract.ClickResult{Clicked: true}, nil
}

// PressKey 模拟Escape未响应，让被测平台的有限重试直接接手。
func (s *detailCloseStub) PressKey(context.Context, contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	return contract.KeyboardPressResult{}, errors.New("合成Escape未响应")
}

// TestDetailCloseRetriesOnlyVisiblePanel 验证已关闭不点击、正常一次、暂态失败最多两次，持续失败仍返回错误。
func TestDetailCloseRetriesOnlyVisiblePanel(t *testing.T) {
	for _, item := range []struct {
		opened, failFirst, alwaysFail bool
		want                          int
		wantErr                       bool
	}{{false, false, false, 0, false}, {true, false, false, 1, false}, {true, true, false, 2, false}, {true, true, true, 2, true}} {
		browser := &detailCloseStub{opened: item.opened, failFirst: item.failFirst, alwaysFail: item.alwaysFail}
		err := NewRuntime().CloseCandidateDetail(context.Background(), browser, loadPositionConfig(t), model.Candidate{})
		if (err != nil) != item.wantErr || browser.clicks != item.want {
			t.Fatalf("关闭次数或结果不正确：clicks=%d err=%v", browser.clicks, err)
		}
	}
}
