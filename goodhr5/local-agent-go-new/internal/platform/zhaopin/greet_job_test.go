// Package zhaopin_test 文件作用：验证岗位搜索延迟加载、完整title匹配、未按下时重新定位，以及不确定点击绝不重复。
package zhaopin_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/platform/zhaopin"
)

// delayedGreetingJobBrowser 在旧结果出现后再显示加载，模拟日志中的偶发刷新，不发送真实消息。
type delayedGreetingJobBrowser struct {
	greetingPageBrowser
	loadingChecks   int
	lastLoading     time.Time
	clickAttempts   int
	firstClickError error
}

// FindAll 模拟延迟两次探测才出现的骨架，并验证完整岗位按title属性匹配。
func (b *delayedGreetingJobBrowser) FindAll(ctx context.Context, req contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if req.Selector.Description == "职位加载骨架" {
		b.loadingChecks++
		if b.loadingChecks == 3 || b.loadingChecks == 4 {
			b.lastLoading = time.Now()
			return []contract.FindAllItem{{}}, nil
		}
		return nil, nil
	}
	if req.Selector.Description == "candidate.greet_job_name" {
		if req.Selector.Target.Attributes["title"] != greetingJobName || req.Selector.Target.Text != "" {
			b.clickAttempts = -100
			return nil, nil
		}
		exact := true
		req.Selector.Target.Text = greetingJobName
		req.Selector.Target.ExactText = &exact
	}
	return b.greetingPageBrowser.FindAll(ctx, req)
}

// Click 只允许骨架消失并连续稳定后点击；第一回执可模拟明确未按下或结果不确定。
func (b *delayedGreetingJobBrowser) Click(ctx context.Context, req contract.ElementClickRequest) (contract.ClickResult, error) {
	if req.Selector.Description == "candidate.greet_job_name" {
		b.clickAttempts++
		if time.Since(b.lastLoading) < 900*time.Millisecond {
			return contract.ClickResult{}, context.DeadlineExceeded
		}
		if b.clickAttempts == 1 && b.firstClickError != nil {
			return contract.ClickResult{}, b.firstClickError
		}
		exact := true
		req.Selector.Target.Text = req.Selector.Target.Attributes["title"]
		req.Selector.Target.ExactText = &exact
	}
	return b.greetingPageBrowser.Click(ctx, req)
}

// TestGreetingJobWaitsForDelayedLoading 验证真实延迟加载模型及只在明确未按下时允许一次重新定位。
func TestGreetingJobWaitsForDelayedLoading(t *testing.T) {
	for _, kind := range []string{"加载后成功", "未按下后重定位", "点击不确定不重试"} {
		t.Run(kind, func(t *testing.T) {
			cfg := loadGreetingFixture(t)
			primary := cfg.Selectors["candidate.greet_job_name"]
			primary.ReadAttribute = "title"
			cfg.Selectors["candidate.greet_job_name"] = primary
			cfg.Selectors["candidate.greet_job_loading"] = contract.SelectorSpec{Description: "职位加载骨架"}
			browser := &delayedGreetingJobBrowser{greetingPageBrowser: greetingPageBrowser{pageURL: cfg.ManualSearchURL, opensJobDialog: true, names: []string{greetingJobName}, canConfirmGreeting: true}}
			if kind != "加载后成功" {
				details := `{"click_started":false}`
				if kind == "点击不确定不重试" {
					details = `{"click_started":true}`
				}
				browser.firstClickError = &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "CLICK_FAILED", Details: json.RawMessage(details)}}
			}
			runtime := zhaopin.NewRuntime()
			if err := runtime.SelectPosition(context.Background(), browser, cfg, model.Position{Name: greetingJobName}); err != nil {
				t.Fatal(err)
			}
			err := runtime.GreetCandidate(context.Background(), browser, cfg, model.Candidate{IdentityTexts: []string{"合成候选人"}}, model.GreetRequest{})
			expected := 1
			if kind == "未按下后重定位" {
				expected = 2
			}
			if (err != nil) != (kind == "点击不确定不重试") || browser.clickAttempts != expected || browser.loadingChecks < 5 {
				t.Fatalf("加载或重复控制错误：%v 点击%d 加载探测%d", err, browser.clickAttempts, browser.loadingChecks)
			}
			if kind == "点击不确定不重试" && browser.selected != "" {
				t.Fatal("不确定结果被重复操作")
			}
		})
	}
}
