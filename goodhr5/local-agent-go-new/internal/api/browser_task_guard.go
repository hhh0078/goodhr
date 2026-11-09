// Package api 本文件统一保护设备浏览器，云端自动回复、普通任务与开窗操作不能并发争用同一设备。
package api

import (
	"fmt"
	"net/http"
)

// beginIdleBrowserOperation 原子保留一次浏览器变更入口；成功后调用方必须在退出前释放操作锁。
func (s *Server) beginIdleBrowserOperation(w http.ResponseWriter) bool {
	if !s.browserOperationMu.TryLock() {
		writeError(w, http.StatusConflict, "BROWSER_OPERATION_PENDING", fmt.Errorf("浏览器正在准备另一项操作，等它完成后再试"))
		return false
	}
	s.cloudReplyMu.Lock()
	cloudActive := s.cloudReplyActive
	s.cloudReplyMu.Unlock()
	if cloudActive {
		s.browserOperationMu.Unlock()
		writeError(w, http.StatusConflict, "CLOUD_DEVICE_BUSY", fmt.Errorf("自动回复正在使用浏览器，请先停止后再运行其他操作"))
		return false
	}
	if s.runner != nil && s.runner.HasActive() {
		s.browserOperationMu.Unlock()
		writeError(w, http.StatusConflict, "TASK_RUNNING", fmt.Errorf("任务正在使用浏览器，请先安全停止任务"))
		return false
	}
	return true
}
