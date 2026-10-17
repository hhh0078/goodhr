// 文件作用说明：验证统一打开入口能读取当前用户鼠标档位，避免手动启动与任务配置相互冲突。
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// TestPageOpenMouseSpeed 验证显式配置、绑定登录和无登录三种打开方式，以及云端失败时不会偷偷降级。
func TestPageOpenMouseSpeed(t *testing.T) {
	var token string
	var failed bool
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token = r.Header.Get("Authorization")
		if failed {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"config":{"mouse_speed":"slow"}}`))
	}))
	defer remote.Close()
	s := &Server{cloud: cloud.New(remote.URL), extensionToken: "bound-token"}
	for _, item := range []struct {
		request                                     pageOpenRequest
		authorization, expectedToken, expectedSpeed string
	}{
		{pageOpenRequest{}, "", "Bearer bound-token", "slow"},
		{pageOpenRequest{Token: "request-token"}, "", "Bearer request-token", "slow"},
		{pageOpenRequest{}, "Bearer header-token", "Bearer header-token", "slow"},
		{pageOpenRequest{MouseSpeed: "ultra_fast"}, "", "", "ultra_fast"},
	} {
		token = ""
		speed, err := s.pageOpenMouseSpeed(context.Background(), item.request, item.authorization)
		if err != nil || speed != item.expectedSpeed || token != item.expectedToken {
			t.Fatalf("手动打开档位错误 speed=%s token=%s err=%v", speed, token, err)
		}
	}
	failed = true
	if _, err := s.pageOpenMouseSpeed(context.Background(), pageOpenRequest{}, ""); err == nil {
		t.Fatal("云端失败没有提示重试")
	}
	s.extensionToken = ""
	if speed, err := s.pageOpenMouseSpeed(context.Background(), pageOpenRequest{}, ""); err != nil || speed != "" {
		t.Fatalf("没有登录信息时没有保留当前档位：%s %v", speed, err)
	}
}
