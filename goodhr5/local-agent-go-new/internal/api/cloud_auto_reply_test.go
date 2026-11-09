// Package api 本文件验证云端执行通道遇到终态拒绝时停止、网络短暂失败时只重交回执。
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/replybridge"
)

// TestCloudDeviceStopsOnTerminalStatus 验证撤销权限、旧任务和冲突均不能无限轮询。
func TestCloudDeviceStopsOnTerminalStatus(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 409, 410, 413} {
		if !cloudDeviceTerminal(&cloud.APIError{StatusCode: status}) {
			t.Fatalf("HTTP %d 未终止", status)
		}
	}
	for _, err := range []error{nil, errors.New("网络中断"), &cloud.APIError{StatusCode: 408}, &cloud.APIError{StatusCode: 429}, &cloud.APIError{StatusCode: 503}} {
		if cloudDeviceTerminal(err) {
			t.Fatalf("临时错误被错误终止 %v", err)
		}
	}
}

// TestCloudReplySurvivesSameOwnerCredentialRefresh 验证命令领取后同账号换凭证仍提交一次原回执，不再执行页面动作。
func TestCloudReplySurvivesSameOwnerCredentialRefresh(t *testing.T) {
	var s *Server
	polls, completions := 0, 0
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auto-reply/runtime/next":
			polls++
			if polls == 1 {
				if r.Header.Get("Authorization") != "Bearer old" {
					t.Error("首次领取未使用原凭证")
				}
				s.extensionMu.Lock()
				s.bindExtensionSessionLocked("renewed", "owner")
				s.extensionMu.Unlock()
				// 已过期命令仅生成拒绝回执，不触发任何浏览器能力。
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "command": replybridge.Command{ID: "one", RunID: "run", PlatformID: "liepin"}})
				return
			}
			if r.Header.Get("Authorization") != "Bearer renewed" {
				t.Error("后续领取未使用同账号新凭证")
			}
			_, _ = w.Write([]byte(`{"ok":true,"running":false}`))
		case "/api/auto-reply/runtime/complete":
			completions++
			if r.Header.Get("Authorization") != "Bearer renewed" {
				t.Error("回执未使用同账号已验证的新凭证")
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Errorf("意外请求%s", r.URL.Path)
		}
	}))
	defer remote.Close()
	s = &Server{cloud: cloud.New(remote.URL), extensionToken: "old", extensionUserID: "owner", cloudReplyRun: "run", cloudReplyActive: true}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s.pumpCloudReply(ctx, cloud.AgentCredentials{Token: "old", MachineID: "device"}, "owner", 0, "run", &replybridge.Executor{PlatformID: "liepin"})
	if polls != 2 || completions != 1 || s.cloudReplyActive || ctx.Err() != nil {
		t.Fatalf("换凭证丢弃了已执行回执：polls=%d complete=%d active=%v ctx=%v", polls, completions, s.cloudReplyActive, ctx.Err())
	}
}

// TestCloudReplyOwnerChangeCannotResume 验证退出或切号立即取消操作，重新登录相同账号也不能复用旧运行。
func TestCloudReplyOwnerChangeCannotResume(t *testing.T) {
	for _, nextOwner := range []string{"", "other"} {
		t.Run(nextOwner, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s := &Server{extensionToken: "old", extensionUserID: "owner", cloudReplyCancel: cancel}
			initial := cloud.AgentCredentials{Token: "old", MachineID: "device"}
			s.extensionMu.Lock()
			s.bindExtensionSessionLocked("next", nextOwner)
			s.bindExtensionSessionLocked("again", "owner")
			s.extensionMu.Unlock()
			if ctx.Err() == nil {
				t.Fatal("切号没有立即取消浏览器动作")
			}
			if _, err := s.cloudReplyRunCredentials(initial, "owner", 0); err == nil {
				t.Fatal("重新登录恢复了旧账号运行")
			}
		})
	}
}

// TestPublicAutoReplyRoutesUseCloud 验证公开状态和重试入口在没有本机流程或业务仓储时仍由服务器完成。
func TestPublicAutoReplyRoutesUseCloud(t *testing.T) {
	calls := map[string]int{}
	s := newExtensionPairTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer web" {
			t.Error("云端请求未使用当前网页会话")
		}
		calls[r.URL.Path]++
		switch r.URL.Path {
		case "/api/auth/me":
			_, _ = w.Write([]byte(`{"user":{"id":"owner"},"session":{"kind":"web"}}`))
		case "/api/auto-reply/runtime/status":
			_, _ = w.Write([]byte(`{"status":"failed","run_id":"server-run","current_step":"scan","error_message":"请确认聊天列表。","analysis":{"kind":"auto_reply","stage":"sync","reason":"聊天记录已保存","processed_count":2}}`))
		case "/api/auto-reply/runtime/retry-reports":
			_, _ = w.Write([]byte(`{"retried_count":2}`))
		default:
			t.Errorf("不应访问本机业务兼容接口：%s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	s.extensionToken, s.extensionUserID = "web", "owner"
	s.deviceID = func() (string, error) { return "device", nil }
	request := httptest.NewRequest(http.MethodGet, "/api/v1/local/auto-reply/status", nil)
	request.Header.Set("Origin", "https://goodhr.58it.cn")
	result := httptest.NewRecorder()
	s.http.Handler.ServeHTTP(result, request)
	var status struct {
		Data cloudRuntimeStatus `json:"data"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &status); err != nil || result.Code != 200 || status.Data.RunID != "server-run" || status.Data.ErrorMessage != "请确认聊天列表。" || status.Data.Analysis == nil || status.Data.Analysis.Stage != "sync" || status.Data.Analysis.ProcessedCount != 2 {
		t.Fatalf("公开状态未读服务器：%d %s", result.Code, result.Body.String())
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/local/auto-reply/retry-reports", strings.NewReader(`{"token":"web"}`))
	request.Header.Set("Origin", "https://goodhr.58it.cn")
	result = httptest.NewRecorder()
	s.http.Handler.ServeHTTP(result, request)
	if result.Code != 200 || !strings.Contains(result.Body.String(), `"retried_count":2`) {
		t.Fatalf("公开重试未交给服务器：%d %s", result.Code, result.Body.String())
	}
	if calls["/api/auto-reply/runtime/status"] != 1 || calls["/api/auto-reply/runtime/retry-reports"] != 1 {
		t.Fatalf("云端调用次数不符：%v", calls)
	}
}
