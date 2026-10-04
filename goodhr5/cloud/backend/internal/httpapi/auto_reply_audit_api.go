// Package httpapi 本文件负责自动回复 AI 总记录和岗位、公司修改建议的前端查询与审核接口。
package httpapi

import (
	"net/http"
	"strconv"
	"strings"
)

// Audit 返回当前团队最近的 AI 输入、返回、工具调用和结果总记录。
func (s *AutoReplyService) Audit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "AI 总记录这里只支持查看")
		return
	}
	requestContext, ok := s.currentRequestContext(w, r, false, false)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.store.ListAutoReplyAudit(r.Context(), requestContext.Tenant.ID, strings.TrimSpace(r.URL.Query().Get("position_id")), limit)
	if err != nil {
		writeAutoReplyInternalError(w, "AUTO_REPLY_AUDIT_LOAD_FAILED", "AI 总记录暂时没读出来，请稍后再试", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "records": items})
}

// Suggestions 返回当前团队的自动回复配置修改建议。
func (s *AutoReplyService) Suggestions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "配置建议这里只支持查看")
		return
	}
	requestContext, ok := s.currentRequestContext(w, r, false, false)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.store.ListAutoReplyConfigSuggestions(r.Context(), requestContext.Tenant.ID, strings.TrimSpace(r.URL.Query().Get("status")), limit)
	if err != nil {
		writeAutoReplyStoreError(w, err, "配置建议暂时没读出来")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "suggestions": items})
}

// Suggestion 审核一条岗位或公司资料修改建议，审核不会自动改原配置。
func (s *AutoReplyService) Suggestion(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/auto-reply/suggestions/"), "/")
	parts := strings.Split(path, "/")
	if r.Method != http.MethodPost || len(parts) != 2 || parts[1] != "review" || strings.TrimSpace(parts[0]) == "" {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "这个配置建议地址暂时不支持这种操作")
		return
	}
	requestContext, ok := s.currentRequestContext(w, r, false, false)
	if !ok {
		return
	}
	var payload struct {
		Status string `json:"status"`
	}
	if err := decodeAutoReplyJSON(w, r, &payload); err != nil {
		return
	}
	item, err := s.store.ReviewAutoReplyConfigSuggestion(r.Context(), requestContext.Tenant.ID, requestContext.Session.Email, parts[0], strings.TrimSpace(payload.Status))
	if err != nil {
		writeAutoReplyStoreError(w, err, "配置建议没审核成功")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "suggestion": item})
}
