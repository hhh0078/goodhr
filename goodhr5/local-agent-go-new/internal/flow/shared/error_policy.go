// Package shared 文件作用：统一判断岗位级致命错误，并记录连续同类操作错误。
package shared

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/ocr"
)

const consecutiveOperationErrorLimit = 3

var operationErrorNumberPattern = regexp.MustCompile(`\d+`)

// ConsecutiveErrorPolicy 记录当前连续出现的同类操作错误。
type ConsecutiveErrorPolicy struct {
	fingerprint string
	count       int
}

// Record 记录一次操作错误；致命错误立即返回，同类错误连续三次时返回停止原因。
// err 为已经包含操作环节的错误，返回 nil 表示允许跳过当前处理对象继续运行。
func (p *ConsecutiveErrorPolicy) Record(err error) error {
	if err == nil {
		p.Reset()
		return nil
	}
	if ShouldStopTaskImmediately(err) {
		return err
	}
	fingerprint := normalizeOperationError(err)
	if fingerprint == p.fingerprint {
		p.count++
	} else {
		p.fingerprint = fingerprint
		p.count = 1
	}
	if p.count >= consecutiveOperationErrorLimit {
		return fmt.Errorf("同一操作连续 3 次出现相同错误，岗位先停一下：%w", err)
	}
	return nil
}

// Reset 在一次候选人或会话处理成功后清空连续错误次数。
func (p *ConsecutiveErrorPolicy) Reset() {
	if p == nil {
		return
	}
	p.fingerprint = ""
	p.count = 0
}

// ShouldStopTaskImmediately 判断错误是否属于继续运行也不会恢复的岗位级故障。
func ShouldStopTaskImmediately(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || ai.IsPositionStoppingError(err) || ocr.IsUnavailable(err) {
		return true
	}
	var workerErr *contract.WorkerError
	if !errors.As(err, &workerErr) {
		return false
	}
	switch workerErr.Body.Code {
	case "BROWSER_NOT_RUNNING", "PAGE_CLOSED", "PAGE_NOT_AVAILABLE":
		return true
	default:
		return false
	}
}

// normalizeOperationError 归一化错误中的数字和空白，避免超时时间变化打断连续计数。
func normalizeOperationError(err error) string {
	if err == nil {
		return ""
	}
	text := operationErrorNumberPattern.ReplaceAllString(strings.ToLower(err.Error()), "#")
	return strings.Join(strings.Fields(text), " ")
}
