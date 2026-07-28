// Package hliepin 文件作用：实现猎聘猎头端打招呼、收藏和不合适动作。
package hliepin

import (
	"context"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// GreetCandidate 清理遗留弹层并按发布职位或快捷搜索模式完成猎聘开聊。
func (r *Runtime) GreetCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate, request model.GreetRequest) error {
	if candidate.Fields["greet_state"] == "continue" {
		return nil
	}
	if err := closeGreetModalIfPresent(ctx, browser, cfg); err != nil {
		return err
	}
	if err := closeCandidatePanels(ctx, browser, cfg); err != nil {
		return err
	}
	greetSelector, err := common.CandidateScopedSelector(cfg, "candidate.greet", candidate.Index)
	if err != nil {
		return err
	}
	modal, err := common.RequiredSelector(cfg, "candidate.greet_modal")
	if err != nil {
		return err
	}
	if _, err = browser.Click(ctx, contract.ElementClickRequest{
		Selector: greetSelector, ViewportMargin: 48,
		Verify: &contract.ClickVerification{TargetVisible: &modal, TimeoutMS: 5000},
	}); err != nil {
		return fmt.Errorf("点击猎聘“立即沟通”失败：%w", err)
	}
	selectedJob := false
	if r.selectJobWhenGreeting {
		selectedJob, err = selectGreetingJob(ctx, browser, cfg, r.positionName)
		if err != nil {
			return err
		}
	}
	if !selectedJob {
		if err = common.ClickRequired(ctx, browser, cfg, "candidate.greet_without_job"); err != nil {
			return fmt.Errorf("点击猎聘“不选择职位开聊”失败：%w", err)
		}
	} else if err = common.ClickRequired(ctx, browser, cfg, "candidate.greet_submit"); err != nil {
		return fmt.Errorf("点击猎聘“立即开聊”失败：%w", err)
	}
	if request.KeepConversationOpen {
		return nil
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err = browser.PressKey(ctx, contract.KeyboardPressRequest{Key: "Escape", DelayMS: 120}); err != nil {
			return fmt.Errorf("猎聘开聊后关闭提示弹层失败：%w", err)
		}
	}
	return nil
}

// FavoriteCandidate 收藏指定猎聘猎头端候选人。
func (r *Runtime) FavoriteCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	return common.CandidateAction(ctx, browser, cfg, candidate, "candidate.favorite")
}

// RejectCandidate 将指定猎聘猎头端候选人标记为不合适。
func (r *Runtime) RejectCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	return common.CandidateAction(ctx, browser, cfg, candidate, "candidate.reject")
}

// selectGreetingJob 在开聊弹框中选择匹配岗位，未匹配时交给不选职位流程。
func selectGreetingJob(ctx context.Context, browser model.Browser, cfg model.Config, positionName string) (bool, error) {
	if strings.TrimSpace(positionName) == "" {
		return false, fmt.Errorf("猎聘开聊岗位名称为空")
	}
	if err := common.ClickRequired(ctx, browser, cfg, "candidate.greet_job_open"); err != nil {
		return false, fmt.Errorf("打开猎聘开聊岗位列表失败：%w", err)
	}
	selector, err := common.RequiredSelector(cfg, "candidate.greet_job_item")
	if err != nil {
		return false, err
	}
	items, err := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: selector, MaxItems: 100})
	if err != nil {
		return false, fmt.Errorf("读取猎聘开聊岗位列表失败：%w", err)
	}
	matchIndex := matchingGreetingJob(items, positionName)
	if matchIndex < 0 {
		return false, nil
	}
	selector.Target.Index = &matchIndex
	if _, err = browser.Click(ctx, contract.ElementClickRequest{Selector: selector, ViewportMargin: 24}); err != nil {
		return false, fmt.Errorf("选择猎聘开聊岗位失败：%w", err)
	}
	return true, nil
}

// matchingGreetingJob 优先完整匹配岗位，再兼容页面省略号截断。
func matchingGreetingJob(items []contract.FindAllItem, positionName string) int {
	target := normalizeGreetingJob(positionName)
	bestIndex := -1
	bestLength := 0
	for _, item := range items {
		name := normalizeGreetingJob(item.Text)
		if name == target && name != "" {
			return item.Index
		}
		trimmed := strings.TrimRight(name, ".…")
		if trimmed == "" || (!strings.Contains(target, trimmed) && !strings.Contains(trimmed, target)) {
			continue
		}
		if length := len([]rune(trimmed)); length > bestLength {
			bestIndex = item.Index
			bestLength = length
		}
	}
	return bestIndex
}

// normalizeGreetingJob 清理猎聘开聊岗位比较文本。
func normalizeGreetingJob(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(common.PositionSearchQuery(value)), ""))
}

// closeGreetModalIfPresent 只关闭遗留的猎聘开聊弹框。
func closeGreetModalIfPresent(ctx context.Context, browser model.Browser, cfg model.Config) error {
	exists, err := common.SelectorExists(ctx, browser, cfg, "candidate.greet_modal")
	if err != nil || !exists {
		return err
	}
	_, err = browser.PressKey(ctx, contract.KeyboardPressRequest{Key: "Escape", DelayMS: 120})
	if err != nil {
		return fmt.Errorf("关闭遗留猎聘开聊弹框失败：%w", err)
	}
	return nil
}

// closeCandidatePanels 关闭遗留的猎聘聊天框和联系人抽屉。
func closeCandidatePanels(ctx context.Context, browser model.Browser, cfg model.Config) error {
	steps := []struct {
		panel string
		close string
	}{
		{panel: "candidate.chat_modal", close: "candidate.chat_close"},
		{panel: "candidate.contact_drawer", close: "candidate.contact_drawer_close"},
	}
	for _, step := range steps {
		exists, err := common.SelectorExists(ctx, browser, cfg, step.panel)
		if err != nil {
			return err
		}
		if exists {
			if err = common.ClickRequired(ctx, browser, cfg, step.close); err != nil {
				return fmt.Errorf("关闭猎聘遗留弹层失败：%w", err)
			}
		}
	}
	return nil
}
