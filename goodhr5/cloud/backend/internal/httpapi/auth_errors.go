// 本文件统一区分登录凭证失效与认证存储故障，避免临时故障导致用户被退出。
package httpapi

import (
	"errors"
	"net/http"
)

var ErrSessionRequired = errors.New("登录凭证缺失")

// writeAuthError 返回稳定的认证错误码；临时故障返回 503 并保留客户端登录态。
func writeAuthError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "登录状态暂时没查到，请稍后重试，无需重新登录"
	if errors.Is(err, ErrSessionRequired) {
		status, code, message = http.StatusUnauthorized, "SESSION_REQUIRED", "请重新登录后继续"
	} else if errors.Is(err, ErrNotFound) {
		status, code, message = http.StatusUnauthorized, "SESSION_EXPIRED", "登录状态已经失效，请重新登录"
	}
	// 保留字符串 error 字段，兼容现有前端及本地程序；code 用于准确识别退出条件。
	writeJSON(w, status, map[string]any{"ok": false, "code": code, "error": message})
}
