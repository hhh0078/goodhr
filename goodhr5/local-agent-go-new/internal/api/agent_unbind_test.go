// Package api 本文件验证网页退出清理当前本机会话，且延迟旧请求不会清除新账号。
package api

import (
	"encoding/json"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAgentUnbindSession 验证旧凭证隔离、当前会话撤销，以及首次绑定等待期间的退出代数。
func TestAgentUnbindSession(t *testing.T) {
	s := &Server{extensionToken: "current", extensionUserID: "owner", extensionPairs: make(map[string]*extensionPair)}
	request := func(token string) {
		t.Helper()
		r := httptest.NewRequest("POST", "/api/v1/session/unbind", strings.NewReader(`{"token":"`+token+`"}`))
		w := httptest.NewRecorder()
		s.handleAgentUnbind(w, r)
		if w.Code != 200 {
			t.Fatalf("退出响应=%d %s", w.Code, w.Body.String())
		}
	}
	request("old")
	if s.extensionToken != "current" || s.extensionUserID != "owner" {
		t.Fatal("旧请求清掉了新账号")
	}
	before := s.extensionGeneration
	request("current")
	if s.extensionToken != "" || s.extensionUserID != "" || s.extensionGeneration <= before {
		t.Fatal("当前会话没有撤销")
	}
	before = s.extensionGeneration
	request("pending-first-login")
	if s.extensionGeneration <= before {
		t.Fatal("首次绑定中的退出没有阻断旧响应")
	}
}

// TestAgentBindLatestRequestWins 验证两次登录交错返回时，旧响应不能使新登录的绑定失败或恢复旧账号。
func TestAgentBindLatestRequestWins(t *testing.T) {
	for _, firstFinish := range []string{"old", "new", "logout"} {
		t.Run(firstFinish, func(t *testing.T) {
			entered := map[string]chan struct{}{"old": make(chan struct{}), "new": make(chan struct{})}
			release := map[string]chan struct{}{"old": make(chan struct{}), "new": make(chan struct{})}
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
				if r.URL.Path == "/api/agents/bind" {
					close(entered[token])
					<-release[token]
					_ = json.NewEncoder(w).Encode(map[string]any{"agent": map[string]string{"machine_id": "synthetic-machine"}})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"user": map[string]string{"id": token}})
			}))
			defer remote.Close()
			s := &Server{cloud: cloud.New(remote.URL), deviceID: func() (string, error) { return "synthetic-machine", nil }, extensionPairs: make(map[string]*extensionPair)}
			done := map[string]chan struct{}{"old": make(chan struct{}), "new": make(chan struct{})}
			for _, token := range []string{"old", "new"} {
				go func() {
					defer close(done[token])
					s.handleAgentBind(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/v1/session/bind", strings.NewReader(`{"token":"`+token+`"}`)))
				}()
				<-entered[token]
			}
			if firstFinish == "logout" {
				s.handleAgentUnbind(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/v1/session/unbind", strings.NewReader(`{"token":"new"}`)))
				close(release["old"])
				close(release["new"])
				<-done["old"]
				<-done["new"]
				if s.extensionToken != "" || s.extensionUserID != "" {
					t.Fatal("退出后的慢响应重新恢复了登录")
				}
				return
			}
			close(release[firstFinish])
			<-done[firstFinish]
			other := "old"
			if firstFinish == "old" {
				other = "new"
			}
			close(release[other])
			<-done[other]
			if s.extensionToken != "new" || s.extensionUserID != "new" {
				t.Fatalf("最终绑定了错误账号：%s", s.extensionUserID)
			}
		})
	}
}
