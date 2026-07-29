// Package shared 提供两个主流程共同使用的运行中云端登录检查。
package shared

import (
	"context"
	"fmt"
	"time"

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
