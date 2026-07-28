// Package api 文件作用：提供本地程序更新状态和启动接口。
package api

import (
	"fmt"
	"net/http"

	"goodhr5/local-agent-go-new/internal/updater"
)

// handleAppUpdateStatus 返回本地程序更新进度。
func (s *Server) handleAppUpdateStatus(w http.ResponseWriter, _ *http.Request) {
	if s.updater == nil {
		writeError(w, http.StatusServiceUnavailable, "UPDATER_NOT_READY", fmt.Errorf("本地更新能力还没准备好"))
		return
	}
	writeSuccess(w, http.StatusOK, s.updater.Progress())
}

// handleAppUpdateStart 校验更新参数并异步下载、启动安装包。
func (s *Server) handleAppUpdateStart(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		writeError(w, http.StatusServiceUnavailable, "UPDATER_NOT_READY", fmt.Errorf("本地更新能力还没准备好"))
		return
	}
	var request updater.Request
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err)
		return
	}
	progress, err := s.updater.Start(request)
	if err != nil {
		writeError(w, http.StatusConflict, "APP_UPDATE_FAILED", err)
		return
	}
	writeSuccess(w, http.StatusAccepted, progress)
}
