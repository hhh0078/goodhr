// Package cloud 本文件连接服务器自动回复编排服务，本机仅领取浏览器动作和提交观察结果。
package cloud

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"goodhr5/local-agent-go-new/internal/system/failurediag"
)

// AutoReplyFailureNotice 只把服务器已经审计的安全故障分类交给发起设备，排障日志不携带候选人资料。
type AutoReplyFailureNotice struct {
	Status     string               `json:"status"`
	Sequence   int64                `json:"sequence"`
	PositionID string               `json:"position_id"`
	Step       string               `json:"step"`
	At         time.Time            `json:"at"`
	Details    []failurediag.Detail `json:"failure_diagnostics"`
}

// AutoReplyRuntimeRequest 在已认证设备下调用服务器运行接口，不走本机业务仓储。
func (c *Client) AutoReplyRuntimeRequest(ctx context.Context, credentials AgentCredentials, action string, input, output any) error {
	switch action {
	case "start", "stop", "status", "next", "complete", "retry-reports":
	default:
		return fmt.Errorf("未知云端自动回复操作")
	}
	if err := validateAutoReplyCredentials(credentials); err != nil {
		return err
	}
	return c.doWithMachineID(ctx, http.MethodPost, "/api/auto-reply/runtime/"+action, credentials.Token, credentials.MachineID, input, output)
}
