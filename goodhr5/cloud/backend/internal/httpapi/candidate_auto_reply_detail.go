// 本文件按简历权限从云端查询沟通资料及岗位推荐报告。
package httpapi

import (
	"net/http"
	"strings"
)

// AutoReplyDetail 复用简历权限校验后按需分页读取云端聊天或推荐报告。
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
	if section == "history" || section == "" || section == "confirmations" {
		if scope.IsExtension {
			writeAutoReplyError(w, 403, "SESSION_NOT_ALLOWED", "请从管理页面查看沟通记录")
			return
		}
		if s.autoReply == nil {
			writeAutoReplyError(w, 503, "STORAGE_UNAVAILABLE", "云端沟通记录暂时无法连接")
			return
		}
		history, err := s.autoReply.candidateHistory(r.Context(), scope, candidateID, r.URL.Query())
		if err != nil {
			writeAutoReplyStoreError(w, err, "沟通记录暂时没读出来")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "auto_reply": history})
		return
	}
	if section != "recommendations" {
		writeAutoReplyError(w, 400, "UNKNOWN_SECTION", "请选择沟通记录或推荐报告")
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
