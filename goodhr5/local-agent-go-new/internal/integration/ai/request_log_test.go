// Package ai 文件作用：以真实HTTP验证请求次数、重试历史、早评分禁止重发和可信上游关联，日志不含请求正文。
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// requestLogCollector 保存异步回调的副本，不与正在接收的评分请求共享切片。
type requestLogCollector struct {
	mu     sync.Mutex
	lines  []RequestLogLine
	causes []error
}

// report 读取与岗位路由相同的归一化日志，不保存任何请求头或正文。
func (r *requestLogCollector) report(line []byte, cause error) {
	var value RequestLogLine
	if json.Unmarshal(line, &value) != nil {
		panic("invalid request log")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, value)
	r.causes = append(r.causes, cause)
}

// snapshot 返回稳定副本，供请求结束后的断言使用。
func (r *requestLogCollector) snapshot() []RequestLogLine {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]RequestLogLine(nil), r.lines...)
}

// TestAIRequestLogRetryOutcomes 验证失败后成功、失败耗尽和退避取消都保留每个真实HTTP的开始结束，不增加次数。
func TestAIRequestLogRetryOutcomes(t *testing.T) {
	for _, mode := range []string{"recover", "exhaust", "cancel_wait"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				n := calls.Add(1)
				if mode == "recover" && n > 1 {
					fmt.Fprint(w, chatEnvelopeForTest(`{"score":80,"reason":"ok"}`))
					return
				}
				w.WriteHeader(503)
				fmt.Fprint(w, "temporary")
			}))
			defer server.Close()
			collector := &requestLogCollector{}
			ctx = WithRequestLogReporter(ctx, "fixed-task", "fixed-position", func(line []byte, cause error) {
				collector.report(line, cause)
				if mode == "cancel_wait" && cause != nil {
					cancel()
				}
			})
			_, err := New().EvaluateCandidatePreview(ctx, cloud.AIConfig{BaseURL: server.URL, APIKey: "PRIVATE_API_KEY", Model: "actual-model"}, cloud.PositionSnapshot{}, model.Candidate{Summary: "PRIVATE_RESUME_PROMPT"})
			want := int32(3)
			if mode == "recover" {
				want = 2
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("真实失败被丢掉")
			}
			if mode == "cancel_wait" {
				want = 1
				if !errors.Is(err, context.Canceled) {
					t.Fatal("退避取消原链丢失")
				}
			}
			if calls.Load() != want {
				t.Fatalf("真实请求次数改变：%d", calls.Load())
			}
			lines := collector.snapshot()
			var starts, ends []RequestLogLine
			for _, line := range lines {
				event := line.Output.Attempt
				if event.Phase == "start" {
					starts = append(starts, line)
				} else if event.Phase == "end" {
					ends = append(ends, line)
				}
				encoded, _ := json.Marshal(line)
				if strings.Contains(string(encoded), "PRIVATE_API_KEY") || strings.Contains(string(encoded), "PRIVATE_RESUME_PROMPT") {
					t.Fatal("日志泄露密钥或请求正文")
				}
			}
			if len(starts) != int(want) || len(ends) != int(want) {
				t.Fatalf("开始或结束日志缺失：%d/%d", len(starts), len(ends))
			}
			for i := range starts {
				start, end := starts[i].Output.Attempt, ends[i].Output.Attempt
				if start.ClientRequestID == "" || start.ClientRequestID != end.ClientRequestID || start.AnalysisGroupID == "" || start.AnalysisGroupID != starts[0].Output.Attempt.AnalysisGroupID || start.ClientAttempt != int64(i+1) || end.Model != "actual-model" || end.Operation != "候选人基础预分析" || ends[i].TaskID != "fixed-task" || end.ElapsedMS < 0 || end.FinishedAt == "" {
					t.Fatalf("请求关联/模型/操作/计时不正确：%+v", end)
				}
				if i > 0 && start.ClientRequestID == starts[i-1].Output.Attempt.ClientRequestID {
					t.Fatal("重试重复使用HTTP编号")
				}
			}
			if !ends[0].IsError || ends[0].Output.Attempt.HTTPStatus != 503 || !ends[0].Output.Attempt.WillRetry || ends[0].Output.Attempt.BackoffMS != 500 {
				t.Fatal("首个失败或重试退避没有保留")
			}
			if ends[0].Output.Attempt.ErrorDetail != "temporary" || ends[0].Output.Attempt.ResponseBytes != int64(len("temporary")) || strings.Count(ends[0].ErrorMessage, "temporary") != 1 {
				t.Fatal("失败具体说明或响应字节未保留，或正文重复说明")
			}
			if mode == "recover" && ends[1].Output.Attempt.Result != "success" {
				t.Fatal("成功回执未保留")
			}
			if mode == "exhaust" && (ends[2].Output.Attempt.WillRetry || ends[2].Output.Attempt.StopReason != "attempts_exhausted") {
				t.Fatal("耗尽次数后仍声称重试")
			}
			if mode == "exhaust" && !strings.Contains(ends[2].ErrorMessage, "temporary") {
				t.Fatal("耗尽后只剩笼统提示，最后一次具体原因丢失")
			}
			if mode == "cancel_wait" && lines[len(lines)-1].Output.Attempt.Phase != "retry_wait_stopped" {
				t.Fatal("计划重试被取消的事实丢失")
			}
		})
	}
}

// TestAIRequestLogEarlyScoreDoesNotRetry 验证首个评分可用后网络断开，最终错误仍保留且不会再发模型请求。
func TestAIRequestLogEarlyScoreDoesNotRetry(t *testing.T) {
	collector := &requestLogCollector{}
	ctx := WithRequestLogReporter(context.Background(), "task", "position", collector.report)
	var calls atomic.Int32
	client := New()
	client.http.Transport = aiDiagnosticTransport(func(_ *http.Request) (*http.Response, error) {
		calls.Add(1)
		body := io.MultiReader(strings.NewReader(streamEnvelopeForTest(`{"score":88,"reason":"已可用"}`)), failingAIReader{})
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(body)}, nil
	})
	evaluation, err := client.EvaluateCandidatePreviewEarly(ctx, cloud.AIConfig{BaseURL: "http://fixture", APIKey: "private", Model: "early-model"}, cloud.PositionSnapshot{}, model.Candidate{})
	if err != nil || evaluation.Decision.Score != 88 || evaluation.Final == nil {
		t.Fatalf("真实提前评分丢失：%+v %v", evaluation, err)
	}
	final := <-evaluation.Final
	if final.Err == nil || !errors.Is(final.Err, io.ErrUnexpectedEOF) || calls.Load() != 1 {
		t.Fatalf("早评分后重发或丢失原错误：calls=%d err=%v", calls.Load(), final.Err)
	}
	lines := collector.snapshot()
	if len(lines) != 2 || lines[1].Output.Attempt.WillRetry || lines[1].Output.Attempt.StopReason != "early_score_available" {
		t.Fatalf("早评分后错误重试决策不正确：%+v", lines)
	}
}

// TestAIRequestLogSingleAndTools 验证简历导入维持单次请求，工具重试各有真实记录且不重复工具调用。
func TestAIRequestLogSingleAndTools(t *testing.T) {
	for _, tools := range []bool{false, true} {
		t.Run(fmt.Sprint(tools), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) == 1 {
					w.WriteHeader(503)
					fmt.Fprint(w, "temporary")
					return
				}
				fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
			}))
			defer server.Close()
			collector := &requestLogCollector{}
			ctx := WithRequestLogReporter(context.Background(), "task", "position", collector.report)
			cfg := cloud.AIConfig{BaseURL: server.URL, APIKey: "secret", Model: "single-model"}
			var err error
			if tools {
				_, err = New().ChatWithTools(ctx, cfg, ToolChatRequest{Messages: []ToolMessage{{Role: "user", Content: "PRIVATE_CHAT"}}, Tools: []ToolDefinition{{Type: "function", Function: ToolFunction{Name: "fixture", Parameters: json.RawMessage(`{"type":"object"}`)}}}})
			} else {
				_, err = New().ParseResume(ctx, cfg, "PRIVATE_RESUME")
			}
			want := int32(1)
			if tools {
				want = 2
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("简历解析失败未保留")
			}
			if calls.Load() != want || len(collector.snapshot()) != int(want)*2 {
				t.Fatal("单次导入或工具请求次数/日志改变")
			}
			if !tools && collector.snapshot()[1].Output.Attempt.WillRetry {
				t.Fatal("单次解析被标记为准备重试")
			}
		})
	}
}

// TestAIRequestLogTrustedUpstreamHistory 验证仅可信协商代理的成功响应可以带回真实失败历史与备用模型，外部自称记录无效。
func TestAIRequestLogTrustedUpstreamHistory(t *testing.T) {
	for _, trusted := range []bool{false, true} {
		t.Run(fmt.Sprint(trusted), func(t *testing.T) {
			collector := &requestLogCollector{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				id, group := r.Header.Get("X-GoodHR-Client-Request-ID"), r.Header.Get("X-GoodHR-Analysis-Group-ID")
				if trusted && (id == "" || group == "") {
					t.Error("可信请求没有关联编号")
				}
				if trusted && r.Header.Get("X-GoodHR-AI-Error-Details") != "1" || !trusted && r.Header.Get("X-GoodHR-AI-Error-Details") != "" {
					t.Error("错误详情能力没有限定在可信内置请求协商")
				}
				now := time.Now().UTC()
				entries := []RequestAttempt{{Layer: "server", ClientRequestID: id, AnalysisGroupID: group, ServerAttempt: 1, MaxAttempts: 4, Operation: "候选人基础预分析", Model: "primary-model", Provider: "primary", StartedAt: now.Add(-20 * time.Millisecond).Format(time.RFC3339Nano), FinishedAt: now.Add(-10 * time.Millisecond).Format(time.RFC3339Nano), ElapsedMS: 10, HTTPStatus: 502, Result: "failed", Reason: "http_status", WillRetry: true, BackoffMS: 500}, {Layer: "server", ClientRequestID: id, AnalysisGroupID: group, ServerAttempt: 2, MaxAttempts: 4, Operation: "候选人基础预分析", Model: "fallback-model", Provider: "fallback", StartedAt: now.Add(-9 * time.Millisecond).Format(time.RFC3339Nano), FinishedAt: now.Format(time.RFC3339Nano), ElapsedMS: 9, HTTPStatus: 200, Result: "success", Reason: "success", ProviderRequestID: "provider-123"}}
				encoded, _ := json.Marshal(entries)
				w.Header().Set("X-GoodHR-Diagnostics", "1")
				w.Header().Set("X-GoodHR-AI-Attempt-Details", string(encoded))
				fmt.Fprint(w, chatEnvelopeForTest(`{"score":80,"reason":"ok"}`))
			}))
			defer server.Close()
			t.Setenv("GOODHR_PUBLIC_BASE_URL", server.URL)
			key := "custom"
			if trusted {
				key = "ghai_fixture"
			}
			ctx := WithRequestLogReporter(context.Background(), "task", "position", collector.report)
			_, err := New().EvaluateCandidatePreview(ctx, cloud.AIConfig{BaseURL: server.URL + "/api/ai-compatible/v1", APIKey: key, Model: "requested-model"}, cloud.PositionSnapshot{}, model.Candidate{})
			if err != nil {
				t.Fatal(err)
			}
			lines := collector.snapshot()
			want := 2
			if trusted {
				want = 6
			}
			if len(lines) != want {
				t.Fatalf("上游历史信任边界或完整记录数不正确：%d", len(lines))
			}
			if trusted {
				var serverEnds []RequestAttempt
				for _, line := range lines {
					v := line.Output.Attempt
					if v.Layer == "server" && v.Phase == "end" {
						serverEnds = append(serverEnds, v)
					}
				}
				if len(serverEnds) != 2 || serverEnds[0].Result != "failed" || serverEnds[1].Model != "fallback-model" || serverEnds[1].Provider != "fallback" {
					t.Fatal("成功后的真实失败/备用模型历史被抹去")
				}
			}
		})
	}
}

// TestAIRequestLogHeaderValidation 验证关联不符、未知分类、过大或过多回执不会混入岗位，也不记录额外正文。
func TestAIRequestLogHeaderValidation(t *testing.T) {
	now := time.Now().UTC()
	base := RequestAttempt{Layer: "server", ClientRequestID: "req-fixed", AnalysisGroupID: "group-fixed", ServerAttempt: 1, MaxAttempts: 4, Operation: "chat_completions", Model: "路径/模型-" + strings.Repeat("x", 140), Provider: "primary", StartedAt: now.Add(-time.Millisecond).Format(time.RFC3339Nano), FinishedAt: now.Format(time.RFC3339Nano), ElapsedMS: 1, HTTPStatus: 200, Result: "failed", Reason: "model_json_invalid", WillRetry: true}
	for _, mode := range []string{"valid_unknown_fields", "wrong_request", "wrong_group", "unknown_reason", "unknown_stop", "too_many", "too_large"} {
		t.Run(mode, func(t *testing.T) {
			collector := &requestLogCollector{}
			record := &requestLogAttempt{ctx: WithRequestLogReporter(context.Background(), "task", "position", collector.report), event: RequestAttempt{ClientRequestID: "req-fixed", AnalysisGroupID: "group-fixed", ClientAttempt: 1, Operation: "候选人详情评分"}}
			entry := base
			switch mode {
			case "wrong_request":
				entry.ClientRequestID = "other"
			case "wrong_group":
				entry.AnalysisGroupID = "other"
			case "unknown_reason":
				entry.Reason = "PRIVATE_RAW_HEADER"
			case "unknown_stop":
				entry.StopReason = "PRIVATE_RAW_HEADER"
			}
			entries := []RequestAttempt{entry}
			if mode == "too_many" {
				entries = append(entries, entry, entry, entry, entry)
			}
			encoded, _ := json.Marshal(entries)
			header := string(encoded)
			if mode == "too_large" {
				header = strings.Repeat(" ", 8<<10) + header
			}
			if mode == "valid_unknown_fields" {
				var rows []map[string]json.RawMessage
				_ = json.Unmarshal(encoded, &rows)
				rows[0]["prompt"] = json.RawMessage(`"PRIVATE_RAW_HEADER"`)
				rows[0]["retry_reason"] = json.RawMessage(`"PRIVATE_RAW_HEADER"`)
				encoded, _ = json.Marshal(rows)
				header = string(encoded)
			}
			record.serverAttempts(header, true)
			lines := collector.snapshot()
			if mode != "valid_unknown_fields" {
				if len(lines) != 0 {
					t.Fatal("不可信或越界元数据被接受")
				}
				return
			}
			if len(lines) != 2 || lines[1].Output.Attempt.Model != base.Model || lines[1].Output.Attempt.Operation != "候选人详情评分" || lines[1].Output.Attempt.RetryReason != "model_json_invalid" {
				t.Fatal("合法实际模型或固定业务归属丢失")
			}
			safe, _ := json.Marshal(lines)
			if strings.Contains(string(safe), "PRIVATE_RAW_HEADER") {
				t.Fatal("未知字段或原始header正文进入日志")
			}
		})
	}
}

// TestAIRequestLogToolRoundsStayIndependent 验证多轮工具请求各自有独立分析编号，不把不同轮次当成重试累计。
func TestAIRequestLogToolRoundsStayIndependent(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer server.Close()
	collector := &requestLogCollector{}
	ctx := WithRequestLogReporter(context.Background(), "task", "position", collector.report)
	client := New()
	cfg := cloud.AIConfig{BaseURL: server.URL, APIKey: "secret", Model: "tools-model"}
	request := ToolChatRequest{Messages: []ToolMessage{{Role: "user", Content: "PRIVATE_CHAT"}}, Tools: []ToolDefinition{{Type: "function", Function: ToolFunction{Name: "fixture", Parameters: json.RawMessage(`{"type":"object"}`)}}}}
	for range 2 {
		if _, err := client.ChatWithTools(ctx, cfg, request); err != nil {
			t.Fatal(err)
		}
	}
	lines := collector.snapshot()
	if calls.Load() != 2 || len(lines) != 4 || lines[0].Output.Attempt.AnalysisGroupID == lines[2].Output.Attempt.AnalysisGroupID || lines[2].Output.Attempt.ClientAttempt != 1 {
		t.Fatal("工具轮次的实际次数或分组发生混淆")
	}
}

// TestAIRequestLogJSONFailureIsNotStreamFailure 验证完整HTTP JSON缺少正文时，不把内容校验失败误报为事件流中断。
func TestAIRequestLogJSONFailureIsNotStreamFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[]}`)
	}))
	defer server.Close()
	collector := &requestLogCollector{}
	ctx := WithRequestLogReporter(context.Background(), "task", "position", collector.report)
	_, err := New().EvaluateCandidatePreview(ctx, cloud.AIConfig{BaseURL: server.URL, APIKey: "secret", Model: "model"}, cloud.PositionSnapshot{}, model.Candidate{})
	lines := collector.snapshot()
	if err == nil || len(lines) != 2 || lines[1].Output.Attempt.Reason != "response_invalid" || lines[1].Output.Attempt.WillRetry || lines[1].Output.Attempt.HTTPStatus != 200 {
		t.Fatal("完整JSON内容错误被误分为网络中断或被重试")
	}
}
