// Package boss 文件作用：使用隔离 Worker 和本机页面回归完整卡片标题读取，验证热搜牛人不被点击且前后普通详情正常。
package boss

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestHotSearchWholeCardBrowser 用真实 Locator 读取卡片外层标题；设置 GOODHR_BOSS_BROWSER_DIR 后启用，普通单元测试跳过。
func TestHotSearchWholeCardBrowser(t *testing.T) {
	browserDir := os.Getenv("GOODHR_BOSS_BROWSER_DIR")
	if browserDir == "" {
		t.Skip("设置 GOODHR_BOSS_BROWSER_DIR 指向已安装的浏览器后执行隔离回归")
	}
	moduleRoot, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	logFile, err := os.Create(filepath.Join(dir, "worker.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	command := exec.Command("node", filepath.Join(moduleRoot, "worker", "dist", "main.js"))
	command.Env = append(os.Environ(), fmt.Sprintf("GOODHR_WORKER_PORT=%d", port), "CAMOUFOX_INSTALL_DIR="+browserDir, "GOODHR_UBO_EXTENSION_PATH="+filepath.Join(browserDir, "addons", "UBO"))
	command.Stdout, command.Stderr = logFile, logFile
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	browser := client.New(fmt.Sprintf("http://127.0.0.1:%d", port))
	for attempt := 0; ; attempt++ {
		if err := browser.Health(ctx); err == nil {
			break
		} else if attempt == 49 {
			t.Fatalf("隔离 Worker 未启动：%v，日志 %s", err, logFile.Name())
		}
		time.Sleep(100 * time.Millisecond)
	}
	defer func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 8*time.Second)
		defer stop()
		_, _ = browser.StopBrowser(cleanupCtx)
	}()
	var promoClicks atomic.Int32
	// 事件处理属于本机测试页面本身；自动化只调用标准 Locator 和真实鼠标，不向招聘页面注入脚本。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch request.URL.Path {
		case "/":
			fmt.Fprint(w, `<!doctype html><meta charset="utf-8"><style>body{margin:0}iframe{border:0;width:1240px;height:900px}.detail{position:fixed;left:200px;top:100px;width:800px;height:700px;background:white}</style><iframe name="recommendFrame" src="/recommend"></iframe><script>function showDetail(name){let frame=document.createElement('iframe');frame.className='detail';frame.src='/web/frame/c-resume/?name='+encodeURIComponent(name);document.body.append(frame)}function closeDetail(){document.querySelector('.detail').remove()}</script>`)
		case "/recommend":
			fmt.Fprint(w, `<!doctype html><meta charset="utf-8"><style>body{margin:64px;font:18px sans-serif}.card-list{height:820px;overflow:auto;padding-bottom:64px;box-sizing:border-box}.candidate-card-wrap{height:160px;border-bottom:1px solid #ddd;padding:10px}.base-info{width:480px;padding:12px;cursor:pointer}.name{font-weight:bold}.btn{float:right;margin:20px}h3{margin:12px}</style><div class="card-list"><div class="list-content">`)
			fmt.Fprint(w, `<div class="normal-wrapper"><div class="candidate-card-wrap"><span class="name">文先生</span><span>热搜</span><button class="btn btn-greet">打招呼</button><div class="base-info join-text-wrap" onclick="parent.showDetail('文先生')">32岁 本科 嵌入式开发</div></div></div>`)
			// 外层标题在候选人小卡片之外，并故意提供招呼按钮，必须仍然直接跳过。
			fmt.Fprint(w, `<section class="promotion"><h3>热搜牛人推荐</h3><a onclick="fetch('/promo-click')">查看更多</a><div class="promotion-body"><div class="candidate-card-wrap"><span class="name">尚先生</span><span>热搜</span><span>首页推荐</span><button class="btn btn-greet" onclick="fetch('/promo-click')">打招呼</button><div class="base-info join-text-wrap" onclick="fetch('/promo-click')">31岁 本科 嵌入式开发</div></div></div></section>`)
			fmt.Fprint(w, `<div class="candidate-card-wrap"><h3>热搜牛人推荐</h3><span class="name">李先生</span><div class="base-info join-text-wrap" onclick="fetch('/promo-click')">29岁 本科 嵌入式开发</div></div>`)
			fmt.Fprint(w, `<div class="normal-wrapper"><div class="candidate-card-wrap"><span class="name">王先生</span><button class="btn btn-greet">打招呼</button><div class="base-info join-text-wrap" onclick="parent.showDetail('王先生')">34岁 本科 嵌入式开发</div></div></div></div><div style="height:80px"></div></div>`)
		case "/web/frame/c-resume/":
			fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><button class="resume-custom-close" onclick="parent.closeDetail()">关闭</button><div id="resume" style="width:760px;height:580px">%s 普通候选人详情</div>`, request.URL.Query().Get("name"))
		case "/promo-click":
			promoClicks.Add(1)
		}
	}))
	defer server.Close()
	headless := true
	if _, err := browser.StartBrowser(ctx, contract.BrowserStartRequest{
		Headless: &headless, UserDataDir: filepath.Join(dir, "profile"), DownloadsPath: filepath.Join(dir, "downloads"), URL: server.URL,
		ExtensionPaths: []string{filepath.Join(browserDir, "addons", "UBO")},
	}); err != nil {
		t.Fatalf("启动隔离浏览器失败：%v，日志 %s", err, logFile.Name())
	}
	cfg, runtime := recommendationConfig(t), NewRuntime()
	candidates, err := runtime.FindCandidates(ctx, browser, cfg, model.CandidateListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 4 || candidates[0].SkipReason != "" || candidates[1].SkipReason == "" || candidates[2].SkipReason == "" || candidates[3].SkipReason != "" {
		t.Fatalf("热搜牛人及相邻普通卡片识别错误：%+v", candidates)
	}
	if strings.Contains(candidates[1].Summary, "热搜牛人") || !strings.Contains(candidates[1].Fields["card_text"], "热搜牛人") {
		t.Fatalf("必须从小卡片外层读取标题：%+v", candidates[1])
	}
	for index, candidate := range candidates {
		if candidate.Index != index {
			t.Fatalf("跳过卡片不能改变原序号：%+v", candidate)
		}
	}
	for _, index := range []int{0, 3} {
		candidate := candidates[index]
		if strings.Contains(candidate.Fields["card_text"], "热搜牛人") {
			t.Fatalf("普通卡片不得读取相邻卡片的标题：%+v", candidate)
		}
		if err := runtime.ScrollToCandidate(ctx, browser, cfg, candidate); err != nil {
			t.Fatal(err)
		}
		if err := runtime.OpenCandidateDetail(ctx, browser, cfg, candidate); err != nil {
			t.Fatal(err)
		}
		detail, err := runtime.ExtractCandidateDetail(ctx, browser, cfg, candidate)
		if err != nil || !strings.Contains(detail.Text, candidate.Name) {
			t.Fatalf("普通卡片详情必须对应当前候选人：%s，错误 %v", candidate.Name, err)
		}
		if err := runtime.CloseCandidateDetail(ctx, browser, cfg, candidate); err != nil {
			t.Fatal(err)
		}
	}
	if count := promoClicks.Load(); count != 0 {
		t.Fatalf("热搜牛人卡片不得被点击，实际点击 %d 次", count)
	}
	t.Log("完整卡片外层标题读取成功；两张热搜卡片直接跳过且点击为零；前后普通详情均正常；原序号保持不变")
}
