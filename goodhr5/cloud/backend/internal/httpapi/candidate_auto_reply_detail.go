// 本文件在原有简历可见范围内读取本地同步的结构化自动回复报告，不读取聊天或附件。
package httpapi

import (
	"net/http"
	"strings"
)

// AutoReplyDetail 复用简历权限校验后按需返回报告，其余本地资料返回明确提示。
func (s *CandidateService) AutoReplyDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	scope, ok := s.libraryScope(w, r)
	if !ok {
		return
	}
	candidateID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/candidates/"), "/auto-reply")
	if _, err := s.store.GetPositionCandidate(scope.TenantID, candidateID, "", scope.UserEmail, scope.IsAdmin); err != nil {
		writeAutoReplyStoreError(w, err, "这份简历暂时没读出来")
		return
	}
	section := strings.TrimSpace(r.URL.Query().Get("section"))
	if section != "recommendations" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "auto_reply": map[string]any{"confirmation_items": []any{}, "local_only": true, "message": "详细沟通、附件和确认记录保存在运行这次任务的本机"}})
		return
	}
	items := []CandidateAutoReplyReport{}
	if s.autoReply != nil {
		var err error
		items, err = s.autoReply.ListCandidateAutoReplyReports(r.Context(), scope.TenantID, candidateID)
		if err != nil {
			writeAutoReplyStoreError(w, err, "自动回复报告暂时没读出来")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "auto_reply": map[string]any{"recommendations": items}})
}
