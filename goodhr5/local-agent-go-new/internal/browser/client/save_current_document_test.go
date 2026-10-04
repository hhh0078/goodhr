// Package client 文件作用：验证 Go 到 Worker 保存当前文档页协议的路径、字段和强类型响应。
package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
)

// TestSaveCurrentDocumentUsesTypedWorkerRoute 验证客户端使用唯一文档保存路由并解析统一下载记录。
func TestSaveCurrentDocumentUsesTypedWorkerRoute(t *testing.T) {
	requestReceived := contract.SaveCurrentDocumentRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/v1/page/save-current-document" {
			t.Errorf("Worker 请求不正确：%s %s", request.Method, request.URL.Path)
		}
		if err := json.NewDecoder(request.Body).Decode(&requestReceived); err != nil {
			t.Errorf("解析 Worker 请求失败：%v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true,"data":{"id":"download-test","filename":"候选人简历.pdf","file_name":"候选人简历.pdf","file_path":"/tmp/候选人简历.pdf","path":"/tmp/候选人简历.pdf","suggested_filename":"候选人简历.pdf","url":"https://attachment.example/resume","page_url":"https://attachment.example/resume","size":128,"status":"saved","error":"","created_at":"2026-08-12T00:00:00Z"},"trace_id":"trace-test"}`))
	}))
	defer server.Close()

	record, err := New(server.URL).SaveCurrentDocument(context.Background(), contract.SaveCurrentDocumentRequest{
		MaxBytes:            26_214_400,
		AllowedContentTypes: []string{"application/pdf"},
		SuggestedFilename:   "候选人简历.pdf",
		TimeoutMS:           30000,
	})
	if err != nil {
		t.Fatalf("保存当前文档请求失败：%v", err)
	}
	if requestReceived.MaxBytes != 26_214_400 || requestReceived.TimeoutMS != 30000 ||
		len(requestReceived.AllowedContentTypes) != 1 || requestReceived.AllowedContentTypes[0] != "application/pdf" ||
		requestReceived.SuggestedFilename != "候选人简历.pdf" {
		t.Fatalf("Worker 文档保存参数不完整：%+v", requestReceived)
	}
	if record.Status != "saved" || record.FilePath != "/tmp/候选人简历.pdf" || record.Size != 128 {
		t.Fatalf("Worker 文档保存结果解析不正确：%+v", record)
	}
}
