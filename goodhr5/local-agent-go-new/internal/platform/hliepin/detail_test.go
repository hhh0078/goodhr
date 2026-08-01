// Package hliepin 文件作用：验证猎聘猎头端关闭候选人详情后按原地址唯一返回标签页。
package hliepin

import (
	"context"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// detailPageBrowser 模拟关闭详情前后的标签页，并记录最终切换目标。
type detailPageBrowser struct {
	model.Browser
	pages      contract.PageListResult
	closed     bool
	useRequest *contract.PageUseRequest
}

// ListPages 返回当前模拟的全部标签页。
func (b *detailPageBrowser) ListPages(context.Context) (contract.PageListResult, error) {
	return b.pages, nil
}

// ClosePage 记录候选人详情页已经关闭。
func (b *detailPageBrowser) ClosePage(context.Context) error {
	b.closed = true
	return nil
}

// UsePage 记录按地址匹配后选择的标签页。
func (b *detailPageBrowser) UsePage(_ context.Context, request contract.PageUseRequest) (contract.PageInfo, error) {
	b.useRequest = &request
	return contract.PageInfo{PageID: request.PageID}, nil
}

// TestCloseCandidateDetailReturnsByUniqueURL 验证唯一匹配原地址时会切回关闭后的最新标签页编号。
func TestCloseCandidateDetailReturnsByUniqueURL(t *testing.T) {
	const returnURL = "https://h.liepin.com/candidate/recommend?job=123"
	browser := &detailPageBrowser{pages: contract.PageListResult{Pages: []contract.PageInfo{
		{PageID: "page-1", URL: "https://www.zhipin.com/web/geek/recommend"},
		{PageID: "page-2", URL: returnURL},
	}}}
	runtime := NewRuntime()
	runtime.detailReturnURL = returnURL

	err := runtime.CloseCandidateDetail(context.Background(), browser, model.Config{}, model.Candidate{})
	if err != nil {
		t.Fatalf("唯一地址应该可以正常返回：%v", err)
	}
	if !browser.closed {
		t.Fatalf("候选人详情页没有关闭")
	}
	if browser.useRequest == nil || browser.useRequest.PageID != "page-2" {
		t.Fatalf("没有切回原地址对应的标签页：%+v", browser.useRequest)
	}
}

// TestCloseCandidateDetailRejectsDuplicateURL 验证存在两个相同地址时直接报错且不猜测标签页。
func TestCloseCandidateDetailRejectsDuplicateURL(t *testing.T) {
	const returnURL = "https://h.liepin.com/candidate/recommend?job=123"
	browser := &detailPageBrowser{pages: contract.PageListResult{Pages: []contract.PageInfo{
		{PageID: "page-1", URL: returnURL},
		{PageID: "page-2", URL: returnURL},
	}}}
	runtime := NewRuntime()
	runtime.detailReturnURL = returnURL

	err := runtime.CloseCandidateDetail(context.Background(), browser, model.Config{}, model.Candidate{})
	if err == nil || !strings.Contains(err.Error(), "2 个地址相同") {
		t.Fatalf("重复地址应该返回明确错误：%v", err)
	}
	if browser.useRequest != nil {
		t.Fatalf("重复地址时不应该猜测并切换标签页：%+v", browser.useRequest)
	}
}

// TestCloseCandidateDetailRejectsMissingURL 验证原地址已经不存在时不会切换到其他平台页面。
func TestCloseCandidateDetailRejectsMissingURL(t *testing.T) {
	const returnURL = "https://h.liepin.com/candidate/recommend?job=123"
	browser := &detailPageBrowser{pages: contract.PageListResult{Pages: []contract.PageInfo{
		{PageID: "page-1", URL: "https://www.zhipin.com/web/geek/recommend"},
	}}}
	runtime := NewRuntime()
	runtime.detailReturnURL = returnURL

	err := runtime.CloseCandidateDetail(context.Background(), browser, model.Config{}, model.Candidate{})
	if err == nil || !strings.Contains(err.Error(), "已经不在了") {
		t.Fatalf("原地址缺失应该返回明确错误：%v", err)
	}
	if browser.useRequest != nil {
		t.Fatalf("原地址缺失时不应该切换到其他标签页：%+v", browser.useRequest)
	}
}
