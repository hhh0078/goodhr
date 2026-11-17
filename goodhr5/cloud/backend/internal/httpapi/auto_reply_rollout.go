// 本文件统一限制自动回复测试开放的平台，保留停止、回执、历史和已有配置编辑。
package httpapi

import (
	"errors"
	"net/http"
	"strings"
)

const autoReplyRolloutMessage = "这个平台暂未开放自动回复，目前仅支持 BOSS直聘、猎聘企业端和智联招聘。"
const autoReplyRolloutCode = "AUTO_REPLY_ROLLOUT_CLOSED"

var errAutoReplyRolloutClosed = errors.New(autoReplyRolloutMessage)

// autoReplyPlatformOpen 仅允许已确认开放的三个平台，空值和未知平台默认拒绝。
func autoReplyPlatformOpen(platformID string) bool {
	switch strings.ToLower(strings.TrimSpace(platformID)) {
	case "boss", "liepin", "zhaopin":
		return true
	default:
		return false
	}
}

// rejectAutoReplyRollout 根据已核对的平台返回测试开放范围，不接触设备命令或模型。
func rejectAutoReplyRollout(w http.ResponseWriter, platformID string) bool {
	if autoReplyPlatformOpen(platformID) {
		return false
	}
	writeAutoReplyError(w, http.StatusForbidden, autoReplyRolloutCode, autoReplyRolloutMessage)
	return true
}
