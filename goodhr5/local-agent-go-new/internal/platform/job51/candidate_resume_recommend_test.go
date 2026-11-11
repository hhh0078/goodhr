// Package job51 文件作用：验证推荐页换序与未加载断点的恢复，以及搜索页原有顺序保护。
package job51

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// resumeAppendBrowser 只提供合成公开编号、最小字段读取和追加滚轮，不允许真实点击。
type resumeAppendBrowser struct {
	job51BulkResumeBrowser
	all         []string
	scrollCount int
	scrollErr   error
}

// FindAll 兼容Worker真实的尾部窗口索引，编号仍交给正式identityBrowser剥离索引尾缀。
func (b *resumeAppendBrowser) FindAll(ctx context.Context, req contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req.FromEnd {
		req.StartIndex = max(0, len(b.ids)-req.MaxItems)
	}
	return b.job51BulkResumeBrowser.FindAll(ctx, req)
}

// Scroll 模拟每次滚轮追加20张卡片，并返回既有列表推进方法要求的尾部证明。
func (b *resumeAppendBrowser) Scroll(ctx context.Context, req contract.ScrollRequest) (contract.ScrollResult, error) {
	if err := ctx.Err(); err != nil {
		return contract.ScrollResult{}, err
	}
	b.scrollCount++
	if b.scrollErr != nil {
		return contract.ScrollResult{}, b.scrollErr
	}
	if req.WheelAnchor == nil || req.ProgressTarget == nil {
		return contract.ScrollResult{}, errors.New("滚轮没有绑定原列表")
	}
	b.ids = b.all[:min(len(b.all), len(b.ids)+20)]
	b.source = resumeListHTML(b.ids)
	yes := true
	return contract.ScrollResult{ProgressTargetVisible: &yes, ProgressTargetMoved: &yes}, nil
}

// TestRecommendResumeFollowsPublicID 验证推荐页前缀变化仍按公开编号续跑，搜索页不放松前缀保护，缺失和重复仍不猜人。
func TestRecommendResumeFollowsPublicID(t *testing.T) {
	old := []string{"100001", "100002", "100003"}
	point := model.CandidateResumePoint{CandidateID: old[1], PrefixHash: candidatePrefixHash(old[:2])}
	cfg := loadPositionConfig(t)
	browser := &identityScopeStub{source: resumeListHTML([]string{"100003", "100002", "100001"})}
	cursor := &model.CandidateListCursor{}
	found, err := NewRuntime().RestoreCandidateList(context.Background(), browser, cfg, point, cursor)
	if err != nil || !found || cursor.StartIndex != 2 || len(browser.scrolls) != 0 {
		t.Fatalf("推荐换序没有按编号恢复：%v %v %+v", found, err, cursor)
	}
	runtime := NewRuntime()
	runtime.manualSearch = true
	found, err = runtime.RestoreCandidateList(context.Background(), browser, cfg, point, cursor)
	if err != nil || found {
		t.Fatal("搜索页的原顺序保护被放松")
	}
	browser.source = resumeListHTML([]string{"100002", "100002"})
	found, err = NewRuntime().RestoreCandidateList(context.Background(), browser, cfg, point, cursor)
	if err == nil || found {
		t.Fatal("重复编号不能用于恢复")
	}
	// 静态名单没有追加能力，唯一编号确实缺失才允许从头。
	cfg.Behavior.CandidateListMode = "static"
	browser.source = resumeListHTML([]string{"100003"})
	found, err = NewRuntime().RestoreCandidateList(context.Background(), browser, cfg, point, cursor)
	if err != nil || found || cursor.StartIndex != 0 {
		t.Fatal("缺失锚点没有安全从头")
	}
}

// TestRecommendResumeLoadsUnloadedAnchor 验证首批20人之外的第46人经原滚轮加载后恢复，读取始终小批且真实错误不变成从头。
func TestRecommendResumeLoadsUnloadedAnchor(t *testing.T) {
	ids := make([]string, 60)
	for i := range ids {
		ids[i] = fmt.Sprint(200000 + i)
	}
	point := model.CandidateResumePoint{CandidateID: ids[45], PrefixHash: candidatePrefixHash(ids[:46])}
	for _, failure := range []error{nil, errors.New("合成浏览器断开")} {
		browser := &resumeAppendBrowser{all: ids, scrollErr: failure}
		browser.ids = ids[:20]
		browser.source = resumeListHTML(browser.ids)
		cursor := &model.CandidateListCursor{}
		found, err := NewRuntime().RestoreCandidateList(context.Background(), browser, loadPositionConfig(t), point, cursor)
		if failure != nil {
			if found || !errors.Is(err, failure) {
				t.Fatalf("断开被误判缺失：%v %v", found, err)
			}
		} else if err != nil || !found || cursor.StartIndex != 46 || browser.scrollCount != 2 || browser.maxRead > 20 {
			t.Fatalf("未加载断点没有恢复：%v %v %+v scrolls=%d max=%d", found, err, cursor, browser.scrollCount, browser.maxRead)
		}
	}
}
