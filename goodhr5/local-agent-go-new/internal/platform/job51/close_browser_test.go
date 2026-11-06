// Package job51 文件作用：用隔离 Camoufox 和本机仿真页面复现关闭后悬浮卡片遮挡，验证正式平台关闭动作会立即移走鼠标。
package job51

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestCloseMovesMouseBrowser 显式指定浏览器目录后验证真实关闭和鼠标事件；不复用用户账号或改变运行中的正式程序。
func TestCloseMovesMouseBrowser(t *testing.T) {
	browserDir := os.Getenv("GOODHR_JOB51_BROWSER_DIR")
	if browserDir == "" {
		t.Skip("设置 GOODHR_JOB51_BROWSER_DIR 后执行隔离浏览器回归")
	}
	root, err := filepath.Abs("../../..")
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
	command := exec.Command("node", filepath.Join(root, "worker/dist/main.js"))
	addonDir := filepath.Join(browserDir, "addons", "UBO")
	if _, err := os.Stat(addonDir); err != nil {
		addonDir = filepath.Join(browserDir, "Camoufox.app/Contents/Resources/addons/UBO")
	}
	command.Env = append(os.Environ(), fmt.Sprintf("GOODHR_WORKER_PORT=%d", port), "CAMOUFOX_INSTALL_DIR="+browserDir, "GOODHR_UBO_EXTENSION_PATH="+addonDir)
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
		} else if attempt >= 49 {
			t.Fatalf("隔离 Worker 启动失败：%v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 8*time.Second)
		defer stop()
		_, _ = browser.StopBrowser(cleanup)
	}()
	// 以下事件仅属于自建的本机测试页面，正式自动化不注入页面脚本。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><meta charset="utf-8"><title>关闭后悬浮回归</title>
<style>body{margin:0;font:20px sans-serif}button{font:inherit;padding:12px}.points{position:fixed;right:60px;top:20px;width:80px;height:50px;background:#ffcd58}.hover{display:none;position:absolute;right:0;top:50px;width:350px;height:300px;background:#fae7bc}.points:hover .hover{display:block}.dialog{position:fixed;inset:0;background:#eee;z-index:10}.close,.operate-item.right{position:fixed;right:60px;top:20px;width:80px;height:50px;padding:0}#open-detail{margin:150px 30px 0}#mouse{display:block;margin:20px}</style>
<div class="points">积分<div class="hover">悬浮卡片挡住操作</div></div><button id="open-detail" onclick="document.querySelector('.resume-detail-modal').hidden=false">打开详情</button><button id="open-chat" onclick="document.querySelector('#chatAreaEmbed').hidden=false">打开聊天</button><output id="mouse">鼠标尚未移动</output>
<div class="dialog resume-detail-modal" hidden>候选人详情<button class="close" onclick="this.parentElement.hidden=true">关闭</button></div>
<div id="chatAreaEmbed" class="dialog chat-container" hidden><div class="chat-new-header"><div class="chat-user-operate"><button class="operate-item right" onclick="document.querySelector('#chatAreaEmbed').hidden=true">关闭</button></div></div>候选人聊天</div>
<script>document.addEventListener('mousemove',e=>{document.querySelector('#mouse').textContent=JSON.stringify({x:e.clientX,y:e.clientY,width:innerWidth,height:innerHeight})})</script>`)
	}))
	defer server.Close()
	headless := true
	if _, err := browser.StartBrowser(ctx, contract.BrowserStartRequest{Headless: &headless, URL: server.URL, UserDataDir: filepath.Join(dir, "profile"), DownloadsPath: filepath.Join(dir, "downloads")}); err != nil {
		t.Fatal(err)
	}
	cfg := loadPositionConfig(t)
	runtime := NewRuntime()
	for _, kind := range []string{"detail", "chat"} {
		open := contract.SelectorSpec{Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: "#open-" + kind}}}, Description: "本机测试打开按钮"}
		if _, err := browser.Click(ctx, contract.ElementClickRequest{Selector: open}); err != nil {
			t.Fatal(err)
		}
		key := "candidate.detail_close"
		if kind == "chat" {
			key = "candidate.chat_close"
		}
		// 对照组直接点击同一个关闭按钮，必须能复现鼠标停留引发的悬浮卡片。
		if _, err := browser.Click(ctx, contract.ElementClickRequest{Selector: cfg.Selectors[key]}); err != nil {
			t.Fatal(err)
		}
		hover := contract.SelectorSpec{Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".hover:visible"}}}, State: "visible", TimeoutMS: 1000, Description: "本机悬浮卡片"}
		if _, err := browser.Find(ctx, contract.ElementFindRequest{Selector: hover}); err != nil {
			t.Fatalf("%s对照组未复现悬浮遮挡：%v", kind, err)
		}
		if evidence := os.Getenv("GOODHR_JOB51_EVIDENCE_DIR"); evidence != "" {
			if _, err := browser.Screenshot(ctx, contract.ScreenshotRequest{Directory: evidence, Filename: kind + "-before.png"}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := browser.Click(ctx, contract.ElementClickRequest{Selector: open}); err != nil {
			t.Fatal(err)
		}
		if kind == "detail" {
			err = runtime.CloseCandidateDetail(ctx, browser, cfg, model.Candidate{})
		} else {
			err = runtime.CloseCandidateConversation(ctx, browser, cfg, model.Candidate{})
		}
		if err != nil {
			t.Fatal(err)
		}
		testCfg := model.Config{Selectors: map[string]contract.SelectorSpec{"hover": hover}}
		if shown, err := common.ProbeSelectorExists(ctx, browser, testCfg, "hover"); err != nil || shown {
			t.Fatalf("%s关闭后仍有悬浮卡片：shown=%t err=%v", kind, shown, err)
		}
		mouse := contract.SelectorSpec{Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: "#mouse"}}}, Description: "本机鼠标坐标"}
		position, err := browser.Read(ctx, contract.ElementReadRequest{Selector: mouse, Property: "text"})
		if err != nil {
			t.Fatal(err)
		}
		var coords struct{ X, Y, Width, Height float64 }
		if err := json.Unmarshal([]byte(position.Value), &coords); err != nil || coords.Width <= 0 || coords.Height <= 0 || math.Abs(coords.X-coords.Width/2) > 2 || math.Abs(coords.Y-coords.Height/2) > 2 {
			t.Fatalf("%s关闭后鼠标未到页面中央：%s err=%v", kind, position.Value, err)
		}
		if evidence := os.Getenv("GOODHR_JOB51_EVIDENCE_DIR"); evidence != "" {
			if _, err := browser.Screenshot(ctx, contract.ScreenshotRequest{Directory: evidence, Filename: kind + "-after.png"}); err != nil {
				t.Fatal(err)
			}
		}
		t.Logf("%s：对照组复现悬浮卡片，正式关闭后消失；鼠标坐标 %s", kind, position.Value)
	}
}
