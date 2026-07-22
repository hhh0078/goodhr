// Package hliepin 文件作用：实现猎聘猎头端基础筛选入口和打招呼后的索要信息流程。
package hliepin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
)

const (
	hliepinContinueButtonSelector = hliepinCandidateButtonTarget
	hliepinRequestPhoneSelector   = ".im-ui-action-button.action-item.action-phone"
	hliepinRequestWechatSelector  = ".im-ui-action-button.action-item.action-wechat"
	hliepinRequestResumeSelector  = ".im-ui-action-button.action-item.action-resume"
	hliepinChatInputSelector      = hliepinChatModalParent + " textarea.ant-im-input.im-ui-textarea[placeholder='请输入文字，按Enter键发送']"
	hliepinChatCloseSelector      = ".im-ui-basic-chat-header-modal-close"
	hliepinCandidateListClose     = ".ant-im-drawer-close"
	hliepinRequestConfirmDialog   = ".ant-im-modal.ant-im-modal-confirm"
	hliepinRequestConfirmButton   = ".ant-im-modal-confirm-btns .ant-im-btn-primary"
	hliepinChatCandidateName      = ".im-ui-basic-chat-header-name"
	hliepinPanelPollInterval      = 0.1
	hliepinChatOpenPollCount      = 100
	hliepinActionReadyPollCount   = 30
	hliepinReusableChatPollCount  = 15
	hliepinPanelQuietPollCount    = 5
	hliepinPanelCleanupPollCount  = 30
)

// hliepinCandidateInfoPanelState 记录猎聘聊天框和联系人抽屉的实时可见状态，用于阻止跨候选人串台。
type hliepinCandidateInfoPanelState struct {
	chatCount     int
	drawerCount   int
	candidateName string
}

// ApplyBasicFilters 保留猎聘猎头端基础筛选入口，当前不改变用户在页面中设置的条件。
func (r *Runtime) ApplyBasicFilters(context.Context, platformcore.Executor, cloudapi.PlatformConfig, map[string]any) error {
	return nil
}

// RequestCandidateInfo 打开猎聘候选人聊天框，按岗位配置索要信息、发送问候语并关闭两个弹层。
func (r *Runtime) RequestCandidateInfo(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate, request platformcore.CandidateInfoRequest) (resultErr error) {
	message := strings.TrimSpace(request.GreetMessage)
	if !request.RequestPhone && !request.RequestWechat && !request.RequestResume && message == "" {
		return nil
	}
	rowParent, err := hliepinCandidateRowParentSelector(candidate)
	if err != nil {
		return err
	}
	interactionMayHaveStarted := true
	defer func() {
		if !interactionMayHaveStarted {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := closeCandidateInfoPanels(cleanupCtx, exec, candidate); err != nil {
			exec.Log("warning", "猎聘索要信息：候选人="+candidateName(candidate)+"，弹层收尾失败，错误="+err.Error())
			if resultErr == nil {
				resultErr = err
			}
		}
	}()
	reused, err := waitForReusableCandidateInfoChat(ctx, exec, candidate)
	if err != nil {
		return err
	}
	if reused {
		exec.Log("info", "猎聘索要信息状态：候选人="+candidateName(candidate)+"，阶段=复用打招呼已打开的聊天框，不再点击继续沟通")
	} else {
		if err := clearStaleCandidateInfoPanels(ctx, exec, candidate); err != nil {
			return err
		}
		if _, err := hliepinStableClick(ctx, exec, rowParent, hliepinContinueButtonSelector, map[string]any{
			"action_name":   "候选人=" + candidateName(candidate) + "，继续沟通",
			"expected_text": "继续沟通", "exact_text": true,
		}); err != nil {
			return fmt.Errorf("点击猎聘“继续沟通”失败：%w", err)
		}
		if err := waitForCandidateInfoChat(ctx, exec, candidate); err != nil {
			return err
		}
	}
	actions := []struct {
		enabled  bool
		label    string
		selector string
	}{
		{enabled: request.RequestPhone, label: "手机", selector: hliepinRequestPhoneSelector},
		{enabled: request.RequestWechat, label: "微信", selector: hliepinRequestWechatSelector},
		{enabled: request.RequestResume, label: "简历", selector: hliepinRequestResumeSelector},
	}
	for _, action := range actions {
		if !action.enabled {
			continue
		}
		exec.Log("info", "猎聘索要信息：准备索要"+action.label)
		if err := waitForCandidateInfoAction(ctx, exec, candidate, action.label, action.selector); err != nil {
			return err
		}
		if _, err := hliepinStableClick(ctx, exec, hliepinChatModalParent, action.selector, map[string]any{
			"expected_text": "索要" + action.label, "exact_text": true,
		}); err != nil {
			return fmt.Errorf("点击猎聘“索要%s”失败：%w", action.label, err)
		}
		if err := confirmCandidateInfoRequestIfPresent(ctx, exec, action.label); err != nil {
			return err
		}
	}
	if message != "" {
		exec.Log("info", "猎聘索要信息：准备发送岗位首次打招呼语")
		if _, err := exec.Post(ctx, "/api/v1/page/type", map[string]any{
			"element": map[string]any{"selector": hliepinChatInputSelector},
			"text":    message,
			"timeout": 5000,
		}); err != nil {
			return fmt.Errorf("输入猎聘首次打招呼语失败：%w", err)
		}
		if _, err := exec.Post(ctx, "/api/v1/page/press-key", map[string]any{"key": "Enter"}); err != nil {
			return fmt.Errorf("发送猎聘首次打招呼语失败：%w", err)
		}
	}
	return nil
}

// waitForReusableCandidateInfoChat 每100毫秒检查打招呼是否已自动打开当前候选人聊天框，存在则直接复用。
func waitForReusableCandidateInfoChat(ctx context.Context, exec platformcore.Executor, candidate platformcore.Candidate) (bool, error) {
	expectedName := candidateName(candidate)
	startedAt := time.Now()
	lastState := hliepinCandidateInfoPanelState{chatCount: -1, drawerCount: -1}
	for attempt := 1; attempt <= hliepinReusableChatPollCount; attempt++ {
		state, err := inspectCandidateInfoPanels(ctx, exec)
		if err != nil {
			return false, err
		}
		if state != lastState || attempt == 1 || attempt%5 == 0 {
			exec.Log("info", fmt.Sprintf("猎聘索要信息状态：候选人=%s，阶段=判断是否复用聊天框，轮次=%d/%d，聊天框=%d，联系人列表=%d，聊天姓名=%s，耗时=%s", expectedName, attempt, hliepinReusableChatPollCount, state.chatCount, state.drawerCount, firstNonEmpty(state.candidateName, "无"), time.Since(startedAt).Round(time.Millisecond)))
			lastState = state
		}
		if state.chatCount > 1 {
			return false, fmt.Errorf("猎聘聊天框数量异常：候选人=%s，数量=%d", expectedName, state.chatCount)
		}
		if state.chatCount == 1 && state.candidateName != "" {
			if hliepinCandidateNamesMatch(expectedName, state.candidateName) {
				return true, nil
			}
			exec.Log("warning", fmt.Sprintf("猎聘索要信息状态：候选人=%s，已打开聊天姓名=%s，不复用并准备清理", expectedName, state.candidateName))
			return false, nil
		}
		if attempt < hliepinReusableChatPollCount {
			if err := exec.Delay(ctx, "等待猎聘打招呼自动打开聊天框", hliepinPanelPollInterval); err != nil {
				return false, err
			}
		}
	}
	exec.Log("info", fmt.Sprintf("猎聘索要信息状态：候选人=%s，打招呼后未自动打开聊天框，准备点击继续沟通，等待=%s", expectedName, time.Since(startedAt).Round(time.Millisecond)))
	return false, nil
}

// waitForCandidateInfoAction 每100毫秒等待猎聘聊天框底部索要按钮完成异步渲染，避免姓名先出现时过早判定按钮缺失。
func waitForCandidateInfoAction(ctx context.Context, exec platformcore.Executor, candidate platformcore.Candidate, label string, selector string) error {
	startedAt := time.Now()
	lastCount := -1
	for attempt := 1; attempt <= hliepinActionReadyPollCount; attempt++ {
		result, err := exec.Post(ctx, "/api/v1/page/find-elements", map[string]any{
			"element":      map[string]any{"selector": hliepinChatModalParent + " " + selector},
			"visible_only": true, "max_items": 2,
		})
		if err != nil {
			return fmt.Errorf("检查猎聘索要%s按钮失败：%w", label, err)
		}
		count := len(mapList(workerData(result, "items")))
		if count != lastCount || attempt == 1 || attempt%5 == 0 {
			exec.Log("info", fmt.Sprintf("猎聘索要信息状态：候选人=%s，阶段=等待索要%s按钮，轮次=%d/%d，按钮=%d，耗时=%s", candidateName(candidate), label, attempt, hliepinActionReadyPollCount, count, time.Since(startedAt).Round(time.Millisecond)))
			lastCount = count
		}
		if count == 1 {
			exec.Log("info", fmt.Sprintf("猎聘索要信息状态：候选人=%s，阶段=索要%s按钮就绪，轮次=%d，耗时=%s", candidateName(candidate), label, attempt, time.Since(startedAt).Round(time.Millisecond)))
			return nil
		}
		if count > 1 {
			return fmt.Errorf("猎聘索要%s按钮数量异常：%d", label, count)
		}
		if attempt < hliepinActionReadyPollCount {
			if err := exec.Delay(ctx, "等待猎聘索要"+label+"按钮渲染", hliepinPanelPollInterval); err != nil {
				return err
			}
		}
	}
	return fmt.Errorf("等待猎聘索要%s按钮超时：候选人=%s，等待=%s", label, candidateName(candidate), time.Since(startedAt).Round(time.Millisecond))
}

// confirmCandidateInfoRequestIfPresent 检查猎聘索要确认弹框，存在则点击确定，不存在则直接继续。
func confirmCandidateInfoRequestIfPresent(ctx context.Context, exec platformcore.Executor, label string) error {
	startedAt := time.Now()
	for attempt := 1; attempt <= 10; attempt++ {
		result, err := exec.Post(ctx, "/api/v1/page/find-elements", map[string]any{
			"element":      map[string]any{"selector": hliepinRequestConfirmDialog},
			"visible_only": true,
			"max_items":    1,
		})
		if err != nil {
			return fmt.Errorf("查找猎聘索要%s确认弹框失败：%w", label, err)
		}
		items := mapList(workerData(result, "items"))
		exec.Log("info", fmt.Sprintf("猎聘索要信息状态：阶段=检测%s确认框，轮次=%d/10，确认框=%d，耗时=%s", label, attempt, len(items), time.Since(startedAt).Round(time.Millisecond)))
		if len(items) > 0 && strings.Contains(stringFromMap(items[0], "text"), "索要") {
			if _, err := hliepinStableClick(ctx, exec, hliepinRequestConfirmDialog, hliepinRequestConfirmButton, map[string]any{
				"action_name":   "确认索要" + label,
				"expected_text": "确定", "exact_text": true, "normalize_text_whitespace": true,
				"wait_for_hidden_selector": hliepinRequestConfirmDialog, "wait_timeout": 5000,
			}); err != nil {
				return fmt.Errorf("确认猎聘向候选人索要%s失败：%w", label, err)
			}
			return nil
		}
		if attempt < 10 {
			if err := exec.Delay(ctx, "轮询猎聘索要"+label+"确认弹框", hliepinPanelPollInterval); err != nil {
				return err
			}
		}
	}
	exec.Log("info", "猎聘索要信息：1秒内未出现索要"+label+"确认弹框，跳过确认")
	return nil
}

// inspectCandidateInfoPanels 查询猎聘聊天框、联系人抽屉及聊天框候选人姓名。
func inspectCandidateInfoPanels(ctx context.Context, exec platformcore.Executor) (hliepinCandidateInfoPanelState, error) {
	state := hliepinCandidateInfoPanelState{}
	chatResult, err := exec.Post(ctx, "/api/v1/page/find-elements", map[string]any{
		"element": map[string]any{"selector": hliepinChatModalParent}, "visible_only": true, "max_items": 2,
		"fields": []any{map[string]any{"candidate_name": map[string]any{"selector": hliepinChatCandidateName}}},
	})
	if err != nil {
		return state, fmt.Errorf("查询猎聘聊天框状态失败：%w", err)
	}
	chatItems := mapList(workerData(chatResult, "items"))
	state.chatCount = len(chatItems)
	if len(chatItems) > 0 {
		state.candidateName = strings.TrimSpace(stringFromMap(mapFromAny(chatItems[0]["fields"]), "candidate_name"))
	}
	drawerResult, err := exec.Post(ctx, "/api/v1/page/find-elements", map[string]any{
		"element": map[string]any{"selector": hliepinCandidateDrawerParent}, "visible_only": true, "max_items": 2,
	})
	if err != nil {
		return state, fmt.Errorf("查询猎聘联系人列表状态失败：%w", err)
	}
	state.drawerCount = len(mapList(workerData(drawerResult, "items")))
	return state, nil
}

// waitForCandidateInfoChat 每100毫秒确认聊天框已打开且姓名属于当前候选人，超时前绝不进入下一候选人。
func waitForCandidateInfoChat(ctx context.Context, exec platformcore.Executor, candidate platformcore.Candidate) error {
	expectedName := candidateName(candidate)
	startedAt := time.Now()
	lastState := hliepinCandidateInfoPanelState{chatCount: -1, drawerCount: -1}
	for attempt := 1; attempt <= hliepinChatOpenPollCount; attempt++ {
		state, err := inspectCandidateInfoPanels(ctx, exec)
		if err != nil {
			return err
		}
		if state != lastState || attempt == 1 || attempt%10 == 0 {
			exec.Log("info", fmt.Sprintf("猎聘索要信息状态：候选人=%s，阶段=等待聊天框，轮次=%d/%d，聊天框=%d，联系人列表=%d，聊天姓名=%s，耗时=%s", expectedName, attempt, hliepinChatOpenPollCount, state.chatCount, state.drawerCount, firstNonEmpty(state.candidateName, "无"), time.Since(startedAt).Round(time.Millisecond)))
			lastState = state
		}
		if state.chatCount > 1 {
			return fmt.Errorf("猎聘聊天框数量异常：候选人=%s，数量=%d", expectedName, state.chatCount)
		}
		if state.chatCount == 1 && state.candidateName != "" {
			if !hliepinCandidateNamesMatch(expectedName, state.candidateName) {
				return fmt.Errorf("猎聘聊天候选人不匹配：预期=%s，实际=%s", expectedName, state.candidateName)
			}
			exec.Log("info", fmt.Sprintf("猎聘索要信息状态：候选人=%s，阶段=聊天框确认完成，聊天姓名=%s，轮次=%d，耗时=%s", expectedName, state.candidateName, attempt, time.Since(startedAt).Round(time.Millisecond)))
			return nil
		}
		if err := exec.Delay(ctx, "等待猎聘当前候选人聊天框", hliepinPanelPollInterval); err != nil {
			return err
		}
	}
	return fmt.Errorf("等待猎聘当前候选人聊天框超时：候选人=%s，等待=%s", expectedName, time.Since(startedAt).Round(time.Millisecond))
}

// hliepinCandidateNamesMatch 对完整姓名做精确比较，对猎聘脱敏姓名至少校验首字，避免明显串到其他候选人。
func hliepinCandidateNamesMatch(expected string, actual string) bool {
	normalize := func(value string) string {
		value = strings.TrimSpace(value)
		for _, suffix := range []string{"先生", "女士"} {
			value = strings.TrimSuffix(value, suffix)
		}
		return strings.ReplaceAll(value, " ", "")
	}
	expected = normalize(expected)
	actual = normalize(actual)
	if expected == "" || actual == "" {
		return false
	}
	if strings.Contains(expected, "*") {
		return []rune(expected)[0] == []rune(actual)[0]
	}
	return expected == actual
}

// clearStaleCandidateInfoPanels 在操作当前候选人前关闭上一次遗留的猎聘弹层。
func clearStaleCandidateInfoPanels(ctx context.Context, exec platformcore.Executor, candidate platformcore.Candidate) error {
	state, err := inspectCandidateInfoPanels(ctx, exec)
	if err != nil {
		return err
	}
	exec.Log("info", fmt.Sprintf("猎聘索要信息状态：候选人=%s，阶段=操作前检查，聊天框=%d，联系人列表=%d，聊天姓名=%s", candidateName(candidate), state.chatCount, state.drawerCount, firstNonEmpty(state.candidateName, "无")))
	if state.chatCount == 0 && state.drawerCount == 0 {
		return nil
	}
	exec.Log("warning", fmt.Sprintf("猎聘索要信息状态：候选人=%s，发现需要清理的现有弹层，开始清理", candidateName(candidate)))
	return closeCandidateInfoPanels(ctx, exec, candidate)
}

// closeCandidateInfoPanels 关闭猎聘聊天框和联系人抽屉，并连续确认页面恢复干净后才允许主流程继续。
func closeCandidateInfoPanels(ctx context.Context, exec platformcore.Executor, candidate platformcore.Candidate) error {
	startedAt := time.Now()
	quietChecks := 0
	var closeErrors []error
	for attempt := 1; attempt <= hliepinPanelCleanupPollCount; attempt++ {
		state, err := inspectCandidateInfoPanels(ctx, exec)
		if err != nil {
			return errors.Join(append(closeErrors, err)...)
		}
		exec.Log("info", fmt.Sprintf("猎聘索要信息状态：候选人=%s，阶段=弹层收尾，轮次=%d/%d，聊天框=%d，联系人列表=%d，连续干净=%d/%d，耗时=%s", candidateName(candidate), attempt, hliepinPanelCleanupPollCount, state.chatCount, state.drawerCount, quietChecks, hliepinPanelQuietPollCount, time.Since(startedAt).Round(time.Millisecond)))
		if state.chatCount == 0 && state.drawerCount == 0 {
			quietChecks++
			if quietChecks >= hliepinPanelQuietPollCount {
				exec.Log("info", fmt.Sprintf("猎聘索要信息状态：候选人=%s，阶段=弹层收尾完成，耗时=%s", candidateName(candidate), time.Since(startedAt).Round(time.Millisecond)))
				return errors.Join(closeErrors...)
			}
		} else {
			quietChecks = 0
			if state.chatCount > 0 {
				if _, err := hliepinStableClick(ctx, exec, hliepinChatModalParent, hliepinChatCloseSelector, map[string]any{
					"action_name":              "候选人=" + candidateName(candidate) + "，关闭聊天框",
					"wait_for_hidden_selector": hliepinChatModalParent, "wait_timeout": 5000,
				}); err != nil {
					closeErrors = append(closeErrors, fmt.Errorf("关闭猎聘聊天框失败：%w", err))
				}
			}
			if state.drawerCount > 0 {
				if _, err := hliepinStableClick(ctx, exec, hliepinCandidateDrawerParent, hliepinCandidateListClose, map[string]any{
					"action_name":              "候选人=" + candidateName(candidate) + "，关闭联系人列表",
					"wait_for_hidden_selector": hliepinCandidateDrawerParent, "wait_timeout": 5000,
				}); err != nil {
					closeErrors = append(closeErrors, fmt.Errorf("关闭猎聘候选人列表失败：%w", err))
				}
			}
		}
		if len(closeErrors) > 0 {
			return errors.Join(closeErrors...)
		}
		if err := exec.Delay(ctx, "等待猎聘弹层状态稳定", hliepinPanelPollInterval); err != nil {
			return err
		}
	}
	return fmt.Errorf("猎聘弹层收尾超时：候选人=%s，耗时=%s", candidateName(candidate), time.Since(startedAt).Round(time.Millisecond))
}
