// Package common 文件作用：验证详情探测不读取正文、诊断参数逐次传递，空结果不能误判为详情已打开。
package common

import (
	"context"
	"errors"
	"goodhr5/local-agent-go-new/internal/system/failurediag"
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

// TestDetailProbeEmptyResultIsUnavailable 验证单个入口只点击一次，空详情最终返回不可用而不是假成功。
func TestDetailProbeEmptyResultIsUnavailable(t *testing.T) {
	browser := &emptyDetailProbeBrowser{}
	cfg := model.Config{Selectors: map[string]contract.SelectorSpec{
		"candidate.item": selector("候选人卡片"), "candidate.open_target": selector("详情入口"), "candidate.detail": selector("详情正文"),
	}}
	err := OpenCandidateDetail(context.Background(), browser, cfg, model.Candidate{Index: 0})
	if !errors.Is(err, model.ErrCandidateDetailUnavailable) || len(browser.clicks) != 1 || len(browser.probes) != 1 {
		t.Fatalf("空详情被当成成功或没有正确重试：%v %+v", err, browser)
	}
	for index, probe := range browser.probes {
		if !probe.ProbeOnly || !probe.Diagnostic || !probe.ExpectedMissing || probe.DiagnosticAttempt != index+1 || !browser.clicks[index].Diagnostic || browser.clicks[index].DiagnosticAttempt != index+1 {
			t.Fatalf("第 %d 次详情诊断或只探测参数缺失：%+v", index+1, probe)
		}
	}
}

// emptyReadCancellation 在成功读到空正文时取消上层，下一次定时检查尚未开始。
type emptyReadCancellation struct {
	model.Browser
	cancel context.CancelFunc
}

// Read 模拟已经定位正文容器但文本尚空，不访问真实简历。
func (b emptyReadCancellation) Read(context.Context, contract.ElementReadRequest) (contract.ReadResult, error) {
	b.cancel()
	return contract.ReadResult{}, nil
}

// TestCancelWaitingAfterEmptyDetail 验证读空后的待重试与一次 Worker 读取中断可区分。
func TestCancelWaitingAfterEmptyDetail(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := model.Config{Selectors: map[string]contract.SelectorSpec{"candidate.detail": selector("详情正文")}}
	_, err := ExtractCandidateDetail(ctx, emptyReadCancellation{cancel: cancel}, cfg)
	d := failurediag.Details(err)
	if !errors.Is(err, context.Canceled) || len(d) != 1 || d[0].Attempt != 1 || d[0].Phase != "等待下一次正文检查" {
		t.Fatalf("等待正文取消证据丢失：%v %+v", err, d)
	}
}
