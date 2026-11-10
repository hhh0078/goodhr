// Package ai 本文件验证普通及工具请求的费用回执、未知费用、重试与提前评分后的后台结算。
package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestBillingReceiptsForTextAndTools 验证四种请求响应组合都只上报一次回执，未知与负数金额不被当作免费。
func TestBillingReceiptsForTextAndTools(t *testing.T) {
	for _, tools := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			for _, tc := range []struct {
				name, key, receipt, want string
				header                   bool
			}{
				{"charged", "ghai_test", `{"status":"charged","cost_units":23,"model":"actual-model"}`, "charged", true},
				{"zero", "ghai_test", `{"status":"charged","cost_units":0}`, "charged", true},
				{"missing", "ghai_test", "null", "unavailable", true},
				{"old_server", "ghai_test", "null", "unavailable", false},
				{"external", "external-key", `{"status":"charged","cost_units":99}`, "external", false},
				{"invalid", "ghai_test", `{"status":"charged","cost_units":-1}`, "unavailable", true},
				{"failed", "ghai_test", `{"status":"failed","cost_units":99}`, "failed", true},
			} {
				t.Run(fmt.Sprintf("tools=%t/stream=%t/%s", tools, stream, tc.name), func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.Header.Get("X-GoodHR-Billing") != "1" {
							t.Error("请求未声明支持回执")
						}
						if tc.header {
							w.Header().Set("X-GoodHR-Billing", "1")
						}
						if stream {
							w.Header().Set("Content-Type", "text/event-stream")
							fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: {\"choices\":[],\"goodhr_billing\":%s}\n\ndata: [DONE]\n\n", tc.receipt)
						} else {
							w.Header().Set("Content-Type", "application/json")
							fmt.Fprintf(w, `{"choices":[{"message":{"content":"ok"}}],"goodhr_billing":%s}`, tc.receipt)
						}
					}))
					defer server.Close()
					var receipts []Billing
					ctx := WithBillingReporter(context.Background(), func(b Billing) { receipts = append(receipts, b) })
					client := New()
					var err error
					if tools {
						_, err = client.doToolChatRequest(ctx, server.URL, tc.key, []byte(`{"model":"test"}`))
					} else {
						_, err = client.doChatRequest(ctx, server.URL, tc.key, []byte(`{"model":"test"}`), chatOptions{})
					}
					if err != nil || len(receipts) != 1 || receipts[0].Status != tc.want {
						t.Fatalf("回执错误：%+v err=%v", receipts, err)
					}
					if tc.want == "charged" {
						want := int64(23)
						if tc.name == "zero" {
							want = 0
						}
						if receipts[0].CostUnits == nil || *receipts[0].CostUnits != want {
							t.Fatalf("金额错误：%+v", receipts[0])
						}
					} else if receipts[0].CostUnits != nil {
						t.Fatal("未知费用不能包含金额")
					}
				})
			}
		}
	}
}

// TestBillingAfterEarlyDecision 验证提前返回评分不等待扣费，完整响应结束后才上报一次准确金额。
func TestBillingAfterEarlyDecision(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-GoodHR-Billing", "1")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"score\\\":80,\\\"reason\\\":\\\"匹配\\\"}\"}}]}\n\n")
		w.(http.Flusher).Flush()
		<-release
		fmt.Fprint(w, "data: {\"choices\":[],\"goodhr_billing\":{\"status\":\"charged\",\"cost_units\":23}}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	receipts := make(chan Billing, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = WithBillingReporter(ctx, func(b Billing) { receipts <- b })
	evaluation, err := New().EvaluateCandidatePreviewEarly(ctx, cloud.AIConfig{BaseURL: server.URL, APIKey: "ghai_test", Model: "test"}, cloud.PositionSnapshot{}, model.Candidate{})
	if err != nil || evaluation.Decision.Score != 80 || evaluation.Final == nil {
		t.Fatalf("未提前返回评分：%+v %v", evaluation, err)
	}
	if len(receipts) != 0 {
		t.Fatal("完整响应前就上报费用")
	}
	close(release)
	released = true
	final := <-evaluation.Final
	if final.Err != nil {
		t.Fatal(final.Err)
	}
	if b := <-receipts; b.Status != "charged" || b.CostUnits == nil || *b.CostUnits != 23 || len(receipts) != 0 {
		t.Fatalf("后台费用错误：%+v", b)
	}
}

// TestBillingEachRetry 验证一次失败后的重试分别记录费用状态，未知金额不被后续成功覆盖。
func TestBillingEachRetry(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, "暂时繁忙")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-GoodHR-Billing", "1")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}],"goodhr_billing":{"status":"charged","cost_units":23}}`)
	}))
	defer server.Close()
	var receipts []Billing
	ctx := WithBillingReporter(context.Background(), func(b Billing) { receipts = append(receipts, b) })
	_, err := New().chat(ctx, cloud.AIConfig{BaseURL: server.URL, APIKey: "ghai_test", Model: "test"}, []chatMessage{textChatMessage("user", "test")}, chatOptions{})
	if err != nil || len(receipts) != 2 || receipts[0].Status != "unavailable" || receipts[1].CostUnits == nil || *receipts[1].CostUnits != 23 {
		t.Fatalf("重试回执丢失：%+v %v", receipts, err)
	}
}

// TestPaidMalformedResponseKeepsReceipt 验证模型正文无法解析时，真实扣费回执仍然保留。
func TestPaidMalformedResponseKeepsReceipt(t *testing.T) {
	observer := &billingObserver{enabled: true}
	_, err := readChatStream(strings.NewReader("data: broken\n\ndata: {\"choices\":[],\"goodhr_billing\":{\"status\":\"charged\",\"cost_units\":23}}\n\ndata: [DONE]\n"), 0, nil, observer)
	if err == nil || observer.billing.CostUnits == nil || *observer.billing.CostUnits != 23 {
		t.Fatalf("解析失败丢失扣费：%+v %v", observer.billing, err)
	}
}
