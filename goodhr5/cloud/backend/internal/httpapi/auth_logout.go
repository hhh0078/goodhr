// Package httpapi 文件作用：撤销当前普通网页会话，使关闭的本地程序重启后也不能恢复已退出的凭证。
package httpapi

import (
	"errors"
	"net/http"
)

// Logout 原子撤销请求自身的普通网页会话，不影响同账号其他登录及独立插件会话。
func (s *AuthService) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" {
		writeError(w, http.StatusUnauthorized, "登录状态已经失效，请重新登录")
		return
	}
	_, err := s.store.ReplaceSession(token, "", 0, "")
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "登录状态已经更新或退出")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "退出暂时没完成，请重试")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
