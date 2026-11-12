// Package common 文件作用：复用既有真实滚轮加载断点后续名单，只读最小身份字段，不发送或修改页面条件。
package common

import (
	"context"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// resumeLoadingBrowser 将临时加载探测的摘要固定为稳定身份，避免活跃时间或按钮文字被误认为名单刷新。
type resumeLoadingBrowser struct {
	model.Browser
	cfg model.Config
}

// FindAll 原样保留索引与字段，仅在当前加载探测中用身份摘要比较名单，不改变正常候选人读取。
func (b resumeLoadingBrowser) FindAll(ctx context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	items, err := b.Browser.FindAll(ctx, request)
	if err != nil {
		return nil, err
	}
	items = append([]contract.FindAllItem(nil), items...)
	for index := range items {
		items[index].Text = candidateResumeIdentity(b.cfg, items[index].Fields)
	}
	return items, nil
}

// LoadMoreResumeCandidates 从真实已加载尾部继续滚动，沿用现有到底、无进展和异步加载检查，每次只读一人的身份字段。
// 调用方负责整次查找预算；Windows及两种Mac均走同一Worker标准定位与滚轮能力。
func LoadMoreResumeCandidates(ctx context.Context, browser model.Browser, cfg model.Config) (bool, error) {
	if cfg.Behavior.CandidateListMode != "infinite_scroll" {
		return false, nil
	}
	window := cfg
	window.MaxItems, window.CandidateBatchSize = 1, 1
	window.CandidateFields = candidateResumeFields(cfg)
	loading := resumeLoadingBrowser{Browser: browser, cfg: cfg}
	selector, err := RequiredSelector(cfg, "candidate.item")
	if err != nil {
		return false, err
	}
	items, err := loading.FindAll(ctx, contract.ElementFindAllRequest{
		Selector: selector, MaxItems: 1, FromEnd: true, Fields: window.CandidateFields,
		ReadConcurrency: 5, ExpectedMissing: true,
	})
	if IsElementMissing(err) || (err == nil && len(items) == 0) {
		empty, probeErr := ProbeSelectorExists(ctx, browser, cfg, "candidate.empty")
		if probeErr != nil || empty {
			return false, probeErr
		}
		return false, fmt.Errorf("候选人名单还没读到，也没确认空结果；上次进度保留，请等页面加载后再开始")
	}
	if err != nil {
		return false, err
	}
	item := items[0]
	before := []model.Candidate{{Index: item.Index, Name: strings.TrimSpace(item.Fields["name"]), Summary: item.Text, Fields: item.Fields}}
	return AdvanceCandidateList(ctx, loading, window, cfg.ID, before, model.CandidateListOptions{
		ReadConcurrency: 5, Cursor: &model.CandidateListCursor{StartIndex: item.Index},
	})
}
