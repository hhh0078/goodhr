// Package common 文件作用：验证详情正文缺失时仍清理已出现的弹窗，并把清理失败标记为任务必须停止的错误。
package common

import (
	"context"
	"errors"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// detailCleanupBrowser 模拟已出现弹窗但未加载正文的现场，记录关闭与验证请求。
type detailCleanupBrowser struct {
	model.Browser
	present, closeFails, escapeWorks      bool
	closeCalls, escapes                   int
	openCalls, bodyProbes, bodyReadyAfter int
	verifiedDialog                        bool
}

// Click 模拟入口打开弹窗，关闭按钮只在验证目标为弹窗本身时成功。
func (b *detailCleanupBrowser) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	if request.Selector.Description == "详情关闭按钮" {
		b.closeCalls++
		b.verifiedDialog = request.Diagnostic && request.Verify != nil && request.Verify.TargetHidden != nil && request.Verify.TargetHidden.Description == "详情弹窗"
		if b.closeFails {
			return contract.ClickResult{}, errors.New("测试关闭按钮没有生效")
		}
		b.present = false
	} else {
		b.openCalls++
		b.present = true
	}
	return contract.ClickResult{Clicked: true}, nil
}

// FindAll 正文一直缺失，弹窗状态独立于正文存在与否。
func (b *detailCleanupBrowser) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if request.Selector.Description == "详情正文" {
		b.bodyProbes++
		if b.bodyReadyAfter > 0 && b.bodyProbes >= b.bodyReadyAfter {
			return []contract.FindAllItem{{}}, nil
		}
	}
	if request.Selector.Description == "详情弹窗" && b.present {
		if b.escapes > 0 && !b.escapeWorks {
			return nil, errors.New("测试弹窗关闭状态仍无法确认")
		}
		return []contract.FindAllItem{{}}, nil
	}
	return nil, nil
}

// PressKey 模拟关闭按钮缺失后的 Escape 降级，并记录是否确认关闭。
func (b *detailCleanupBrowser) PressKey(context.Context, contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	b.escapes++
	if b.escapeWorks {
		b.present = false
	}
	return contract.KeyboardPressResult{}, nil
}

// cleanupConfig 提供与平台无关的详情入口、正文、弹窗和关闭目标。
func cleanupConfig() model.Config {
	return model.Config{Selectors: map[string]contract.SelectorSpec{
		"candidate.item": selector("候选人卡片"), "candidate.open_target": selector("详情入口"), "candidate.detail": selector("详情正文"),
		"candidate.detail_dialog": selector("详情弹窗"), "candidate.detail_close": selector("详情关闭按钮"),
	}}
}

// TestFailedDetailOpenCleansVisibleDialog 验证误报详情缺失后，弹窗被关闭并验证，不留在后续列表前面。
func TestFailedDetailOpenCleansVisibleDialog(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		browser := &detailCleanupBrowser{closeFails: fallback, escapeWorks: fallback}
		err := OpenCandidateDetail(context.Background(), browser, cleanupConfig(), model.Candidate{})
		if !errors.Is(err, model.ErrCandidateDetailUnavailable) || browser.present || browser.closeCalls != 1 || browser.openCalls != 1 || !browser.verifiedDialog {
			t.Fatalf("打开失败后没有清理独立弹窗：%v %+v", err, browser)
		}
		if fallback && browser.escapes != 1 {
			t.Fatal("关闭按钮失败后没有按 Escape")
		}
	}
}

// TestVisibleDialogWaitsForBodyWithoutReclick 验证详情仍在加载时继续等正文，不点击被弹窗遮住的卡片。
func TestVisibleDialogWaitsForBodyWithoutReclick(t *testing.T) {
	browser := &detailCleanupBrowser{bodyReadyAfter: 2}
	err := OpenCandidateDetail(context.Background(), browser, cleanupConfig(), model.Candidate{})
	if err != nil || browser.openCalls != 1 || browser.bodyProbes != 2 || browser.closeCalls != 0 || !browser.present {
		t.Fatalf("正文加载期间不应重点击或关闭弹窗：%v %+v", err, browser)
	}
}

// TestFailedDialogCleanupCannotBeSkipped 验证关闭无法确认时返回停止任务错误，而不是候选人可跳过错误。
func TestFailedDialogCleanupCannotBeSkipped(t *testing.T) {
	browser := &detailCleanupBrowser{closeFails: true}
	err := OpenCandidateDetail(context.Background(), browser, cleanupConfig(), model.Candidate{})
	if !errors.Is(err, model.ErrCandidateDetailCleanupFailed) || errors.Is(err, model.ErrCandidateDetailUnavailable) {
		t.Fatalf("弹窗未关闭却允许跳过并继续：%v", err)
	}
}

// TestMissingDialogDoesNotSendEscape 验证页面没有详情弹窗时不发送多余按键。
func TestMissingDialogDoesNotSendEscape(t *testing.T) {
	browser := &detailCleanupBrowser{}
	if err := CloseCandidateDetail(context.Background(), browser, cleanupConfig()); err != nil || browser.closeCalls != 0 || browser.escapes != 0 {
		t.Fatalf("没有弹窗时不应继续关闭：%v %+v", err, browser)
	}
}
