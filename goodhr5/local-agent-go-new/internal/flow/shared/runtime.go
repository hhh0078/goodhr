// Package shared 提供两个主流程共同使用的运行中登录检查和通用页面浮层辅助。
package shared

import (
	"context"
	"fmt"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// EnsureCloudSession 在任务运行中检查登录态；临时网络错误只记警告，明确失效才停止任务。
func EnsureCloudSession(ctx context.Context, cloudClient *cloud.Client, token string, taskID string, flow string, logger Logger) error {
	_, err := cloudClient.ValidateSession(ctx, token)
	if err == nil {
		return nil
	}
	if contextError := ctx.Err(); contextError != nil {
		return contextError
	}
	if cloud.IsAuthExpired(err) {
		return fmt.Errorf("云端登录状态已经失效，任务先停一下：%w", err)
	}
	if logger != nil {
		logger.Step(taskID, flow, "check_cloud_session", "warning", time.Now(), err)
	}
	return nil
}

// ShowThinkingOverlay 同步显示通用 AI 思考浮层，显示失败只记警告。
func ShowThinkingOverlay(ctx context.Context, browser *client.Client, taskID string, flow string, title string, subtitle string, message string, logger Logger) bool {
	_, err := browser.ShowOverlay(ctx, contract.OverlayShowRequest{
		OverlayID: "goodhr-ai-thinking",
		Title:     title,
		Subtitle:  subtitle,
		Message:   message,
		Level:     "info",
		MaxAgeMS:  10 * 60 * 1000,
	})
	if err != nil {
		if logger != nil {
			logger.Step(taskID, flow, "show_ai_overlay", "warning", time.Now(), err)
		}
		return false
	}
	return true
}

// CloseThinkingOverlay 同步关闭通用 AI 思考浮层，关闭失败只记警告。
func CloseThinkingOverlay(ctx context.Context, browser *client.Client, taskID string, flow string, logger Logger) {
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if _, err := browser.CloseOverlay(closeCtx, contract.OverlayCloseRequest{OverlayID: "goodhr-ai-thinking"}); err != nil && logger != nil {
		logger.Step(taskID, flow, "close_ai_overlay", "warning", time.Now(), err)
	}
}
