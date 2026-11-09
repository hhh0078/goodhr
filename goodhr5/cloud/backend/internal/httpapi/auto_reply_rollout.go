// 本文件集中关闭尚未开放的自动回复执行入口，保留停止、回执、历史和已有配置编辑。
package httpapi

import (
	"errors"
	"net/http"
)

// 自动回复尚未发布；开放需要经过正式代码发布，不提供环境变量或账号名单绕过。
const autoReplyPublicRolloutOpen = false
const autoReplyRolloutMessage = "功能灰度测试中，暂不开放"
const autoReplyRolloutCode = "AUTO_REPLY_ROLLOUT_CLOSED"

var errAutoReplyRolloutClosed = errors.New(autoReplyRolloutMessage)

// rejectAutoReplyRollout 在公开执行入口统一返回灰度提示，不接触运行任务、设备命令或模型。
func rejectAutoReplyRollout(w http.ResponseWriter) bool {
	if autoReplyPublicRolloutOpen {
		return false
	}
	writeAutoReplyError(w, http.StatusForbidden, autoReplyRolloutCode, autoReplyRolloutMessage)
	return true
}
