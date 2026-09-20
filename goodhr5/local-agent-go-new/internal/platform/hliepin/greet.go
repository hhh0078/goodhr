// Package hliepin 文件作用：实现猎聘猎头端打招呼、收藏和不合适动作。
package hliepin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// GreetCandidate 清理遗留弹层并按发布职位或快捷搜索模式完成猎聘开聊，失败时清理开聊弹框。
func (r *Runtime) GreetCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate, request model.GreetRequest) (resultErr error) {
	defer func() {
		if resultErr == nil {
			return
		}
		if contract.IsSessionUnavailable(resultErr) {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
		defer cancel()
		if cleanupErr := closeGreetingPanels(cleanupCtx, browser, cfg, true); cleanupErr != nil {
			resultErr = fmt.Errorf("%w；遗留的猎聘弹层也没清理好：%v", resultErr, cleanupErr)
		}
	}()
	if candidate.Fields["greet_state"] == "continue" {
		return model.ErrCandidateAlreadyContacted
	}
	if err := closeGreetingPanels(ctx, browser, cfg, false); err != nil {
		return err
	}
	if err := openGreetingModal(ctx, browser, cfg, candidate); err != nil {
		return err
	}
	selectedJob, err := selectGreetingJob(ctx, browser, cfg, r.positionName)
	if err != nil {
		return err
	}
	confirmKey := "candidate.greet_submit"
	confirmLabel := "立即开聊"
	if !selectedJob {
		confirmKey = "candidate.greet_without_job"
		confirmLabel = "不选择职位开聊"
	}
	if err = confirmGreeting(ctx, browser, cfg, candidate, confirmKey, confirmLabel); err != nil {
		return err
	}
	if err = closePostGreetPromotion(ctx, browser, cfg, postGreetPromotionPollAttempts); err != nil {
		return err
	}
	return finishGreetingConversation(
		ctx,
		browser,
		cfg,
		candidate,
		selectedJob,
		request.KeepConversationOpen,
	)
}

// finishGreetingConversation 按猎聘开聊方式决定是否等待自动聊天框。
// 选择匹配岗位后平台不会自动打开聊天框，后续公共流程会点击“继续沟通”；不选岗位开聊时才复用自动聊天框。
func finishGreetingConversation(
	ctx context.Context,
	browser model.Browser,
	cfg model.Config,
	candidate model.Candidate,
	selectedJob bool,
	keepConversationOpen bool,
) error {
	if selectedJob {
		return nil
	}
	if keepConversationOpen {
		if err := common.WaitCandidateConversation(ctx, browser, cfg, candidate); err != nil {
			return fmt.Errorf("等待猎聘开聊后的候选人聊天框失败：%w", err)
		}
		return nil
	}
	if err := common.CloseCandidateChat(ctx, browser, cfg); err != nil {
		return fmt.Errorf("猎聘开聊后关闭候选人聊天框失败：%w", err)
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
	nameSelector, err := common.RequiredSelector(cfg, "candidate.greet_job_name")
	if err != nil {
		return false, err
	}
	items, err := browser.FindAll(ctx, contract.ElementFindAllRequest{
		Selector:        selector,
		MaxItems:        100,
		Fields:          map[string]contract.SelectorSpec{"position_name": nameSelector},
		ExpectedMissing: true,
	})
	if common.IsElementMissing(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("读取猎聘开聊岗位列表失败：%w", err)
	}
	matchIndex := matchingGreetingJob(items, positionName)
	if matchIndex < 0 {
		return false, nil
	}
	selector.Target.Index = &matchIndex
	list, err := common.RequiredSelector(cfg, "candidate.greet_job_list")
	if err != nil {
		return false, err
	}
	list.TimeoutMS = 200
	requireFull := false
	if _, err = browser.Scroll(ctx, contract.ScrollRequest{
		Target:         &selector,
		WheelAnchor:    &list,
		Distance:       160,
		MaxAttempts:    16,
		WaitMS:         180,
		RequireFull:    &requireFull,
		ViewportMargin: 24,
	}); err != nil {
		return false, fmt.Errorf("滚动到猎聘开聊岗位失败：%w", err)
	}
	_, clickErr := browser.Click(ctx, contract.ElementClickRequest{
		Selector: selector, ViewportMargin: 24,
	})
	selected, verifyErr := greetingJobAlreadySelected(ctx, browser, cfg, positionName)
	if selected {
		return true, nil
	}
	if clickErr != nil {
		return false, fmt.Errorf("选择猎聘开聊岗位失败：%w", clickErr)
	}
	if verifyErr != nil {
		return false, fmt.Errorf("确认猎聘已选开聊岗位失败：%w", verifyErr)
	}
	return false, fmt.Errorf("猎聘开聊岗位点击完成了，但页面没有选中目标岗位")
}

// greetingJobAlreadySelected 判断开聊弹框当前选中岗位是否匹配后端岗位名称。
func greetingJobAlreadySelected(ctx context.Context, browser model.Browser, cfg model.Config, positionName string) (bool, error) {
	selectedName, found, err := common.ReadOptional(ctx, browser, cfg, "candidate.greet_job_selected_name")
	if err != nil || !found {
		return false, err
	}
	matchIndex := matchingGreetingJob([]contract.FindAllItem{{
		Index: 0,
		Fields: map[string]string{
			"position_name": selectedName,
		},
	}}, positionName)
	return matchIndex == 0, nil
}

// confirmGreeting 点击猎聘开聊确认按钮，并先确认职位弹框已经关闭。
func confirmGreeting(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate, key string, label string) error {
	selector, err := common.RequiredSelector(cfg, key)
	if err != nil {
		return err
	}
	modal, err := common.RequiredSelector(cfg, "candidate.greet_modal")
	if err != nil {
		return err
	}
	modal.TimeoutMS = 200
	if _, err = browser.Click(ctx, contract.ElementClickRequest{
		Selector: selector,
		Verify: &contract.ClickVerification{
			TargetHidden: &modal,
			TimeoutMS:    5000,
		},
	}); err != nil {
		var workerErr *contract.WorkerError
		if errors.As(err, &workerErr) && workerErr.Body.Code == "CLICK_VERIFICATION_FAILED" {
			// 已经松开鼠标，不再重复提交；只复查弹框和当前候选人的沟通状态。
			if verifyErr := verifyGreetingResult(ctx, browser, cfg, candidate); verifyErr == nil {
				return nil
			} else {
				return fmt.Errorf("已点击猎聘“%s”，但开聊结果尚未确认，请查看当前聊天状态：%w", label, errors.Join(err, verifyErr))
			}
		}
		return fmt.Errorf("点击猎聘“%s”失败：%w", label, err)
	}
	return nil
}

// verifyGreetingResult 在验证超时后只读等待最多八秒，以弹框关闭或本候选人继续沟通按钮确认结果。
func verifyGreetingResult(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	for {
		opened, err := common.ProbeSelectorExists(ctx, browser, cfg, "candidate.greet_modal")
		if err != nil {
			return err
		}
		if !opened {
			return nil
		}
		contacted, err := common.CandidateActionExists(ctx, browser, cfg, candidate, "candidate.continue")
		if err != nil {
			return err
		}
		if contacted {
			return nil
		}
		timer := time.NewTimer(300 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// matchingGreetingJob 优先完整匹配岗位，再按页面省略号前缀选择唯一且最长的匹配项。
func matchingGreetingJob(items []contract.FindAllItem, positionName string) int {
	target := normalizeGreetingJob(positionName)
	bestIndex := -1
	bestLength := 0
	ambiguous := false
	for _, item := range items {
		name := normalizeGreetingJob(greetingJobName(item))
		if name == target && name != "" {
			return item.Index
		}
		trimmed := strings.TrimRight(name, ".…")
		if trimmed == name || len([]rune(trimmed)) < 4 || !strings.HasPrefix(target, trimmed) {
			continue
		}
		length := len([]rune(trimmed))
		if length > bestLength {
			bestIndex = item.Index
			bestLength = length
			ambiguous = false
		} else if length == bestLength {
			ambiguous = true
		}
	}
	if ambiguous {
		return -1
	}
	return bestIndex
}

// greetingJobName 优先读取职位标题字段，避免公司、薪资和城市参与岗位匹配。
func greetingJobName(item contract.FindAllItem) string {
	if value := strings.TrimSpace(item.Fields["position_name"]); value != "" {
		return value
	}
	return strings.TrimSpace(item.Text)
}

// normalizeGreetingJob 清理猎聘开聊岗位比较文本。
func normalizeGreetingJob(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(common.PositionSearchQuery(value)), ""))
}

// closeGreetModalIfPresent 只关闭遗留的猎聘开聊弹框。
func closeGreetModalIfPresent(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return closeModalWithEscape(ctx, browser, cfg, "candidate.greet_modal", "猎聘开聊弹框")
}

// closeModalWithEscape 关闭已识别的弹框并短暂等待关闭动画，避免遗留遮罩挡住后续输入。
func closeModalWithEscape(ctx context.Context, browser model.Browser, cfg model.Config, key, label string) error {
	exists, err := common.ProbeSelectorExists(ctx, browser, cfg, key)
	if err != nil || !exists {
		return err
	}
	_, err = browser.PressKey(ctx, contract.KeyboardPressRequest{Key: "Escape", DelayMS: 120})
	if err != nil {
		return fmt.Errorf("关闭%s失败：%w", label, err)
	}
	for attempt := 0; attempt < 4; attempt++ {
		opened, err := common.ProbeSelectorExists(ctx, browser, cfg, key)
		if err != nil || !opened {
			return err
		}
		timer := time.NewTimer(150 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("%s按 Escape 后仍未关闭", label)
}

// closeCandidatePanels 关闭遗留的猎聘聊天框和联系人抽屉。
func closeCandidatePanels(ctx context.Context, browser model.Browser, cfg model.Config) error {
	if err := common.CloseCandidatePanels(ctx, browser, cfg); err != nil {
		return fmt.Errorf("关闭猎聘遗留弹层失败：%w", err)
	}
	return nil
}
