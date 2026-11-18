// Package boss_test 文件作用：通过显式指定的唯一Worker和无脚本本地页面，回归名单换位及iframe内40段长截图；不访问招聘账号。
package boss_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestCandidateReorderAndLongScreenshotExistingWorker 仅使用已启动的隔离Worker；三端共用Go与Worker链路，不创建第二条浏览器连接。
func TestCandidateReorderAndLongScreenshotExistingWorker(t *testing.T) {
	endpoint := os.Getenv("GOODHR_REGRESSION_TEST_WORKER_URL")
	if endpoint == "" {
		t.Skip("需显式指定已启动的隔离Worker，不自动启动其他Worker")
	}
	if endpoint != "http://127.0.0.1:39881" {
		t.Fatal("本回归只借用明确管理的39881隔离Worker")
	}
	dir := os.Getenv("GOODHR_REGRESSION_EVIDENCE")
	if dir == "" {
		dir = t.TempDir()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	browser := client.New(endpoint)
	status, err := browser.BrowserStatus(ctx)
	if err != nil || status.Running {
		t.Fatal("回归开始前Worker浏览器必须为空，不接管用户浏览器")
	}
	server := regressionFixtureServer()
	defer server.Close()
	headless := true
	if _, err = browser.StartBrowser(ctx, contract.BrowserStartRequest{Headless: &headless, URL: server.URL + "/list", UserDataDir: filepath.Join(dir, "fixture-profile")}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_, _ = browser.StopBrowser(cleanup)
	}()

	cfg, err := platform.LoadConfig("zhaopin")
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := common.FindCandidates(ctx, browser, cfg, "zhaopin", model.CandidateListOptions{Cursor: &model.CandidateListCursor{IdentityByIndex: true}, ReadConcurrency: 5})
	if err != nil || len(candidates) != 20 {
		t.Fatalf("真实DOM名单读取失败：%d %v", len(candidates), err)
	}
	candidate := candidates[12]
	if _, err = browser.OpenPage(ctx, contract.PageOpenRequest{URL: server.URL + "/list?rotate=1"}); err != nil {
		t.Fatal(err)
	}
	legacy := cfg
	legacy.Behavior.CandidateUniqueIdentity = false
	var workerError *contract.WorkerError
	if err = common.ScrollToCandidate(ctx, browser, legacy, candidate); !errors.As(err, &workerError) || workerError.Body.Code != "ELEMENT_NOT_FOUND" {
		t.Fatalf("旧行号未复现预期的身份失配：%v", err)
	}
	if err = common.ScrollToCandidate(ctx, browser, cfg, candidate); err != nil {
		t.Fatalf("换位后未找到原合成人选：%v", err)
	}
	selector, err := common.CandidateSelector(cfg, candidate)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := browser.Read(ctx, contract.ElementReadRequest{Selector: selector, Property: "text"})
	if err != nil || !strings.Contains(identity.Value, "合成人选12") {
		t.Fatalf("换位后身份核对失败：%v", err)
	}
	if _, err = browser.OpenPage(ctx, contract.PageOpenRequest{URL: server.URL + "/list?rotate=1&duplicate=1"}); err != nil {
		t.Fatal(err)
	}
	err = common.ScrollToCandidate(ctx, browser, cfg, candidate)
	if !errors.As(err, &workerError) || workerError.Body.Code != "ELEMENT_AMBIGUOUS" {
		t.Fatalf("完整身份重复未停止：%v", err)
	}
	t.Log("真实DOM：旧行号失配；唯一身份换位后匹配；重复身份停止；未发送消息")

	if _, err = browser.OpenPage(ctx, contract.PageOpenRequest{URL: server.URL + "/detail"}); err != nil {
		t.Fatal(err)
	}
	bossCfg, err := platform.LoadConfig("boss")
	if err != nil {
		t.Fatal(err)
	}
	request := contract.LongScreenshotRequest{Target: bossCfg.Selectors["candidate.detail_screenshot"], Directory: filepath.Join(dir, "parts"), Filename: "synthetic-detail.png", Distance: 520, MaxParts: 20, WaitMS: 50, ViewportOnly: true}
	_, err = browser.LongScreenshot(ctx, request)
	var diagnostic struct {
		CapturedParts    int    `json:"captured_parts"`
		CompletionReason string `json:"completion_reason"`
	}
	if !errors.As(err, &workerError) || workerError.Body.Code != "SCREENSHOT_FAILED" || json.Unmarshal(workerError.Body.Details, &diagnostic) != nil || diagnostic.CapturedParts != 20 || diagnostic.CompletionReason != "part_limit" {
		t.Fatalf("20段上限未复现预期的分段超限：%v %+v", err, diagnostic)
	}
	request.MaxParts, request.Filename = 40, "synthetic-detail-40.png"
	result, err := browser.LongScreenshot(ctx, request)
	if err != nil || !result.Complete || result.Count <= 20 || result.Count > 40 {
		t.Fatalf("40段真实浏览器回归未完整：count=%d complete=%t err=%v", result.Count, result.Complete, err)
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil || os.WriteFile(filepath.Join(dir, "native-screenshot-result.json"), data, 0o600) != nil {
		t.Fatal("保存真实浏览器回归凭据失败")
	}
	t.Logf("真实iframe及真实滚轮：20段失败，40段上限内%d段确认完整；仅采集合成内容", result.Count)
}

// regressionFixtureServer 提供普通HTML与原生iframe滚动，名单换位通过重新导航，完全不需要页面脚本。
func regressionFixtureServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html;charset=utf-8")
		switch r.URL.Path {
		case "/list":
			_, _ = fmt.Fprint(w, `<!doctype html><meta charset="utf-8"><style>body{margin:0}.recommend-resume-item__inner{height:150px;padding:12px;border:1px solid #999}</style><div class="recommend-list"><div role="group">`)
			for index := 0; index < 20; index++ {
				id := index
				if r.URL.Query().Get("rotate") != "" {
					id = (index + 1) % 20
				}
				_, _ = fmt.Fprint(w, syntheticIdentityCard(id))
			}
			if r.URL.Query().Get("duplicate") != "" {
				_, _ = fmt.Fprint(w, syntheticIdentityCard(12))
			}
			_, _ = fmt.Fprint(w, `</div></div>`)
		case "/detail":
			_, _ = fmt.Fprint(w, `<!doctype html><style>body{margin:0}iframe{position:absolute;left:100px;top:80px;width:700px;height:650px;border:0}</style><iframe src="/web/frame/c-resume/fixture"></iframe>`)
		case "/web/frame/c-resume/fixture":
			_, _ = fmt.Fprint(w, `<!doctype html><meta charset="utf-8"><style>html,body{margin:0}#resume{box-sizing:border-box;height:10000px;background:repeating-linear-gradient(#fff 0px,#eee 71px,#fff 137px);font:20px sans-serif}p{height:170px;margin:0;border-bottom:1px solid #555}</style><div id="resume" style="width:650px;transform:translateY(0px)">`)
			for index := 0; index < 58; index++ {
				_, _ = fmt.Fprintf(w, "<p>仅本地合成经历段落 %d，验证真实滚轮与长截图完整覆盖。</p>", index)
			}
			_, _ = fmt.Fprint(w, `</div>`)
		default:
			http.NotFound(w, r)
		}
	}))
}

// syntheticIdentityCard 生成姓名、基础信息和经历都可核对的合成卡片，不包含真实账号资料。
func syntheticIdentityCard(id int) string {
	return fmt.Sprintf(`<div class="recommend-item__inner recommend-resume-item__inner"><span class="talent-basic-info__name--inner">合成人选%d</span><div class="talent-basic-info__basic">30岁 5年经验</div><div class="resume-item__content resume-card-exp">合成学校%d 本科</div></div>`, id, id)
}
