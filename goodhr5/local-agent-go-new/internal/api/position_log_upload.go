// Package api 文件作用：允许岗位页在复制排障链接前主动同步当前本地日志。
package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// handlePositionLogUpload 校验登录凭证并请求任务层批量上传岗位日志。
func (s *Server) handlePositionLogUpload(w http.ResponseWriter, r *http.Request) {
	positionID := strings.TrimSpace(r.PathValue("position_id"))
	var body struct {
		Token string `json:"token"`
	}
	if positionID == "" || decodeJSON(w, r, &body) != nil || strings.TrimSpace(body.Token) == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", fmt.Errorf("岗位编号或登录凭证还没有准备好"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	if err := s.runner.UploadPositionLogs(ctx, body.Token, positionID); err != nil {
		writeError(w, http.StatusBadGateway, "LOG_UPLOAD_FAILED", err)
		return
	}
	writeSuccess(w, http.StatusOK, struct {
		Uploaded bool `json:"uploaded"`
	}{Uploaded: true})
}
