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
)

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
	if _, err := hliepinStableClick(ctx, exec, rowParent, hliepinContinueButtonSelector, map[string]any{
		"expected_text": "继续沟通", "exact_text": true,
		"wait_for_selector": hliepinChatModalParent, "wait_timeout": 5000,
	}); err != nil {
		return fmt.Errorf("点击猎聘“继续沟通”失败：%w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := closeCandidateInfoPanels(cleanupCtx, exec); err != nil {
			exec.Log("warning", "猎聘索要信息：关闭聊天弹层失败，错误="+err.Error())
			if resultErr == nil {
				resultErr = err
			}
		}
	}()
	if err := exec.Delay(ctx, "等待猎聘继续沟通弹层", 1); err != nil {
		return err
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

// confirmCandidateInfoRequestIfPresent 检查猎聘索要确认弹框，存在则点击确定，不存在则直接继续。
func confirmCandidateInfoRequestIfPresent(ctx context.Context, exec platformcore.Executor, label string) error {
	if err := exec.Delay(ctx, "等待猎聘索要"+label+"确认弹框", 1); err != nil {
		return err
	}
	result, err := exec.Post(ctx, "/api/v1/page/find-elements", map[string]any{
		"element":      map[string]any{"selector": hliepinRequestConfirmDialog},
		"visible_only": true,
		"max_items":    1,
	})
	if err != nil {
		return fmt.Errorf("查找猎聘索要%s确认弹框失败：%w", label, err)
	}
	items := mapList(workerData(result, "items"))
	if len(items) == 0 || !strings.Contains(stringFromMap(items[0], "text"), "索要") {
		exec.Log("info", "猎聘索要信息：未出现索要"+label+"确认弹框，跳过确认")
		return nil
	}
	if _, err := hliepinStableClick(ctx, exec, hliepinRequestConfirmDialog, hliepinRequestConfirmButton, map[string]any{
		"expected_text": "确定", "exact_text": true,
		"wait_for_hidden_selector": hliepinRequestConfirmDialog, "wait_timeout": 5000,
	}); err != nil {
		return fmt.Errorf("确认猎聘向候选人索要%s失败：%w", label, err)
	}
	if err := exec.Delay(ctx, "等待猎聘索要"+label+"确认完成", 0.2); err != nil {
		return err
	}
	return nil
}

// closeCandidateInfoPanels 按聊天框、候选人列表的顺序关闭猎聘两个沟通弹层。
func closeCandidateInfoPanels(ctx context.Context, exec platformcore.Executor) error {
	var closeErrors []error
	if _, err := hliepinStableClick(ctx, exec, hliepinChatModalParent, hliepinChatCloseSelector, map[string]any{
		"wait_for_hidden_selector": hliepinChatModalParent, "wait_timeout": 5000,
	}); err != nil {
		closeErrors = append(closeErrors, fmt.Errorf("关闭猎聘聊天框失败：%w", err))
	}
	if err := exec.Delay(ctx, "等待猎聘聊天框关闭", 0.2); err != nil {
		closeErrors = append(closeErrors, err)
	}
	if _, err := hliepinStableClick(ctx, exec, hliepinCandidateDrawerParent, hliepinCandidateListClose, map[string]any{
		"wait_for_hidden_selector": hliepinCandidateDrawerParent, "wait_timeout": 5000,
	}); err != nil {
		closeErrors = append(closeErrors, fmt.Errorf("关闭猎聘候选人列表失败：%w", err))
	}
	return errors.Join(closeErrors...)
}
