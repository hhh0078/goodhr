// Package job51 文件作用：只在前程无忧招呼职位弹框内精确选择任务岗位，不修改人才搜索条件。
package job51

import (
	"context"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// selectGreetingJob 在平台实际要求时选择唯一完整同名岗位，核对回显后仅执行平台配置的确认步骤。
func selectGreetingJob(ctx context.Context, browser model.Browser, cfg model.Config, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("GoodHR 岗位名称还没填，先检查岗位设置再试")
	}
	if _, configured := cfg.Selectors["candidate.greet_job_open"]; configured {
		if err := common.ClickRequired(ctx, browser, cfg, "candidate.greet_job_open"); err != nil {
			return fmt.Errorf("打开招呼职位列表失败：%w", err)
		}
	}
	if _, configured := cfg.Selectors["candidate.greet_job_input"]; configured {
		if err := common.InputRequired(ctx, browser, cfg, "candidate.greet_job_input", common.PositionSearchQuery(name)); err != nil {
			return fmt.Errorf("搜索招呼职位失败：%w", err)
		}
	}
	selector, err := common.RequiredSelector(cfg, "candidate.greet_job_name")
	if err != nil {
		return err
	}
	selector = selector.MapVariants(func(spec contract.SelectorSpec) contract.SelectorSpec {
		exact := true
		spec.Target.Text = name
		spec.Target.Texts = nil
		spec.Target.ExactText = &exact
		spec.Target.Index = nil
		return spec
	})
	items, err := browser.FindAll(ctx, contract.ElementFindAllRequest{
		Selector: selector, MaxItems: 2, ExpectedMissing: true, ProbeOnly: true,
	})
	if err != nil && !common.IsElementMissing(err) {
		return fmt.Errorf("读取招呼职位列表失败：%w", err)
	}
	switch len(items) {
	case 0:
		return fmt.Errorf("前程无忧没有找到同名职位“%s”，请核对完整岗位名称", name)
	case 1:
	default:
		return fmt.Errorf("前程无忧有多个职位叫“%s”，请先核对平台岗位", name)
	}
	if _, err = browser.Click(ctx, contract.ElementClickRequest{Selector: selector}); err != nil {
		return fmt.Errorf("选择招呼职位失败：%w", err)
	}
	if err := waitGreetingJobSelection(ctx, browser, cfg, name); err != nil {
		return err
	}
	if _, configured := cfg.Selectors["candidate.greet_job_confirm"]; configured {
		return common.ClickRequired(ctx, browser, cfg, "candidate.greet_job_confirm")
	}
	return nil
}

// waitGreetingJobSelection 等待平台异步回显完整岗位名称，读到旧值时只等待，不重复选岗或发送。
func waitGreetingJobSelection(ctx context.Context, browser model.Browser, cfg model.Config, name string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		selected, found, err := common.ReadOptional(ctx, browser, cfg, "candidate.greet_job_selected")
		if err != nil {
			return fmt.Errorf("核对已选招呼职位失败：%w", err)
		}
		if found && selected == name {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("没确认前程无忧选中了“%s”，请检查招呼弹框：%w", name, ctx.Err())
		case <-ticker.C:
		}
	}
}
