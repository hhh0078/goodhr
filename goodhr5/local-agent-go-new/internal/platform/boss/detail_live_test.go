// Package boss_test 文件作用：在明确指定的已登录BOSS真页验证正式卡片点击、详情正文与关闭，不调用招呼、输入或资料索要。
package boss_test

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestBossDetailExistingPageLive 只复用已运行的本机Worker，在当前BOSS页面打开一位候选人的图片详情并用正式能力分段截图，随后确认弹窗关闭。
func TestBossDetailExistingPageLive(t *testing.T) {
	base := os.Getenv("GOODHR_BOSS_DETAIL_LIVE_WORKER_URL")
	if base == "" {
		t.Skip("需用户登录BOSS且任务停止后显式指定当前Worker")
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		t.Fatal("只允许明确指定本机Worker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	browser := &detailRecordingBrowser{Client: client.New(base)}
	cfg, err := platform.LoadConfig("boss")
	if err != nil {
		t.Fatal(err)
	}
	pages, err := browser.ListPages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	current := ""
	for _, page := range pages.Pages {
		if page.Current {
			current = page.URL
		}
	}
	if current != cfg.EntryURL {
		t.Fatal("当前不在已配置BOSS推荐页，不操作")
	}
	shown, err := common.ProbeSelectorExists(ctx, browser, cfg, "candidate.detail_dialog")
	if err != nil || shown {
		t.Fatalf("已有详情弹窗或状态未知，不操作：%v", err)
	}
	runtime, err := platform.RuntimeFor("boss")
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxItems = 3
	rows, err := runtime.FindCandidates(ctx, browser, cfg, model.CandidateListOptions{ReadConcurrency: 5})
	if err != nil || len(rows) == 0 {
		t.Fatalf("当前名单未读到：%v", err)
	}
	var candidate model.Candidate
	found := false
	for _, row := range rows {
		if row.SkipReason == "" {
			candidate, found = row, true
			break
		}
	}
	if !found {
		t.Fatal("首批均为推广卡片，本次不点击")
	}
	if err := runtime.ScrollToCandidate(ctx, browser, cfg, candidate); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := runtime.CloseCandidateDetail(cleanupCtx, browser, cfg, candidate); err != nil {
			t.Errorf("关闭测试详情失败：%v", err)
		}
	})
	if err := runtime.OpenCandidateDetail(ctx, browser, cfg, candidate); err != nil {
		t.Fatal(err)
	}
	openClicks := len(browser.clicks)
	if openClicks < 1 || openClicks > len(cfg.Selectors["candidate.open_target"].Variants()) {
		t.Fatal("详情点击次数不符合配置路径上限")
	}
	// BOSS正式AI详情走截图，图片简历没有DOM正文，不能用ExtractCandidateDetail判定打开失败。
	directory := os.Getenv("GOODHR_BOSS_DETAIL_LIVE_PROOF_DIR")
	if directory == "" {
		directory = t.TempDir()
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	target := cfg.Selectors["candidate.detail_screenshot"]
	var anchor *contract.SelectorSpec
	if spec, ok := cfg.Selectors["candidate.detail_scroll"]; ok {
		anchor = &spec
	}
	images, err := browser.LongScreenshot(ctx, contract.LongScreenshotRequest{Target: target, WheelAnchor: anchor, Directory: directory, Filename: "boss-detail-live.png", Distance: 520, MaxParts: 20, WaitMS: 300})
	if err != nil || len(images.Parts) == 0 {
		t.Fatalf("正式图片详情截图未完成：%v", err)
	}
	for _, part := range images.Parts {
		info, err := os.Stat(part.Path)
		if err != nil || info.Size() <= 0 {
			t.Fatal("详情截图分段未保存完整")
		}
	}
	if err := runtime.CloseCandidateDetail(ctx, browser, cfg, candidate); err != nil {
		t.Fatal(err)
	}
	shown, err = common.ProbeSelectorExists(ctx, browser, cfg, "candidate.detail_dialog")
	if err != nil || shown {
		t.Fatalf("详情未确认关闭：%v", err)
	}
	t.Logf("BOSS真页通过：打开请求%d次，正式图片详情截图%d段；Escape%d次，备用关闭点击%d次；弹窗已消失，未发招呼、未输入、未改筛选", openClicks, len(images.Parts), browser.escapes, len(browser.clicks)-openClicks)
}
