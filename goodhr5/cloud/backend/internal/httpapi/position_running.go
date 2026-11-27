// Package httpapi 本文件在页面开始准备岗位前，只读检查当前账号的云端运行岗位，不改变任何任务状态。
package httpapi

import "net/http"

// Running 返回当前登录账号正在运行的岗位；最终启动仍由 ClaimPositionStart 原子防止并发抢占。
func (s *PositionExecutionService) Running(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writePositionStartError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "这个接口只支持 GET")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	tenantID, _ := s.getTenantInfo(session.Email)
	positions, err := s.store.ListPositions(tenantID, session.Email, false)
	if err != nil {
		writePositionStartError(w, http.StatusServiceUnavailable, "RUNNING_POSITION_CHECK_FAILED", "运行状态暂时没查清楚，请稍后再试。")
		return
	}
	for _, position := range positions {
		if position.UserEmail == session.Email && position.Status == "running" {
			writeJSON(w, http.StatusOK, map[string]any{"running": true, "position": map[string]string{"id": position.ID, "name": position.Name, "platform_id": position.PlatformID}})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"running": false})
}
