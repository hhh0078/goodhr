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
	hliepinContinueButtonSelector = ".ant-btn.ant-btn-default.ant-btn-lg.lp-ant-btn-light"
	hliepinRequestPhoneSelector   = ".im-ui-action-button.action-item.action-phone"
	hliepinRequestWechatSelector  = ".im-ui-action-button.action-item.action-wechat"
	hliepinRequestResumeSelector  = ".im-ui-action-button.action-item.action-resume"
	hliepinChatInputSelector      = "textarea.ant-im-input.im-ui-textarea[placeholder='请输入文字，按Enter键发送']"
	hliepinChatCloseSelector      = ".im-ui-basic-chat-header-modal-close"
	hliepinCandidateListClose     = ".ant-im-drawer-close"
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
	item := candidateItemElement(candidate, cfg)
	if item == nil {
		return fmt.Errorf("猎聘候选人卡片选择器为空，无法继续沟通")
	}
	if _, err := exec.Post(ctx, "/api/v1/page/list-click-by-index", map[string]any{
		"index":       intFromMap(candidate, "card_index"),
		"item":        item,
		"clickTarget": map[string]any{"selector": hliepinContinueButtonSelector},
		"timeout":     10000,
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
	if err := exec.Delay(ctx, "等待猎聘继续沟通弹层", 0.8); err != nil {
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
		if _, err := exec.Post(ctx, "/api/v1/page/click", map[string]any{
			"element": map[string]any{"selector": action.selector},
			"timeout": 5000,
		}); err != nil {
			return fmt.Errorf("点击猎聘“索要%s”失败：%w", action.label, err)
		}
		if err := exec.Delay(ctx, "等待猎聘索要"+action.label+"操作生效", 0.25); err != nil {
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

// closeCandidateInfoPanels 按聊天框、候选人列表的顺序关闭猎聘两个沟通弹层。
func closeCandidateInfoPanels(ctx context.Context, exec platformcore.Executor) error {
	var closeErrors []error
	if _, err := exec.Post(ctx, "/api/v1/page/click", map[string]any{
		"element": map[string]any{"selector": hliepinChatCloseSelector},
		"timeout": 5000,
	}); err != nil {
		closeErrors = append(closeErrors, fmt.Errorf("关闭猎聘聊天框失败：%w", err))
	}
	if err := exec.Delay(ctx, "等待猎聘聊天框关闭", 0.2); err != nil {
		closeErrors = append(closeErrors, err)
	}
	if _, err := exec.Post(ctx, "/api/v1/page/click", map[string]any{
		"element": map[string]any{"selector": hliepinCandidateListClose},
		"timeout": 5000,
	}); err != nil {
		closeErrors = append(closeErrors, fmt.Errorf("关闭猎聘候选人列表失败：%w", err))
	}
	return errors.Join(closeErrors...)
}
