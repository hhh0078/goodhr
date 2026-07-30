// Package greeting 文件作用：验证候选人 AI 预判断结果会按页面原顺序消费。
package greeting

import (
	"context"
	"testing"
	"time"
)

// TestOrderCandidatePreviews 验证乱序完成的预判断结果会恢复为页面顺序。
func TestOrderCandidatePreviews(t *testing.T) {
	results := make(chan candidatePreviewResult, 3)
	ordered := make(chan candidatePreviewResult, 3)
	results <- candidatePreviewResult{Index: 2}
	results <- candidatePreviewResult{Index: 0}
	results <- candidatePreviewResult{Index: 1}
	close(results)

	go orderCandidatePreviews(context.Background(), results, ordered)
	index := 0
	for item := range ordered {
		if item.Index != index {
			t.Fatalf("预判断顺序错误：得到 %d，期望 %d", item.Index, index)
		}
		index++
	}
	if index != 3 {
		t.Fatalf("预判断结果数量错误：得到 %d，期望 3", index)
	}
}

// TestOrderCandidatePreviewsDoesNotWaitForWholeBatch 验证第一位完成后立即交给主流程，不等待后续候选人。
func TestOrderCandidatePreviewsDoesNotWaitForWholeBatch(t *testing.T) {
	results := make(chan candidatePreviewResult)
	ordered := make(chan candidatePreviewResult)
	go orderCandidatePreviews(context.Background(), results, ordered)
	go func() {
		results <- candidatePreviewResult{Index: 0}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	item, ok, err := waitCandidatePreview(ctx, nil, ordered)
	if err != nil || !ok || item.Index != 0 {
		t.Fatalf("first preview = %+v, open=%v, err=%v", item, ok, err)
	}
	close(results)
}

// TestCandidateBatchLimitReached 验证默认无限加载，同时保留明确配置的批数上限。
func TestCandidateBatchLimitReached(t *testing.T) {
	if candidateBatchLimitReached(0, 3) {
		t.Fatal("未配置批数时不应在第三批自动结束")
	}
	if candidateBatchLimitReached(5, 4) {
		t.Fatal("明确配置五批时不应提前结束")
	}
	if !candidateBatchLimitReached(5, 5) {
		t.Fatal("明确配置五批时应在第五批完成后结束")
	}
}
