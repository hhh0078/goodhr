// Package taskrunner 文件作用：统一定义候选人级错误和必须停止任务的错误处理策略。
package taskrunner

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"goodhr5/local-agent-go/internal/localai"
)

var errorNumberPattern = regexp.MustCompile(`\d+`)

// candidateOperationError 表示允许跳过当前候选人、但需要统计连续次数的平台操作错误。
type candidateOperationError struct {
	Operation string
	Err       error
}

// Error 返回候选人操作错误文本。
func (e *candidateOperationError) Error() string {
	if e == nil || e.Err == nil {
		return "候选人操作失败"
	}
	return e.Err.Error()
}

// Unwrap 返回候选人操作错误的底层原因。
func (e *candidateOperationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// consecutiveOperationErrorTracker 记录同一平台环节连续出现的相同错误。
type consecutiveOperationErrorTracker struct {
	states map[string]consecutiveOperationErrorState
}

// consecutiveOperationErrorState 保存单个平台环节最近一次错误指纹和连续次数。
type consecutiveOperationErrorState struct {
	fingerprint string
	count       int
}

// Record 记录一次候选人操作错误并返回当前连续次数。
func (t *consecutiveOperationErrorTracker) Record(err error) int {
	var candidateErr *candidateOperationError
	if !errors.As(err, &candidateErr) {
		return 0
	}
	operation := strings.TrimSpace(candidateErr.Operation)
	fingerprint := normalizeOperationError(candidateErr.Err)
	if t.states == nil {
		t.states = map[string]consecutiveOperationErrorState{}
	}
	state := t.states[operation]
	if fingerprint != state.fingerprint {
		state = consecutiveOperationErrorState{fingerprint: fingerprint, count: 1}
	} else {
		state.count++
	}
	t.states[operation] = state
	return state.count
}

// Reset 在指定平台操作成功后清除该环节的连续错误计数。
func (t *consecutiveOperationErrorTracker) Reset(operation string) {
	if t == nil || t.states == nil {
		return
	}
	delete(t.states, strings.TrimSpace(operation))
}

// normalizeOperationError 归一化错误中的数字和空白，避免超时毫秒数影响相同错误判断。
func normalizeOperationError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	text = errorNumberPattern.ReplaceAllString(text, "#")
	return strings.Join(strings.Fields(text), " ")
}

// stopAfterCandidateOperationError 判断候选人错误是否达到停止整个任务的条件。
func stopAfterCandidateOperationError(tracker *consecutiveOperationErrorTracker, err error) error {
	if shouldStopTaskImmediately(err) {
		return err
	}
	if tracker == nil {
		return nil
	}
	if count := tracker.Record(err); count >= 3 {
		return fmt.Errorf("同一平台环节连续%d个候选人出现相同错误，任务已自动停止：%w", count, err)
	}
	return nil
}

// shouldStopTaskImmediately 判断错误是否属于继续运行会持续失败的任务级错误。
func shouldStopTaskImmediately(err error) bool {
	return err != nil && (localai.IsTaskStoppingError(err) || isBrowserClosedTaskError(err))
}

// isFatalOCRError 判断 OCR 错误是否表示组件未安装、配置损坏或进程已经不可用。
func isFatalOCRError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "ocr 组件未配置") ||
		strings.Contains(text, "ocr 组件未安装") ||
		strings.Contains(text, "ocr 模型文件不完整") ||
		strings.Contains(text, "启动 ocr 组件失败") ||
		strings.Contains(text, "ocr 组件已退出") ||
		strings.Contains(text, "ocr 组件没有返回结果并已关闭输出")
}
