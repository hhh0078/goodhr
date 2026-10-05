// Package boss 文件作用：使用隔离 Camoufox 和本机聊天页面验证新旧发送按钮及回车发送，不操作真实招聘账号。
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

// TestMessageSendingBrowser 验证实际发布配置能够通过新旧按钮发送，缺失时只按一次回车，失败时保留错误。
func TestMessageSendingBrowser(t *testing.T) {
	browserDir := os.Getenv("GOODHR_BOSS_BROWSER_DIR")
	if browserDir == "" {
		t.Skip("设置 GOODHR_BOSS_BROWSER_DIR 后执行隔离发送回归")
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
	_ = listener.Close()
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
		if browser.Health(ctx) == nil {
			break
		}
		if attempt >= 49 {
			t.Fatal("隔离 Worker 未启动")
		}
		time.Sleep(100 * time.Millisecond)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 8*time.Second)
		defer stop()
		_, _ = browser.StopBrowser(cleanup)
	}()
	var buttonSends, keySends atomic.Int32
	// 脚本属于本机测试页面的交互实现，自动化只使用标准 Locator、鼠标和键盘。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sent" {
			if r.URL.Query().Get("via") == "button" {
				buttonSends.Add(1)
			} else {
				keySends.Add(1)
			}
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		button := ""
		if r.URL.Path == "/new" {
			button = `<div class="submit-content"><div class="submit active" onclick="send('button')">发送</div></div>`
		}
		if r.URL.Path == "/old" {
			button = `<button class="btn-send" onclick="send('button')">发送</button>`
		}
		fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><style>body{margin:80px;font:20px sans-serif}.chat-global-conversation{width:700px;padding:20px}.bosschat-chat-input{height:120px;border:1px solid #888;margin:30px 0;padding:15px}.submit,.btn-send{display:inline-block;padding:15px;background:#a87e25;color:white;cursor:pointer}</style><div class="chat-global-conversation"><div class="chatview-name">测试候选人</div><div class="bosschat-chat-input chat-global-message boss-chat-editor-input" contenteditable="true"></div>%s</div><script>const input=document.querySelector('[contenteditable]'); function send(via){if(location.pathname==='/broken')return;fetch('/sent?via='+via);input.textContent=''}input.addEventListener('keydown',event=>{if(event.key==='Enter'){event.preventDefault();send('key')}})</script>`, button)
	}))
	defer server.Close()
	headless := true
	if _, err := browser.StartBrowser(ctx, contract.BrowserStartRequest{Headless: &headless, UserDataDir: filepath.Join(dir, "profile"), DownloadsPath: filepath.Join(dir, "downloads"), URL: server.URL + "/new", ExtensionPaths: []string{filepath.Join(browserDir, "addons", "UBO")}}); err != nil {
		t.Fatal(err)
	}
	cfg, runtime := recommendationConfig(t), NewRuntime()
	if len(cfg.Selectors["candidate.followup_send"].Target.Selectors) != 2 {
		t.Fatal("发送按钮必须同时保留新旧候选数组")
	}
	for _, path := range []string{"/new", "/old", "/missing", "/broken"} {
		if _, err := browser.OpenPage(ctx, contract.PageOpenRequest{URL: server.URL + path, WaitUntil: "domcontentloaded"}); err != nil {
			t.Fatal(err)
		}
		err := runtime.SendCandidateMessage(ctx, browser, cfg, model.Candidate{Name: "测试候选人"}, "测试消息")
		if path == "/broken" {
			if err == nil || !strings.Contains(err.Error(), "发送结果尚未确认") {
				t.Fatalf("回车未发送却报告成功：%v", err)
			}
		} else if err != nil {
			t.Fatalf("%s 发送失败：%v", path, err)
		}
	}
	if buttonSends.Load() != 2 || keySends.Load() != 1 {
		t.Fatalf("发送次数错误：按钮=%d 回车=%d", buttonSends.Load(), keySends.Load())
	}
	content, err := os.ReadFile(logFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `"error_code":"ELEMENT_NOT_FOUND"`) || !strings.Contains(string(content), "改用回车发送消息") {
		t.Fatal("发送按钮缺失或回车兜底日志丢失")
	}
	t.Log("真实 Camoufox：新版和旧版各发送一次；缺失按钮时回车发送一次；回车无效明确报错且不重发；错误和兜底日志完整")
}
