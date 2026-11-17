// 本文件验证同进程编排通过真实代理身份检查读取空闲状态，不需要额外端口、容器或招聘账号。
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/cloudruntime"
)

// TestEmbeddedReplyProxyStatus 验证已绑定设备通过后端读取真实编排状态；伪造身份头不能跳过登录。
func TestEmbeddedReplyProxyStatus(t *testing.T) {
	_, service, machine := newRolloutHTTPFixture(t)
	const secret = "embedded-private-service-credential"
	t.Setenv("GOODHR_AUTO_REPLY_SERVICE_TOKEN", secret)
	t.Setenv("GOODHR_AUTO_REPLY_RUNTIME_URL", "")
	runtime, err := cloudruntime.New("http://127.0.0.1:1", secret, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	service.runtime = runtime
	for _, authorized := range []bool{true, false} {
		r := httptest.NewRequest(http.MethodPost, "/api/auto-reply/runtime/status", strings.NewReader(`{}`))
		r.Header.Set("X-GoodHR-Machine-ID", machine)
		r.Header.Set("X-GoodHR-Runtime-User", "伪造的用户")
		if authorized {
			r.Header.Set("Authorization", "Bearer rollout-web-token")
		}
		w := httptest.NewRecorder()
		service.runtimeProxy(w, r)
		if !authorized {
			if w.Code != 401 {
				t.Fatalf("未登录请求未被拒绝：%d", w.Code)
			}
			continue
		}
		var status struct {
			OK      bool   `json:"ok"`
			Status  string `json:"status"`
			Running bool   `json:"running"`
		}
		if err = json.Unmarshal(w.Body.Bytes(), &status); err != nil || w.Code != 200 || !status.OK || status.Status != "idle" || status.Running {
			t.Fatalf("同进程状态读取失败：%d %s", w.Code, w.Body.String())
		}
	}
}
