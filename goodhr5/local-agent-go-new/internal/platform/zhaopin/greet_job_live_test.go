// Package zhaopin 文件作用：在用户明确授权的现有智联选岗弹窗上验证正式选岗与一次招呼，默认跳过，不自动联系其他候选人。
package zhaopin

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// liveGreetingRecorder 只记录正式封装点击的结果，不重新发送任何失败或不确定的动作。
type liveGreetingRecorder struct {
	*client.Client
	clicks  []contract.ElementClickRequest
	results []contract.ClickResult
	inputs  int
}

// TestGreetingCurrentPageInspectLive 仅在显式启用时读取当前选岗弹框及截图，核对真实目标后才允许另外的发送测试。
func TestGreetingCurrentPageInspectLive(t *testing.T) {
	if os.Getenv("GOODHR_ZHAOPIN_LIVE_INSPECT") != "1" {
		t.Skip("只在用户授权实操且指定现有本机Worker时启用")
	}
	base := os.Getenv("GOODHR_ZHAOPIN_LIVE_WORKER_URL")
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		t.Fatal("必须指定本机现有Worker")
	}
	source, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg model.Config
	if err := json.Unmarshal(source, &cfg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	browser := client.New(base)
	if pageID := os.Getenv("GOODHR_ZHAOPIN_LIVE_PAGE_ID"); pageID != "" {
		if _, err := browser.UsePage(ctx, contract.PageUseRequest{PageID: pageID}); err != nil {
			t.Fatal(err)
		}
	}
	search, err := common.CurrentPageUsesManualSearch(ctx, browser, cfg)
	if err != nil || !search {
		t.Fatal("当前不是智联搜索页，不操作", err)
	}
	if dir := os.Getenv("GOODHR_ZHAOPIN_LIVE_SCREENSHOTS"); dir != "" {
		if _, err := browser.Screenshot(ctx, contract.ScreenshotRequest{Directory: dir, Filename: "zhaopin-inspect.png"}); err != nil {
			t.Fatal(err)
		}
	}
	opened, err := common.ProbeSelectorExists(ctx, browser, cfg, greetingJobDialog)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("选岗弹框可见：%v", opened)
	if opened {
		selected, found, err := common.ReadOptional(ctx, browser, cfg, "candidate.greet_job_selected")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("完整已选岗位：%q，找到：%v", selected, found)
	}
}

// Click 保留是否实际按下的成功回执，供验收统计选岗、确定和招呼语的次数。
func (b *liveGreetingRecorder) Click(ctx context.Context, req contract.ElementClickRequest) (contract.ClickResult, error) {
	result, err := b.Client.Click(ctx, req)
	b.clicks = append(b.clicks, req)
	b.results = append(b.results, result)
	return result, err
}

// Input 记录正式输入次数，验证同岗快路径没有多余搜索。
func (b *liveGreetingRecorder) Input(ctx context.Context, req contract.ElementInputRequest) (contract.InputResult, error) {
	b.inputs++
	return b.Client.Input(ctx, req)
}

// TestGreetingCurrentDialogLive 只处理已打开且候选人身份与授权参数一致的选岗弹窗，确认前核对岗位，发送后检查同一卡片。
func TestGreetingCurrentDialogLive(t *testing.T) {
	if os.Getenv("GOODHR_ZHAOPIN_LIVE_GREET") != "1" {
		t.Skip("仅用户已明确授权真实打招呼并给定现有弹窗候选人后启用")
	}
	base := os.Getenv("GOODHR_ZHAOPIN_LIVE_WORKER_URL")
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		t.Fatal("必须指定本机现有Worker")
	}
	name := os.Getenv("GOODHR_ZHAOPIN_LIVE_CANDIDATE")
	position := os.Getenv("GOODHR_ZHAOPIN_LIVE_POSITION")
	if name == "" || position == "" {
		t.Fatal("必须提供已授权弹窗中的候选人与岗位")
	}
	source, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg model.Config
	if err := json.Unmarshal(source, &cfg); err != nil {
		t.Fatal(err)
	}
	browser := &liveGreetingRecorder{Client: client.New(base)}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	search, err := common.CurrentPageUsesManualSearch(ctx, browser, cfg)
	if err != nil || !search {
		t.Fatalf("没有停在智联搜索页：%v", err)
	}
	dialog, err := browser.Read(ctx, contract.ElementReadRequest{Selector: cfg.Selectors[greetingJobDialog], Property: "html"})
	if err != nil || !strings.Contains(dialog.Value, name) {
		t.Fatal("现有弹窗候选人不符合授权，不点击或发送")
	}
	cfg.CandidateBatchSize = 20
	candidates, err := NewRuntime().FindCandidates(ctx, browser, cfg, model.CandidateListOptions{ReadConcurrency: 5})
	if err != nil {
		t.Fatalf("列表身份没有核对上：%v", err)
	}
	var candidate model.Candidate
	matches := 0
	for _, row := range candidates {
		if row.Name == name {
			candidate = row
			matches++
		}
	}
	if matches != 1 {
		t.Fatal("弹框候选人在当前名单中无法唯一确认，不发送")
	}
	inputProbe := cfg.Selectors["candidate.greet_job_input"]
	inputProbe.TimeoutMS = 1
	expanded, err := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: inputProbe, MaxItems: 1, ExpectedMissing: true, ProbeOnly: true})
	if err != nil && !common.IsElementMissing(err) {
		t.Fatal(err)
	}
	if len(expanded) > 0 {
		if _, err := browser.PressKey(ctx, contract.KeyboardPressRequest{Key: "Escape", DelayMS: 80}); err != nil {
			t.Fatal(err)
		}
	}
	if exists, err := common.ProbeSelectorExists(ctx, browser, cfg, greetingJobDialog); err != nil || !exists {
		t.Fatal("选岗弹窗已变化，不继续")
	}
	selected, found, err := common.ReadOptional(ctx, browser, cfg, "candidate.greet_job_selected")
	if err != nil {
		t.Fatal(err)
	}
	expectedChosen := 1
	if found && selected == position {
		expectedChosen = 0
	}
	if err := selectSearchGreetingJob(ctx, browser, cfg, position); err != nil {
		t.Fatal(err)
	}
	sentPhrase := false
	verified := false
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		phrase, err := common.ProbeSelectorExists(ctx, browser, cfg, "candidate.greet_dialog")
		if err != nil {
			t.Fatal(err)
		}
		if phrase && !sentPhrase {
			if err := common.ClickRequired(ctx, browser, cfg, "candidate.greet_send"); err != nil {
				t.Fatal(err)
			}
			sentPhrase = true
		}
		contacted, err := common.CandidateActionExists(ctx, browser, cfg, candidate, "candidate.continue")
		if err != nil {
			t.Fatal(err)
		}
		if contacted && !phrase {
			verified = true
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if !verified {
		t.Fatal("尚未看到同一卡片变为继续沟通，不重发")
	}
	chosen, confirmed, phraseSent := 0, 0, 0
	for i, req := range browser.clicks {
		if !browser.results[i].Clicked {
			continue
		}
		switch req.Selector.Description {
		case cfg.Selectors["candidate.greet_job_name"].Description:
			chosen++
		case cfg.Selectors["candidate.greet_job_confirm"].Description:
			confirmed++
		case cfg.Selectors["candidate.greet_send"].Description:
			phraseSent++
		}
	}
	if chosen != expectedChosen || confirmed != 1 || phraseSent > 1 || (expectedChosen == 0 && browser.inputs != 0) {
		t.Fatalf("点击次数不正确：选岗%d 确定%d 招呼语%d", chosen, confirmed, phraseSent)
	}
	if dir := os.Getenv("GOODHR_ZHAOPIN_LIVE_SCREENSHOTS"); dir != "" {
		if _, err := browser.Screenshot(ctx, contract.ScreenshotRequest{Directory: dir, Filename: "zhaopin-after.png"}); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("真实页面通过：完整岗位选中%d次、确定%d次、招呼语%d次；已确认当前候选人显示继续沟通，没有联系其他候选人", chosen, confirmed, phraseSent)
}
