// Package platform_test 文件作用：借用已运行 Worker，在独立本机标签验证收藏布局、唯一定位和重复保护，结束后恢复原标签页。
package platform_test

import (
	"context"
	"errors"
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
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// favoriteFixtureCounter 保存测试页面自身收到的真实点击，不读取或修改招聘页面。
type favoriteFixtureCounter struct{ favorite, group, save, noise atomic.Int32 }

// snapshot 返回固定顺序的点击计数，便于确认重复调用和歧义定位没有额外点击。
func (c *favoriteFixtureCounter) snapshot() [4]int32 {
	return [4]int32{c.favorite.Load(), c.group.Load(), c.save.Load(), c.noise.Load()}
}

// favoriteRecordingBrowser 保留正式封装点击及验证结果，不创建其他浏览器操作链路。
type favoriteRecordingBrowser struct {
	*client.Client
	clicks  []contract.ClickResult
	scrolls []contract.ScrollResult
}

// Click 复用正式 Browser Client，并记录 Worker 返回的点击和验证结果。
func (b *favoriteRecordingBrowser) Click(ctx context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	result, err := b.Client.Click(ctx, request)
	b.clicks = append(b.clicks, result)
	return result, err
}

// Scroll 复用真实鼠标滚轮封装，记录平台是否把详情顶部按钮滚回视口。
func (b *favoriteRecordingBrowser) Scroll(ctx context.Context, request contract.ScrollRequest) (contract.ScrollResult, error) {
	result, err := b.Client.Scroll(ctx, request)
	b.scrolls = append(b.scrolls, result)
	return result, err
}

// TestFavoriteExistingWorkerLayouts 使用明确指定的现有 Worker 验证本机收藏页面；真实岗位必须已停止，默认跳过。
func TestFavoriteExistingWorkerLayouts(t *testing.T) {
	workerURL := strings.TrimSpace(os.Getenv("GOODHR_FAVORITE_TEST_WORKER_URL"))
	if workerURL == "" {
		t.Skip("真实岗位停止后设置 GOODHR_FAVORITE_TEST_WORKER_URL，借用现有 Worker 执行本机收藏回归")
	}
	parsed, err := url.Parse(workerURL)
	if err != nil || parsed.Scheme != "http" || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost") || parsed.RawQuery != "" || (parsed.Path != "" && parsed.Path != "/") || parsed.User != nil || parsed.Fragment != "" {
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
		t.Fatalf("测试必须借用已经运行的浏览器：running=%t error=%v", status.Running, err)
	}
	before, err := browser.ListPages(ctx)
	if err != nil || len(before.Pages) == 0 {
		t.Fatalf("无法保存原标签页，测试未打开页面：%v", err)
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
		t.Fatal("当前标签页不唯一，测试未打开页面")
	}
	cases := []struct {
		name, platformID string
		ambiguous        bool
		wantClicks       int
		wantCounters     [4]int32
	}{
		{name: "boss-frame", platformID: "boss", wantClicks: 1, wantCounters: [4]int32{1, 0, 0, 0}},
		{name: "boss-root", platformID: "boss", wantClicks: 1, wantCounters: [4]int32{1, 0, 0, 0}},
		{name: "boss-ambiguous", platformID: "boss", ambiguous: true},
		{name: "hliepin", platformID: "hliepin", wantClicks: 3, wantCounters: [4]int32{1, 1, 1, 0}},
		{name: "hliepin-sticky", platformID: "hliepin", wantClicks: 3, wantCounters: [4]int32{1, 1, 1, 0}},
		{name: "hliepin-long", platformID: "hliepin", wantClicks: 3, wantCounters: [4]int32{1, 1, 1, 0}},
		{name: "hliepin-ambiguous", platformID: "hliepin", ambiguous: true},
	}
	counts := make(map[string]*favoriteFixtureCounter, len(cases))
	for _, test := range cases {
		counts[test.name] = &favoriteFixtureCounter{}
	}
	server := newFavoriteFixtureServer(counts)
	defer server.Close()
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if err := restoreFavoriteFixturePage(cleanupCtx, browser, original); err != nil {
			t.Errorf("恢复原标签失败：%v", err)
			return
		}
		after, err := browser.ListPages(cleanupCtx)
		if err != nil || !sameFavoriteFixturePages(before.Pages, after.Pages) {
			t.Errorf("测试结束后原有标签页集合发生变化：%v", err)
		}
	})
	for _, test := range cases {
		if !t.Run(test.name, func(t *testing.T) {
			fixtureURL := server.URL + "/layout/" + test.name
			t.Cleanup(func() {
				cleanupCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
				defer stop()
				if err := closeFavoriteFixturePage(cleanupCtx, browser, fixtureURL); err != nil {
					t.Errorf("关闭本机测试标签失败：%v", err)
				}
				if err := restoreFavoriteFixturePage(cleanupCtx, browser, original); err != nil {
					t.Errorf("恢复原标签失败：%v", err)
				}
			})
			caseCtx := client.WithTraceID(ctx, "favorite-fixture-"+test.name)
			newTab := true
			page, err := browser.OpenPage(caseCtx, contract.PageOpenRequest{URL: fixtureURL, NewTab: &newTab, WaitUntil: "load", TimeoutMS: 10_000})
			if err != nil || page.URL != fixtureURL {
				t.Fatalf("创建独立本机测试标签失败：%v", err)
			}
			cfg, err := platform.LoadConfig(test.platformID)
			if err != nil {
				t.Fatal(err)
			}
			runtime, err := platform.RuntimeFor(test.platformID)
			if err != nil {
				t.Fatal(err)
			}
			if test.name == "hliepin-long" {
				anchor := cfg.Selectors["candidate.detail"]
				rolled, err := browser.Scroll(caseCtx, contract.ScrollRequest{WheelAnchor: &anchor, Distance: 700, MaxAttempts: 3, WaitMS: 60})
				if err != nil || !rolled.Scrolled || rolled.Attempts == 0 {
					t.Fatalf("必须先用真实滚轮浏览到详情下面：result=%+v err=%v", rolled, err)
				}
				button, err := browser.Find(caseCtx, contract.ElementFindRequest{Selector: cfg.Selectors["candidate.favorite"]})
				if err != nil || button.View.InViewport {
					t.Fatalf("长详情回归前收藏按钮应处于视口上方：view=%+v err=%v", button.View, err)
				}
			}
			recorded := &favoriteRecordingBrowser{Client: browser}
			err = runtime.FavoriteCandidate(caseCtx, recorded, cfg, model.Candidate{Fingerprint: "local-fixture"})
			if test.ambiguous {
				var workerErr *contract.WorkerError
				if !errors.As(err, &workerErr) || workerErr.Body.Code != "ELEMENT_AMBIGUOUS" || counts[test.name].snapshot() != [4]int32{} {
					t.Fatalf("两个可见同名目标必须拒绝点击：err=%v counters=%v", err, counts[test.name].snapshot())
				}
				t.Log("两个可见同名收藏目标已拒绝，页面真实点击数保持 0")
				return
			}
			if err != nil {
				t.Fatalf("正式平台收藏流程失败：%v", err)
			}
			if len(recorded.clicks) != test.wantClicks || counts[test.name].snapshot() != test.wantCounters {
				t.Fatalf("必须只点击真实详情的正确目标：clicks=%+v counters=%v", recorded.clicks, counts[test.name].snapshot())
			}
			if test.name == "hliepin-long" && (len(recorded.scrolls) != 1 || !recorded.scrolls[0].Scrolled || recorded.scrolls[0].Attempts == 0) {
				t.Fatalf("平台必须用真实滚轮返回顶部收藏按钮：scrolls=%+v", recorded.scrolls)
			}
			for index, result := range recorded.clicks {
				if !result.Clicked || !result.Verified {
					t.Fatalf("第 %d 次正式点击尚未验证：%+v", index+1, result)
				}
			}
			saved := cfg.Selectors["candidate.favorite_saved"]
			items, err := browser.FindAll(caseCtx, contract.ElementFindAllRequest{Selector: saved, MaxItems: 2, ExpectedMissing: true, ProbeOnly: true})
			if err != nil || len(items) != 1 {
				t.Fatalf("真实点击后应只有一个已收藏状态：count=%d err=%v", len(items), err)
			}
			if test.platformID == "hliepin" {
				panel := cfg.Selectors["candidate.favorite_panel"]
				items, err = browser.FindAll(caseCtx, contract.ElementFindAllRequest{Selector: panel, MaxItems: 2, ExpectedMissing: true, ProbeOnly: true})
				if err != nil || len(items) != 0 {
					t.Fatalf("猎聘收藏保存后应关闭分组弹层：count=%d err=%v", len(items), err)
				}
			}
			if err := runtime.FavoriteCandidate(caseCtx, recorded, cfg, model.Candidate{Fingerprint: "local-fixture"}); err != nil {
				t.Fatalf("已收藏重复调用失败：%v", err)
			}
			if len(recorded.clicks) != test.wantClicks || counts[test.name].snapshot() != test.wantCounters {
				t.Fatalf("已收藏不得再次点击或改动分组：clicks=%d counters=%v", len(recorded.clicks), counts[test.name].snapshot())
			}
			t.Logf("布局=%s 正式点击=%d 验证全部通过 再次调用零新增点击 同名干扰目标零点击", test.name, test.wantClicks)
		}) {
			break
		}
	}
}

// newFavoriteFixtureServer 提供测试页面自身的普通点击事件；这些脚本仅由本机页面加载，严禁向招聘页面注入。
func newFavoriteFixtureServer(counts map[string]*favoriteFixtureCounter) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/clicked" {
			counter, exists := counts[r.URL.Query().Get("layout")]
			if !exists {
				http.Error(w, "未知测试页面", http.StatusBadRequest)
				return
			}
			switch r.URL.Query().Get("action") {
			case "favorite":
				counter.favorite.Add(1)
			case "group":
				counter.group.Add(1)
			case "save":
				counter.save.Add(1)
			case "noise":
				counter.noise.Add(1)
			default:
				http.Error(w, "未知测试点击", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		const header = `<!doctype html><meta charset="utf-8"><style>body{margin:24px;font:18px sans-serif}button{padding:12px 24px;font:18px sans-serif;margin:8px}iframe{width:1000px;height:600px;border:0}.ant-popover{padding:20px;width:360px;border:1px solid #777}.ant-radio-wrapper{display:inline-flex;align-items:center;padding:16px;gap:10px}.ant-radio-input{width:24px;height:24px}.lib-standard-resume{padding:24px;border:1px solid #777}</style>`
		layout := strings.TrimPrefix(r.URL.Path, "/layout/")
		if r.URL.Path == "/layout/boss-frame" {
			fmt.Fprint(w, header+`<div class="candidate-card">卡片上的同名收藏<button onclick="fetch('/clicked?layout=boss-frame&action=noise')">收藏</button></div><iframe name="recommendFrame" src="/recommend/boss-frame"></iframe>`)
			return
		}
		if r.URL.Path == "/recommend/boss-frame" {
			layout = "boss-frame"
		}
		if _, exists := counts[layout]; !exists {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(layout, "boss") {
			fmt.Fprint(w, header+favoriteBossFixture(layout))
			return
		}
		fmt.Fprint(w, header+favoriteHLiepinFixture(layout))
	}))
}

// favoriteBossFixture 提供可见详情、隐藏旧详情和卡片同名按钮；成功点击只更新测试页面自身状态。
func favoriteBossFixture(layout string) string {
	noise := fmt.Sprintf(`<button class="like-icon-and-text" onclick="fetch('/clicked?layout=%s&action=noise')">收藏</button>`, layout)
	button := fmt.Sprintf(`<button class="like-icon-and-text" onclick="const b=this;fetch('/clicked?layout=%s&action=favorite').then(()=>{b.textContent='已收藏'})">收藏</button>`, layout)
	if layout == "boss-ambiguous" {
		button += button
	}
	return `<div class="candidate-card"><div class="resume-right-side"><div class="communication">` + noise + `</div></div></div><div class="lib-standard-resume" style="display:none"><div class="resume-right-side"><div class="communication">` + noise + `</div></div></div><div class="lib-standard-resume"><div class="resume-right-side"><div class="communication">` + button + `</div></div></div>`
}

// favoriteHLiepinFixture 提供真实属性形状、多个隐藏收藏弹层及默认分组保存事件，屏蔽无关同名按钮。
func favoriteHLiepinFixture(layout string) string {
	button := fmt.Sprintf(`<button id="detail-favorite" class="resume-operation-btn" data-tlg-elem-id="h_pc_res_detail_favorite_btn" onclick="fetch('/clicked?layout=%s&action=favorite').then(()=>{document.getElementById('live-panel').style.display='block'})">收藏</button>`, layout)
	if layout == "hliepin-sticky" {
		button = strings.ReplaceAll(button, "h_pc_res_detail_favorite_btn", "h_pc_res_detail_top_favorite_btn")
	}
	if layout == "hliepin-ambiguous" {
		button += strings.ReplaceAll(button, `id="detail-favorite"`, `id="second-favorite"`)
	}
	noise := fmt.Sprintf(`<button onclick="fetch('/clicked?layout=%s&action=noise')">收藏</button>`, layout)
	hiddenTop := fmt.Sprintf(`<button class="resume-operation-btn" data-tlg-elem-id="h_pc_res_detail_top_favorite_btn" style="display:none" onclick="fetch('/clicked?layout=%s&action=noise')">收藏</button>`, layout)
	noisePanel := fmt.Sprintf(`<div class="ant-popover" style="display:none"><div class="rd-collecting-wrap"><label class="ant-radio-wrapper"><span class="ant-radio"><input class="ant-radio-input" type="radio" onclick="fetch('/clicked?layout=%s&action=noise')"></span><span>未分组</span></label><div class="btn-box"><button class="ant-btn-primary" onclick="fetch('/clicked?layout=%s&action=noise')">保存</button></div></div></div>`, layout, layout)
	panel := fmt.Sprintf(`<div id="live-panel" class="ant-popover" style="display:none"><div class="rd-collecting-wrap"><label class="ant-radio-wrapper"><span class="ant-radio"><input class="ant-radio-input" type="radio" onclick="const i=this;fetch('/clicked?layout=%s&action=group').then(()=>{i.parentElement.classList.add('ant-radio-checked');document.getElementById('save-favorite').disabled=false})"></span><span>未分组</span></label><div class="btn-box"><button id="save-favorite" class="ant-btn-primary" disabled onclick="fetch('/clicked?layout=%s&action=save').then(()=>{document.getElementById('detail-favorite').textContent='已收藏';document.getElementById('live-panel').style.display='none'})">保存</button></div></div></div>`, layout, layout)
	body := `<div class="candidate-card">候选人卡片上的同名按钮` + noise + `</div>` + hiddenTop + noisePanel + noisePanel + button + panel
	if layout == "hliepin-long" {
		body += `<div class="resume-detail" style="height:2600px">本机长详情正文，仅用于真实滚轮回归</div><div id="resume-bottom" style="height:60px">详情底部</div>`
	}
	return body
}

// closeFavoriteFixturePage 通过独占本机 URL 两次确认标签身份，只关闭本次创建的测试页。
func closeFavoriteFixturePage(ctx context.Context, browser *client.Client, fixtureURL string) error {
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
		return fmt.Errorf("本机测试标签身份不唯一，不关闭任何页面")
	}
	selected, err := browser.UsePage(ctx, contract.PageUseRequest{PageID: fixture.PageID})
	if err != nil || selected.URL != fixtureURL {
		return fmt.Errorf("本机测试标签身份变化，不关闭任何页面：%w", err)
	}
	status, err := browser.BrowserStatus(ctx)
	if err != nil || status.CurrentURL != fixtureURL {
		return fmt.Errorf("当前页不是本机测试标签，不关闭任何页面：%w", err)
	}
	return browser.ClosePage(ctx)
}

// restoreFavoriteFixturePage 优先核对原位置和地址，只恢复唯一匹配的原标签页，不重新导航招聘页面。
func restoreFavoriteFixturePage(ctx context.Context, browser *client.Client, original contract.PageInfo) error {
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
					return fmt.Errorf("恢复原位置标签失败：%w", err)
				}
				return nil
			}
		}
	}
	if matches != 1 {
		return fmt.Errorf("原标签地址不唯一，无法安全恢复")
	}
	selected, err := browser.UsePage(ctx, contract.PageUseRequest{PageID: target.PageID})
	if err != nil || selected.URL != original.URL {
		return fmt.Errorf("恢复原地址唯一标签失败：%w", err)
	}
	return nil
}

// sameFavoriteFixturePages 按地址和重复次数确认原有标签集合保持，不输出招聘页面内容或地址。
func sameFavoriteFixturePages(before, after []contract.PageInfo) bool {
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
