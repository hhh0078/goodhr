// Package client 文件作用：验证可选元素缺失返回正常空列表，而必需元素缺失和真实浏览器故障仍然报错。
package client

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
)

// TestOptionalMissingDoesNotLogFailure 验证预期缺失既不返回操作错误，也不在上传日志中制造错误标记。
func TestOptionalMissingDoesNotLogFailure(t *testing.T) {
	for _, item := range []struct {
		code     string
		optional bool
		wantErr  bool
	}{
		{code: "ELEMENT_NOT_FOUND", optional: true, wantErr: false},
		{code: "ELEMENT_NOT_FOUND", optional: false, wantErr: true},
		{code: "ELEMENT_NOT_VISIBLE", optional: true, wantErr: true},
		{code: "BROWSER_WINDOW_NOT_READY", optional: true, wantErr: true},
	} {
		t.Run(fmt.Sprintf("%s_optional_%t", item.code, item.optional), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprintf(w, `{"ok":false,"error":{"code":%q,"message":"测试位置缺失","action":"element.find_all","step":"find"}}`, item.code)
			}))
			defer server.Close()
			var output bytes.Buffer
			previous := log.Writer()
			log.SetOutput(&output)
			defer log.SetOutput(previous)
			items, err := New(server.URL).FindAll(context.Background(), contract.ElementFindAllRequest{ExpectedMissing: item.optional, ProbeOnly: true})
			if (err != nil) != item.wantErr || len(items) != 0 {
				t.Fatalf("可选缺失处理不正确：列表=%+v 错误=%v", items, err)
			}
			if item.wantErr {
				if !strings.Contains(output.String(), `"is_error":true`) {
					t.Fatalf("真实故障必须保留错误记录：%s", output.String())
				}
			} else if !strings.Contains(output.String(), `"is_error":false`) || !strings.Contains(output.String(), `"expected_missing":true`) || strings.Contains(output.String(), "worker_action_failed") {
				t.Fatalf("正常缺失不能写成错误记录：%s", output.String())
			}
		})
	}
}
