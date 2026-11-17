// Package common 文件作用：复现跳过底部候选人、长列表追加、虚拟列表刷新和加载等待的回归场景。
package common

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// scrollingCandidateBrowser 模拟只在真实滚轮到达底部后才追加或替换候选人的页面。
type scrollingCandidateBrowser struct {
	model.Browser
	appendAfterReads                          int
	count, scrolls, moveThrough, visibleAfter int
	appendAt, replaceAt, generation           int
	missingProgress, missingHead              bool
	duplicateNames                            bool
	scrollError                               error
	onScroll                                  func()
	requests                                  []contract.ScrollRequest
}

// FindAll 按真实列表序号返回指定读取窗口，超出已加载范围时返回空列表。
func (b *scrollingCandidateBrowser) FindAll(ctx context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.missingHead {
		return nil, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
	}
	if b.scrolls > 0 && b.appendAfterReads > 0 {
		b.appendAfterReads--
		if b.appendAfterReads == 0 {
			b.count += 20
		}
	}
	items := []contract.FindAllItem{}
	for index := request.StartIndex; index < min(b.count, request.StartIndex+request.MaxItems); index++ {
		if request.ProbeOnly {
			items = append(items, contract.FindAllItem{Index: index, TotalMatches: b.count})
			continue
		}
		name := fmt.Sprintf("候选人%d", index+b.generation)
		if b.duplicateNames && index%100 == 5 {
			name = "同名候选人"
		}
		items = append(items, contract.FindAllItem{Index: index, Text: name + " 30岁", Fields: map[string]string{
			"name": name, "age": "30", "platform_candidate_id": fmt.Sprint(index + b.generation),
		}})
	}
	return items, nil
}

// probeOnlyNextBrowser 模拟下一批完整读取需要等待，但存在性探测能立即返回的慢页面。
type probeOnlyNextBrowser struct {
	*scrollingCandidateBrowser
	probes, fullReads int
}

// FindAll 拒绝在滚动阶段读取下一批资料，确保该阶段只复用无字段的单卡探测。
func (b *probeOnlyNextBrowser) FindAll(ctx context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if request.StartIndex >= 20 {
		if !request.ProbeOnly {
			b.fullReads++
			return nil, context.DeadlineExceeded
		}
		if request.MaxItems != 1 || len(request.Fields) != 0 || !request.ExpectedMissing {
			return nil, fmt.Errorf("新人探测读取了整批或候选资料：%+v", request)
		}
		b.probes++
	}
	return b.scrollingCandidateBrowser.FindAll(ctx, request)
}

// TestCandidateScrollProbesNextWithoutReadingBatch 验证新人已加载或滚动后追加时，剩余预算很少也不重复读取整批资料。
func TestCandidateScrollProbesNextWithoutReadingBatch(t *testing.T) {
	for _, afterScroll := range []bool{false, true} {
		t.Run(fmt.Sprintf("滚动后追加=%t", afterScroll), func(t *testing.T) {
			inner := &scrollingCandidateBrowser{count: 20, appendAt: 2}
			if !afterScroll {
				inner.count, inner.appendAt = 40, 0
			}
			browser := &probeOnlyNextBrowser{scrollingCandidateBrowser: inner}
			cfg := scrollCandidateConfig()
			cfg.MaxItems = 20
			options := model.CandidateListOptions{Cursor: &model.CandidateListCursor{}}
			before, err := FindCandidates(context.Background(), browser, cfg, "test", options)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			advanced, err := AdvanceCandidateList(ctx, browser, cfg, "test", before, options)
			if err != nil || !advanced || options.Cursor.StartIndex != 20 || browser.fullReads != 0 || browser.probes == 0 {
				t.Fatalf("轻量探测失败：advanced=%v cursor=%d full=%d probes=%d err=%v", advanced, options.Cursor.StartIndex, browser.fullReads, browser.probes, err)
			}
			if !afterScroll && inner.scrolls != 0 {
				t.Fatal("已有新人时不应额外滚动")
			}
			// 下一批用原有完整读取路径，字段和行号保持完整，探测结果不能代替候选人资料。
			next, err := FindCandidates(context.Background(), inner, cfg, "test", options)
			if err != nil || len(next) != 20 || next[0].Index != 20 || next[0].Name == "" || next[0].Fields["platform_candidate_id"] == "" {
				t.Fatalf("下一批资料读取不完整：len=%d err=%v", len(next), err)
			}
		})
	}
}

// Scroll 记录真实滚轮请求，按场景模拟列表移动、到底、追加内容或浏览器错误。
func (b *scrollingCandidateBrowser) Scroll(_ context.Context, request contract.ScrollRequest) (contract.ScrollResult, error) {
	b.scrolls++
	b.requests = append(b.requests, request)
	if b.onScroll != nil {
		b.onScroll()
	}
	if b.scrollError != nil {
		return contract.ScrollResult{}, b.scrollError
	}
	if b.appendAt > 0 && b.scrolls >= b.appendAt {
		b.count += 20
		b.appendAt = 0
	}
	if b.replaceAt > 0 && b.scrolls >= b.replaceAt {
		b.generation += 1000
		b.replaceAt = 0
	}
	visible, moved := b.scrolls >= b.visibleAfter, b.scrolls <= b.moveThrough
	result := contract.ScrollResult{Scrolled: moved, Attempts: 1}
	if !b.missingProgress {
		result.ProgressTargetVisible = &visible
		result.ProgressTargetMoved = &moved
	}
	return result, nil
}

// scrollCandidateConfig 提供滚动回归使用的最小平台配置，不包含任何真实网站操作。
func scrollCandidateConfig() model.Config {
	return model.Config{ID: "test", Name: "测试平台", MaxItems: 100, ScrollDistance: 620,
		Behavior: model.Behavior{CandidateListMode: "infinite_scroll"},
		Selectors: map[string]contract.SelectorSpec{
			"candidate.item": {Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".candidate"}}}, Description: "候选人卡片"},
			"candidate.list": {Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".list"}}}, Description: "候选人列表"},
		},
	}
}

// TestAdvanceCandidateScrollContinuesPastSkippedTail 验证全部候选人提前跳过后仍会滚超过三次并加载下一批。
func TestAdvanceCandidateScrollContinuesPastSkippedTail(t *testing.T) {
	browser := &scrollingCandidateBrowser{count: 20, moveThrough: 8, visibleAfter: 7, appendAt: 8}
	cfg := scrollCandidateConfig()
	options := model.CandidateListOptions{ReadConcurrency: 10, Cursor: &model.CandidateListCursor{}}
	before, err := FindCandidates(context.Background(), browser, cfg, "test", options)
	if err != nil {
		t.Fatal(err)
	}
	advanced, err := AdvanceCandidateList(context.Background(), browser, cfg, "test", before, options)
	if err != nil || !advanced || browser.scrolls != 8 {
		t.Fatalf("advanced=%v scrolls=%d err=%v", advanced, browser.scrolls, err)
	}
	if options.Cursor.StartIndex != 20 {
		t.Fatalf("下一批起点=%d，预期20", options.Cursor.StartIndex)
	}
	for _, request := range browser.requests {
		if request.Target != nil || request.ProgressTarget == nil || request.ProgressTarget.Target.Index == nil || *request.ProgressTarget.Target.Index != 19 || request.WheelAnchor == nil {
			t.Fatalf("没有用真实滚轮追踪已加载列表末尾：%+v", request)
		}
	}
}

// TestCandidateAppendBeyondBatchLimit 验证已加载超过一百人时按窗口继续读取，不漏读后续候选人。
func TestCandidateAppendBeyondBatchLimit(t *testing.T) {
	browser := &scrollingCandidateBrowser{count: 250}
	cfg := scrollCandidateConfig()
	options := model.CandidateListOptions{Cursor: &model.CandidateListCursor{}}
	for _, expected := range []struct{ start, count int }{{0, 100}, {100, 100}, {200, 50}} {
		candidates, err := FindCandidates(context.Background(), browser, cfg, "test", options)
		if err != nil || len(candidates) != expected.count || candidates[0].Index != expected.start {
			t.Fatalf("候选人窗口错误：len=%d cursor=%d err=%v", len(candidates), options.Cursor.StartIndex, err)
		}
		if expected.start < 200 {
			advanced, err := AdvanceCandidateList(context.Background(), browser, cfg, "test", candidates, options)
			if err != nil || !advanced {
				t.Fatalf("加载后续窗口失败：advanced=%v err=%v", advanced, err)
			}
		}
	}
	if browser.scrolls != 0 {
		t.Fatalf("已经加载的后续候选人不需要额外滚轮：%d", browser.scrolls)
	}
}

// TestCandidateWindowPreservesDuplicateIdentity 验证跨批同名同龄候选人的定位顺序，重复读取不会重复累计。
func TestCandidateWindowPreservesDuplicateIdentity(t *testing.T) {
	browser := &scrollingCandidateBrowser{count: 150, duplicateNames: true}
	cfg := scrollCandidateConfig()
	options := model.CandidateListOptions{Cursor: &model.CandidateListCursor{}}
	before, err := FindCandidates(context.Background(), browser, cfg, "test", options)
	if err != nil {
		t.Fatal(err)
	}
	advanced, err := AdvanceCandidateList(context.Background(), browser, cfg, "test", before, options)
	if err != nil || !advanced {
		t.Fatalf("advanced=%v err=%v", advanced, err)
	}
	for read := 0; read < 2; read++ {
		next, err := FindCandidates(context.Background(), browser, cfg, "test", options)
		if err != nil || len(next) != 50 {
			t.Fatalf("下一批读取失败：len=%d err=%v", len(next), err)
		}
		selector, err := CandidateSelector(cfg, next[5])
		if err != nil || next[5].Index != 105 || next[5].IdentityOccurrence != 1 || selector.Target.Index == nil || *selector.Target.Index != 1 {
			t.Fatalf("跨批同名候选人定位错误：candidate=%+v selector=%+v err=%v", next[5], selector, err)
		}
	}
}

// TestVirtualCandidateListResetsCursor 验证虚拟列表替换旧行时重新从零读取新行，避免旧序号挡住新候选人。
func TestVirtualCandidateListResetsCursor(t *testing.T) {
	browser := &scrollingCandidateBrowser{count: 120, moveThrough: 2, replaceAt: 2, visibleAfter: 3}
	cfg := scrollCandidateConfig()
	options := model.CandidateListOptions{Cursor: &model.CandidateListCursor{StartIndex: 100, IdentityOccurrences: map[string]int{"旧行": 2}}}
	before, _ := FindCandidates(context.Background(), browser, cfg, "test", options)
	advanced, err := AdvanceCandidateList(context.Background(), browser, cfg, "test", before, options)
	if err != nil || !advanced || options.Cursor.StartIndex != 0 {
		t.Fatalf("advanced=%v cursor=%d err=%v", advanced, options.Cursor.StartIndex, err)
	}
	if len(options.Cursor.IdentityOccurrences) != 0 {
		t.Fatal("虚拟列表刷新后没有清空旧行的身份顺序")
	}
	next, _ := FindCandidates(context.Background(), browser, cfg, "test", options)
	if next[0].Fields["platform_candidate_id"] != "1000" {
		t.Fatalf("没有读取替换后的列表：%+v", next[0])
	}
}

// TestCandidateScrollConfirmsStableBottom 验证只有列表末尾可见、向下滚轮无位移并充分等待后才返回正常结束。
func TestCandidateScrollConfirmsStableBottom(t *testing.T) {
	t.Parallel()
	browser := &scrollingCandidateBrowser{count: 20}
	cfg := scrollCandidateConfig()
	options := model.CandidateListOptions{Cursor: &model.CandidateListCursor{}}
	before, _ := FindCandidates(context.Background(), browser, cfg, "test", options)
	started := time.Now()
	advanced, err := AdvanceCandidateList(context.Background(), browser, cfg, "test", before, options)
	// 首轮滚动前会先追赶一次本批末尾，因此总滚轮次数为稳定轮数加一。
	if err != nil || advanced || browser.scrolls != candidateBottomStableRounds+1 {
		t.Fatalf("advanced=%v scrolls=%d err=%v", advanced, browser.scrolls, err)
	}
	if time.Since(started) < 15*time.Second {
		t.Fatal("底部没有给网络加载保留足够的等待时间")
	}
}

// TestCandidateScrollWaitsForDelayedAppend 验证到底后网络延迟追加时仍等待新人，不提前判定结束。
func TestCandidateScrollWaitsForDelayedAppend(t *testing.T) {
	t.Parallel()
	browser := &scrollingCandidateBrowser{count: 20, appendAfterReads: 6}
	cfg := scrollCandidateConfig()
	options := model.CandidateListOptions{Cursor: &model.CandidateListCursor{}}
	before, _ := FindCandidates(context.Background(), browser, cfg, "test", options)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	started := time.Now()
	advanced, err := AdvanceCandidateList(ctx, browser, cfg, "test", before, options)
	if err != nil || !advanced {
		t.Fatalf("advanced=%v err=%v", advanced, err)
	}
	if time.Since(started) < candidateListPollInterval {
		t.Fatal("没有等待底部的异步加载")
	}
}

// TestCandidateScrollErrorsAreNotCompletion 验证未到底卡住、缺少滚动证据和浏览器关闭都不能被当成结束。
func TestCandidateScrollErrorsAreNotCompletion(t *testing.T) {
	for _, test := range []struct {
		name    string
		browser *scrollingCandidateBrowser
		want    string
	}{
		{"未到底卡住", &scrollingCandidateBrowser{count: 20, visibleAfter: 1000}, "列表末尾还没进入窗口"},
		{"旧工具没有位置证据", &scrollingCandidateBrowser{count: 20, missingProgress: true}, "没有返回列表末尾的位置"},
		{"浏览器关闭", &scrollingCandidateBrowser{count: 20, scrollError: errors.New("browser closed")}, "browser closed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := scrollCandidateConfig()
			before, _ := FindCandidates(context.Background(), test.browser, cfg, "test", model.CandidateListOptions{})
			advanced, err := AdvanceCandidateList(context.Background(), test.browser, cfg, "test", before, model.CandidateListOptions{})
			if advanced || err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("advanced=%v err=%v", advanced, err)
			}
		})
	}
}

// TestCandidateScrollCancellationAndMissingList 验证等待过程可取消，暂时读不到列表时不能返回正常结束。
func TestCandidateScrollCancellationAndMissingList(t *testing.T) {
	t.Parallel()
	browser := &scrollingCandidateBrowser{count: 20}
	cfg := scrollCandidateConfig()
	before, _ := FindCandidates(context.Background(), browser, cfg, "test", model.CandidateListOptions{})
	browser.onScroll = func() { browser.missingHead = true }
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()
	advanced, err := AdvanceCandidateList(ctx, browser, cfg, "test", before, model.CandidateListOptions{})
	if advanced || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("advanced=%v err=%v", advanced, err)
	}
}

// TestAdvanceCandidateScrollCatchesUpTailOnce 验证连续跳过后首轮滚动前先一次追到本批末尾，且只追赶一次。
func TestAdvanceCandidateScrollCatchesUpTailOnce(t *testing.T) {
	browser := &scrollingCandidateBrowser{count: 20, moveThrough: 8, visibleAfter: 7, appendAt: 8}
	cfg := scrollCandidateConfig()
	options := model.CandidateListOptions{ReadConcurrency: 10, Cursor: &model.CandidateListCursor{}}
	before, err := FindCandidates(context.Background(), browser, cfg, "test", options)
	if err != nil {
		t.Fatal(err)
	}
	advanced, err := AdvanceCandidateList(context.Background(), browser, cfg, "test", before, options)
	if err != nil || !advanced {
		t.Fatalf("advanced=%v err=%v", advanced, err)
	}
	catchUps := 0
	for index, request := range browser.requests {
		if !request.StopWhenProgressVisible {
			continue
		}
		catchUps++
		if index != 0 || request.Distance != candidateTailCatchUpDistance || request.MaxAttempts != candidateTailCatchUpMaxAttempts ||
			request.ProgressTarget == nil || request.ProgressTarget.Target.Index == nil || *request.ProgressTarget.Target.Index != 19 || request.WheelAnchor == nil {
			t.Fatalf("追赶请求不符合预期：第%d次 %+v", index+1, request)
		}
	}
	if catchUps != 1 {
		t.Fatalf("追赶本批末尾次数=%d，预期1", catchUps)
	}
}

// TestAdvanceCandidateScrollSkipsCatchUpWhenNextLoaded 验证下一批已在页面中时不追赶也不滚动，直接进入下一批。
func TestAdvanceCandidateScrollSkipsCatchUpWhenNextLoaded(t *testing.T) {
	browser := &scrollingCandidateBrowser{count: 60}
	cfg := scrollCandidateConfig()
	cfg.MaxItems = 20
	options := model.CandidateListOptions{Cursor: &model.CandidateListCursor{}}
	before, err := FindCandidates(context.Background(), browser, cfg, "test", options)
	if err != nil {
		t.Fatal(err)
	}
	advanced, err := AdvanceCandidateList(context.Background(), browser, cfg, "test", before, options)
	if err != nil || !advanced || browser.scrolls != 0 || options.Cursor.StartIndex != 20 {
		t.Fatalf("advanced=%v scrolls=%d start=%d err=%v", advanced, browser.scrolls, options.Cursor.StartIndex, err)
	}
}

// catchUpFailingBrowser 只让追赶本批末尾的滚轮失败，其余滚轮沿用原模拟。
type catchUpFailingBrowser struct {
	*scrollingCandidateBrowser
}

// Scroll 追赶请求返回浏览器错误，用于验证追赶失败不会终止加载。
func (b catchUpFailingBrowser) Scroll(ctx context.Context, request contract.ScrollRequest) (contract.ScrollResult, error) {
	if request.StopWhenProgressVisible {
		b.requests = append(b.requests, request)
		return contract.ScrollResult{}, errors.New("合成追赶失败")
	}
	return b.scrollingCandidateBrowser.Scroll(ctx, request)
}

// TestAdvanceCandidateScrollContinuesWhenCatchUpFails 验证追赶失败只记日志，仍按原逐轮滚动加载出下一批。
func TestAdvanceCandidateScrollContinuesWhenCatchUpFails(t *testing.T) {
	inner := &scrollingCandidateBrowser{count: 20, moveThrough: 3, visibleAfter: 2, appendAt: 3}
	browser := catchUpFailingBrowser{inner}
	cfg := scrollCandidateConfig()
	options := model.CandidateListOptions{Cursor: &model.CandidateListCursor{}}
	before, err := FindCandidates(context.Background(), browser, cfg, "test", options)
	if err != nil {
		t.Fatal(err)
	}
	advanced, err := AdvanceCandidateList(context.Background(), browser, cfg, "test", before, options)
	if err != nil || !advanced || inner.scrolls != 3 {
		t.Fatalf("追赶失败后没有继续加载：advanced=%v scrolls=%d err=%v", advanced, inner.scrolls, err)
	}
}
