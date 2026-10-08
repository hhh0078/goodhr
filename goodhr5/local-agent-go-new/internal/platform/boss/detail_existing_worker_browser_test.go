// Package boss_test 文件作用：借用已运行的 Worker，在独立本机标签页回归 Boss 三种详情关闭布局，并恢复原标签页，不启动或停止浏览器。
package boss_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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

// PressKey 复用正式键盘封装，记录测试是否意外退回 Escape。
func (b *detailRecordingBrowser) PressKey(ctx context.Context, request contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	if request.Key == "Escape" {
		b.escapes++
	}
	return b.Client.PressKey(ctx, request)
}

// TestBossDetailCloseExistingWorkerLayouts 使用显式现有 Worker URL 回归三种关闭布局；运行前须确认真实岗位已停止，默认跳过。
func TestBossDetailCloseExistingWorkerLayouts(t *testing.T) {
	workerURL := strings.TrimSpace(os.Getenv("GOODHR_BOSS_DETAIL_TEST_WORKER_URL"))
	if workerURL == "" {
		t.Skip("真实岗位停止后设置 GOODHR_BOSS_DETAIL_TEST_WORKER_URL，借用现有 Worker 执行本机布局回归")
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
	counts := map[string]*atomic.Int32{"current": {}, "legacy-root": {}, "legacy-nested": {}}
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
	for _, layout := range []string{"current", "legacy-root", "legacy-nested"} {
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
			recorded := &detailRecordingBrowser{Client: browser}
			if err := common.CloseCandidateDetail(caseCtx, recorded, cfg); err != nil {
				t.Fatalf("正式平台关闭流程失败：%v", err)
			}
			if len(recorded.clicks) != 1 || !recorded.clicks[0].Clicked || !recorded.clicks[0].Verified || recorded.escapes != 0 || counts[layout].Load() != 1 {
				t.Fatalf("必须点击该布局自己的按钮并验证关闭：clicks=%+v escapes=%d fixture_clicks=%d", recorded.clicks, recorded.escapes, counts[layout].Load())
			}
			dialog.TimeoutMS = 200
			items, err = browser.FindAll(caseCtx, contract.ElementFindAllRequest{Selector: dialog, MaxItems: 1, ExpectedMissing: true, ProbeOnly: true})
			if err != nil || len(items) != 0 {
				t.Fatalf("真实点击后详情仍可见：count=%d error=%v", len(items), err)
			}
			t.Logf("layout=%s clicked=true verified=true fixture_clicks=1 hidden=true escape_calls=0", layout)
		}) {
			break
		}
	}
}

// newDetailLayoutServer 提供三种本机页面；onclick 脚本属于测试页面自身实现，不向招聘页面注入或执行任何脚本。
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
		switch r.URL.Path {
		case "/layout/current", "/layout/legacy-nested":
			layout := strings.TrimPrefix(r.URL.Path, "/layout/")
			fmt.Fprintf(w, `%s<iframe name="recommendFrame" src="/recommend/%s"></iframe>`, header, layout)
		case "/layout/legacy-root":
			fmt.Fprint(w, header+`<iframe class="detail" src="/web/frame/c-resume/legacy-root"></iframe>`)
		case "/recommend/current":
			fmt.Fprint(w, header+`<div class="lib-standard-resume"><button class="close-btn" onclick="fetch('/clicked?layout=current').then(()=>{document.querySelector('.lib-standard-resume').hidden=true})">关闭测试详情</button><br><iframe class="detail" src="/web/frame/c-resume/current"></iframe></div>`)
		case "/recommend/legacy-nested":
			fmt.Fprint(w, header+`<iframe class="detail" src="/web/frame/c-resume/legacy-nested"></iframe>`)
		case "/web/frame/c-resume/current":
			fmt.Fprint(w, header+`<div id="resume">本机测试详情正文，关闭按钮在外层。</div>`)
		case "/web/frame/c-resume/legacy-root":
			fmt.Fprint(w, header+`<button class="resume-custom-close" onclick="fetch('/clicked?layout=legacy-root').then(()=>{window.frameElement.hidden=true})">关闭旧版测试详情</button><div id="resume">本机并列框架测试正文。</div>`)
		case "/web/frame/c-resume/legacy-nested":
			fmt.Fprint(w, header+`<button class="boss-popup__close" onclick="fetch('/clicked?layout=legacy-nested').then(()=>{window.frameElement.hidden=true})">关闭旧版测试详情</button><div id="resume">本机内嵌框架测试正文。</div>`)
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
