// Package common 文件作用：提供所有平台复用的候选人对话框确认、侧边栏切换、索要资料和消息发送能力。
package common

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

const candidateConversationPollInterval = 300 * time.Millisecond
const candidateConversationPollAttempts = 20

// EnsureCandidateConversation 打开或复用指定候选人的聊天框，并在返回前核对候选人姓名。
func EnsureCandidateConversation(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	continued, err := ensureCandidateConversationFromCard(ctx, browser, cfg, candidate, true)
	if err != nil {
		return err
	}
	if continued {
		return nil
	}
	drawerReady, err := ensureCandidateContactDrawer(ctx, browser, cfg)
	if err != nil {
		return err
	}
	if drawerReady {
		switched, switchErr := activateCandidateFromContactList(ctx, browser, cfg, candidate)
		if switchErr != nil {
			return switchErr
		}
		if switched {
			return nil
		}
	}
	return fmt.Errorf("%s当前候选人还没有“继续沟通”入口，右侧联系人列表里也没找到，消息没有发送", cfg.Name)
}

// EnsureCandidateConversationFromCard 只通过当前候选人卡片的继续沟通入口打开聊天框。
// 该路径不会打开或关闭联系人列表，适用于打招呼后按钮必然变成继续沟通的平台。
func EnsureCandidateConversationFromCard(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	continued, err := ensureCandidateConversationFromCard(ctx, browser, cfg, candidate, false)
	if err != nil {
		return err
	}
	if !continued {
		return fmt.Errorf("%s打招呼后的“继续沟通”按钮还没出现，聊天框没有打开", cfg.Name)
	}
	return nil
}

// ensureCandidateConversationFromCard 复用聊天框或点击当前卡片的继续沟通按钮。
// closeContactDrawer 表示切换候选人前是否同时清理联系人列表。
func ensureCandidateConversationFromCard(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate, closeContactDrawer bool) (bool, error) {
	opened, err := ProbeSelectorExists(ctx, browser, cfg, "candidate.chat_modal")
	if err != nil {
		return false, err
	}
	if opened {
		matches, matchErr := candidateChatMatches(ctx, browser, cfg, candidate)
		if matchErr != nil {
			return false, matchErr
		}
		if matches {
			return true, nil
		}
	}
	if closeContactDrawer {
		err = CloseCandidatePanels(ctx, browser, cfg)
	} else {
		err = CloseCandidateChat(ctx, browser, cfg)
	}
	if err != nil {
		return false, fmt.Errorf("关闭%s其他候选人的沟通弹层失败：%w", cfg.Name, err)
	}
	return activateCandidateFromCard(ctx, browser, cfg, candidate)
}

// ensureCandidateContactDrawer 在推荐页打开或复用右侧联系人列表。
// 返回 true 表示联系人列表已准备好；平台未配置联系人入口时返回 false 并保留卡片入口兜底。
func ensureCandidateContactDrawer(ctx context.Context, browser model.Browser, cfg model.Config) (bool, error) {
	if _, configured := cfg.Selectors["candidate.contact_drawer"]; !configured {
		return false, nil
	}
	opened, err := ProbeSelectorExists(ctx, browser, cfg, "candidate.contact_drawer")
	if err != nil {
		return false, err
	}
	if opened {
		return true, nil
	}
	if _, configured := cfg.Selectors["candidate.contact_trigger"]; !configured {
		return false, nil
	}
	if err = ClickRequired(ctx, browser, cfg, "candidate.contact_trigger"); err != nil {
		return false, fmt.Errorf("打开%s推荐页右侧联系人列表失败：%w", cfg.Name, err)
	}
	for attempt := 1; attempt <= candidateConversationPollAttempts; attempt++ {
		opened, err = ProbeSelectorExists(ctx, browser, cfg, "candidate.contact_drawer")
		if err != nil {
			return false, err
		}
		if opened {
			return true, nil
		}
		if attempt < candidateConversationPollAttempts {
			if err = waitConversationPoll(ctx); err != nil {
				return false, err
			}
		}
	}
	return false, fmt.Errorf("%s推荐页右侧联系人列表没有在 6 秒内打开", cfg.Name)
}

// RequestCandidateInfo 按配置在已经确认身份的聊天框内索要电话、微信和简历。
func RequestCandidateInfo(ctx context.Context, browser model.Browser, cfg model.Config, request model.CandidateInfoRequest) error {
	steps := []struct {
		enabled bool
		key     string
		label   string
	}{
		{request.RequestPhone, "candidate.request_phone", "索要手机号"},
		{request.RequestWechat, "candidate.request_wechat", "索要微信"},
		{request.RequestResume, "candidate.request_resume", "索要简历"},
	}
	var requestErrors []error
	for _, step := range steps {
		if !step.enabled {
			continue
		}
		if err := ClickRequired(ctx, browser, cfg, step.key); err != nil {
			requestErrors = append(requestErrors, fmt.Errorf("%s失败：%w", step.label, err))
			continue
		}
		if err := ClickOptional(ctx, browser, cfg, step.key+"_confirm"); err != nil {
			requestErrors = append(requestErrors, fmt.Errorf("确认%s失败：%w", step.label, err))
		}
	}
	return errors.Join(requestErrors...)
}

// SendCandidateMessage 向已经确认属于当前候选人的聊天框发送非空消息。
func SendCandidateMessage(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate, message string) error {
	if strings.TrimSpace(message) == "" {
		return fmt.Errorf("候选人消息不能为空")
	}
	matches, err := candidateChatMatches(ctx, browser, cfg, candidate)
	if err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("%s聊天框候选人与当前处理对象不一致，消息没有发送", cfg.Name)
	}
	return sendMessage(ctx, browser, cfg, "candidate.followup_input", "candidate.followup_send", message)
}

// CloseCandidateConversation 关闭当前候选人的聊天框和可能同时打开的侧边栏。
func CloseCandidateConversation(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return CloseCandidatePanels(ctx, browser, cfg)
}

// activateCandidateFromCard 检查并点击当前候选人卡片的继续沟通入口。
// 返回 false 表示首次消息后按钮还没出现，需要再从推荐页右侧联系人列表查找。
func activateCandidateFromCard(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) (bool, error) {
	continued, err := CandidateActionExists(ctx, browser, cfg, candidate, "candidate.continue")
	if err != nil {
		return false, err
	}
	if !continued {
		return false, nil
	}
	if err = CandidateAction(ctx, browser, cfg, candidate, "candidate.continue"); err != nil {
		return false, fmt.Errorf("打开%s候选人聊天框失败：%w", cfg.Name, err)
	}
	if err = WaitCandidateConversation(ctx, browser, cfg, candidate); err == nil {
		return true, nil
	}
	opened, probeErr := ProbeSelectorExists(ctx, browser, cfg, "candidate.chat_modal")
	if probeErr != nil {
		return false, probeErr
	}
	if opened {
		return false, err
	}
	if retryErr := CandidateAction(ctx, browser, cfg, candidate, "candidate.continue"); retryErr != nil {
		return false, fmt.Errorf("再次打开%s候选人聊天框失败：%w", cfg.Name, retryErr)
	}
	if retryErr := WaitCandidateConversation(ctx, browser, cfg, candidate); retryErr != nil {
		return false, retryErr
	}
	return true, nil
}

// activateCandidateFromContactList 尝试从已经打开的联系人侧边栏切换到指定候选人。
func activateCandidateFromContactList(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) (bool, error) {
	selector, configured := cfg.Selectors["candidate.contact_item"]
	if !configured || len(selector.Target.Selectors) == 0 {
		return false, nil
	}
	items, err := browser.FindAll(ctx, contract.ElementFindAllRequest{
		Selector: selector, MaxItems: 100, ExpectedMissing: true,
	})
	if IsElementMissing(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	matchIndex := -1
	for _, item := range items {
		if conversationTextMatchesCandidate(candidate.Name, item.Text) {
			matchIndex = item.Index
			break
		}
	}
	if matchIndex < 0 {
		return false, nil
	}
	selector.Target.Index = &matchIndex
	drawer, err := RequiredSelector(cfg, "candidate.contact_drawer")
	if err != nil {
		return false, err
	}
	requireFull := false
	if _, err = browser.Scroll(ctx, contract.ScrollRequest{
		Target:         &selector,
		WheelAnchor:    &drawer,
		Distance:       180,
		MaxAttempts:    18,
		WaitMS:         180,
		RequireFull:    &requireFull,
		ViewportMargin: 24,
	}); err != nil {
		return false, fmt.Errorf("滚动到%s联系人列表中的当前候选人失败：%w", cfg.Name, err)
	}
	if _, err = browser.Click(ctx, contract.ElementClickRequest{
		Selector: selector, ViewportMargin: 24,
	}); err != nil {
		return false, fmt.Errorf("从%s联系人列表切换候选人失败：%w", cfg.Name, err)
	}
	if err = WaitCandidateConversation(ctx, browser, cfg, candidate); err != nil {
		return false, err
	}
	return true, nil
}

// WaitCandidateConversation 每 300 毫秒确认聊天框已经出现且候选人姓名正确。
func WaitCandidateConversation(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	for attempt := 1; attempt <= candidateConversationPollAttempts; attempt++ {
		opened, err := ProbeSelectorExists(ctx, browser, cfg, "candidate.chat_modal")
		if err != nil {
			return err
		}
		if opened {
			matches, matchErr := candidateChatMatches(ctx, browser, cfg, candidate)
			if matchErr != nil {
				return matchErr
			}
			if matches {
				return nil
			}
		}
		if attempt < candidateConversationPollAttempts {
			if err = waitConversationPoll(ctx); err != nil {
				return err
			}
		}
	}
	return fmt.Errorf("%s候选人聊天框没有在 6 秒内准备好，消息没有发送", cfg.Name)
}

// candidateChatMatches 判断当前聊天框是否属于正在处理的候选人。
func candidateChatMatches(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) (bool, error) {
	expected := normalizeCandidateName(candidate.Name)
	if expected == "" {
		return false, fmt.Errorf("%s当前候选人姓名为空，无法安全确认聊天对象", cfg.Name)
	}
	if _, configured := cfg.Selectors["candidate.chat_name"]; !configured {
		return false, fmt.Errorf("%s没有配置聊天框候选人姓名选择器，消息没有发送", cfg.Name)
	}
	for attempt := 1; attempt <= 5; attempt++ {
		actual, found, err := ReadOptional(ctx, browser, cfg, "candidate.chat_name")
		if err != nil {
			return false, err
		}
		if found && CandidateNamesMatch(expected, actual) {
			return true, nil
		}
		if attempt < 5 {
			if err = waitConversationPoll(ctx); err != nil {
				return false, err
			}
		}
	}
	return false, nil
}

// conversationTextMatchesCandidate 判断侧边栏一项是否包含目标候选人姓名。
func conversationTextMatchesCandidate(expected string, actual string) bool {
	expected = normalizeCandidateName(expected)
	actual = normalizeCandidateName(actual)
	if expected == "" || actual == "" {
		return false
	}
	if strings.Contains(expected, "*") {
		return []rune(expected)[0] == []rune(actual)[0]
	}
	return strings.Contains(actual, expected)
}

// CandidateNamesMatch 比较完整或脱敏的候选人姓名。
func CandidateNamesMatch(expected string, actual string) bool {
	expected = normalizeCandidateName(expected)
	actual = normalizeCandidateName(actual)
	if expected == "" || actual == "" {
		return false
	}
	if strings.Contains(expected, "*") || strings.Contains(actual, "*") {
		return []rune(expected)[0] == []rune(actual)[0]
	}
	return expected == actual || strings.Contains(expected, actual) || strings.Contains(actual, expected)
}

// normalizeCandidateName 清理姓名空白和常见称谓。
func normalizeCandidateName(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), "")
	value = strings.TrimSuffix(value, "先生")
	return strings.TrimSuffix(value, "女士")
}

// waitConversationPoll 等待下一轮对话框状态查询并响应任务取消。
func waitConversationPoll(ctx context.Context) error {
	timer := time.NewTimer(candidateConversationPollInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// sendMessage 使用指定输入框和发送按钮发送消息，未配置按钮时按 Enter。
func sendMessage(ctx context.Context, browser model.Browser, cfg model.Config, inputKey string, sendKey string, message string) error {
	if strings.TrimSpace(message) == "" {
		return fmt.Errorf("发送内容不能为空")
	}
	if err := InputRequired(ctx, browser, cfg, inputKey, message); err != nil {
		return fmt.Errorf("输入消息失败：%w", err)
	}
	if _, configured := cfg.Selectors[sendKey]; configured {
		if err := ClickRequired(ctx, browser, cfg, sendKey); err != nil {
			return fmt.Errorf("点击发送消息失败：%w", err)
		}
		// 招聘页面发送消息后会短暂刷新聊天组件，等一轮再执行关闭等后续动作。
		return waitConversationPoll(ctx)
	}
	if _, err := browser.PressKey(ctx, contract.KeyboardPressRequest{Key: "Enter", DelayMS: 80}); err != nil {
		return fmt.Errorf("按 Enter 发送消息失败：%w", err)
	}
	// 招聘页面发送消息后会短暂刷新聊天组件，等一轮再执行关闭等后续动作。
	return waitConversationPoll(ctx)
}
