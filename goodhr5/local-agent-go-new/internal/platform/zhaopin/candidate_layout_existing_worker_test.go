// Package zhaopin_test 文件作用：通过唯一空闲Worker与无脚本合成页回归分块排版、名单换位和身份歧义，不访问招聘账号。
package zhaopin_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestZhaopinLayoutWhitespaceExistingWorker 仅借用明确空闲的隔离Worker，对照旧失败并检查同一个人的真实DOM定位。
func TestZhaopinLayoutWhitespaceExistingWorker(t *testing.T) {
	endpoint := os.Getenv("GOODHR_REGRESSION_TEST_WORKER_URL")
	if endpoint == "" {
		t.Skip("需显式指定空闲隔离Worker")
	}
	if endpoint != "http://127.0.0.1:39881" {
		t.Fatal("只允许明确管理的隔离Worker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	browser := client.New(endpoint)
	status, err := browser.BrowserStatus(ctx)
	if err != nil || status.Running {
		t.Fatal("不接管用户浏览器")
	}
	server := httptest.NewServer(http.HandlerFunc(layoutFixture))
	defer server.Close()
	dir := os.Getenv("GOODHR_REGRESSION_EVIDENCE")
	if dir == "" {
		dir = t.TempDir()
	}
	headless := true
	if _, err = browser.StartBrowser(ctx, contract.BrowserStartRequest{Headless: &headless, URL: server.URL, UserDataDir: filepath.Join(dir, "fixture-profile")}); err != nil {
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
	for _, mode := range []string{"flat", "block", "flex"} {
		t.Run(mode, func(t *testing.T) {
			if _, err = browser.OpenPage(ctx, contract.PageOpenRequest{URL: server.URL + "/?mode=" + mode}); err != nil {
				t.Fatal(err)
			}
			candidates, readErr := common.FindCandidates(ctx, browser, cfg, "zhaopin", model.CandidateListOptions{Cursor: &model.CandidateListCursor{IdentityByIndex: true}})
			if readErr != nil || len(candidates) != 6 {
				t.Fatalf("读卡片失败 count=%d err=%v", len(candidates), readErr)
			}
			candidate := candidates[0]
			broken, selectErr := common.CandidateSelector(cfg, candidate)
			if selectErr != nil {
				t.Fatal(selectErr)
			}
			broken = broken.MapVariants(func(v contract.SelectorSpec) contract.SelectorSpec { v.Target.TextLayoutWhitespace = false; return v })
			_, oldErr := browser.Find(ctx, contract.ElementFindRequest{Selector: broken})
			var workerErr *contract.WorkerError
			if mode == "flat" {
				if oldErr != nil {
					t.Fatal(oldErr)
				}
			} else if !errors.As(oldErr, &workerErr) || workerErr.Body.Code != "ELEMENT_NOT_FOUND" {
				t.Fatalf("未复现6.3.4.7排版问题: %v", oldErr)
			}
			if err = common.ScrollToCandidate(ctx, browser, cfg, candidate); err != nil {
				t.Fatalf("新定位未兼容排版: %v", err)
			}
			if _, err = browser.OpenPage(ctx, contract.PageOpenRequest{URL: server.URL + "/?mode=" + mode + "&rotate=1&other_school=1"}); err != nil {
				t.Fatal(err)
			}
			if err = common.ScrollToCandidate(ctx, browser, cfg, candidate); err != nil {
				t.Fatalf("名单换位或同名不同经历失败: %v", err)
			}
			if _, err = browser.OpenPage(ctx, contract.PageOpenRequest{URL: server.URL + "/?mode=" + mode + "&duplicate=1"}); err != nil {
				t.Fatal(err)
			}
			err = common.ScrollToCandidate(ctx, browser, cfg, candidate)
			if !errors.As(err, &workerErr) || workerErr.Body.Code != "ELEMENT_AMBIGUOUS" {
				t.Fatalf("完全相同身份未停止: %v", err)
			}
			t.Log("旧版排版失败已对照；新定位兼容排版和换位；同名不同学校区分；完全重复拒绝；未发送消息")
		})
	}
}

// layoutFixture 提供真实块布局与flex布局，身份字段不写入节点间空格，以覆盖innerText与节点文字的差异。
func layoutFixture(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html;charset=utf-8")
	mode := r.URL.Query().Get("mode")
	style := ""
	if mode == "flex" {
		style = ".talent-basic-info__basic,.resume-item__experience{display:flex;gap:12px}"
	}
	_, _ = fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><style>body{margin:0;padding-top:130px}.recommend-item__inner{height:130px;border:1px solid #999}%s</style><div class="recommend-list"><div role="group">`, style)
	ids := []int{0, 1, 2, 3, 4, 5}
	if r.URL.Query().Get("rotate") == "1" {
		ids = []int{1, 2, 3, 4, 5, 0}
	}
	if r.URL.Query().Get("duplicate") == "1" {
		ids = append(ids, 0)
	}
	if r.URL.Query().Get("other_school") == "1" {
		ids = append(ids, 6)
	}
	for _, id := range ids {
		person := id
		school := fmt.Sprintf("示例学校%d[A]+", id)
		if id == 6 {
			person = 0
		}
		basic, education := "30岁 5年经验", school+" 本科"
		if mode != "flat" {
			basic = "<div>30岁</div><div>5年经验</div>"
			education = "<div>" + school + "</div><div>本科</div>"
		}
		_, _ = fmt.Fprintf(w, `<div class="recommend-item__inner recommend-resume-item__inner"><div class="talent-basic-info__name--inner">合成人选%d</div><div class="talent-basic-info__basic">%s</div><div class="resume-item__content resume-card-exp"><div class="resume-item__experience">%s</div>%s</div></div>`, person, basic, education, layoutNoise(r))
	}
	_, _ = fmt.Fprint(w, "</div></div>")
}

// layoutNoise 模拟现场经历外层混入的隐藏提示和响应式重复按钮，避免测试只覆盖纯正文。
func layoutNoise(r *http.Request) string {
	if r.URL.Query().Get("noise") != "1" {
		return ""
	}
	return `<span style="display:none">隐藏提示</span><button>打招呼</button><button style="display:none">打招呼</button>`
}
