// Package positionrunner 文件作用：验证岗位运行级、候选人级和连续平台错误的分类策略。
package positionrunner

import (
	"errors"
	"strings"
	"testing"

	"goodhr5/local-agent-go/internal/localai"
)

// TestConsecutiveOperationErrorsStopAtThree 验证同一平台环节连续三次相同错误会停止岗位运行。
func TestConsecutiveOperationErrorsStopAtThree(t *testing.T) {
	tracker := &consecutiveOperationErrorTracker{}
	for index := 1; index <= 3; index++ {
		err := &candidateOperationError{Operation: "执行打招呼", Err: errors.New("selector timeout 1000ms")}
		stopErr := stopAfterCandidateOperationError(tracker, err)
		if index < 3 && stopErr != nil {
			t.Fatalf("第%d次不应停止：%v", index, stopErr)
		}
		if index == 3 && (stopErr == nil || !strings.Contains(stopErr.Error(), "连续3个候选人")) {
			t.Fatalf("第三次应停止岗位运行：%v", stopErr)
		}
	}
}

// TestPositionStoppingAIErrorStopsImmediately 验证永久性 AI 错误无需等待连续三次。
func TestPositionStoppingAIErrorStopsImmediately(t *testing.T) {
	tracker := &consecutiveOperationErrorTracker{}
	err := &localai.ServiceError{StatusCode: 402, Body: "余额不足", Fatal: true}
	if stopErr := stopAfterCandidateOperationError(tracker, err); stopErr == nil {
		t.Fatal("AI余额不足应立即停止岗位运行")
	}
}

// TestOperationErrorResetBreaksConsecutiveCount 验证一次成功会清除平台连续错误计数。
func TestOperationErrorResetBreaksConsecutiveCount(t *testing.T) {
	tracker := &consecutiveOperationErrorTracker{}
	err := &candidateOperationError{Operation: "读取候选人详情", Err: errors.New("selector timeout 1000ms")}
	_ = tracker.Record(err)
	_ = tracker.Record(err)
	tracker.Reset("读取候选人详情")
	if count := tracker.Record(err); count != 1 {
		t.Fatalf("成功后应重新从1计数，count=%d", count)
	}
}

// TestFatalOCRErrorClassification 验证 OCR 组件不可用会停止岗位运行，而单张图片未识别到文字不会停止。
func TestFatalOCRErrorClassification(t *testing.T) {
	if !isFatalOCRError(errors.New("OCR 组件未安装，请先安装 RapidOCR-json 运行组件")) {
		t.Fatal("OCR组件未安装应停止岗位运行")
	}
	if isFatalOCRError(errors.New("OCR 未识别到文字")) {
		t.Fatal("单张图片未识别到文字只应跳过当前候选人")
	}
}
