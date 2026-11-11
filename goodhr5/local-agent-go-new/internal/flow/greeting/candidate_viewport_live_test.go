// Package platform_test 文件作用：在显式授权的BOSS、智联推荐或无忧推荐、搜索真页验证完整启动后的首张完整卡片起点；不发送消息、不写用户进度，结束后还原原活动标签。
package greeting

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// recommendationViewportBrowser 拒绝启动阶段所有交互，证明没有向前或向后滚动找旧候选人。
type recommendationViewportBrowser struct{ *client.Client }

// Scroll 拒绝启动滚轮。
func (*recommendationViewportBrowser) Scroll(context.Context, contract.ScrollRequest) (contract.ScrollResult, error) {
	return contract.ScrollResult{}, fmt.Errorf("起跑只读验收不允许滚轮")
}

// Click 拒绝发送或其他点击。
func (*recommendationViewportBrowser) Click(context.Context, contract.ElementClickRequest) (contract.ClickResult, error) {
	return contract.ClickResult{}, fmt.Errorf("起跑只读验收不允许点击")
}

// Input 拒绝修改筛选或草稿。
func (*recommendationViewportBrowser) Input(context.Context, contract.ElementInputRequest) (contract.InputResult, error) {
	return contract.InputResult{}, fmt.Errorf("起跑只读验收不允许输入")
}

// PressKey 拒绝按键以免意外发送。
func (*recommendationViewportBrowser) PressKey(context.Context, contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	return contract.KeyboardPressResult{}, fmt.Errorf("起跑只读验收不允许按键")
}

// TestRecommendationViewportExistingWorkerLive 复用或新开对应页面，经正式打开、初始化、选岗、筛选和统一起点后两次重建运行时，核对完整卡片起点一致且无启动交互。
func TestRecommendationViewportExistingWorkerLive(t *testing.T) {
	id, base := os.Getenv("GOODHR_VIEWPORT_LIVE_PLATFORM"), os.Getenv("GOODHR_VIEWPORT_LIVE_WORKER_URL")
	if id == "" || base == "" {
		t.Skip("需已登录目标平台、停止任务，并显式指定现有本机Worker")
	}
	if id != "zhaopin" && id != "51job" && id != "boss" {
		t.Fatal("本轮只验收BOSS、智联或无忧")
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		t.Fatal("仅允许本机Worker")
	}
	cfg, err := platform.LoadConfig(id)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxItems = 3
	cfg.CandidateBatchSize = 3
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	raw := client.New(base)
	pages, err := raw.ListPages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	target := ""
	targetURL := cfg.EntryURL
	search := os.Getenv("GOODHR_VIEWPORT_LIVE_SEARCH") == "1"
	if search {
		targetURL = cfg.ManualSearchURL
		if targetURL == "" {
			t.Fatal("无搜索入口")
		}
	}
	for _, page := range pages.Pages {
		if page.Current {
			defer func(pageID string) {
				c, stop := context.WithTimeout(context.Background(), 5*time.Second)
				defer stop()
				_, _ = raw.UsePage(c, contract.PageUseRequest{PageID: pageID})
			}(page.PageID)
		}
		if common.PageURLMatches(page.URL, targetURL) {
			target = page.PageID
		}
	}
	if target != "" {
		_, err = raw.UsePage(ctx, contract.PageUseRequest{PageID: target})
	} else {
		yes := true
		_, err = raw.OpenPage(ctx, contract.PageOpenRequest{URL: targetURL, NewTab: &yes, WaitUntil: "domcontentloaded", TimeoutMS: 20000})
	}
	if err != nil {
		t.Fatal(err)
	}
	if search && os.Getenv("GOODHR_VIEWPORT_LIVE_SUBMIT_SEARCH") == "1" {
		if err := common.ClickRequired(ctx, raw, cfg, "search.submit"); err != nil {
			t.Fatal("布置现有筛选搜索结果失败", err)
		}
	}

	setup, _ := platform.RuntimeFor(id)
	if err := setup.InitializeGreetingPage(ctx, raw, cfg); err != nil {
		t.Fatal(err)
	}
	positionName, found, err := common.ReadOptional(ctx, raw, cfg, "position.current")
	if err != nil || !found || positionName == "" {
		t.Fatal("无法核实当前岗位", err)
	}
	b := &recommendationViewportBrowser{raw}
	var first model.Candidate
	for round := 0; round < 2; round++ {
		runtime, err := platform.RuntimeFor(id)
		if err != nil {
			t.Fatal(err)
		}

		if round == 0 {
			if value := os.Getenv("GOODHR_VIEWPORT_LIVE_PREPARE_INDEX"); value != "" {
				index, err := strconv.Atoi(value)
				if err != nil || index < 1 || index > 20 {
					t.Fatal("测试布置序号仅允许1至20")
				}
				if err := common.ScrollToCandidate(ctx, raw, cfg, model.Candidate{Index: index}); err != nil {
					t.Fatal("准备中部位置失败", err)
				}
			}
		}
		if directory := os.Getenv("GOODHR_VIEWPORT_LIVE_SCREENSHOT_DIR"); directory != "" {
			if _, err := raw.Screenshot(ctx, contract.ScreenshotRequest{Directory: directory, Filename: id + "-" + strconv.FormatBool(search) + "-viewport.png"}); err != nil {
				t.Fatal(err)
			}
		}
		expected := &model.CandidateListCursor{}
		if err := common.StartCandidateViewport(ctx, b, cfg, expected); err != nil {
			t.Fatal(err)
		}
		// 按正式任务顺序覆盖打开、初始化、选岗、筛选、统一起点、首次读取。
		if err := runtime.OpenGreetingPage(ctx, b, cfg); err != nil {
			t.Fatal(err)
		}
		if err := runtime.InitializeGreetingPage(ctx, b, cfg); err != nil {
			t.Fatal(err)
		}
		position := model.Position{Name: positionName}
		if err := runtime.SelectPosition(ctx, b, cfg, position); err != nil {
			t.Fatal("同岗启动不应重选", err)
		}
		if err := runtime.ApplyBasicFilters(ctx, b, cfg, position); err != nil {
			t.Fatal(err)
		}
		cursor := &model.CandidateListCursor{}
		if _, err := (&Flow{}).prepareCandidateResume(ctx, b, shared.PreparedTask{Platform: cfg}, runtime, cursor); err != nil {
			t.Fatal(err)
		}
		if cursor.StartIndex != expected.StartIndex {
			t.Fatalf("启动改变首卡位置：%d => %d", expected.StartIndex, cursor.StartIndex)
		}

		rows, err := runtime.FindCandidates(ctx, b, cfg, model.CandidateListOptions{Cursor: cursor, ReadConcurrency: 5})
		if err != nil || len(rows) == 0 || rows[0].Index != cursor.StartIndex {
			t.Fatal("起点读取失败", err)
		}
		view, err := common.CandidateViewport(ctx, b, cfg, rows[0].Index)
		if err != nil || !common.CandidateFullyVisible(view, cfg) {
			t.Fatal("首张卡片没有完整显示", err)
		}
		if round == 0 {
			first = rows[0]
		} else if rows[0].Index != first.Index || rows[0].Fingerprint != first.Fingerprint {
			t.Fatal("重开后起点不一致")
		}
		if rows[0].Index > 0 {
			before, err := common.CandidateViewport(ctx, b, cfg, rows[0].Index-1)
			if err != nil || common.CandidateFullyVisible(before, cfg) {
				t.Fatal("跳过了更靠前的完整卡片", err)
			}
		}
		t.Logf("%s完整启动链真页第%d次起跑：原始行号%d，读取%d人；完整卡片y=%.1f height=%.1f，视口高%.1f，顶部/底部避让%d/%d；无启动滚轮或发送", id, round+1, cursor.StartIndex, len(rows), view.Box.Y, view.Box.Height, view.Viewport.Height, cfg.Behavior.CandidateViewportTopInset, cfg.Behavior.CandidateViewportBottomInset)
	}
}
