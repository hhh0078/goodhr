// 本文件统一云端 AI 的请求、解析和结果校验重试；调用方取消、账号与本地账本错误立即结束，不在透明代理重复重试。
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const maxAIAttempts = 4

// aiRetryError 保存明确不可重试的错误或上游要求的等待时间。
type aiRetryError struct {
	cause     error
	permanent bool
	delay     time.Duration
}

// Error 返回原错误的安全提示。
func (e *aiRetryError) Error() string { return e.cause.Error() }

// Unwrap 保留原错误类型供业务判断。
func (e *aiRetryError) Unwrap() error { return e.cause }

// aiRetryExhausted 标记当前模型步骤已经用完重试预算，外层不得重新执行整条付费流程。
type aiRetryExhausted struct{ cause error }

// Error 返回明确尝试次数及最后的安全错误。
func (e *aiRetryExhausted) Error() string {
	return fmt.Sprintf("AI 已尝试%d次，仍未完成：%s", maxAIAttempts, e.cause.Error())
}

// Unwrap 保留最后错误，并让助手流程识别为已经结束的步骤。
func (e *aiRetryExhausted) Unwrap() []error { return []error{e.cause, assistantFatalError{e.Error()}} }

// noAIRetry 标记重发模型也不能修复的配置、权限或本地账本故障。
func noAIRetry(err error) error { return &aiRetryError{cause: err, permanent: true} }

// aiHTTPFailure 区分账号或请求错误与限流、超时、服务端临时错误，并读取 Retry-After。
func aiHTTPFailure(response *http.Response, message string) *aiRetryError {
	status := response.StatusCode
	delay := time.Duration(0)
	value := strings.TrimSpace(response.Header.Get("Retry-After"))
	if seconds, err := strconv.ParseInt(value, 10, 32); err == nil && seconds > 0 {
		delay = time.Duration(seconds) * time.Second
	} else if until, err := http.ParseTime(value); err == nil {
		delay = max(0, time.Until(until))
	}
	return &aiRetryError{cause: errors.New(message), permanent: status != 408 && status != 429 && status < 500, delay: delay}
}

// retryAI 使用唯一四次预算执行单次请求及其校验，每次尝试由原能力负责记录实际费用。
func retryAI[T any](ctx context.Context, operation string, call func(int) (T, error)) (T, error) {
	var empty T
	for attempt := 1; attempt <= maxAIAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		result, err := call(attempt)
		if err == nil {
			return result, nil
		}
		var policy *aiRetryError
		var fatal assistantFatalError
		var embedding *embeddingCallError
		var exhausted *aiRetryExhausted
		if ctx.Err() != nil {
			return empty, ctx.Err()
		}
		if errors.Is(err, context.Canceled) || errors.As(err, &fatal) || errors.As(err, &embedding) || errors.As(err, &exhausted) || errors.As(err, &policy) && policy.permanent {
			return empty, err
		}
		log.Printf("[AI重试] operation=%s failed_attempt=%d max_attempts=%d", operation, attempt, maxAIAttempts)
		if attempt == maxAIAttempts {
			return empty, &aiRetryExhausted{cause: err}
		}
		delay := time.Duration(attempt) * 500 * time.Millisecond
		if policy != nil {
			delay = max(delay, policy.delay)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return empty, ctx.Err()
		case <-timer.C:
		}
	}
	return empty, nil
}
