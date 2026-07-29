// Package greeting 文件作用：验证候选人 AI 预判断结果会按页面原顺序消费。
package greeting

import (
	"context"
	"testing"
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
