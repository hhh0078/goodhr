// Package console 文件作用：验证控制台地址能保留原参数并追加本地端口。
package console

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWithLocalPort 验证控制台地址安全追加本地端口。
func TestWithLocalPort(t *testing.T) {
	result, err := withLocalPort("https://goodhr5.58it.cn/admin/?from=agent", 43129)
	if err != nil {
		t.Fatalf("withLocalPort() error = %v", err)
	}
	if !strings.Contains(result, "from=agent") || !strings.Contains(result, "local_port=43129") {
		t.Fatalf("withLocalPort() = %q", result)
	}
	developmentResult, err := withLocalPort("http://localhost:5173/admin/", 43129)
	if err != nil {
		t.Fatalf("development withLocalPort() error = %v", err)
	}
	if developmentResult != "http://localhost:5173/admin/?local_port=43129" {
		t.Fatalf("development withLocalPort() = %q", developmentResult)
	}
}

// TestExistingAgent 验证只有带完整 GoodHR 身份字段的健康响应才会复用旧实例。
func TestExistingAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"data":{"status":"ok","version":"5.3.5","port":43129,"dataDir":"/tmp/goodhr"}}`))
	}))
	defer server.Close()
	if !ExistingAgent(context.Background(), server.URL, 43129) {
		t.Fatal("完整 GoodHR 健康响应应识别为已有实例")
	}
	if ExistingAgent(context.Background(), server.URL, 55272) {
		t.Fatal("端口不一致时不应复用已有实例")
	}
}
