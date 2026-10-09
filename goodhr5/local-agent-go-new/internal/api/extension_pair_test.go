// 本文件验证插件配对的单次领取、密钥校验、退出撤销和云请求期间切号的隔离。
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/config"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/storage"
)

// newExtensionPairTestServer 创建隔离的本地数据库和模拟云端，不使用真实账号。
func newExtensionPairTestServer(t *testing.T, cloudHandler http.Handler) *Server {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "pair.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cloudServer := httptest.NewServer(cloudHandler)
	t.Cleanup(cloudServer.Close)
	return NewServer(config.Config{Host: "127.0.0.1", Port: 43129, CloudURL: cloudServer.URL}, Dependencies{Store: store, Cloud: cloud.New(cloudServer.URL)})
}

// extensionPairTestRequest 通过真实本地路由提交授权请求。
func extensionPairTestRequest(s *Server, path string, body any) *httptest.ResponseRecorder {
	encoded, _ := json.Marshal(body)
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(encoded)))
	request.Header.Set("Origin", "https://goodhr.58it.cn")
	response := httptest.NewRecorder()
	s.http.Handler.ServeHTTP(response, request)
	return response
}

// TestExtensionPairSingleUseAndRevoke 验证页面凭证不会交付、错误挑战不能领取、撤销后不能恢复。
func TestExtensionPairSingleUseAndRevoke(t *testing.T) {
	s := newExtensionPairTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/me":
			_, _ = w.Write([]byte(`{"user":{"id":"user-a","email":"a@example.com"}}`))
		case "/api/auth/extension-session":
			_, _ = w.Write([]byte(`{"ok":true,"token":"plugin-only","user":{"id":"user-a","email":"a@example.com"}}`))
		default:
			w.WriteHeader(404)
		}
	}))
	installation, challenge := strings.Repeat("i", 32), strings.Repeat("c", 64)
	start := extensionPairTestRequest(s, "/api/v1/extensions/pair/start", extensionPairRequest{InstallationID: installation, Challenge: challenge})
	var started struct {
		Data struct {
			PairingID string `json:"pairing_id"`
		} `json:"data"`
	}
	if json.Unmarshal(start.Body.Bytes(), &started) != nil || started.Data.PairingID == "" {
		t.Fatal("未创建配对")
	}
	approve := extensionPairTestRequest(s, "/api/v1/extensions/pair/approve", extensionPairRequest{PairingID: started.Data.PairingID, Token: "web-private"})
	if approve.Code != 200 || strings.Contains(approve.Body.String(), "web-private") {
		t.Fatal("授权失败或泄露网页凭证")
	}
	req := extensionPairRequest{PairingID: started.Data.PairingID, InstallationID: installation, Challenge: strings.Repeat("x", 64)}
	if extensionPairTestRequest(s, "/api/v1/extensions/pair/status", req).Code != 404 {
		t.Fatal("错误挑战被接受")
	}
	req.Challenge = challenge
	status := extensionPairTestRequest(s, "/api/v1/extensions/pair/status", req)
	var result struct {
		Data struct {
			PairingToken string `json:"pairing_token"`
			Token        string `json:"token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(status.Body.Bytes(), &result)
	if status.Code != 200 || result.Data.Token != "plugin-only" || strings.Contains(status.Body.String(), "web-private") {
		t.Fatal("单次领取不正确")
	}
	if extensionPairTestRequest(s, "/api/v1/extensions/pair/status", req).Code != 404 {
		t.Fatal("配对被重复领取")
	}
	restore := extensionPairRequest{InstallationID: installation, PairingToken: result.Data.PairingToken}
	if extensionPairTestRequest(s, "/api/v1/extensions/session/restore", restore).Code != 200 {
		t.Fatal("已授权安装不能恢复")
	}
	bad := restore
	bad.PairingToken = strings.Repeat("x", 64)
	if extensionPairTestRequest(s, "/api/v1/extensions/pair/revoke", bad).Code != 401 {
		t.Fatal("错误密钥能撤销其他安装")
	}
	for range 2 {
		if extensionPairTestRequest(s, "/api/v1/extensions/pair/revoke", restore).Code != 200 {
			t.Fatal("撤销不幂等")
		}
	}
	if extensionPairTestRequest(s, "/api/v1/extensions/session/restore", restore).Code != 401 {
		t.Fatal("已撤销安装还能恢复")
	}
}

// TestExtensionRestoreRejectsAccountChange 验证云请求期间切号后拒绝交付旧账号凭证，并撤销临时会话。
func TestExtensionRestoreRejectsAccountChange(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var revoked atomic.Bool
	s := newExtensionPairTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/me":
			_, _ = w.Write([]byte(`{"user":{"id":"user-a","email":"a@example.com"}}`))
		case "/api/auth/extension-session":
			close(entered)
			<-release
			_, _ = w.Write([]byte(`{"ok":true,"token":"temporary-plugin","user":{"id":"user-a"}}`))
		case "/api/auth/extension-logout":
			revoked.Store(r.Header.Get("Authorization") == "Bearer temporary-plugin")
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			w.WriteHeader(404)
		}
	}))
	installation, secret := strings.Repeat("i", 32), strings.Repeat("s", 64)
	identity, _ := json.Marshal(extensionPairIdentity{UserID: "user-a", SecretHash: extensionHash(secret)})
	if err := s.store.SetSetting(context.Background(), extensionIdentityKey(installation), string(identity)); err != nil {
		t.Fatal(err)
	}
	s.extensionMu.Lock()
	s.bindExtensionSessionLocked("web-a", "user-a")
	s.extensionMu.Unlock()
	s.extensionPairs["unclaimed"] = &extensionPair{ExpiresAt: time.Now().Add(time.Minute), Approved: &cloud.ExtensionSession{User: cloud.ExtensionUser{ID: "user-a"}}}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- extensionPairTestRequest(s, "/api/v1/extensions/session/restore", extensionPairRequest{InstallationID: installation, PairingToken: secret})
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("未进入云端交换")
	}
	s.extensionMu.Lock()
	s.bindExtensionSessionLocked("web-b", "user-b")
	cleared := len(s.extensionPairs) == 0
	s.extensionMu.Unlock()
	close(release)
	response := <-done
	if response.Code != 401 || strings.Contains(response.Body.String(), "temporary-plugin") || !revoked.Load() || !cleared {
		t.Fatal("切号隔离或过时会话撤销失败")
	}
}
