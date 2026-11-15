// Package ai 文件作用：验证 AI 解析失败和流式中断会保留完整原文、模型及准确的失败位置。
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
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestResponseErrorKeepsOriginalTextAndOffset 验证 Markdown 外包装和长正文不会丢失，位置对应原始字节。
func TestResponseErrorKeepsOriginalTextAndOffset(t *testing.T) {
	prefix := "模型原始说明\n```json\n"
	content := prefix + `{"score":80,"reason":"` + strings.Repeat("完整正文", 4000) + `",BAD}` + "\n```\n结束说明"
	_, err := parseDecision(content, 70)
	diagnostic, ok := ResponseDiagnosticFromError(fmt.Errorf("流程失败：%w", err))
	if !ok || diagnostic.Response != content || diagnostic.ResponseKind != "model_content" || !diagnostic.ResponseComplete {
		t.Fatalf("诊断没有保留完整模型原文：ok=%v bytes=%d", ok, len(diagnostic.Response))
	}
	if diagnostic.ParseOffset != int64(strings.Index(content, "BAD")+1) || !strings.Contains(diagnostic.ParseError, "invalid character") {
		t.Fatalf("解析位置没有对应原始字节：offset=%d error=%s", diagnostic.ParseOffset, diagnostic.ParseError)
	}
	var responseErr *ResponseError
	var syntaxErr *json.SyntaxError
	if !errors.As(err, &responseErr) || !errors.As(err, &syntaxErr) {
		t.Fatal("错误包装没有保留原 JSON 错误链")
	}
}

// TestResponseErrorDiagnosticIDStaysStable 验证并发读取与多层模型补充共享同一诊断编号。
func TestResponseErrorDiagnosticIDStaysStable(t *testing.T) {
	original := &ResponseError{Cause: errors.New("解析失败"), Diagnostic: ResponseDiagnostic{Response: "实际返回"}}
	wrapped := withResponseModel(fmt.Errorf("流程包装：%w", original), "model")
	var outer *ResponseError
	if !errors.As(wrapped, &outer) {
		t.Fatal("没有找到响应错误")
	}
	ids := make(chan uint64, 20)
	for range 20 {
		go func() { ids <- outer.DiagnosticID() }()
	}
	want := original.DiagnosticID()
	for range 20 {
		if got := <-ids; got != want || got == 0 {
			t.Fatalf("诊断编号不稳定：got=%d want=%d", got, want)
		}
	}
	other := &ResponseError{Cause: errors.New("另一次失败")}
	if other.DiagnosticID() == want {
		t.Fatal("不同回复共用了诊断编号")
	}
}

// TestResponseErrorKeepsModelAcrossCandidateEntrypoints 验证预分析、最终文字评分和图片评分都保留原始返回与模型。
func TestResponseErrorKeepsModelAcrossCandidateEntrypoints(t *testing.T) {
	content := "原始前言\n```json\n" + `{"score":"不是数字","reason":"` + strings.Repeat("原文", 3000) + `"}` + "\n```\n原始后记"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, chatEnvelopeForTest(content))
	}))
	defer server.Close()
	client := New()
	cfg := cloud.AIConfig{BaseURL: server.URL, APIKey: "request-only-secret", Model: "test-model"}
	position := cloud.PositionSnapshot{Name: "测试岗位"}
	for _, entry := range []struct {
		name string
		call func() error
	}{
		{"基础预分析", func() error {
			_, err := client.EvaluateCandidatePreview(context.Background(), cfg, position, model.Candidate{})
			return err
		}},
		{"最终文字评分", func() error {
			_, err := client.EvaluateCandidate(context.Background(), cfg, position, model.Candidate{}, model.CandidateDetail{})
			return err
		}},
		{"图片评分", func() error {
			_, err := client.EvaluateCandidateVision(context.Background(), cfg, position, model.Candidate{}, model.CandidateDetail{}, [][]byte{{1}})
			return err
		}},
	} {
		t.Run(entry.name, func(t *testing.T) {
			diagnostic, ok := ResponseDiagnosticFromError(entry.call())
			if !ok || diagnostic.Model != cfg.Model || diagnostic.Response != content || diagnostic.ParseField != "score" || diagnostic.ParseOffset <= 0 {
				t.Fatalf("入口丢失响应诊断：ok=%v model=%q field=%q offset=%d bytes=%d", ok, diagnostic.Model, diagnostic.ParseField, diagnostic.ParseOffset, len(diagnostic.Response))
			}
			if strings.Contains(diagnostic.Response, cfg.APIKey) {
				t.Fatal("诊断包含了请求密钥")
			}
		})
	}
}

// TestResponseErrorNamesMissingFields 验证缺少评分和图片文字字段时仍能查看实际返回。
func TestResponseErrorNamesMissingFields(t *testing.T) {
	content := `{"analysis":{"reason":"缺少评分"}}`
	_, err := parseDecision(content, 70)
	diagnostic, ok := ResponseDiagnosticFromError(err)
	if !ok || diagnostic.Response != content || diagnostic.ParseField != "analysis.score" {
		t.Fatalf("缺少评分的诊断不完整：%+v", diagnostic)
	}
	for _, content := range []string{`{"other":"实际返回"}`, `{"text":["类型不对"]}`} {
		t.Run(content, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprint(w, chatEnvelopeForTest(content))
			}))
			defer server.Close()
			_, err := New().ReadDetailTextVision(context.Background(), cloud.AIConfig{BaseURL: server.URL, APIKey: "fixture", Model: "vision-model"}, [][]byte{{1}})
			diagnostic, ok := ResponseDiagnosticFromError(err)
			if !ok || diagnostic.Response != content || diagnostic.Model != "vision-model" || diagnostic.ParseField != "text" {
				t.Fatalf("详情文字诊断不完整：%+v", diagnostic)
			}
		})
	}
}

// TestResponseErrorKeepsHTTPErrorBody 验证超过旧六十四 KiB 上限的 HTTP 错误正文完整保留且重试策略不变。
func TestResponseErrorKeepsHTTPErrorBody(t *testing.T) {
	body := strings.Repeat("错误原文", 20000) + "最后一字"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, body)
	}))
	defer server.Close()
	_, err := New().EvaluateCandidatePreview(context.Background(), cloud.AIConfig{BaseURL: server.URL, APIKey: "fixture", Model: "http-model"}, cloud.PositionSnapshot{}, model.Candidate{})
	diagnostic, ok := ResponseDiagnosticFromError(err)
	if !ok || diagnostic.Response != body || diagnostic.Model != "http-model" || diagnostic.ResponseKind != "http_body" || !diagnostic.ResponseComplete {
		t.Fatalf("HTTP 失败正文丢失：ok=%v bytes=%d", ok, len(diagnostic.Response))
	}
	if !IsPositionStoppingError(err) {
		t.Fatal("响应包装改变了 HTTP 鉴权错误的停止策略")
	}
}

// TestResponseErrorKeepsLargeJSONAndStreamContent 验证超过旧四 MiB 上限的实际返回在普通 JSON 和单条 SSE 中都不截断。
func TestResponseErrorKeepsLargeJSONAndStreamContent(t *testing.T) {
	content := strings.Repeat("x", 4<<20+100) + "返回末尾"
	for _, streamed := range []bool{false, true} {
		t.Run(fmt.Sprint(streamed), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if streamed {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprint(w, streamEnvelopeForTest(content)+"data: [DONE]\n\n")
					return
				}
				_, _ = fmt.Fprint(w, chatEnvelopeForTest(content))
			}))
			defer server.Close()
			_, err := New().EvaluateCandidatePreview(context.Background(), cloud.AIConfig{BaseURL: server.URL, APIKey: "fixture", Model: "large-model"}, cloud.PositionSnapshot{}, model.Candidate{})
			diagnostic, ok := ResponseDiagnosticFromError(err)
			if !ok || diagnostic.Response != content || diagnostic.Model != "large-model" {
				t.Fatalf("长正文被截断：ok=%v want=%d got=%d", ok, len(content), len(diagnostic.Response))
			}
		})
	}
}

// TestResponseErrorKeepsMalformedEnvelope 验证普通响应外壳和 SSE 分块解析失败时保存实际协议正文及错误位置。
func TestResponseErrorKeepsMalformedEnvelope(t *testing.T) {
	body := "  {\"choices\":BAD}"
	_, err := readChatJSON([]byte(body))
	diagnostic, ok := ResponseDiagnosticFromError(err)
	if !ok || diagnostic.Response != body || diagnostic.ResponseKind != "http_body" || diagnostic.ParseOffset != int64(strings.Index(body, "BAD")+1) {
		t.Fatalf("普通外壳错误诊断不完整：%+v", diagnostic)
	}
	streamBody := "data: {\"choices\":BAD}\n\n" + streamEnvelopeForTest("坏块之后的完整实际返回") + "data: [DONE]\n\n"
	_, err = readChatStream(strings.NewReader(streamBody), 70, nil)
	diagnostic, ok = ResponseDiagnosticFromError(err)
	if !ok || diagnostic.Response != streamBody || diagnostic.ResponseKind != "sse_body" || diagnostic.ParseOffset != int64(strings.Index(streamBody, "BAD")+1) || !diagnostic.ResponseComplete {
		t.Fatalf("流式外壳错误诊断不完整：%+v", diagnostic)
	}
}

// TestResponseErrorKeepsInterruptedStream 验证流式读取中断保留已收到的完整部分和原始事件流。
func TestResponseErrorKeepsInterruptedStream(t *testing.T) {
	content := `{"score":80,"reason":"` + strings.Repeat("收到的部分", 3000)
	body := streamEnvelopeForTest(content) + `data: {"choices":[`
	_, err := readChatStream(io.MultiReader(strings.NewReader(body), failingAIReader{}), 70, nil)
	diagnostic, ok := ResponseDiagnosticFromError(err)
	if !ok || diagnostic.Response != content || diagnostic.ResponseBody != body || diagnostic.ResponseComplete || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("中断前内容丢失：ok=%v modelBytes=%d wireBytes=%d complete=%v", ok, len(diagnostic.Response), len(diagnostic.ResponseBody), diagnostic.ResponseComplete)
	}
	if !IsPositionStoppingError(err) {
		t.Fatal("流式错误包装丢失了服务错误策略")
	}
}

// TestResponseErrorKeepsBackgroundFinalFailure 验证提前评分成功后的后台解析失败仍携带完整模型原文。
func TestResponseErrorKeepsBackgroundFinalFailure(t *testing.T) {
	release := make(chan struct{})
	first := `{"analysis":{"score":88,"reason":"匹配"},`
	last := `"raw_text":"` + strings.Repeat("简历原文", 3000) + `",BAD}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, streamEnvelopeForTest(first))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = fmt.Fprint(w, streamEnvelopeForTest(last)+"data: [DONE]\n\n")
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	evaluation, err := New().EvaluateCandidateEarly(ctx, cloud.AIConfig{BaseURL: server.URL, APIKey: "fixture", Model: "background-model"}, cloud.PositionSnapshot{}, model.Candidate{}, model.CandidateDetail{})
	if err != nil || evaluation.Final == nil || evaluation.Decision.Score != 88 {
		close(release)
		t.Fatalf("提前评分失败：%+v %v", evaluation, err)
	}
	close(release)
	final := <-evaluation.Final
	diagnostic, ok := ResponseDiagnosticFromError(final.Err)
	if !ok || diagnostic.Response != first+last || diagnostic.Model != "background-model" {
		t.Fatalf("后台最终错误丢失原文：ok=%v bytes=%d", ok, len(diagnostic.Response))
	}
}

// failingAIReader 模拟已经交付部分响应后发生的网络中断。
type failingAIReader struct{}

// TestResponseErrorKeepsEarlierBodyWhenRetryCannotConnect 验证后续重试无响应时不会覆盖先前已收到的失败正文。
func TestResponseErrorKeepsEarlierBodyWhenRetryCannotConnect(t *testing.T) {
	body := strings.Repeat("第一次完整返回", 2000)
	attempts := 0
	client := New()
	client.http.Transport = aiDiagnosticTransport(func(_ *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return &http.Response{StatusCode: http.StatusBadGateway, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
		}
		return nil, errors.New("测试后续连接断开")
	})
	_, err := client.EvaluateCandidatePreview(context.Background(), cloud.AIConfig{BaseURL: "http://fixture", APIKey: "fixture", Model: "retry-model"}, cloud.PositionSnapshot{}, model.Candidate{})
	diagnostic, ok := ResponseDiagnosticFromError(err)
	if !ok || attempts != 3 || diagnostic.Response != body || diagnostic.RequestAttempt != 1 || !strings.Contains(diagnostic.FinalRequestError, "测试后续连接断开") {
		t.Fatalf("重试覆盖了之前的实际正文：ok=%v attempts=%d bodyBytes=%d responseAttempt=%d lastError=%s", ok, attempts, len(diagnostic.Response), diagnostic.RequestAttempt, diagnostic.FinalRequestError)
	}
}

// aiDiagnosticTransport 提供可控制的响应与连接错误，避免测试依赖真实 AI 服务。
type aiDiagnosticTransport func(*http.Request) (*http.Response, error)

// RoundTrip 执行测试提供的响应步骤。
func (transport aiDiagnosticTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

// TestEvaluationCancellationKeepsReceivedResponse 验证取消与读取收尾同时发生时不会丢掉已收到的原文。
func TestEvaluationCancellationKeepsReceivedResponse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	content := "取消前收到的完整部分"
	_, err := startEvaluation(ctx, func(_ func(Decision)) (Decision, error) {
		<-ctx.Done()
		return Decision{}, newResponseError(ctx.Err(), content, "model_content", false)
	})
	diagnostic, ok := ResponseDiagnosticFromError(err)
	if !ok || diagnostic.Response != content || diagnostic.ResponseComplete || !errors.Is(err, context.Canceled) {
		t.Fatalf("取消后的响应诊断丢失：ok=%v diagnostic=%+v error=%v", ok, diagnostic, err)
	}
}

// Read 返回确定的中断错误，供流式读取回归测试复用。
func (failingAIReader) Read(_ []byte) (int, error) { return 0, io.ErrUnexpectedEOF }

// chatEnvelopeForTest 构建真实 JSON 外壳，使测试覆盖 HTTP 与模型正文的两次解析。
func chatEnvelopeForTest(content string) string {
	encoded, _ := json.Marshal(content)
	return `{"choices":[{"message":{"content":` + string(encoded) + `}}]}`
}

// streamEnvelopeForTest 构建单条 SSE 原始事件，避免测试文本自行转义破坏真实原文。
func streamEnvelopeForTest(content string) string {
	encoded, _ := json.Marshal(content)
	return "data: {\"choices\":[{\"delta\":{\"content\":" + string(encoded) + "}}]}\n\n"
}
