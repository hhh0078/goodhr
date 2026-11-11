// Package zhaopin 文件作用：在智联沟通职位搜索完成后按完整名称选岗，处理加载竞态与显示省略号，不重发招呼。
package zhaopin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// selectSearchGreetingJob 等待搜索加载结束并确认唯一完整岗位，再选择、回读及确认；明确未按下时仅重新定位一次。
func selectSearchGreetingJob(ctx context.Context, browser model.Browser, cfg model.Config, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("GoodHR 岗位名称为空，请先检查岗位设置")
	}
	if present, err := common.ProbeSelectorExists(ctx, browser, cfg, "candidate.greet_job_selected"); err != nil {
		return fmt.Errorf("检查已选沟通职位失败：%w", err)
	} else if present {
		// 选择器优先读取完整title，旧页面读取完整文本；省略文字不能按前缀放行。
		selected, found, err := common.ReadOptional(ctx, browser, cfg, "candidate.greet_job_selected")
		if err != nil {
			return fmt.Errorf("核对已选沟通职位失败：%w", err)
		}
		if found && selected == name {
			return common.ClickRequired(ctx, browser, cfg, "candidate.greet_job_confirm")
		}
	}
	if err := common.ClickRequired(ctx, browser, cfg, "candidate.greet_job_open"); err != nil {
		return fmt.Errorf("打开沟通职位列表失败：%w", err)
	}
	if err := common.InputRequired(ctx, browser, cfg, "candidate.greet_job_input", common.PositionSearchQuery(name)); err != nil {
		return fmt.Errorf("搜索沟通职位失败：%w", err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		selector, err := waitSearchGreetingJob(ctx, browser, cfg, name)
		if err != nil {
			return err
		}
		_, err = browser.Click(ctx, contract.ElementClickRequest{Selector: selector, Diagnostic: true, DiagnosticAttempt: attempt})
		if err == nil {
			break
		}
		if attempt == 2 || !common.ClickNotStarted(err) {
			return fmt.Errorf("选择沟通职位失败：%w", err)
		}
	}
	// 岗位回显也可能异步更新；只等正确结果，不再次点击职位或提交。
	verifyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		selected, found, err := common.ReadOptional(verifyCtx, browser, cfg, "candidate.greet_job_selected")
		if err != nil {
			return fmt.Errorf("核对已选沟通职位失败：%w", err)
		}
		if found && selected == name {
			return common.ClickRequired(ctx, browser, cfg, "candidate.greet_job_confirm")
		}
		select {
		case <-verifyCtx.Done():
			return fmt.Errorf("没确认智联选中了“%s”，请检查弹框中的沟通职位：%w", name, verifyCtx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// waitSearchGreetingJob 只读等待骨架消失且唯一目标连续稳定900ms，覆盖输入完成后才开始的延迟搜索加载。
func waitSearchGreetingJob(ctx context.Context, browser model.Browser, cfg model.Config, name string) (contract.SelectorSpec, error) {
	waitCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	stableSince := time.Time{}
	lastVariant := -1
	for {
		loading, err := searchGreetingJobLoading(waitCtx, browser, cfg)
		if err != nil {
			return contract.SelectorSpec{}, err
		}
		match := contract.SelectorSpec{}
		matchedVariant := -1
		count := 0
		if !loading {
			for index, key := range []string{"candidate.greet_job_name", "candidate.greet_job_name_legacy"} {
				selector, exists := cfg.Selectors[key]
				if !exists {
					continue
				}
				selector.TimeoutMS = 1
				if selector.ReadAttribute != "" {
					attributes := make(map[string]string, len(selector.Target.Attributes)+1)
					for key, value := range selector.Target.Attributes {
						attributes[key] = value
					}
					attributes[selector.ReadAttribute] = name
					selector.Target.Attributes = attributes
				} else {
					exact := true
					selector.Target.Text = name
					selector.Target.ExactText = &exact
				}
				items, err := browser.FindAll(waitCtx, contract.ElementFindAllRequest{Selector: selector, MaxItems: 2, ExpectedMissing: true, ProbeOnly: true, Diagnostic: true})
				if err != nil && !common.IsElementMissing(err) {
					return contract.SelectorSpec{}, fmt.Errorf("读取沟通职位结果失败：%w", err)
				}
				count += len(items)
				if len(items) > 0 {
					match, matchedVariant = selector, index
				}
			}
		}
		if count > 1 {
			return contract.SelectorSpec{}, fmt.Errorf("智联有多个职位叫“%s”，请先在智联手动选好沟通职位", name)
		}
		if loading || count != 1 {
			stableSince = time.Time{}
			lastVariant = -1
		} else {
			if stableSince.IsZero() || lastVariant != matchedVariant {
				stableSince = time.Now()
				lastVariant = matchedVariant
			}
			if time.Since(stableSince) >= 900*time.Millisecond {
				match.TimeoutMS = 5000
				return match, nil
			}
		}
		select {
		case <-waitCtx.Done():
			return contract.SelectorSpec{}, fmt.Errorf("智联搜索结果还没稳定或没有找到同名职位“%s”，请检查职位列表后重试：%w", name, waitCtx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// searchGreetingJobLoading 即时检查真实下拉层中的可见加载骨架，旧页面未配置加载标记时保持兼容。
func searchGreetingJobLoading(ctx context.Context, browser model.Browser, cfg model.Config) (bool, error) {
	selector, configured := cfg.Selectors["candidate.greet_job_loading"]
	if !configured {
		return false, nil
	}
	selector.TimeoutMS = 1
	items, err := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: selector, MaxItems: 1, ExpectedMissing: true, ProbeOnly: true})
	if common.IsElementMissing(err) {
		return false, nil
	}
	return len(items) > 0, err
}
