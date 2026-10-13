// Package lifecycle 文件作用：用隔离的真实 Worker 和静态页面验证日志包装后的详情浏览确实产生滚轮位移。
package lifecycle

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/hliepin"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestLoggedRuntimeDetailBrowseRealWheelSmoke 仅在指定隔离 Worker 时验证真实滚轮与详情浏览结果日志。
func TestLoggedRuntimeDetailBrowseRealWheelSmoke(t *testing.T) {
	workerURL := strings.TrimSpace(os.Getenv("GOODHR_DETAIL_BROWSE_WORKER_URL"))
	if workerURL == "" {
		t.Skip("未设置 GOODHR_DETAIL_BROWSE_WORKER_URL，跳过真实滚轮测试")
	}
	parsed, err := url.Parse(workerURL)
	if err != nil || parsed.Scheme != "http" || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost" && parsed.Hostname() != "::1") {
		t.Fatal("真实滚轮测试仅允许连接本机隔离 Worker")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	ctx = client.WithTraceID(ctx, "detail-browse-isolated-smoke")
	browser := client.New(workerURL)
	if err := browser.Health(ctx); err != nil {
		t.Fatalf("隔离 Worker 不可用：%v", err)
	}
	previousStatus, err := browser.BrowserStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if previousStatus.Running {
		t.Fatal("这个 Worker 已有运行中的浏览器，请改用空闲的隔离 Worker")
	}

	profileDir := filepath.Join(t.TempDir(), "isolated-profile")
	// 清理前再次核对 Profile，只关闭本测试创建的浏览器，绝不关闭其他会话。
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		status, statusErr := browser.BrowserStatus(cleanupCtx)
		if statusErr != nil {
			t.Errorf("隔离浏览器清理前状态检查失败：%v", statusErr)
			return
		}
		if status.Running && filepath.Clean(status.UserDataDir) == filepath.Clean(profileDir) {
			if _, stopErr := browser.StopBrowser(cleanupCtx); stopErr != nil {
				t.Errorf("隔离浏览器关闭失败：%v", stopErr)
			}
		}
	})
	headless := false
	status, err := browser.StartBrowser(ctx, contract.BrowserStartRequest{UserDataDir: profileDir, Headless: &headless})
	if err != nil {
		t.Fatalf("隔离浏览器启动失败：%v", err)
	}
	if status.Reused || filepath.Clean(status.UserDataDir) != filepath.Clean(profileDir) {
		t.Fatal("Worker 没有创建预期的独立 Profile，停止测试")
	}

	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, detailBrowseSmokeHTML())
	}))
	defer fixture.Close()
	if _, err := browser.OpenPage(ctx, contract.PageOpenRequest{URL: fixture.URL, WaitUntil: "load", TimeoutMS: 15000}); err != nil {
		t.Fatal(err)
	}
	marker := detailBrowseSmokeSelector("#detail-marker", "详情首行位移标记", "attached")
	before, err := browser.Find(ctx, contract.ElementFindRequest{Selector: marker})
	if err != nil {
		t.Fatal(err)
	}
	anchor := detailBrowseSmokeSelector("#detail-scroll", "隔离详情滚动区域", "visible")
	config := model.Config{ID: "hliepin", Selectors: map[string]contract.SelectorSpec{"candidate.detail_scroll": anchor}}
	collector := &runtimeLogCollector{}
	runtime := newLoggedRuntime(hliepin.NewRuntime(), "detail-browse-isolated-smoke", collector)
	detailBrowser, ok := runtime.(model.DetailBrowser)
	if !ok {
		t.Fatal("日志包装丢失了猎聘猎头端详情浏览能力")
	}
	if err := detailBrowser.BrowseCandidateDetail(ctx, browser, config, model.Candidate{Fingerprint: "isolated-fixture"}); err != nil {
		t.Fatalf("真实详情滚轮浏览失败：%v", err)
	}
	after, err := browser.Find(ctx, contract.ElementFindRequest{Selector: marker})
	if err != nil {
		t.Fatal(err)
	}
	shift := before.View.Box.Y - after.View.Box.Y
	if before.View.Box.Height <= 0 || after.View.Box.Height <= 0 || shift < 80 {
		t.Fatalf("静态详情内容没有明显向上移动：before_y=%.1f after_y=%.1f shift=%.1f", before.View.Box.Y, after.View.Box.Y, shift)
	}
	if len(collector.events) != 1 || collector.events[0] != (runtimeLogEvent{"browse_candidate_detail", "success"}) || collector.errors[0] != nil {
		t.Fatalf("真实详情浏览成功日志缺失：%+v", collector.events)
	}
	t.Logf("真实滚轮证据：仅打开本机静态详情；首行 Y 从 %.1f 变为 %.1f，向上位移 %.1f 像素；日志 browse_candidate_detail=success。", before.View.Box.Y, after.View.Box.Y, shift)
}

// detailBrowseSmokeSelector 创建只用于本机静态详情测试的标准定位参数。
func detailBrowseSmokeSelector(css, description, state string) contract.SelectorSpec {
	return contract.SelectorSpec{Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: css}}}, State: state, TimeoutMS: 5000, Description: description}
}

// detailBrowseSmokeHTML 生成无页面脚本的长详情区域，页面自身不滚动，位移只能来自区域真实滚轮。
func detailBrowseSmokeHTML() string {
	var content strings.Builder
	content.WriteString(`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>GoodHR 隔离详情滚轮测试</title><style>
html,body{margin:0;height:100%;overflow:hidden;background:#f6f8f7;color:#203b2d;font:16px sans-serif}
h1{margin:24px;font-size:20px}#detail-scroll{margin:24px;width:560px;height:340px;overflow-y:scroll;background:white;border:2px solid #129766}
#detail-marker{height:48px;background:#e3f6eb;padding:12px;box-sizing:border-box}.detail-row{height:140px;padding:18px;border-bottom:1px solid #ddd;box-sizing:border-box}
</style><h1>仅用于验证真实滚轮，不包含真实候选人资料</h1><div id="detail-scroll"><div id="detail-marker">首行位移标记</div>`)
	for index := 1; index <= 60; index++ {
		fmt.Fprintf(&content, `<div class="detail-row">合成详情第 %d 段 · 软件开发测试经历</div>`, index)
	}
	content.WriteString(`</div></html>`)
	return content.String()
}
