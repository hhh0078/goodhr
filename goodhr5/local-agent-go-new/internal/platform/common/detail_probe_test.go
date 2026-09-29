// Package common 文件作用：验证详情探测不读取正文、诊断参数逐次传递，空结果不能误判为详情已打开。
package common

import (
	"context"
	"errors"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// emptyDetailProbeBrowser 返回成功但没有元素的探测，记录点击和探测请求供边界验证。
type emptyDetailProbeBrowser struct {
	model.Browser
	clicks []contract.ElementClickRequest
	probes []contract.ElementFindAllRequest
}

// Click 记录点击诊断参数，不操作外部页面。
func (b *emptyDetailProbeBrowser) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	b.clicks = append(b.clicks, request)
	return contract.ClickResult{Clicked: true}, nil
}

// FindAll 模拟可成功执行查找但没有满足条件的详情。
func (b *emptyDetailProbeBrowser) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	b.probes = append(b.probes, request)
	return nil, nil
}

// TestDetailProbeEmptyResultIsUnavailable 验证空结果继续第二次探测，最终返回不可用而不是假成功。
func TestDetailProbeEmptyResultIsUnavailable(t *testing.T) {
	browser := &emptyDetailProbeBrowser{}
	cfg := model.Config{Selectors: map[string]contract.SelectorSpec{
		"candidate.item": selector("候选人卡片"), "candidate.open_target": selector("详情入口"), "candidate.detail": selector("详情正文"),
	}}
	err := OpenCandidateDetail(context.Background(), browser, cfg, model.Candidate{Index: 0})
	if !errors.Is(err, model.ErrCandidateDetailUnavailable) || len(browser.clicks) != 2 || len(browser.probes) != 2 {
		t.Fatalf("空详情被当成成功或没有正确重试：%v %+v", err, browser)
	}
	for index, probe := range browser.probes {
		if !probe.ProbeOnly || !probe.Diagnostic || !probe.ExpectedMissing || probe.DiagnosticAttempt != index+1 || !browser.clicks[index].Diagnostic || browser.clicks[index].DiagnosticAttempt != index+1 {
			t.Fatalf("第 %d 次详情诊断或只探测参数缺失：%+v", index+1, probe)
		}
	}
}
