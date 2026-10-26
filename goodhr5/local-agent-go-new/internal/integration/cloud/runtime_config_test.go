// Package cloud 文件作用：验证组件清单沿用既有鉴权接口、强类型边界以及取消和缺失配置处理。
package cloud

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestRuntimeManifestUsesExistingAuthenticatedConfig 验证只取 config.runtime_components，保留原平台别名供组件层选择。
func TestRuntimeManifestUsesExistingAuthenticatedConfig(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/runtime/config" || r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Error("组件清单没有复用既有鉴权 GET 接口")
		}
		_, _ = w.Write([]byte(`{"runtime_components":{"camoufox":{"wrong":{"version":"wrong"}}},"config":{"private_field":"ignored","runtime_components":{"camoufox":{"mac":{"version":"152.0.4-beta.30","url":"https://example.invalid/arm.zip","sha256":"` + strings.Repeat("a", 64) + `"},"darwin-x64":{"version":"152.0.4-beta.30","url":"https://example.invalid/intel.zip","sha256":"` + strings.Repeat("b", 64) + `"}},"node_runtime":{"mac":{"version":"22"}}}}}`))
	}))
	defer remote.Close()
	manifest, err := New(remote.URL).RuntimeManifest(context.Background(), "fixture-token")
	if err != nil || len(manifest.Camoufox) != 2 || manifest.Camoufox["mac"].SHA256 != strings.Repeat("a", 64) || manifest.Camoufox["darwin-x64"].URL != "https://example.invalid/intel.zip" {
		t.Fatalf("组件清单未正确保留：%+v err=%v", manifest, err)
	}
}

// TestRuntimeManifestRejectsMissingCredentialsAndConfig 验证无凭证不发请求，鉴权失败、缺配置和坏 JSON 均明确失败。
func TestRuntimeManifestRejectsMissingCredentialsAndConfig(t *testing.T) {
	for _, test := range []struct {
		name, token, body string
		status            int
		wantRequests      int32
	}{
		{"empty_token", "", `{}`, 200, 0},
		{"auth_failed", "fixture", `{"error":"session is invalid or expired"}`, 401, 1},
		{"missing_config", "fixture", `{"runtime_components":{"camoufox":{"mac":{}}}}`, 200, 1},
		{"bad_json", "fixture", `{"config":`, 200, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer remote.Close()
			if _, err := New(remote.URL).RuntimeManifest(context.Background(), test.token); err == nil {
				t.Fatal("不完整的组件配置被接受")
			}
			if requests.Load() != test.wantRequests {
				t.Fatal("无凭证时不应请求云端")
			}
		})
	}
}

// TestRuntimeManifestHonorsCancellation 验证主程序退出可中断正在等待的配置请求。
func TestRuntimeManifestHonorsCancellation(t *testing.T) {
	entered := make(chan struct{})
	remote := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
	}))
	defer remote.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { _, err := New(remote.URL).RuntimeManifest(ctx, "fixture"); finished <- err }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("本机配置请求未开始")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("配置请求没有响应取消：%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("取消后配置请求仍未退出")
	}
}
