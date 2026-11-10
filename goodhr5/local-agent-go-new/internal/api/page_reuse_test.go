// Package api 文件作用：验证控制台可选多 URL 复用请求的严格解析和旧字段兼容。
package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPageOpenReuseURLsDecoding 验证缺省字段和字符串数组可用，错误结构仍被严格拒绝。
func TestPageOpenReuseURLsDecoding(t *testing.T) {
	for _, test := range []struct {
		name    string
		body    string
		wantLen int
		wantErr bool
	}{
		{name: "旧请求", body: `{"url":"https://example.com/recommend"}`},
		{name: "支持页面数组", body: `{"url":"https://example.com/recommend","reuse_urls":["https://example.com/recommend","https://example.com/search"]}`, wantLen: 2},
		{name: "空数组", body: `{"url":"https://example.com/recommend","reuse_urls":[]}`},
		{name: "错误字符串", body: `{"url":"https://example.com/recommend","reuse_urls":"https://example.com/search"}`, wantErr: true},
		{name: "错误对象", body: `{"url":"https://example.com/recommend","reuse_urls":[{"url":"https://example.com/search"}]}`, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/page/open", strings.NewReader(test.body))
			var parsed pageOpenRequest
			err := decodeJSON(httptest.NewRecorder(), request, &parsed)
			if (err != nil) != test.wantErr {
				t.Fatalf("请求解析结果不正确：%v", err)
			}
			if !test.wantErr && (parsed.URL != "https://example.com/recommend" || len(parsed.ReuseURLs) != test.wantLen) {
				t.Fatalf("请求字段丢失：%+v", parsed)
			}
		})
	}
}
