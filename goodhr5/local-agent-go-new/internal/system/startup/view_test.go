// Package startup 文件作用：验证迁移期间仅展示进度、错误保留和正常服务就绪后的跳转。
package startup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/config"
)

// readState 通过真实本机 HTTP 请求读取启动状态，验证页面和迁移线程之间的边界。
func readState(t *testing.T, v *View) State {
	t.Helper()
	r, err := http.Get(v.URL + "status")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("启动进度不能缓存")
	}
	var state State
	if err := json.NewDecoder(r.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

// TestStartupProgressAndFailure 验证完成迁移前不会进入控制台，失败保留具体原因且不能跨源关闭。
func TestStartupProgressAndFailure(t *testing.T) {
	v, err := New("https://example.test/admin?source=start", 43129)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	v.Update(config.DataProgress{Stage: "copying", Message: "正在复制", Done: 32, Total: 64})
	s := readState(t, v)
	if s.Done != 32 || s.Total != 64 || s.Target != "" {
		t.Fatalf("错误进度：%+v", s)
	}
	v.Fail(fmt.Errorf("测试目录没有写入权限"))
	s = readState(t, v)
	if s.Stage != "failed" || s.Error == "" || s.Target != "" {
		t.Fatalf("错误被隐藏：%+v", s)
	}
	r, err := http.Post(v.URL+"dismiss", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatal("不能跨源关闭启动页")
	}
	base, _ := url.Parse(v.URL)
	request, _ := http.NewRequest(http.MethodPost, v.URL+"dismiss", nil)
	request.Header.Set("Origin", base.Scheme+"://"+base.Host)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatal("失败实例应能从自身页面关闭")
	}
	select {
	case <-v.Done:
	case <-time.After(time.Second):
		t.Fatal("失败实例未退出")
	}
}

// TestStartupWaitsForBusinessHealth 验证业务健康确认后才跳转，并传递自定义本地端口。
func TestStartupWaitsForBusinessHealth(t *testing.T) {
	v, err := New("https://example.test/admin?source=start", 45123)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":true,"data":{"status":"ok","version":"6.1.2.2","port":45123,"dataDir":"test"}}`)
	}))
	defer health.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	v.FollowReady(ctx, health.URL, 45123)
	s := readState(t, v)
	if s.Stage != "ready" || s.Target != "https://example.test/admin?local_port=45123&source=start" {
		t.Fatalf("提前或错误跳转：%+v", s)
	}
	v.Fail(fmt.Errorf("服务启动失败"))
	v.FollowReady(ctx, health.URL, 45123)
	if readState(t, v).Target != "" {
		t.Fatal("失败不能被迟到的健康响应覆盖")
	}
}
