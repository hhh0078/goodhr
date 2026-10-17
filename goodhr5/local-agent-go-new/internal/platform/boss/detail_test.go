// Package boss 文件作用：验证 Boss 候选人详情中的平台附加内容不会进入判断文本。
package boss

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// detailCloseBrowser 模拟 Boss 弹窗和关闭动作，只允许使用正式 Browser 标准查找、按键和点击接口。
type detailCloseBrowser struct {
	model.Browser
	present, escapeWorks, buttonMissing bool
	escapeErr, clickErr                 error
	probeErrAfterEscape                 error
	cancelOnEscape                      context.CancelFunc
	keys                                []contract.KeyboardPressRequest
	clicks                              []contract.ElementClickRequest
	probes                              []contract.ElementFindAllRequest
	actions                             []string
}

// FindAll 独立模拟弹窗及按钮，记录关闭成功后是否多余查找按钮或访问已销毁正文。
func (b *detailCloseBrowser) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	b.probes = append(b.probes, request)
	if strings.Contains(request.Selector.Description, "关闭按钮") {
		b.actions = append(b.actions, "find-close")
		if !b.buttonMissing && b.present {
			return []contract.FindAllItem{{}}, nil
		}
		return nil, nil
	}
	if len(b.keys) > 0 && b.probeErrAfterEscape != nil {
		return nil, b.probeErrAfterEscape
	}
	if b.present {
		return []contract.FindAllItem{{}}, nil
	}
	return nil, nil
}

// PressKey 模拟真实 Escape 能否关闭弹窗，以及用户恰在按键后取消的边界情况。
func (b *detailCloseBrowser) PressKey(_ context.Context, request contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	b.keys = append(b.keys, request)
	b.actions = append(b.actions, "escape")
	if b.cancelOnEscape != nil {
		b.cancelOnEscape()
	}
	if b.escapeWorks && b.escapeErr == nil {
		b.present = false
	}
	return contract.KeyboardPressResult{}, b.escapeErr
}

// Click 模拟备用关闭按钮及 Worker 对弹窗隐藏的验证，保留真实点击错误。
func (b *detailCloseBrowser) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	b.clicks = append(b.clicks, request)
	b.actions = append(b.actions, "click")
	if b.clickErr != nil {
		return contract.ClickResult{}, b.clickErr
	}
	b.present = false
	return contract.ClickResult{Clicked: true, Verified: true}, nil
}

// TestCloseCandidateDetailEscapeFirst 验证 Boss 的 Escape 关闭成功后不再查找或点击备用按钮。
func TestCloseCandidateDetailEscapeFirst(t *testing.T) {
	cfg := recommendationConfig(t)
	if !cfg.Behavior.DetailCloseEscapeFirst {
		t.Fatal("Boss 必须默认先按 Escape 关闭详情")
	}
	browser := &detailCloseBrowser{present: true, escapeWorks: true}
	if err := NewRuntime().CloseCandidateDetail(context.Background(), browser, cfg, model.Candidate{}); err != nil {
		t.Fatal(err)
	}
	if browser.present || len(browser.keys) != 1 || browser.keys[0].Key != "Escape" || len(browser.clicks) != 0 || !reflect.DeepEqual(browser.actions, []string{"escape"}) {
		t.Fatalf("Escape 已关闭后不应查找或点击按钮：%+v", browser)
	}
	for _, request := range browser.probes {
		if request.Selector.Description != cfg.Selectors["candidate.detail_dialog"].Description || !request.ExpectedMissing || !request.ProbeOnly || request.MaxItems != 1 {
			t.Fatalf("只应检查弹窗存在状态，不能读取已销毁的正文：%+v", request)
		}
	}
}

// TestCloseCandidateDetailEscapeFallback 验证 Escape 无效或按键异常时才点击备用按钮，并要求 Worker 确认弹窗消失。
func TestCloseCandidateDetailEscapeFallback(t *testing.T) {
	for _, keyErr := range []error{nil, errors.New("测试按键没有发送成功")} {
		browser := &detailCloseBrowser{present: true, escapeErr: keyErr}
		cfg := recommendationConfig(t)
		if err := NewRuntime().CloseCandidateDetail(context.Background(), browser, cfg, model.Candidate{}); err != nil {
			t.Fatal(err)
		}
		if browser.present || !reflect.DeepEqual(browser.actions, []string{"escape", "find-close", "click"}) || len(browser.keys) != 1 || len(browser.clicks) != 1 {
			t.Fatalf("必须先 Escape、确认未关闭后再找备用按钮：%+v", browser)
		}
		request := browser.clicks[0]
		if !request.Diagnostic || request.Verify == nil || request.Verify.TargetHidden == nil || request.Verify.TargetHidden.Description != cfg.Selectors["candidate.detail_dialog"].Description || request.Verify.TimeoutMS != 2000 {
			t.Fatalf("备用点击必须验证弹窗隐藏，不能依赖正文是否加载：%+v", request)
		}
	}
}

// TestCloseCandidateDetailEscapeAndButtonFail 验证两种方式均失败时保留错误，不重复 Escape 或误报关闭成功。
func TestCloseCandidateDetailEscapeAndButtonFail(t *testing.T) {
	clickErr := &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "CLICK_VERIFY_FAILED"}}
	browser := &detailCloseBrowser{present: true, clickErr: clickErr}
	err := NewRuntime().CloseCandidateDetail(context.Background(), browser, recommendationConfig(t), model.Candidate{})
	if !errors.Is(err, clickErr) || !browser.present || len(browser.keys) != 1 || len(browser.clicks) != 1 {
		t.Fatalf("两种关闭方式均失败后必须保留点击错误并停止：%v %+v", err, browser)
	}
}

// TestCloseCandidateDetailAlreadyClosed 验证已关闭弹窗时无多余按键或点击，重复调用仍安全。
func TestCloseCandidateDetailAlreadyClosed(t *testing.T) {
	browser := &detailCloseBrowser{}
	for attempt := 0; attempt < 2; attempt++ {
		if err := NewRuntime().CloseCandidateDetail(context.Background(), browser, recommendationConfig(t), model.Candidate{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(browser.keys) != 0 || len(browser.clicks) != 0 {
		t.Fatalf("没有弹窗时不能发送 Escape 或点其他页面：%+v", browser)
	}
}

// TestCloseCandidateDetailStopsOnCancellation 验证按键后用户取消或按键返回取消错误时，绝不再点击备用按钮。
func TestCloseCandidateDetailStopsOnCancellation(t *testing.T) {
	for _, returnedError := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		browser := &detailCloseBrowser{present: true}
		if returnedError {
			browser.escapeErr = context.Canceled
		} else {
			browser.cancelOnEscape = cancel
		}
		err := NewRuntime().CloseCandidateDetail(ctx, browser, recommendationConfig(t), model.Candidate{})
		cancel()
		if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(browser.actions, []string{"escape"}) {
			t.Fatalf("取消后不能继续查找和点击按钮：%v %+v", err, browser)
		}
	}
}

// TestCloseCandidateDetailPreservesSessionFailure 验证按键或关闭验证遇到浏览器故障时直接保留错误，不使用备用按钮掩盖故障。
func TestCloseCandidateDetailPreservesSessionFailure(t *testing.T) {
	fatal := &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "WORKER_UNRESPONSIVE"}}
	for _, duringProbe := range []bool{false, true} {
		browser := &detailCloseBrowser{present: true}
		if duringProbe {
			browser.probeErrAfterEscape = fatal
		} else {
			browser.escapeErr = fatal
		}
		err := NewRuntime().CloseCandidateDetail(context.Background(), browser, recommendationConfig(t), model.Candidate{})
		if !errors.Is(err, fatal) || !reflect.DeepEqual(browser.actions, []string{"escape"}) {
			t.Fatalf("浏览器故障后不能继续点击备用按钮：%v %+v", err, browser)
		}
	}
}

// TestCloseCandidateDetailMissingFallback 验证 Escape 无效且按钮缺失时明确失败，绝不把找不到按钮当作已关闭。
func TestCloseCandidateDetailMissingFallback(t *testing.T) {
	browser := &detailCloseBrowser{present: true, buttonMissing: true, escapeErr: errors.New("测试按键失败")}
	err := NewRuntime().CloseCandidateDetail(context.Background(), browser, recommendationConfig(t), model.Candidate{})
	if err == nil || !browser.present || len(browser.keys) != 1 || len(browser.clicks) != 0 {
		t.Fatalf("备用按钮缺失时必须明确关闭失败：%v %+v", err, browser)
	}
}

// TestCleanCandidateDetailText 验证牛人分析器之后的内容会被移除。
func TestCleanCandidateDetailText(t *testing.T) {
	raw := "解婷 25岁 大专 工作经历 主播\n牛人分析器\nVIP专享 同类牛人\n平台隐私声明"
	cleaned := NewRuntime().CleanCandidateDetailText(raw)
	if cleaned != "解婷 25岁 大专 工作经历 主播" {
		t.Fatalf("Boss 详情清理结果不正确：%q", cleaned)
	}
}
