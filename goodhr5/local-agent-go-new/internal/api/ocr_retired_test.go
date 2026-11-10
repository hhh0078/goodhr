// Package api 本文件验证旧OCR接口不再检查文件或启动进程，缺少OCR依赖时也能明确返回停用状态。
package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRetiredOCREndpoints 不注入任何OCR客户端，确保兼容接口不会再访问本机识别器。
func TestRetiredOCREndpoints(t *testing.T) {
	server := &Server{}
	status := httptest.NewRecorder()
	server.handleOCRStatus(status, httptest.NewRequest(http.MethodGet, "/api/v1/local/ocr/status", nil))
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"enabled":false`) || !strings.Contains(status.Body.String(), `"installed":false`) {
		t.Fatalf("旧状态接口没有明确停用：%d %s", status.Code, status.Body.String())
	}
	result := httptest.NewRecorder()
	server.handleOCRRecognize(result, httptest.NewRequest(http.MethodPost, "/api/v1/local/ocr/recognize", strings.NewReader(`{"path":"not-a-real-file"}`)))
	if result.Code != http.StatusGone || !strings.Contains(result.Body.String(), "OCR_DISABLED") {
		t.Fatalf("旧识别接口仍能执行：%d %s", result.Code, result.Body.String())
	}
}
