// Package zhaopin 文件作用：提供智联招聘基础筛选和打招呼后索要信息的平台扩展入口。
package zhaopin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
)

const (
	zhaopinChatFooterSelector   = "#im-session-detail-footer"
	zhaopinPhoneButtonSelector  = zhaopinChatFooterSelector + " [zp-stat-id='im_ask_for_the_phone']"
	zhaopinWechatButtonSelector = zhaopinChatFooterSelector + " [zp-stat-id='im_ask_for_wx_open']"
	zhaopinResumeButtonScope    = zhaopinChatFooterSelector + " .session-new-action--left a"
	zhaopinChatInputSelector    = zhaopinChatFooterSelector + " textarea[placeholder='从这里开启对话...']"
	zhaopinChatCloseSelector    = ".im-widget-session__close"
	zhaopinPhoneConfirmSelector = ".km-popover.im-ask-for-contact__popper a[zp-stat-id='render_time_track']"
	zhaopinResumeButtonText     = "要附件简历"
)

// ApplyBasicFilters 保留智联招聘基础筛选入口，当前不执行页面操作。
func (r *Runtime) ApplyBasicFilters(context.Context, platformcore.Executor, cloudapi.PlatformConfig, map[string]any) error {
	return nil
}

// RequestCandidateInfo 打开智联候选人聊天框，按岗位配置索要信息、发送问候语并关闭聊天框。
func (r *Runtime) RequestCandidateInfo(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate, request platformcore.CandidateInfoRequest) (resultErr error) {
	message := strings.TrimSpace(request.GreetMessage)
	if !request.RequestPhone && !request.RequestWechat && !request.RequestResume && message == "" {
		return nil
	}
	payload := zhaopinCandidateVisiblePayload(cfg, candidate)
	payload["debug_stage"] = "request-info-before"
	if _, err := exec.Post(ctx, "/api/v1/boss/candidates/visible", payload); err != nil {
		return fmt.Errorf("定位智联候选人失败：%w", err)
	}
	payload["debug_stage"] = "request-info-open-chat"
	if _, err := exec.Post(ctx, "/api/v1/boss/candidates/greet", payload); err != nil {
		return fmt.Errorf("点击智联候选人“继续沟通”失败：%w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
		defer cancel()
		if err := closeZhaopinChatDialog(cleanupCtx, exec); err != nil {
			exec.Log("warning", "智联索要信息：关闭聊天框失败，错误="+err.Error())
			if resultErr == nil {
				resultErr = err
			}
		}
	}()
	if err := exec.Delay(ctx, "等待智联继续沟通聊天框打开", 1); err != nil {
		return err
	}
	if request.RequestPhone {
		exec.Log("info", "智联索要信息：准备索要手机号")
		if err := clickZhaopinChatElement(ctx, exec, zhaopinPhoneButtonSelector, "要电话"); err != nil {
			return err
		}
		if err := confirmZhaopinPhoneRequest(ctx, exec); err != nil {
			return err
		}
		if err := exec.Delay(ctx, "等待智联手机号索要操作完成", 0.2); err != nil {
			return err
		}
	}
	if request.RequestWechat {
		exec.Log("info", "智联索要信息：准备索要微信")
		if err := clickZhaopinChatElement(ctx, exec, zhaopinWechatButtonSelector, "换微信"); err != nil {
			return err
		}
		if err := exec.Delay(ctx, "等待智联微信索要操作完成", 0.2); err != nil {
			return err
		}
	}
	if request.RequestResume {
		exec.Log("info", "智联索要信息：准备索要附件简历")
		if err := clickZhaopinChatText(ctx, exec, zhaopinResumeButtonScope, zhaopinResumeButtonText); err != nil {
			return err
		}
		if err := exec.Delay(ctx, "等待智联附件简历索要操作完成", 0.2); err != nil {
			return err
		}
	}
	if message != "" {
		exec.Log("info", "智联索要信息：准备发送岗位首次打招呼语")
		if _, err := exec.Post(ctx, "/api/v1/page/type", map[string]any{
			"element": map[string]any{"selector": zhaopinChatInputSelector},
			"text":    message,
			"timeout": 5000,
		}); err != nil {
			return fmt.Errorf("输入智联首次打招呼语失败：%w", err)
		}
		if _, err := exec.Post(ctx, "/api/v1/page/press-key", map[string]any{"key": "Enter"}); err != nil {
			return fmt.Errorf("发送智联首次打招呼语失败：%w", err)
		}
	}
	return nil
}

// clickZhaopinChatElement 在智联聊天框的稳定父级范围内点击指定操作按钮。
func clickZhaopinChatElement(ctx context.Context, exec platformcore.Executor, selector string, label string) error {
	if _, err := exec.Post(ctx, "/api/v1/page/click", map[string]any{
		"element": map[string]any{"selector": selector},
		"timeout": 5000,
	}); err != nil {
		return fmt.Errorf("点击智联“%s”失败：%w", label, err)
	}
	return nil
}

// clickZhaopinChatText 在智联聊天框底部限定范围内按精确文字点击操作按钮。
func clickZhaopinChatText(ctx context.Context, exec platformcore.Executor, scope string, text string) error {
	if _, err := exec.Post(ctx, "/api/v1/page/click-by-text", map[string]any{
		"element": map[string]any{"selector": scope},
		"text":    text,
		"exact":   true,
		"timeout": 5000,
	}); err != nil {
		return fmt.Errorf("点击智联“%s”失败：%w", text, err)
	}
	return nil
}

// confirmZhaopinPhoneRequest 查找手机号方式弹层中的“向对方索要”，存在则点击，不存在则直接跳过。
func confirmZhaopinPhoneRequest(ctx context.Context, exec platformcore.Executor) error {
	if err := exec.Delay(ctx, "等待智联手机号索要方式弹层", 0.25); err != nil {
		return err
	}
	result, err := exec.Post(ctx, "/api/v1/page/find-elements", map[string]any{
		"element":      map[string]any{"selector": zhaopinPhoneConfirmSelector},
		"visible_only": true,
		"max_items":    1,
	})
	if err != nil {
		return fmt.Errorf("查找智联手机号索要确认项失败：%w", err)
	}
	items := mapList(workerData(result, "items"))
	if len(items) == 0 {
		exec.Log("info", "智联索要信息：未出现手机号索要方式弹层，跳过确认")
		return nil
	}
	payload := map[string]any{"timeout": 5000}
	if ref := firstNonEmpty(stringFromMap(items[0], "element_ref"), stringFromMap(items[0], "ref")); ref != "" {
		payload["element_ref"] = ref
	} else {
		payload["element"] = map[string]any{"selector": zhaopinPhoneConfirmSelector}
	}
	if _, err := exec.Post(ctx, "/api/v1/page/click", payload); err != nil {
		return fmt.Errorf("确认智联向候选人索要手机号失败：%w", err)
	}
	return nil
}

// closeZhaopinChatDialog 关闭智联“继续沟通”打开的单个聊天框。
func closeZhaopinChatDialog(ctx context.Context, exec platformcore.Executor) error {
	if _, err := exec.Post(ctx, "/api/v1/page/click", map[string]any{
		"element": map[string]any{"selector": zhaopinChatCloseSelector},
		"timeout": 5000,
	}); err != nil {
		return fmt.Errorf("关闭智联聊天框失败：%w", err)
	}
	return nil
}
