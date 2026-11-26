// Package greeting 文件作用：验证候选详情和基础预分析共用真实完整性文案，尾部失败只记诊断，不覆盖提前评分或重试动作。
package greeting

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/system/failurediag"
)

// completionWarning 保存两个收尾入口实际发出的警告，不保存或修改评分状态。
type completionWarning struct {
	step, status string
	err          error
}

// completionWarningLogger 用有界通道捕获后台警告，避免测试与异步收尾读取共享切片。
type completionWarningLogger struct{ warnings chan completionWarning }

// Step 只记录完整输出收尾，不替换提前评分或触发任何页面操作。
func (l *completionWarningLogger) Step(_ string, _ string, step, status string, _ time.Time, err error) {
	l.warnings <- completionWarning{step, status, err}
}

// TestAICompletionWarningsPreserveResultAndEvidence 验证两个真实收尾入口对完整、截断、中断、取消和未知使用相同规则且不重试或回滚。
func TestAICompletionWarningsPreserveResultAndEvidence(t *testing.T) {
	for _, entry := range []string{"detail", "preview"} {
		for _, scenario := range []string{"complete_type_error", "complete_truncated_json", "interrupted", "cancelled", "unknown", "final_request_error"} {
			t.Run(entry+"/"+scenario, func(t *testing.T) {
				var cloudCalls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					cloudCalls.Add(1)
					_, _ = w.Write([]byte(`{"ok":true}`))
				}))
				defer server.Close()
				logger := &completionWarningLogger{warnings: make(chan completionWarning, 4)}
				flow := &Flow{Logger: logger, Cloud: cloud.New(server.URL)}
				rootCause := error(errors.New("模型内容字段类型不匹配"))
				diagnostic := ai.ResponseDiagnostic{Model: "synthetic-model", Response: `{"score":88,"raw_text":[],"code":"ORIGINAL","stack":"original\\nstack"}`, ResponseKind: "model_content", ResponseBody: "原始完整事件流", ParseError: rootCause.Error(), ParseField: "raw_text", ParseOffset: 24, ResponseComplete: true, RequestAttempt: 1}
				prefix := "AI 输出已接收完成，但处理失败"
				switch scenario {
				case "complete_truncated_json":
					diagnostic.Response = `{"score":88,"reason":"unfinished`
					diagnostic.ParseError = "unexpected end of JSON input"
				case "interrupted":
					rootCause = &ai.ServiceError{StatusCode: 502, Message: "实际流中断", Retryable: true, Fatal: true, Cause: io.ErrUnexpectedEOF}
					diagnostic.ResponseComplete = false
					prefix = "AI 完整输出没有接收完"
				case "cancelled":
					rootCause = context.Canceled
					diagnostic.ResponseComplete = false
					prefix = "AI 完整输出没有接收完"
				case "unknown":
					diagnostic.ResponseComplete, diagnostic.ResponseCompletionUnknown = false, true
					prefix = "AI 输出收尾失败"
				case "final_request_error":
					diagnostic.ResponseKind = "http_body"
					diagnostic.FinalRequestError = "最终连接失败"
					rootCause = &ai.ServiceError{StatusCode: 503, Message: "最终连接失败", Fatal: true, Cause: io.ErrClosedPipe}
					prefix = "AI 输出收尾失败"
				}
				response := &ai.ResponseError{Cause: rootCause, Diagnostic: diagnostic}
				cause := failurediag.Wrap(response, failurediag.Detail{Source: "AI 客户端", Operation: "合成评分", Phase: "收尾", Reason: "保留原始现场", ReceivedBytes: 321, HTTPStatus: 200})
				diagnosticID := response.DiagnosticID()
				final := make(chan ai.EvaluationResult, 1)
				final <- ai.EvaluationResult{Err: cause}
				close(final)
				var cancels atomic.Int32
				cancelled := make(chan struct{})
				cancel := func() {
					if cancels.Add(1) == 1 {
						close(cancelled)
					}
				}
				early := ai.Evaluation{Decision: ai.Decision{Score: 88, Reason: "已经用于页面流程的评分"}, Final: final}
				step := "complete_ai_output"
				if entry == "detail" {
					flow.finishCandidateEvaluationAsync(shared.PreparedTask{Request: shared.StartRequest{TaskID: "completion-task"}, Position: cloud.PositionSnapshot{CommonConfig: cloud.PositionCommonConfig{OutputStructuredResume: true}}}, model.Candidate{Name: "合成候选人"}, "greeted", early, cancel)
				} else {
					step = "complete_ai_preview_output"
					flow.finishPreviewOutput("completion-task", final, cancel)
				}
				var warning completionWarning
				select {
				case warning = <-logger.warnings:
				case <-time.After(time.Second):
					t.Fatal("收尾警告没有记录")
				}
				select {
				case <-cancelled:
				case <-time.After(time.Second):
					t.Fatal("收尾没有释放请求上下文")
				}
				if warning.step != step || warning.status != "warning" || !strings.HasPrefix(warning.err.Error(), prefix+"：") || !errors.Is(warning.err, rootCause) {
					t.Fatalf("收尾入口的文案、步骤或原错误被改变：%+v", warning)
				}
				got, ok := ai.ResponseDiagnosticFromError(warning.err)
				var original *ai.ResponseError
				if !ok || !reflect.DeepEqual(got, diagnostic) || !errors.As(warning.err, &original) || original != response || original.DiagnosticID() != diagnosticID || !reflect.DeepEqual(failurediag.Details(warning.err), failurediag.Details(cause)) {
					t.Fatal("收尾改写了原响应、错误字段、故障现场或诊断编号")
				}
				var service *ai.ServiceError
				if expected, isService := rootCause.(*ai.ServiceError); isService && (!errors.As(warning.err, &service) || service != expected || !ai.IsPositionStoppingError(warning.err)) {
					t.Fatal("原服务错误状态码、停止或重试策略丢失")
				}
				if cancels.Load() != 1 || cloudCalls.Load() != 0 || early.Decision.Score != 88 || early.Decision.Reason != "已经用于页面流程的评分" || len(logger.warnings) != 0 {
					t.Fatal("尾部失败重复重试、同步、覆盖提前评分或重复记录")
				}
			})
		}
	}
}
