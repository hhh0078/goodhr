// Package api 本文件验证单会话参数在访问任何浏览器或云端依赖之前完成校验，不能覆盖临时进程限制。
package api

import (
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAutoReplyStartRejectsTargetOverride 验证正常启动接口遇到缺失身份或扩大范围时直接拒绝，不触发浏览器。
func TestAutoReplyStartRejectsTargetOverride(t *testing.T) {
	t.Setenv(shared.AutoReplyTargetEnvironment, `{"candidate_name":"目标候选人","platform_thread_id":"thread","platform_candidate_id":"candidate"}`)
	for _, body := range []string{
		`{"token":"test","auto_reply_target":{"candidate_name":"目标候选人"}}`,
		`{"token":"test","auto_reply_target":{"candidate_name":"目标候选人","platform_thread_id":"other-thread","platform_candidate_id":"candidate"}}`,
	} {
		response := httptest.NewRecorder()
		(&Server{}).handleLocalAutoReplyStart(response, httptest.NewRequest(http.MethodPost, "/api/v1/local/auto-reply/start", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatal("启动接口未拒绝限制覆盖", response.Code, response.Body.String())
		}
	}
}
