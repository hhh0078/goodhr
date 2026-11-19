// Package zhaopin_test 文件作用：在明确指定的现有Worker中新建本地测试页，回归经历范围污染，结束后还原原标签页。
package zhaopin_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestZhaopinExperienceExistingWorker 只在主动指定且岗位已停止时使用现有浏览器，不启动第二个Worker，不操作招聘网页。
func TestZhaopinExperienceExistingWorker(t *testing.T) {
	endpoint := os.Getenv("GOODHR_ZHAOPIN_EXPERIENCE_TEST_WORKER_URL")
	if endpoint == "" {
		t.Skip("需显式指定已有Worker，并先确认岗位已停止")
	}
	if endpoint != "http://127.0.0.1:39881" {
		t.Fatal("只允许本机已有Worker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	browser := client.New(endpoint)
	pages, err := browser.ListPages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	original := ""
	for _, page := range pages.Pages {
		if page.Current {
			original = page.PageID
		}
	}
	if original == "" {
		t.Fatal("未确认原标签页，停止测试")
	}
	server := httptest.NewServer(http.HandlerFunc(experienceFixture))
	defer server.Close()
	newTab := true
	if _, err = browser.OpenPage(ctx, contract.PageOpenRequest{URL: server.URL + "/?mode=flat&noise=1", NewTab: &newTab}); err != nil {
		t.Fatal(err)
	}
	testPages, err := browser.ListPages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	testID := ""
	for _, page := range testPages.Pages {
		if page.Current {
			testID = page.PageID
		}
	}
	if testID == "" || testID == original {
		t.Fatal("没有独立测试标签页")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if _, useErr := browser.UsePage(cleanup, contract.PageUseRequest{PageID: testID}); useErr == nil {
			_ = browser.ClosePage(cleanup)
		}
		if _, restoreErr := browser.UsePage(cleanup, contract.PageUseRequest{PageID: original}); restoreErr != nil {
			t.Error("恢复原标签页失败", restoreErr)
		}
	}()
	cfg, err := platform.LoadConfig("zhaopin")
	if err != nil {
		t.Fatal(err)
	}
	old := cfg
	old.CandidateFields = make(map[string]contract.SelectorSpec, len(cfg.CandidateFields))
	for key, value := range cfg.CandidateFields {
		old.CandidateFields[key] = value
	}
	field := old.CandidateFields["education"]
	field.Target.Selectors = []contract.SelectorCandidate{{Type: "css", Value: ".resume-item__content.resume-card-exp"}}
	old.CandidateFields["education"] = field
	options := model.CandidateListOptions{Cursor: &model.CandidateListCursor{IdentityByIndex: true}}
	before, err := common.FindCandidates(ctx, browser, old, "zhaopin", options)
	if err != nil || len(before) != 6 {
		t.Fatalf("旧配置读卡失败 count=%d err=%v", len(before), err)
	}
	err = common.ScrollToCandidate(ctx, browser, old, before[0])
	var workerErr *contract.WorkerError
	if !errors.As(err, &workerErr) || workerErr.Body.Code != "ELEMENT_NOT_FOUND" {
		t.Fatalf("未复现6.3.4.8现场错误: %v", err)
	}
	for _, mode := range []string{"flat", "block", "flex"} {
		t.Run(mode, func(t *testing.T) {
			if _, err = browser.OpenPage(ctx, contract.PageOpenRequest{URL: server.URL + "/?mode=" + mode + "&noise=1"}); err != nil {
				t.Fatal(err)
			}
			candidates, readErr := common.FindCandidates(ctx, browser, cfg, "zhaopin", options)
			if readErr != nil || len(candidates) != 6 {
				t.Fatalf("新配置读卡失败 count=%d err=%v", len(candidates), readErr)
			}
			candidate := candidates[0]
			if strings.Contains(candidate.Fields["education"], "打招呼") || strings.Contains(candidate.Fields["education"], "隐藏提示") {
				t.Fatal("经历仍混入界面文字")
			}
			if err = common.ScrollToCandidate(ctx, browser, cfg, candidate); err != nil {
				t.Fatal(err)
			}
			if _, err = browser.OpenPage(ctx, contract.PageOpenRequest{URL: server.URL + "/?mode=" + mode + "&noise=1&rotate=1&other_school=1"}); err != nil {
				t.Fatal(err)
			}
			if err = common.ScrollToCandidate(ctx, browser, cfg, candidate); err != nil {
				t.Fatal("换位或同名不同经历定位失败", err)
			}
			if _, err = browser.OpenPage(ctx, contract.PageOpenRequest{URL: server.URL + "/?mode=" + mode + "&noise=1&duplicate=1"}); err != nil {
				t.Fatal(err)
			}
			err = common.ScrollToCandidate(ctx, browser, cfg, candidate)
			if !errors.As(err, &workerErr) || workerErr.Body.Code != "ELEMENT_AMBIGUOUS" {
				t.Fatalf("重复身份没有拒绝操作: %v", err)
			}
		})
	}
	if _, err = browser.OpenPage(ctx, contract.PageOpenRequest{URL: server.URL + "/search"}); err != nil {
		t.Fatal(err)
	}
	for _, config := range []model.Config{old, cfg} {
		items, readErr := common.FindCandidates(ctx, browser, config, "zhaopin", model.CandidateListOptions{})
		if readErr != nil || len(items) != 1 || items[0].Fields["education"] != "示例学校 本科" {
			t.Fatalf("搜索页原字段读取变化: count=%d err=%v", len(items), readErr)
		}
	}
	t.Log("旧6.3.4.8失败对照通过；新配置三种排版、换位、同名区分、重复拒绝及搜索兼容通过；未发送消息")
}

// experienceFixture 提供推荐页现场结构和保持旧字段结构的搜索页，全部内容均为合成资料。
func experienceFixture(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/search" {
		layoutFixture(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html;charset=utf-8")
	_, _ = fmt.Fprint(w, `<!doctype html><meta charset="utf-8"><div class="search-resume-list"><div class="search-resume-item__inner"><span class="talent-basic-info__name--inner">合成人选</span><div class="talent-basic-info__basic">30岁 5年经验</div><div class="resume-item__content resume-card-exp">示例学校 本科</div></div></div>`)
}
