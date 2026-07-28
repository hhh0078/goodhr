// Package greeting 文件作用：验证候选人超时和不可继续错误会按旧版规则停止任务。
package greeting

import (
	"context"
	"errors"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/integration/ocr"
)

// TestCandidateTimeout 验证单个候选人处理上限保持三分钟。
func TestCandidateTimeout(t *testing.T) {
	if candidateTimeout != 180*time.Second {
		t.Fatalf("候选人处理上限应为三分钟，实际为 %s", candidateTimeout)
	}
}

// TestShouldStopImmediately 验证任务取消、浏览器关闭和 OCR 故障立即停止。
func TestShouldStopImmediately(t *testing.T) {
	cases := []error{
		context.Canceled,
		&ocr.Error{Code: ocr.ErrorUnavailable, Message: "组件不可用"},
		&contract.WorkerError{Body: contract.WorkerErrorBody{Code: "BROWSER_NOT_RUNNING"}},
		&contract.WorkerError{Body: contract.WorkerErrorBody{Code: "PAGE_CLOSED"}},
	}
	for _, err := range cases {
		if !shouldStopImmediately(err) {
			t.Fatalf("错误应立即停止任务：%v", err)
		}
	}
	if shouldStopImmediately(errors.New("单个候选人选择器没找到")) {
		t.Fatal("普通候选人错误应继续使用连续错误策略")
	}
}
