// Package common 文件作用：验证纯存在性检查不读取正在销毁的节点正文，同时保留候选人字段读取和真实错误。
package common

import (
	"context"
	"errors"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// selectorProbeBrowser 模拟详情节点被关闭后正文不可读取，但仍能可靠探测存在状态。
type selectorProbeBrowser struct {
	model.Browser
	present                         bool
	escapes, textReads, closeChecks int
	probeErr, fieldErr              error
	fieldRequests                   []contract.ElementFindAllRequest
}

// FindAll 区分纯探测和正文读取；被销毁节点的正文读取返回实际的列表超时错误。
func (b *selectorProbeBrowser) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if len(request.Fields) > 0 {
		b.fieldRequests = append(b.fieldRequests, request)
		if b.fieldErr != nil {
			return nil, b.fieldErr
		}
		return []contract.FindAllItem{{Index: 0, Text: "测试候选人 25岁", Fields: map[string]string{"name": "测试候选人"}}}, nil
	}
	if !request.ProbeOnly {
		b.textReads++
		return nil, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ACTION_TIMEOUT", Message: "列表元素重读后仍不完整，暂不跳过"}}
	}
	if b.probeErr != nil {
		return nil, b.probeErr
	}
	if request.Selector.Description == "详情关闭按钮" {
		return nil, nil
	}
	if b.escapes > 0 {
		b.closeChecks++
	}
	if b.present {
		return []contract.FindAllItem{{Index: 0}}, nil
	}
	return nil, nil
}

// PressKey 模拟 Escape 关闭详情节点，后续只能检查存在状态。
func (b *selectorProbeBrowser) PressKey(context.Context, contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	b.escapes++
	b.present = false
	return contract.KeyboardPressResult{Pressed: true}, nil
}

// TestCloseCandidateDetailDoesNotReadDestroyedNode 验证详情已关闭时不被销毁节点的正文读取超时误判成关闭失败。
func TestCloseCandidateDetailDoesNotReadDestroyedNode(t *testing.T) {
	browser := &selectorProbeBrowser{present: true}
	if err := CloseCandidateDetail(context.Background(), browser, cleanupConfig()); err != nil {
		t.Fatalf("详情已关闭，不应再读取销毁节点并报错：%v", err)
	}
	if browser.present || browser.escapes != 1 || browser.closeChecks != 1 || browser.textReads != 0 {
		t.Fatalf("关闭必须纯探测确认隐藏，不能读取正文：%+v", browser)
	}
}

// TestSelectorExistenceChecksPreserveRealErrors 验证存在性检查不把真实会话故障、读取超时或任务取消当作元素消失。
func TestSelectorExistenceChecksPreserveRealErrors(t *testing.T) {
	cfg := model.Config{Selectors: map[string]contract.SelectorSpec{
		"optional": selector("存在性检查"), "candidate.item": selector("候选人卡片"), "candidate.continue": selector("存在性检查"),
	}}
	checks := []struct {
		name string
		call func(model.Browser) (bool, error)
	}{
		{name: "selector", call: func(browser model.Browser) (bool, error) {
			return SelectorExists(context.Background(), browser, cfg, "optional")
		}},
		{name: "quick_selector", call: func(browser model.Browser) (bool, error) {
			return ProbeSelectorExists(context.Background(), browser, cfg, "optional")
		}},
		{name: "candidate_action", call: func(browser model.Browser) (bool, error) {
			return CandidateActionExists(context.Background(), browser, cfg, model.Candidate{}, "candidate.continue")
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			browser := &selectorProbeBrowser{present: true}
			if exists, err := check.call(browser); err != nil || !exists || browser.textReads != 0 {
				t.Fatalf("存在性检查不应读取正文：exists=%v err=%v reads=%d", exists, err, browser.textReads)
			}
			for _, actualErr := range []error{
				&contract.WorkerError{Body: contract.WorkerErrorBody{Code: "WORKER_UNRESPONSIVE"}},
				&contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ACTION_TIMEOUT"}},
				context.Canceled,
			} {
				browser.probeErr = actualErr
				if exists, err := check.call(browser); exists || !errors.Is(err, actualErr) {
					t.Fatalf("真实异常不能被隐藏：exists=%v err=%v want=%v", exists, err, actualErr)
				}
			}
			browser.probeErr = &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
			if exists, err := check.call(browser); exists || err != nil {
				t.Fatalf("可选元素不存在应正常返回：exists=%v err=%v", exists, err)
			}
		})
	}
}

// TestCandidateFieldsKeepReadingAndErrorChecks 验证真实候选人字段请求仍读取正文，完整性异常原样返回。
func TestCandidateFieldsKeepReadingAndErrorChecks(t *testing.T) {
	cfg := model.Config{
		Selectors:       map[string]contract.SelectorSpec{"candidate.item": selector("候选人卡片")},
		CandidateFields: map[string]contract.SelectorSpec{"name": selector("候选人姓名")},
	}
	for _, actualErr := range []error{nil, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ACTION_TIMEOUT"}}} {
		browser := &selectorProbeBrowser{fieldErr: actualErr}
		items, err := FindCandidates(context.Background(), browser, cfg, "test", model.CandidateListOptions{})
		if !errors.Is(err, actualErr) || len(browser.fieldRequests) != 1 {
			t.Fatalf("候选人字段读取异常：items=%d err=%v want=%v requests=%d", len(items), err, actualErr, len(browser.fieldRequests))
		}
		request := browser.fieldRequests[0]
		if request.ProbeOnly || request.ExpectedMissing || len(request.Fields) != 1 {
			t.Fatalf("真实字段读取不能退化为存在性检查：%+v", request)
		}
		if actualErr == nil && len(items) != 1 || actualErr != nil && len(items) != 0 {
			t.Fatalf("真实字段读取结果未保留：items=%d err=%v", len(items), err)
		}
	}
}
