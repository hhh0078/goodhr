// Package ai 文件作用：验证 AI 等待阶段、重试、上游来源与早评分尾部故障能进入同一真实错误链。
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
	"sync/atomic"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/system/failurediag"
)

// TestAIDiagnosticBeforeHeaders 验证已经发出的请求被上层时限中断时，能与 AI 自身时限区分。
func TestAIDiagnosticBeforeHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.Copy(io.Discard, r.Body); <-r.Context().Done() }))
	defer server.Close()
	for _, parentExpires := range []bool{true, false} {
		parentLimit, aiLimit := time.Second, 70*time.Millisecond
		if parentExpires {
			parentLimit, aiLimit = aiLimit, parentLimit
		}
		parent, stop := context.WithTimeout(context.Background(), parentLimit)
		ctx, cancel, _ := ScoringContext(parent, aiLimit)
		_, err := New().EvaluateCandidatePreview(ctx, cloud.AIConfig{BaseURL: server.URL, APIKey: "custom", Model: "test"}, cloud.PositionSnapshot{}, model.Candidate{})
		cancel()
		stop()
		d := failurediag.Details(err)
		reason := "本轮 AI 评分时限已到"
		if parentExpires {
			reason = "上层任务时限已到，取消 AI 等待"
		}
		if len(d) != 1 || d[0].Phase != "等待 AI 响应头" || d[0].Reason != reason || d[0].Operation != "候选人基础预分析" || d[0].HTTPStatus != 0 || d[0].LimitMS <= 0 || d[0].LimitMS > 70 {
			t.Fatalf("响应头尚未返回：err=%v evidence=%+v", err, d)
		}
	}
}

// TestAIStreamFirstContentAndToolDelta 验证已收心跳与已收工具增量能分开诊断，不保存实际工具数据。
func TestAIStreamFirstContentAndToolDelta(t *testing.T) {
	for _, hasTool := range []bool{false, true} {
		body := ": heartbeat\n\n"
		if hasTool {
			body += "data: " + `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"私密参数"}}]}}]}` + "\n\n"
		}
		c := &Client{http: &http.Client{Transport: aiDiagnosticTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Request: r, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(io.MultiReader(strings.NewReader(body), failingAIReader{}))}, nil
		})}}
		_, err := c.doToolChatRequest(context.Background(), "http://example.test/v1/chat/completions", "not-a-real-key", []byte("{}"))
		var se *ServiceError
		d := failurediag.Details(err)
		want := "等待首段 AI 内容"
		if hasTool {
			want = "读取 AI 流式内容"
		}
		if !errors.As(err, &se) || !se.Retryable || len(d) != 1 || d[0].Phase != want || d[0].ReceivedBytes != int64(len(body)) {
			t.Fatalf("实际流进度不符：err=%v evidence=%+v", err, d)
		}
		public, _ := json.Marshal(d)
		if strings.Contains(string(public), "私密参数") || strings.Contains(string(public), "not-a-real-key") {
			t.Fatal("诊断泄露请求内容")
		}
	}
}

// TestAIStatusAndRetryCancellation 验证网关、服务端超时与限流分类不同，退避取消保留真实原因和上次 HTTP 状态。
func TestAIStatusAndRetryCancellation(t *testing.T) {
	for _, status := range []int{408, 429, 502, 504} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "5")
			http.Error(w, "simulated upstream", status)
		}))
		var err error
		if status == 429 {
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
			_, err = New().ChatWithTools(ctx, cloud.AIConfig{BaseURL: s.URL, APIKey: "custom", Model: "test"}, ToolChatRequest{Messages: []ToolMessage{{Role: "user"}}, Tools: []ToolDefinition{{Type: "function"}}})
			cancel()
			if !errors.Is(err, context.DeadlineExceeded) || IsPositionStoppingError(err) {
				t.Fatal("退避错误不得继承上次可重试服务故障的停止行为")
			}
		} else {
			_, err = New().doChatRequest(context.Background(), s.URL, "custom", []byte("{}"), chatOptions{})
		}
		s.Close()
		details := failurediag.Details(err)
		if status == 429 && (len(details) < 2 || details[0].Phase != "等待下一次重试" || details[0].LimitMS != 5000) {
			t.Fatalf("退避上下文丢失：%+v", details)
		}
		var received bool
		for _, d := range details {
			if d.HTTPStatus == status && strings.Contains(d.Reason, fmt.Sprint(status)) && (status == 429 || d.Impact == "外部服务内部原因未知") {
				received = true
			}
		}
		if !received {
			t.Fatalf("丢失状态 %d 的独立分类：%+v", status, details)
		}
	}
}

// TestTrustedProxyTailPreservesDecisionAndBilling 验证兼容代理报告尾部中断时保留已出评分和计费；外部同名事件没有权限伪造内置故障。
func TestTrustedProxyTailPreservesDecisionAndBilling(t *testing.T) {
	const chunk = `data: {"choices":[{"delta":{"content":"{\"score\":80,\"reason\":\"满足\"}"}}]}`
	const signal = `data: {"choices":[],"goodhr_upstream_failure":{"phase":"read_stream","reason":"timeout","http_status":200,"limit_ms":180000,"elapsed_ms":180001,"received_bytes":123}}`
	var headers atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-GoodHR-Diagnostics") == "1" {
			headers.Add(1)
		}
		w.Header().Set("X-GoodHR-Diagnostics", "1")
		w.Header().Set("X-GoodHR-Billing", "1")
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, chunk)
		fmt.Fprintln(w, signal)
		fmt.Fprintln(w, `data: {"choices":[],"goodhr_billing":{"status":"unavailable"}}`)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer s.Close()
	t.Setenv("GOODHR_PUBLIC_BASE_URL", s.URL)
	for _, key := range []string{"custom", "ghai_test"} {
		billed := make(chan Billing, 1)
		ctx := WithBillingReporter(context.Background(), func(b Billing) { billed <- b })
		evaluation, err := New().EvaluateCandidatePreviewEarly(ctx, cloud.AIConfig{BaseURL: s.URL + "/api/ai-compatible/v1", APIKey: key, Model: "test"}, cloud.PositionSnapshot{}, model.Candidate{})
		if err != nil || evaluation.Final == nil || evaluation.Decision.Score != 80 {
			t.Fatalf("已出评分丢失：%+v %v", evaluation, err)
		}
		final := <-evaluation.Final
		if b := <-billed; b.Status != "unavailable" {
			t.Fatalf("尾部计费回执未处理：%+v", b)
		}
		d := failurediag.Details(final.Err)
		if key == "custom" {
			if final.Err != nil {
				t.Fatal("外部 API 的自称代理故障应被忽略")
			}
			continue
		}
		if len(d) != 2 || d[0].Impact != "评分已返回，完整输出未收完" || d[1].Source != "内置 AI 代理" || d[1].LimitMS != 180000 {
			t.Fatalf("丢失已出评分或上游诊断：%+v", d)
		}
		resp, ok := ResponseDiagnosticFromError(final.Err)
		if !ok || resp.ResponseComplete || resp.RequestAttempt != 1 {
			t.Fatalf("应保留流不完整且不重试的原始结果：%+v", resp)
		}
	}
	if headers.Load() != 1 {
		t.Fatalf("只有可信内置请求才协商；实际次数 %d", headers.Load())
	}
}

// TestAIProxyHeaderRequiresNegotiatedOrigin 验证未升级代理和外部接口不能声称已取得内部计时证据。
func TestAIProxyHeaderRequiresNegotiatedOrigin(t *testing.T) {
	for _, v := range []struct{ trusted, negotiated bool }{{true, true}, {true, false}, {false, true}} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if v.negotiated {
				w.Header().Set("X-GoodHR-Diagnostics", "1")
			}
			w.Header().Set("X-GoodHR-Upstream-Failure", `{"phase":"wait_headers","reason":"timeout","elapsed_ms":180005,"limit_ms":180000}`)
			http.Error(w, "上游请求失败", http.StatusBadGateway)
		}))
		if v.trusted {
			t.Setenv("GOODHR_PUBLIC_BASE_URL", s.URL)
		} else {
			t.Setenv("GOODHR_PUBLIC_BASE_URL", "https://unrelated.example.test")
		}
		_, err := New().doChatRequest(context.Background(), s.URL+"/api/ai-compatible/v1/chat/completions", "ghai_fixture", []byte("{}"), chatOptions{Operation: "候选人详情评分"})
		s.Close()
		d := failurediag.Details(err)
		if len(d) < 1 || d[0].HTTPStatus != 502 {
			t.Fatalf("客户端收到的 502 状态丢失：%v", d)
		}
		if v.trusted && v.negotiated {
			if len(d) != 2 || d[1].HTTPStatus != 0 || d[1].Phase != "等待上游响应头" || d[1].Reason != "等待上游超时" {
				t.Fatalf("内置 502 应展示已知上游超时：%v", d)
			}
		} else if len(d) != 1 {
			t.Fatalf("来源或协议未经核对却接受了上游证据：%v", d)
		}
	}
}

// TestAIProxyConnectionPhases 验证可信代理细分 DNS、TCP 和 TLS，不把未知阶段猜成连接失败。
func TestAIProxyConnectionPhases(t *testing.T) {
	for phase, want := range map[string]string{"dns_lookup": "解析上游模型域名", "tcp_connect": "建立上游 TCP 连接", "tls_handshake": "与上游进行 TLS 握手", "anything_else": ""} {
		attempt := &aiAttempt{trustedResponse: true}
		payload, err := json.Marshal(map[string]string{"phase": phase, "reason": "timeout"})
		if err != nil {
			t.Fatal(err)
		}
		attempt.proxySignal(payload)
		if want == "" && attempt.proxy != nil || want != "" && (attempt.proxy == nil || attempt.proxy.Phase != want) {
			t.Fatalf("代理阶段 %s 映射有误：%+v", phase, attempt.proxy)
		}
	}
}

// TestAIStatusBeforeInterruptedBody 验证错误响应先到、正文后中断时分别保留状态和取消原因。
func TestAIStatusBeforeInterruptedBody(t *testing.T) {
	for _, status := range []int{408, 429, 504} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(status)
			_, _ = io.WriteString(w, "part")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}))
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		_, err := New().doChatRequest(ctx, s.URL, "fixture", []byte("{}"), chatOptions{})
		cancel()
		s.Close()
		d := failurediag.Details(err)
		if len(d) != 2 || !strings.Contains(d[0].Reason, fmt.Sprintf("HTTP %d", status)) || d[0].HTTPStatus != status || d[1].Phase != "读取 AI 错误响应正文" || d[1].Reason != "调用方等待时限已到" || d[1].ReceivedBytes != 4 {
			t.Fatalf("状态与读正文错误应同时保留，实际 %+v，%v", d, err)
		}
	}
}

// TestAIMissingStreamEndOnlyForExistingParseError 验证不强制供应商发送结束标记，已有解析失败时才标记完整性未知。
func TestAIMissingStreamEndOnlyForExistingParseError(t *testing.T) {
	for _, tc := range []struct {
		name, content, end string
		valid, unknown     bool
	}{
		{"缺结束标记", "{bad", "", false, true},
		{"已有 DONE", "{bad", "data: [DONE]\n", false, false},
		{"已有 finish_reason", "{bad", "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n", false, false},
		{"可解析的无标记服务", "{\"score\":80,\"reason\":\"符合\"}", "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				data, _ := json.Marshal(tc.content)
				fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%s}}]}\n%s", data, tc.end)
			}))
			defer s.Close()
			result, err := New().EvaluateCandidatePreview(context.Background(), cloud.AIConfig{BaseURL: s.URL, APIKey: "fixture", Model: "test"}, cloud.PositionSnapshot{}, model.Candidate{})
			if tc.valid {
				if err != nil || result.Score != 80 {
					t.Fatalf("兼容的成功响应被当成失败：%+v %v", result, err)
				}
				return
			}
			diag, exists := ResponseDiagnosticFromError(err)
			if !exists || diag.ResponseCompletionUnknown != tc.unknown || diag.ResponseComplete == tc.unknown {
				t.Fatalf("已有解析失败的完整性诊断错误：%+v, %v", diag, err)
			}
			d := failurediag.Details(err)
			if tc.unknown && (len(d) != 1 || !strings.Contains(d[0].Reason, "完整性未知")) || !tc.unknown && len(d) != 0 {
				t.Fatalf("流结束信号证据有误：%+v", d)
			}
		})
	}
}

// TestAIElapsedAloneCannotIdentifyHTTPClientTimeout 验证耗时碰巧超过配置值不能证明 HTTP 客户端的计时器先触发。
func TestAIElapsedAloneCannotIdentifyHTTPClientTimeout(t *testing.T) {
	a := aiAttempt{ctx: context.Background(), started: time.Now().Add(-time.Second), limit: time.Millisecond}
	if got := a.boundaryReason(context.DeadlineExceeded); got != "AI 网络请求超时" {
		t.Fatalf("单凭耗时误认领了客户端超时：%s", got)
	}
}

// TestBuiltinUnmarkedUpstreamOnlyDiagnosesParseFailure 验证代理补发 DONE 不会抹去真实上游证据，也不会损失正常计费或可用评分。
func TestBuiltinUnmarkedUpstreamOnlyDiagnosesParseFailure(t *testing.T) {
	for _, valid := range []bool{true, false} {
		content := "{bad"
		if valid {
			content = "{\"score\":81,\"reason\":\"符合\"}"
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-GoodHR-Diagnostics", "1")
			w.Header().Set("X-GoodHR-Billing", "1")
			w.Header().Set("Content-Type", "text/event-stream")
			encoded, _ := json.Marshal(content)
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%s}}]}\n", encoded)
			fmt.Fprintln(w, `data: {"choices":[],"goodhr_upstream_failure":{"phase":"read_stream","reason":"completion_unknown","elapsed_ms":16,"received_bytes":123,"http_status":200}}`)
			fmt.Fprintln(w, `data: {"choices":[],"goodhr_billing":{"status":"charged","cost_units":2}}`)
			fmt.Fprintln(w, "data: [DONE]")
		}))
		t.Setenv("GOODHR_PUBLIC_BASE_URL", server.URL)
		var bill Billing
		ctx := WithBillingReporter(context.Background(), func(b Billing) { bill = b })
		decision, err := New().EvaluateCandidatePreview(ctx, cloud.AIConfig{BaseURL: server.URL + "/api/ai-compatible/v1", APIKey: "ghai_fixture", Model: "test"}, cloud.PositionSnapshot{}, model.Candidate{})
		server.Close()
		if bill.CostUnits == nil || *bill.CostUnits != 2 {
			t.Fatalf("计费回执丢失：%+v", bill)
		}
		if valid {
			if err != nil || decision.Score != 81 {
				t.Fatalf("代理正常上游 EOF 错记成了失败：%+v %v", decision, err)
			}
			continue
		}
		diag, ok := ResponseDiagnosticFromError(err)
		d := failurediag.Details(err)
		if !ok || !diag.ResponseCompletionUnknown || len(d) != 1 || d[0].Source != "内置 AI 代理" || d[0].ReceivedBytes != 123 || d[0].HTTPStatus != 200 {
			t.Fatalf("代理补发 DONE 不应覆盖上游缺结束标记：%+v %+v %v", diag, d, err)
		}
	}
}

// TestAIHeartbeatDeadlineAtFirstAndLastAttempt 验证真实连接挂在首段正文时，最后一次网络读取仍归属于实际到期的那层。
func TestAIHeartbeatDeadlineAtFirstAndLastAttempt(t *testing.T) {
	for _, lastAttempt := range []int{1, 3} {
		for _, parentExpires := range []bool{false, true} {
			t.Run(fmt.Sprintf("attempt%d-parent%t", lastAttempt, parentExpires), func(t *testing.T) {
				t.Parallel()
				var requests atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.Copy(io.Discard, r.Body)
					if requests.Add(1) < int32(lastAttempt) {
						http.Error(w, "temporary", 502)
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, ": heartbeat\n\n")
					w.(http.Flusher).Flush()
					<-r.Context().Done()
				}))
				defer server.Close()
				budget := 130 * time.Millisecond
				if lastAttempt == 3 {
					budget += 2 * time.Second
				}
				parentBudget, aiBudget := budget+time.Second, budget
				want := "本轮 AI 评分时限已到"
				if parentExpires {
					parentBudget, aiBudget = aiBudget, parentBudget
					want = "上层任务时限已到，取消 AI 等待"
				}
				parent, stop := context.WithTimeout(context.Background(), parentBudget)
				defer stop()
				ctx, cancel, _ := ScoringContext(parent, aiBudget)
				defer cancel()
				_, err := New().EvaluateCandidatePreview(ctx, cloud.AIConfig{BaseURL: server.URL, APIKey: "fixture", Model: "test"}, cloud.PositionSnapshot{}, model.Candidate{})
				var observed bool
				for _, d := range failurediag.Details(err) {
					if d.Attempt != int64(lastAttempt) || d.Phase != "等待首段 AI 内容" {
						continue
					}
					observed = d.Reason == want && d.HTTPStatus == 200 && d.ReceivedBytes == 13 && d.ElapsedMS > 0
				}
				if !observed {
					t.Fatalf("未在发生等待的 S​SE 读取步骤标出时限来源：%+v %v", failurediag.Details(err), err)
				}
			})
		}
	}
}
