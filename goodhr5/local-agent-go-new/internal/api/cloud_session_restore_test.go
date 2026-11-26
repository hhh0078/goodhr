// Package api 文件作用：通过真实本机 HTTP/WS 和可控响应验证无浏览器重启恢复、退出、换号、鉴权失败及写入来源隔离。
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"goodhr5/local-agent-go-new/internal/cloudws"
	"goodhr5/local-agent-go-new/internal/config"
	downloadresume "goodhr5/local-agent-go-new/internal/flow/resume_download"
	resumeflow "goodhr5/local-agent-go-new/internal/flow/resume_import"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/storage"
	"goodhr5/local-agent-go-new/internal/system/sessionfile"
)

// sessionFixtureServer 复用正式 NewServer 和真实云端客户端，仅将数据根及网络目标隔离到测试环境。
func sessionFixtureServer(t *testing.T, cfg config.Config) *Server {
	t.Helper()
	s := NewServer(cfg, Dependencies{Cloud: cloud.New(cfg.CloudURL), DeviceID: func() (string, error) { return "synthetic-machine", nil }})
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
	return s
}

// sessionFixtureRequest 向正式本地路由提交合成凭证；错误断言不打印凭证正文。
func sessionFixtureRequest(s *Server, path, token, origin string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(agentBindRequest{Token: token})
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
	r.Header.Set("Origin", origin)
	r.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	s.http.Handler.ServeHTTP(w, r)
	return w
}

// sessionFixtureUser 返回已由测试云端确认的账号，模拟原有 /api/auth/me 响应。
func sessionFixtureUser(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	_ = json.NewEncoder(w).Encode(struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}{User: struct {
		ID string `json:"id"`
	}{ID: "owner-" + token}})
}

// TestCloudSessionRestartAuthenticatesWithoutBrowser 验证保存后新服务启动不联网等待，并在真实 WS 认证后恢复现有运行账号。
func TestCloudSessionRestartAuthenticatesWithoutBrowser(t *testing.T) {
	var requests atomic.Int32
	upgrader := websocket.Upgrader{}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == "/api/auth/me" {
			sessionFixtureUser(w, r)
			return
		}
		if r.URL.Path != "/api/agents/ws" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer synthetic-restart" {
			t.Error("恢复后的 WS 没有使用同一凭证请求头")
			w.WriteHeader(401)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage()
	}))
	defer remote.Close()
	cfg := config.Config{DataDir: t.TempDir(), CloudURL: remote.URL, CloudWSURL: remote.URL}
	first := sessionFixtureServer(t, cfg)
	if sessionFixtureRequest(first, "/api/v1/cloud/session", "synthetic-restart", "").Code != 200 {
		t.Fatal("首次验证与保存失败")
	}
	before := requests.Load()
	restarted := sessionFixtureServer(t, cfg)
	if restarted.CurrentCloudToken() != "synthetic-restart" || requests.Load() != before {
		t.Fatal("重启没有恢复凭证，或本地启动等待了云端网络")
	}
	ready := make(chan struct{})
	ws := cloudws.NewClient(cfg.CloudWSURL, restarted.CurrentCloudToken, nil)
	ws.SetSessionHandlers(func(ctx context.Context, token string) {
		restarted.CloudSessionConnected(ctx, token)
		close(ready)
	}, restarted.CloudSessionRejected)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); ws.Run(ctx) }()
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("重启后没有自动恢复 WS 账号")
	}
	if restarted.extensionUserID != "owner-synthetic-restart" || restarted.extensionToken != restarted.CurrentCloudToken() {
		t.Error("命令通道和现有运行绑定身份不一致")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("退出没有取消恢复连接")
	}
}

// TestCloudSessionRestoreFailurePolicy 验证只有明确 401 删除凭证；403、5xx 和网络中断均保留重试机会。
func TestCloudSessionRestoreFailurePolicy(t *testing.T) {
	for _, status := range []int{401, 403, 503, 0} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var responseStatus atomic.Int32
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if current := responseStatus.Load(); current != 0 {
					w.WriteHeader(int(current))
					return
				}
				sessionFixtureUser(w, r)
			}))
			defer remote.Close()
			cfg := config.Config{DataDir: t.TempDir(), CloudURL: remote.URL}
			s := sessionFixtureServer(t, cfg)
			if sessionFixtureRequest(s, "/api/v1/cloud/session", "synthetic-saved", "").Code != 200 {
				t.Fatal("初始化会话失败")
			}
			responseStatus.Store(int32(status))
			if status == 0 {
				remote.Close()
			}
			restored := sessionFixtureServer(t, cfg)
			restored.CloudSessionConnected(context.Background(), "synthetic-saved")
			persisted, err := sessionfile.Load(cfg.DataDir, cfg.CloudURL, "")
			wantSaved := status != 401
			if err != nil || (persisted != "") != wantSaved || (restored.CurrentCloudToken() != "") != wantSaved {
				t.Fatal("暂时故障被当成注销，或明确失效没有撤销", err)
			}
		})
	}
}

// TestCloudSessionLatestAccountAndLogout 验证迟到的旧验证、旧退出及旧 WS 401 均不能覆盖或删除新账号。
func TestCloudSessionLatestAccountAndLogout(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer synthetic-slow" {
			close(entered)
			<-release
		}
		sessionFixtureUser(w, r)
	}))
	defer remote.Close()
	cfg := config.Config{DataDir: t.TempDir(), CloudURL: remote.URL}
	s := sessionFixtureServer(t, cfg)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- sessionFixtureRequest(s, "/api/v1/cloud/session", "synthetic-slow", "") }()
	<-entered
	if sessionFixtureRequest(s, "/api/v1/cloud/session", "synthetic-new", "").Code != 200 {
		t.Fatal("新账号连接失败")
	}
	close(release)
	if (<-done).Code != 409 {
		t.Error("旧验证完成后没有报告账号已变化")
	}
	if sessionFixtureRequest(s, "/api/v1/session/unbind", "synthetic-slow", "").Code != 200 {
		t.Error("旧账号退出响应不正确")
	}
	s.CloudSessionRejected("synthetic-slow")
	persisted, err := sessionfile.Load(cfg.DataDir, cfg.CloudURL, "")
	if err != nil || persisted != "synthetic-new" || s.CurrentCloudToken() != "synthetic-new" || s.extensionToken != "synthetic-new" {
		t.Fatal("迟到的旧账号操作覆盖或删除了新身份", err)
	}
	if sessionFixtureRequest(s, "/api/v1/session/unbind", "synthetic-new", "").Code != 200 {
		t.Fatal("当前账号退出失败")
	}
	if s.CurrentCloudToken() != "" || s.extensionToken != "" || sessionFixtureServer(t, cfg).CurrentCloudToken() != "" {
		t.Fatal("退出后内存或重启仍恢复了旧账号")
	}
}

// TestCloudSessionRejectsCrossOriginAndRestrictedToken 验证简单跨域写入在鉴权前被拒绝，受限插件凭证不落盘。
func TestCloudSessionRejectsCrossOriginAndRestrictedToken(t *testing.T) {
	var calls atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"user":{"id":"restricted"},"session":{"kind":"browser_extension"}}`))
	}))
	defer remote.Close()
	cfg := config.Config{DataDir: t.TempDir(), CloudURL: remote.URL}
	s := sessionFixtureServer(t, cfg)
	for _, path := range []string{"/api/v1/cloud/session", "/api/v1/session/bind", "/api/v1/session/unbind"} {
		if sessionFixtureRequest(s, path, "synthetic-cross-origin", "https://evil.example").Code != 403 {
			t.Fatal("恶意来源的简单请求未被明确拒绝")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("拒绝来源前仍然发出了云端认证请求")
	}
	if sessionFixtureRequest(s, "/api/v1/cloud/session", "synthetic-restricted", "https://goodhr.58it.cn").Code != 403 {
		t.Fatal("受限插件会话不应被持久化为本地完整会话")
	}
	if token, err := sessionfile.Load(cfg.DataDir, cfg.CloudURL, ""); err != nil || token != "" {
		t.Fatal("拒绝的受限会话仍被保存", err)
	}
}

// TestCloudSessionPendingAccountLogout 验证 B 尚在校验时退出会取消 B，而迟到的 A 退出不阻止 B 登录。
func TestCloudSessionPendingAccountLogout(t *testing.T) {
	for _, path := range []string{"/api/v1/cloud/session", "/api/v1/session/bind"} {
		for _, logoutToken := range []string{"synthetic-A", "synthetic-B"} {
			t.Run(path+"/"+logoutToken, func(t *testing.T) {
				entered, release := make(chan struct{}), make(chan struct{})
				remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/api/agents/bind" {
						_, _ = w.Write([]byte(`{"agent":{"machine_id":"synthetic-machine"}}`))
						return
					}
					if r.Header.Get("Authorization") == "Bearer synthetic-B" {
						close(entered)
						<-release
					}
					sessionFixtureUser(w, r)
				}))
				defer remote.Close()
				cfg := config.Config{DataDir: t.TempDir(), CloudURL: remote.URL}
				s := sessionFixtureServer(t, cfg)
				if sessionFixtureRequest(s, "/api/v1/cloud/session", "synthetic-A", "").Code != 200 {
					t.Fatal("账号 A 初始化失败")
				}
				done := make(chan *httptest.ResponseRecorder, 1)
				go func() { done <- sessionFixtureRequest(s, path, "synthetic-B", "") }()
				<-entered
				if sessionFixtureRequest(s, "/api/v1/session/unbind", logoutToken, "").Code != 200 {
					t.Error("在途登录的退出未处理")
				}
				close(release)
				response := <-done
				wantToken, wantStatus := "synthetic-B", http.StatusOK
				if logoutToken == "synthetic-B" {
					wantToken, wantStatus = "synthetic-A", http.StatusConflict
				}
				persisted, err := sessionfile.Load(cfg.DataDir, cfg.CloudURL, "")
				if response.Code != wantStatus || s.CurrentCloudToken() != wantToken || persisted != wantToken || err != nil {
					t.Fatal("退出复活了待确认账号，或旧退出取消了新登录", err)
				}
			})
		}
	}
}

// TestCloudSessionDelayedRestoreDoesNotReplaceNewAccount 验证旧连接延迟返回的账号校验不能修改已经保存的新身份。
func TestCloudSessionDelayedRestoreDoesNotReplaceNewAccount(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var delayed atomic.Bool
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delayed.Load() && r.Header.Get("Authorization") == "Bearer synthetic-A" {
			close(entered)
			<-release
		}
		sessionFixtureUser(w, r)
	}))
	defer remote.Close()
	cfg := config.Config{DataDir: t.TempDir(), CloudURL: remote.URL}
	s := sessionFixtureServer(t, cfg)
	if sessionFixtureRequest(s, "/api/v1/cloud/session", "synthetic-A", "").Code != 200 {
		t.Fatal("账号 A 初始化失败")
	}
	delayed.Store(true)
	done := make(chan struct{})
	go func() { defer close(done); s.CloudSessionConnected(context.Background(), "synthetic-A") }()
	<-entered
	if sessionFixtureRequest(s, "/api/v1/cloud/session", "synthetic-B", "").Code != 200 {
		t.Error("新账号 B 连接失败")
	}
	close(release)
	<-done
	if s.CurrentCloudToken() != "synthetic-B" || s.extensionUserID != "owner-synthetic-B" {
		t.Fatal("迟到的恢复校验替换了新账号")
	}
}

// TestCloudSessionSaveFailurePreservesCurrentAccount 验证新凭证无法持久化时明确报错，旧文件和当前身份均保持完整。
func TestCloudSessionSaveFailurePreservesCurrentAccount(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(sessionFixtureUser))
	defer remote.Close()
	cfg := config.Config{DataDir: t.TempDir(), CloudURL: remote.URL}
	s := sessionFixtureServer(t, cfg)
	if sessionFixtureRequest(s, "/api/v1/cloud/session", "synthetic-A", "").Code != 200 {
		t.Fatal("账号 A 初始化失败")
	}
	oversized := strings.Repeat("synthetic-B", 1000)
	response := sessionFixtureRequest(s, "/api/v1/cloud/session", oversized, "")
	persisted, err := sessionfile.Load(cfg.DataDir, cfg.CloudURL, "")
	if response.Code != 500 || strings.Contains(response.Body.String(), oversized) || s.CurrentCloudToken() != "synthetic-A" || persisted != "synthetic-A" || s.extensionToken != "synthetic-A" || err != nil {
		t.Fatal("保存失败破坏了旧身份或向响应泄露新凭证", err)
	}
}

// TestDownloadSettingsWebSessionPersistence 验证已校验网页凭证刷新同步更新 WS 身份，且在途退出不会复活刷新凭证。
func TestDownloadSettingsWebSessionPersistence(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/resume-library/settings" {
			_, _ = w.Write([]byte(`{"settings":{"auto_import_downloads":false}}`))
			return
		}
		if r.Header.Get("Authorization") == "Bearer synthetic-pending" {
			close(entered)
			<-release
		}
		_, _ = w.Write([]byte(`{"user":{"id":"same-owner"}}`))
	}))
	defer remote.Close()
	cfg := config.Config{DataDir: t.TempDir(), CloudURL: remote.URL}
	db, err := storage.Open(filepath.Join(cfg.DataDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := sessionFixtureServer(t, cfg)
	s.resumeImports = &resumeflow.Manager{}
	s.resumeDownloads, err = downloadresume.New(db, s.cloud, s.resumeImports)
	if err != nil {
		t.Fatal(err)
	}
	if sessionFixtureRequest(s, "/api/v1/cloud/session", "synthetic-first", "").Code != 200 {
		t.Fatal("初始化会话失败")
	}
	request := func(token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/resume-library/settings/refresh", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.http.Handler.ServeHTTP(w, r)
		return w
	}
	if request("synthetic-refreshed").Code != 200 {
		t.Fatal("同账号网页凭证刷新失败")
	}
	if saved, err := sessionfile.Load(cfg.DataDir, cfg.CloudURL, ""); err != nil || saved != "synthetic-refreshed" || s.CurrentCloudToken() != saved || s.extensionToken != saved {
		t.Fatal("下载设置刷新留下了与运行身份不同的持久会话", err)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- request("synthetic-pending") }()
	<-entered
	if sessionFixtureRequest(s, "/api/v1/session/unbind", "synthetic-pending", "").Code != 200 {
		t.Error("待确认的刷新登录没有退出")
	}
	close(release)
	if (<-done).Code != 409 || s.CurrentCloudToken() != "synthetic-refreshed" {
		t.Fatal("待确认的下载设置刷新在退出后恢复了身份")
	}
}
