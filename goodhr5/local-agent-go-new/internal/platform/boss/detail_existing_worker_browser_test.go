// Package boss_test 文件作用：借用明确指定的 Worker，在独立本机标签页回归 Boss 三种详情布局的 Escape 优先及备用按钮，并恢复原标签页。
package boss_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/common"
)

// detailRecordingBrowser 记录正式封装点击的返回值和 Escape 次数，不增加另一条浏览器操作链路。
type detailRecordingBrowser struct {
	*client.Client
	clicks  []contract.ClickResult
	escapes int
}

// Click 复用正式 Browser Client，并保存点击及隐藏验证结果。
func (b *detailRecordingBrowser) Click(ctx context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	result, err := b.Client.Click(ctx, request)
	b.clicks = append(b.clicks, result)
	return result, err
}

// PressKey 复用正式键盘封装，记录每种布局是否先使用 Escape。
func (b *detailRecordingBrowser) PressKey(ctx context.Context, request contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	if request.Key == "Escape" {
		b.escapes++
	}
	return b.Client.PressKey(ctx, request)
}

// TestBossDetailCloseExistingWorkerLayouts 使用显式 Worker URL 回归三种布局的两种关闭方式；优先指定隔离测试 Worker，默认跳过。
func TestBossDetailCloseExistingWorkerLayouts(t *testing.T) {
	workerURL := strings.TrimSpace(os.Getenv("GOODHR_BOSS_DETAIL_TEST_WORKER_URL"))
	if workerURL == "" && strings.TrimSpace(os.Getenv("GOODHR_BOSS_BROWSER_DIR")) != "" {
		workerURL = isolatedDetailTestWorker(t, os.Getenv("GOODHR_BOSS_BROWSER_DIR"))
	}
	if workerURL == "" {
		t.Skip("设置 GOODHR_BOSS_BROWSER_DIR 自动启动隔离回归，或显式指定 GOODHR_BOSS_DETAIL_TEST_WORKER_URL")
	}
	parsed, err := url.Parse(workerURL)
	if err != nil || parsed.Scheme != "http" || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost") || parsed.RawQuery != "" || (parsed.Path != "" && parsed.Path != "/") {
		t.Fatal("测试只接受明确指定的本机现有 Worker HTTP 地址")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	browser := client.New(strings.TrimRight(workerURL, "/"))
	if err := browser.Health(ctx); err != nil {
		t.Fatalf("现有 Worker 不可用，测试不会启动另一个 Worker：%v", err)
	}
	status, err := browser.BrowserStatus(ctx)
	if err != nil || !status.Running {
		t.Fatalf("测试必须借用已运行的浏览器：running=%t error=%v", status.Running, err)
	}
	before, err := browser.ListPages(ctx)
	if err != nil || len(before.Pages) == 0 {
		t.Fatalf("无法保存现有标签页，测试未打开页面：%v", err)
	}
	var original contract.PageInfo
	currentCount := 0
	for _, page := range before.Pages {
		if page.Current {
			original = page
			currentCount++
		}
	}
	if currentCount != 1 {
		t.Fatal("现有当前标签页不唯一，测试未打开页面")
	}
	cfg, err := platform.LoadConfig("boss")
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]*atomic.Int32{"current": {}, "legacy-root": {}, "legacy-nested": {}, "current-escape": {}, "legacy-root-escape": {}, "legacy-nested-escape": {}}
	server := newDetailLayoutServer(counts)
	defer server.Close()
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if err := restoreDetailTestPage(cleanupCtx, browser, original); err != nil {
			t.Errorf("恢复原标签页失败：%v", err)
			return
		}
		after, err := browser.ListPages(cleanupCtx)
		if err != nil || !sameDetailTestPages(before.Pages, after.Pages) {
			t.Errorf("测试结束时原有标签页集合未保持：error=%v", err)
		}
	})
	for _, layout := range []string{"current-escape", "legacy-root-escape", "legacy-nested-escape", "current", "legacy-root", "legacy-nested"} {
		if !t.Run(layout, func(t *testing.T) {
			fixtureURL := server.URL + "/layout/" + layout
			t.Cleanup(func() {
				cleanupCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
				defer stop()
				if err := closeDetailTestPage(cleanupCtx, browser, fixtureURL); err != nil {
					t.Errorf("关闭自己创建的本机测试标签失败：%v", err)
				}
				if err := restoreDetailTestPage(cleanupCtx, browser, original); err != nil {
					t.Errorf("恢复原标签页失败：%v", err)
				}
			})
			caseCtx := client.WithTraceID(ctx, "boss-detail-layout-"+layout)
			newTab := true
			page, err := browser.OpenPage(caseCtx, contract.PageOpenRequest{URL: fixtureURL, NewTab: &newTab, WaitUntil: "load", TimeoutMS: 10_000})
			if err != nil || page.URL != fixtureURL {
				t.Fatalf("创建独立本机测试标签失败：%v", err)
			}
			dialog := cfg.Selectors["candidate.detail_dialog"]
			items, err := browser.FindAll(caseCtx, contract.ElementFindAllRequest{Selector: dialog, MaxItems: 1, ExpectedMissing: true, ProbeOnly: true})
			if err != nil || len(items) != 1 {
				t.Fatalf("本机测试详情未出现：count=%d error=%v", len(items), err)
			}
			escapeWorks := strings.HasSuffix(layout, "-escape")
			if escapeWorks {
				// 真正点进详情 iframe，证明焦点在内层文档时 Escape 同样可关闭，不手工修改焦点。
				if _, err := browser.Click(caseCtx, contract.ElementClickRequest{Selector: cfg.Selectors["candidate.detail"]}); err != nil {
					t.Fatalf("真实点击详情正文以定位键盘焦点失败：%v", err)
				}
			}
			recorded := &detailRecordingBrowser{Client: browser}
			if err := common.CloseCandidateDetail(caseCtx, recorded, cfg); err != nil {
				t.Fatalf("正式平台关闭流程失败：%v", err)
			}
			if escapeWorks {
				if len(recorded.clicks) != 0 || recorded.escapes != 1 || counts[layout].Load() != 0 {
					t.Fatalf("Escape 成功后不能再点击按钮：clicks=%+v escapes=%d fixture_clicks=%d", recorded.clicks, recorded.escapes, counts[layout].Load())
				}
			} else if len(recorded.clicks) != 1 || !recorded.clicks[0].Clicked || !recorded.clicks[0].Verified || recorded.escapes != 1 || counts[layout].Load() != 1 {
				t.Fatalf("Escape 无效后必须点击该布局自己的按钮并验证关闭：clicks=%+v escapes=%d fixture_clicks=%d", recorded.clicks, recorded.escapes, counts[layout].Load())
			}
			dialog.TimeoutMS = 200
			items, err = browser.FindAll(caseCtx, contract.ElementFindAllRequest{Selector: dialog, MaxItems: 1, ExpectedMissing: true, ProbeOnly: true})
			if err != nil || len(items) != 0 {
				t.Fatalf("真实点击后详情仍可见：count=%d error=%v", len(items), err)
			}
			t.Logf("layout=%s escape_calls=%d fixture_clicks=%d hidden=true", layout, recorded.escapes, counts[layout].Load())
		}) {
			break
		}
	}
}

// isolatedDetailTestWorker 复用正式 Worker 和本机浏览器，以独立临时 Profile 运行回归，结束后只关闭本测试创建的进程。
func isolatedDetailTestWorker(t *testing.T, browserDir string) string {
	t.Helper()
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
	directory := t.TempDir()
	logFile, err := os.Create(filepath.Join(directory, "worker.log"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("node", filepath.Join(moduleRoot, "worker", "dist", "main.js"))
	command.Env = append(os.Environ(), fmt.Sprintf("GOODHR_WORKER_PORT=%d", port), "CAMOUFOX_INSTALL_DIR="+browserDir, "GOODHR_UBO_EXTENSION_PATH="+filepath.Join(browserDir, "addons", "UBO"))
	command.Stdout, command.Stderr = logFile, logFile
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		t.Fatal(err)
	}
	workerURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	browser := client.New(workerURL)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_, _ = browser.StopBrowser(cleanupCtx)
		_ = command.Process.Kill()
		_ = command.Wait()
		_ = logFile.Close()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	for attempt := 0; ; attempt++ {
		if err := browser.Health(ctx); err == nil {
			break
		} else if attempt == 49 {
			t.Fatalf("隔离 Worker 未启动：%v，日志 %s", err, logFile.Name())
		}
		time.Sleep(100 * time.Millisecond)
	}
	headless := true
	if _, err := browser.StartBrowser(ctx, contract.BrowserStartRequest{
		Headless: &headless, UserDataDir: filepath.Join(directory, "profile"), DownloadsPath: filepath.Join(directory, "downloads"), URL: "about:blank",
		ExtensionPaths: []string{filepath.Join(browserDir, "addons", "UBO")},
	}); err != nil {
		t.Fatalf("启动隔离浏览器失败：%v，日志 %s", err, logFile.Name())
	}
	return workerURL
}

// newDetailLayoutServer 提供三种本机布局及各自的按键响应版本；事件脚本属于测试页面自身实现，不向招聘页面注入脚本。
func newDetailLayoutServer(counts map[string]*atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path == "/clicked" {
			if counter, exists := counts[r.URL.Query().Get("layout")]; exists {
				counter.Add(1)
				w.WriteHeader(http.StatusNoContent)
				return
			}
			http.Error(w, "未知测试布局", http.StatusBadRequest)
			return
		}
		const header = `<!doctype html><meta charset="utf-8"><style>body{margin:24px;font:18px sans-serif}iframe{width:960px;height:600px;border:0}button{padding:16px 24px;font:18px sans-serif}iframe.detail{width:840px;height:450px}</style>`
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		layout := parts[len(parts)-1]
		baseLayout := strings.TrimSuffix(layout, "-escape")
		if _, exists := counts[layout]; !exists {
			http.NotFound(w, r)
			return
		}
		var closeScript string
		if baseLayout == "current" && strings.HasPrefix(r.URL.Path, "/recommend/") {
			closeScript = "document.querySelector('.lib-standard-resume').hidden=true"
		} else if strings.HasPrefix(r.URL.Path, "/web/frame/c-resume/") {
			closeScript = "parent.closeTestDetail()"
		} else {
			closeScript = "document.querySelector('iframe.detail').hidden=true"
		}
		fmt.Fprint(w, header)
		fmt.Fprintf(w, `<script>function closeTestDetail(){%s}</script>`, closeScript)
		if strings.HasSuffix(layout, "-escape") {
			fmt.Fprint(w, `<script>addEventListener('keydown',event=>{if(event.key==='Escape')closeTestDetail()})</script>`)
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/layout/") && baseLayout != "legacy-root":
			fmt.Fprintf(w, `<iframe name="recommendFrame" src="/recommend/%s"></iframe>`, layout)
		case strings.HasPrefix(r.URL.Path, "/layout/") && baseLayout == "legacy-root":
			fmt.Fprintf(w, `<iframe class="detail" src="/web/frame/c-resume/%s"></iframe>`, layout)
		case strings.HasPrefix(r.URL.Path, "/recommend/") && baseLayout == "current":
			fmt.Fprintf(w, `<div class="lib-standard-resume"><button class="close-btn" onclick="fetch('/clicked?layout=%s').then(closeTestDetail)">关闭测试详情</button><br><iframe class="detail" src="/web/frame/c-resume/%s"></iframe></div>`, layout, layout)
		case strings.HasPrefix(r.URL.Path, "/recommend/") && baseLayout == "legacy-nested":
			fmt.Fprintf(w, `<iframe class="detail" src="/web/frame/c-resume/%s"></iframe>`, layout)
		case strings.HasPrefix(r.URL.Path, "/web/frame/c-resume/"):
			if baseLayout != "current" {
				closeClass := "resume-custom-close"
				if baseLayout == "legacy-nested" {
					closeClass = "boss-popup__close"
				}
				fmt.Fprintf(w, `<button class="%s" onclick="fetch('/clicked?layout=%s').then(closeTestDetail)">关闭旧版测试详情</button>`, closeClass, layout)
			}
			fmt.Fprint(w, `<div id="resume" style="width:760px;height:300px">本机测试详情正文。</div>`)
		default:
			http.NotFound(w, r)
		}
	}))
}

// closeDetailTestPage 按独占测试 URL 重新确认标签身份，只关闭当前测试标签，不关闭原有页面或浏览器。
func closeDetailTestPage(ctx context.Context, browser *client.Client, fixtureURL string) error {
	pages, err := browser.ListPages(ctx)
	if err != nil {
		return err
	}
	var fixture contract.PageInfo
	matches := 0
	for _, page := range pages.Pages {
		if page.URL == fixtureURL {
			fixture = page
			matches++
		}
	}
	if matches == 0 {
		return nil
	}
	if matches != 1 {
		return fmt.Errorf("测试标签身份不唯一，不执行关闭")
	}
	selected, err := browser.UsePage(ctx, contract.PageUseRequest{PageID: fixture.PageID})
	if err != nil || selected.URL != fixtureURL {
		return fmt.Errorf("测试标签身份已变化，不执行关闭：%w", err)
	}
	status, err := browser.BrowserStatus(ctx)
	if err != nil || status.CurrentURL != fixtureURL {
		return fmt.Errorf("当前页面不是测试标签，不执行关闭：%w", err)
	}
	return browser.ClosePage(ctx)
}

// restoreDetailTestPage 优先核对原位置和地址，位置变化时只在地址唯一匹配后恢复，不猜测其他招聘页面。
func restoreDetailTestPage(ctx context.Context, browser *client.Client, original contract.PageInfo) error {
	pages, err := browser.ListPages(ctx)
	if err != nil {
		return err
	}
	var target contract.PageInfo
	matches := 0
	for _, page := range pages.Pages {
		if page.URL == original.URL {
			target = page
			matches++
			if page.PageID == original.PageID {
				selected, err := browser.UsePage(ctx, contract.PageUseRequest{PageID: page.PageID})
				if err != nil || selected.URL != original.URL {
					return fmt.Errorf("恢复原位置的标签失败：%w", err)
				}
				return nil
			}
		}
	}
	if matches != 1 {
		return fmt.Errorf("原标签位置已变化且地址匹配不唯一")
	}
	selected, err := browser.UsePage(ctx, contract.PageUseRequest{PageID: target.PageID})
	if err != nil || selected.URL != original.URL {
		return fmt.Errorf("恢复原地址的唯一标签失败：%w", err)
	}
	return nil
}

// sameDetailTestPages 用地址及重复次数确认全部原有标签仍在，不输出招聘页面地址或内容。
func sameDetailTestPages(before, after []contract.PageInfo) bool {
	if len(before) != len(after) {
		return false
	}
	counts := make(map[string]int, len(before))
	for _, page := range before {
		counts[page.URL]++
	}
	for _, page := range after {
		counts[page.URL]--
		if counts[page.URL] < 0 {
			return false
		}
	}
	return true
}
