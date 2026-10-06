// Package ai 文件作用：保留 AI 失败时的完整实际返回与解析位置，供岗位诊断日志统一读取。
package ai

import (
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
)

var responseDiagnosticSequence atomic.Uint64

// ResponseDiagnostic 保存失败响应的原始内容，不包含请求密钥，也不截断实际返回。
type ResponseDiagnostic struct {
	Model             string `json:"model"`
	Response          string `json:"response"`
	ResponseKind      string `json:"response_kind"`
	ResponseBody      string `json:"response_body,omitempty"`
	RequestAttempt    int    `json:"request_attempt,omitempty"`
	FinalRequestError string `json:"final_request_error,omitempty"`
	ParseError        string `json:"parse_error"`
	ParseField        string `json:"parse_field,omitempty"`
	ParseOffset       int64  `json:"parse_offset,omitempty"`
	ResponseComplete  bool   `json:"response_complete"`
}

// ResponseError 在保留原错误分类与短文案的同时，附带完整 AI 失败回复。
type ResponseError struct {
	Cause        error
	Diagnostic   ResponseDiagnostic
	diagnosticID atomic.Uint64
}

// DiagnosticID 返回当前失败回复的稳定唯一编号，日志去重无需缓存完整原文。
func (e *ResponseError) DiagnosticID() uint64 {
	if e == nil {
		return 0
	}
	if id := e.diagnosticID.Load(); id != 0 {
		return id
	}
	var original *ResponseError
	var id uint64
	if errors.As(e.Cause, &original) && original != nil {
		id = original.DiagnosticID()
	} else {
		id = responseDiagnosticSequence.Add(1)
	}
	if e.diagnosticID.CompareAndSwap(0, id) {
		return id
	}
	return e.diagnosticID.Load()
}

// Error 返回原来的简短错误说明，完整返回只在诊断出参中展示。
func (e *ResponseError) Error() string {
	if e == nil || e.Cause == nil {
		return "AI 返回内容无法识别"
	}
	return e.Cause.Error()
}

// Unwrap 保留网络重试、岗位停止与 JSON 解析错误的原有判断能力。
func (e *ResponseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// ResponseDiagnosticFromError 从流程包装过的错误中读取 AI 完整失败诊断。
func ResponseDiagnosticFromError(err error) (ResponseDiagnostic, bool) {
	var responseErr *ResponseError
	if !errors.As(err, &responseErr) || responseErr == nil {
		return ResponseDiagnostic{}, false
	}
	return responseErr.Diagnostic, true
}

// newResponseError 包装实际收到的响应，并提取 JSON 出错字段和从一开始计算的字节位置。
func newResponseError(cause error, response string, kind string, complete bool) *ResponseError {
	diagnostic := ResponseDiagnostic{
		Response: response, ResponseKind: kind, ResponseComplete: complete,
	}
	if cause != nil {
		diagnostic.ParseError = cause.Error()
	}
	var syntaxErr *json.SyntaxError
	if errors.As(cause, &syntaxErr) {
		diagnostic.ParseOffset = syntaxErr.Offset
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(cause, &typeErr) {
		diagnostic.ParseField = typeErr.Field
		diagnostic.ParseOffset = typeErr.Offset
	}
	return &ResponseError{Cause: cause, Diagnostic: diagnostic}
}

// newParsedResponseError 保留转换前的原文，并把解析位置还原到该原文中的字节位置。
func newParsedResponseError(cause error, response string, parsed string, kind string) *ResponseError {
	responseErr := newResponseError(cause, response, kind, true)
	if responseErr.Diagnostic.ParseOffset > 0 {
		if prefix := strings.Index(response, parsed); prefix > 0 {
			responseErr.Diagnostic.ParseOffset += int64(prefix)
		}
	}
	return responseErr
}

// withResponseModel 补充当前请求模型，保留原始错误链且不修改其他调用持有的错误对象。
func withResponseModel(err error, model string) error {
	if diagnostic, ok := ResponseDiagnosticFromError(err); ok {
		diagnostic.Model = strings.TrimSpace(model)
		return &ResponseError{Cause: err, Diagnostic: diagnostic}
	}
	return err
}

// streamResponseError 同时保留已接收的模型正文和原始事件流，便于定位中断或协议错误。
func streamResponseError(cause error, content string, body string, complete bool) *ResponseError {
	response, kind := content, "model_content"
	if content == "" {
		response, kind = body, "sse_body"
	}
	responseErr := newResponseError(cause, response, kind, complete)
	responseErr.Diagnostic.ResponseBody = body
	return responseErr
}
