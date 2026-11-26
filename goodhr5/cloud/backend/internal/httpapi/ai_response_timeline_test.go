// 本文件以本地合成流验证 DONE 收尾、完整结果、阶段时间与错误保留，不调用付费 AI。
package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestAIResponseDoneClosesBeforeEOF 验证上游 DONE 后挂起不影响完成，保留完整简历及 usage。
func TestAIResponseDoneClosesBeforeEOF(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		wire := serverAIEnvelope(`{"score":90,"reason":"ok","resume":{"experience":["完整经历"]}}`, true)
		wire = strings.Replace(wire, "data: {\"choices\":[],\"usage\":", "data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":", 1)
		io.WriteString(w, wire)
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/v1/chat/completions", strings.NewReader(`{"stream":true,"response_format":{"type":"json_object"}}`))
	var events []serverAIAttemptEvent
	resp, err := doServerAIRequest(server.Client(), req, serverAIRequestOptions{Observe: func(e serverAIAttemptEvent) {
		if e.Event == "end" {
			events = append(events, e)
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	result, err := readAIWire(raw)
	if err != nil || !strings.Contains(result.Content, "完整经历") || result.Prompt != 10 || result.Completion != 2 || len(events) != 1 {
		t.Fatalf("result=%+v events=%+v err=%v", result, events, err)
	}
	tm := events[0].Timeline
	for k, v := range map[string]string{"written": tm.RequestWrittenAt, "response": tm.FirstResponseAt, "headers": tm.HeadersAt, "first": tm.FirstDataAt, "last": tm.LastDataAt, "done": tm.DoneAt, "finish_reason": tm.FinishReasonAt, "end": tm.EndAt} {
		if _, err := time.Parse(time.RFC3339Nano, v); err != nil {
			t.Fatalf("%s=%q", k, v)
		}
	}
	if tm.ErrorAt != "" || tm.EndReason != "success" || tm.ReceivedBytes == 0 {
		t.Fatalf("timeline=%+v", tm)
	}
}

// TestAIResponseTerminalBoundaries 验证完成前错误和无效正文仍失败，完成后错误不再覆盖正常结果。
func TestAIResponseTerminalBoundaries(t *testing.T) {
	good := serverAIEnvelope(`{"score":80,"reason":"ok"}`, true)
	bad := "data: {\"error\":{\"message\":\"precise timeout\",\"code\":\"upstream_timeout\"}}\n\n"
	for _, tc := range []struct {
		name, wire string
		valid      bool
	}{{"late", good + bad, true}, {"early", bad + good, false}, {"invalid", serverAIEnvelope(`{"resume":`, true), false}} {
		t.Run(tc.name, func(t *testing.T) {
			r := &aiTimedReader{source: strings.NewReader(tc.wire)}
			raw, err := readAIResponse(r, true)
			if err != nil {
				t.Fatal(err)
			}
			if (validateAIWire(raw, "json", true) == nil) != tc.valid {
				t.Fatalf("wrong validity: %s", raw)
			}
			if tc.name == "late" {
				if strings.Contains(string(raw), "precise timeout") {
					t.Fatal("late error forwarded")
				}
				if _, err := readAIWire([]byte(tc.wire)); err != nil {
					t.Fatal(err)
				}
			}
			if tc.name == "early" && r.timeline.ErrorAt == "" {
				t.Fatal("no error time")
			}
		})
	}
}

// TestAITimelineReadFailure 验证普通 JSON、实际 EOF 与读取中断分别记时，不虚构完成标志。
func TestAITimelineReadFailure(t *testing.T) {
	r := &aiTimedReader{source: strings.NewReader(`{"error":{"message":"precise HTTP503"}}`)}
	raw, err := readAIResponse(r, false)
	if err != nil || r.timeline.EOFAt == "" || r.timeline.DoneAt != "" {
		t.Fatalf("timeline=%+v err=%v", r.timeline, err)
	}
	event := serverAIAttemptEvent{}
	fillAIErrorDetail(&event, &http.Response{StatusCode: 503, Header: http.Header{"Content-Type": {"application/json"}}}, raw, []byte(`{}`), "http_status", "", nil)
	if !strings.Contains(event.ErrorDetail, "precise HTTP503") {
		t.Fatalf("lost error %+v", event)
	}
	pr, pw := io.Pipe()
	defer pr.Close()
	go func() { pw.Write([]byte("data: {")); pw.CloseWithError(io.ErrUnexpectedEOF) }()
	r = &aiTimedReader{source: pr}
	_, err = readAIResponse(r, true)
	if err == nil || r.timeline.ErrorAt == "" {
		t.Fatalf("timeline=%+v err=%v", r.timeline, err)
	}
}

// TestAITimelinePartialStreamTimeout 验证没有 DONE 的停滞流仍遵守取消保护，并记录真实阶段和超时错误。
func TestAITimelinePartialStreamTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/v1/chat/completions", strings.NewReader(`{"stream":true}`))
	var events []serverAIAttemptEvent
	_, err := doServerAIRequest(server.Client(), req, serverAIRequestOptions{Observe: func(e serverAIAttemptEvent) {
		if e.Event == "end" {
			events = append(events, e)
		}
	}})
	if err == nil || len(events) != 1 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	tm := events[0].Timeline
	if events[0].Reason != "timeout" || tm.FirstDataAt == "" || tm.LastDataAt == "" || tm.ErrorAt == "" || tm.EndAt == "" || tm.DoneAt != "" {
		t.Fatalf("event=%+v timing=%+v", events[0], tm)
	}
}
