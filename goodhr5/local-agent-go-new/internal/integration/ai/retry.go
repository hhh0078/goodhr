// Package ai 本文件统一执行 AI 请求与结果校验，首次失败后最多重试三次，并保留逐次诊断和计费归属。
package ai

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/system/failurediag"
)

const maxAIAttempts = 4

// runAIRequest 统一执行请求和校验；call 每次只发一次请求，账号错误与调用方取消立即结束。
func runAIRequest[T any](ctx context.Context, operation, model string, call func(int) (T, error)) (T, error) {
	var empty T
	var previous []failurediag.Detail
	var received *ResponseDiagnostic
	for attempt := 1; attempt <= maxAIAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return empty, withAIHistory(err, previous)
		}
		result, err := call(attempt)
		if err == nil {
			return result, nil
		}
		err = withResponseModel(err, model)
		if diagnostic, ok := ResponseDiagnosticFromError(err); ok {
			diagnostic.RequestAttempt = attempt
			received = &diagnostic
			err = &ResponseError{Cause: err, Diagnostic: diagnostic}
		} else if received != nil {
			diagnostic := *received
			diagnostic.FinalRequestError = err.Error()
			err = &ResponseError{Cause: err, Diagnostic: diagnostic}
		}
		var service *ServiceError
		retryable := !errors.As(err, &service) || service.Retryable
		kind, status := "response_invalid", 0
		if service != nil {
			kind, status = "transport_or_service", service.StatusCode
		}
		log.Printf("[AI调用失败] operation=%s attempt=%d max_attempts=%d error_kind=%s http_status=%d", operation, attempt, maxAIAttempts, kind, status)
		if canceled := ctx.Err(); canceled != nil {
			// 读取层可能返回自定义取消原因，仍保留标准取消标识供岗位收尾识别。
			if !errors.Is(err, canceled) {
				err = fmt.Errorf("%w；%w", err, canceled)
			}
			return empty, withAIHistory(err, previous)
		}
		if errors.Is(err, context.Canceled) || !retryable || attempt == maxAIAttempts {
			return empty, withAIHistory(err, previous)
		}
		delay := time.Duration(attempt) * 500 * time.Millisecond
		if service != nil {
			delay = max(delay, service.RetryAfter)
		}
		// 日志仅含操作和次数；上游正文、候选人资料与密钥不能进入公共日志。
		log.Printf("[AI重试] operation=%s failed_attempt=%d max_attempts=%d wait=%s", operation, attempt, maxAIAttempts, delay)
		started := time.Now()
		if waitErr := waitRetry(ctx, delay); waitErr != nil {
			waitErr = withAIHistory(retryWaitError(ctx, waitErr, err, operation, attempt, started, delay), previous)
			if diagnostic, ok := ResponseDiagnosticFromError(err); ok {
				return empty, &ResponseError{Cause: waitErr, Diagnostic: diagnostic}
			}
			return empty, waitErr
		}
		previous = append(previous, failurediag.Details(err)...)
	}
	return empty, nil
}

// chatResult 将结果解析纳入 chat 的同一重试预算，调用方无需捕获异常或另写重试循环。
func chatResult[T any](ctx context.Context, c *Client, cfg cloud.AIConfig, messages []chatMessage, options chatOptions, parse func(string) (T, error)) (T, error) {
	var result T
	completion := options.Completion
	if completion == nil {
		completion = &streamCompletion{}
		options.Completion = completion
	}
	options.Validate = func(content string) error {
		var err error
		result, err = parse(content)
		if err != nil {
			if _, exists := ResponseDiagnosticFromError(err); !exists {
				err = newResponseError(err, content, "model_content", true)
			}
		}
		return completion.parseError(withResponseModel(err, cfg.Model))
	}
	_, err := c.chat(ctx, cfg, messages, options)
	if err != nil {
		var empty T
		return empty, err
	}
	return result, nil
}
