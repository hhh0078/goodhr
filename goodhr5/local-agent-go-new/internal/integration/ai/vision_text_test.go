// Package ai 文件作用：回归验证图片转文字兼容 JSON 外包装，并保留无效响应诊断和空图片请求边界。
package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// TestReadDetailTextVisionSupportsJSONWrapping 验证普通和流式响应兼容裸 JSON、代码块与说明文字，正文和空文字保持原有含义。
func TestReadDetailTextVisionSupportsJSONWrapping(t *testing.T) {
	for _, fixture := range []struct {
		name    string
		content string
		want    string
	}{
		{"裸JSON", `{"text":"测试正文"}`, "测试正文"},
		{"JSON代码块", "```json\n" + `{"text":"测试正文"}` + "\n```", "测试正文"},
		{"普通代码块", "```\n" + `{"text":"测试正文"}` + "\n```", "测试正文"},
		{"前后说明", "识别结果如下：\n" + `{"text":"测试正文"}` + "\n以上为可见文字。", "测试正文"},
		{"正文转义字符", "```json\n" + `{"text":"  第一行\n含\"引号\"与{花括号}\n末行  "}` + "\n```", "第一行\n含\"引号\"与{花括号}\n末行"},
		{"空文字", `{"text":""}`, ""},
		{"空白文字", `{"text":" \n\t "}`, ""},
	} {
		for _, streamed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", fixture.name, streamed), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					if streamed {
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = fmt.Fprint(w, streamEnvelopeForTest(fixture.content)+"data: [DONE]\n\n")
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprint(w, chatEnvelopeForTest(fixture.content))
				}))
				defer server.Close()
				cfg := cloud.AIConfig{BaseURL: server.URL, APIKey: "fixture", Model: "vision-model"}
				got, err := New().ReadDetailTextVision(context.Background(), cfg, [][]byte{{1}})
				if err != nil || got != fixture.want {
					t.Fatalf("图片文字解析不正确：got=%q want=%q err=%v", got, fixture.want, err)
				}
			})
		}
	}
}

// TestReadDetailTextVisionRejectsInvalidJSON 验证缺失、空值、类型错误和无效 JSON 继续失败，并保留原始响应与包装前的错误位置。
func TestReadDetailTextVisionRejectsInvalidJSON(t *testing.T) {
	for _, fixture := range []struct {
		name        string
		content     string
		field       string
		checkOffset bool
	}{
		{name: "缺少文字字段", content: `{"other":"测试正文"}`, field: "text"},
		{name: "文字字段为null", content: "```json\n" + `{"text":null}` + "\n```", field: "text"},
		{name: "文字字段为数组", content: `{"text":["测试正文"]}`, field: "text"},
		{name: "文字字段为数字", content: `{"text":7}`, field: "text"},
		{name: "裸JSON语法错误", content: `{"text":"测试正文",BAD}`, checkOffset: true},
		{name: "代码块内语法错误", content: "模型说明\n```json\n" + `{"text":"测试正文",BAD}` + "\n```\n结束说明", checkOffset: true},
		{name: "JSON被截断", content: `{"text":"未完成正文`},
		{name: "没有JSON对象", content: "暂时没有可读取的正文"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, chatEnvelopeForTest(fixture.content))
			}))
			defer server.Close()
			cfg := cloud.AIConfig{BaseURL: server.URL, APIKey: "fixture", Model: "vision-model"}
			text, err := New().ReadDetailTextVision(context.Background(), cfg, [][]byte{{1}})
			if err == nil || text != "" {
				t.Fatalf("无效文字不应通过：text=%q err=%v", text, err)
			}
			diagnostic, ok := ResponseDiagnosticFromError(err)
			if !ok || diagnostic.Response != fixture.content || diagnostic.Model != cfg.Model || diagnostic.ResponseKind != "model_content" || !diagnostic.ResponseComplete || diagnostic.ParseError == "" {
				t.Fatalf("图片文字响应诊断丢失：ok=%v diagnostic=%+v", ok, diagnostic)
			}
			if fixture.field != "" && diagnostic.ParseField != fixture.field {
				t.Fatalf("错误字段不正确：got=%q want=%q", diagnostic.ParseField, fixture.field)
			}
			if fixture.checkOffset {
				wantOffset := int64(strings.Index(fixture.content, "BAD") + 1)
				if diagnostic.ParseOffset != wantOffset {
					t.Fatalf("错误位置没有对应原始响应：got=%d want=%d", diagnostic.ParseOffset, wantOffset)
				}
			}
		})
	}
}

// TestReadDetailTextVisionSkipsEmptyImages 验证图片为空时立即报错，不向 AI 发出请求。
func TestReadDetailTextVisionSkipsEmptyImages(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = fmt.Fprint(w, chatEnvelopeForTest(`{"text":"不应请求"}`))
	}))
	defer server.Close()
	cfg := cloud.AIConfig{BaseURL: server.URL, APIKey: "fixture", Model: "vision-model"}
	for _, images := range [][][]byte{nil, {}, {nil, {}}} {
		text, err := New().ReadDetailTextVision(context.Background(), cfg, images)
		if err == nil || text != "" {
			t.Fatalf("空图片应立即报错：text=%q err=%v", text, err)
		}
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("空图片仍请求了 AI：requests=%d", got)
	}
}
