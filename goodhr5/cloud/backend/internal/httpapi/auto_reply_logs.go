// Package httpapi 本文件提供按本人账号隔离的自动回复独立日志查询，只返回经过脱敏的排错字段。
package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// AutoReplyLogs 读取当前登录成员的最近千条日志；页码不会参与越权查询，未登录和插件凭证均禁止访问。
func (s *AutoReplyService) AutoReplyLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAutoReplyError(w, 405, "METHOD_NOT_ALLOWED", "请使用读取方式查看运行日志")
		return
	}
	access, ok := s.currentRequestContext(w, r, false, false)
	if !ok {
		return
	}
	if access.Session.Kind == extensionSessionKind {
		writeAutoReplyError(w, 403, "SESSION_NOT_ALLOWED", "请从管理页面查看运行日志")
		return
	}
	before := int64(0)
	if raw := r.URL.Query().Get("before"); raw != "" {
		var err error
		before, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || before < 1 {
			writeAutoReplyError(w, 400, "INVALID_CURSOR", "日志页码没有读明白，请重新打开面板")
			return
		}
	}
	rows, err := s.store.db.QueryContext(r.Context(), `SELECT l.sequence,l.event_id,l.payload FROM auto_reply_logs l JOIN users u ON u.tenant_id=l.tenant_id AND u.id=l.user_id WHERE l.tenant_id=$1 AND u.email=$2 AND ($3::bigint=0 OR l.sequence<$3) ORDER BY l.sequence DESC LIMIT 51`, access.Tenant.ID, access.Session.Email, before)
	if err != nil {
		writeAutoReplyStoreError(w, err, "运行日志暂时没读出来，可以重试")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0, 50)
	next := ""
	for rows.Next() {
		var sequence int64
		var id string
		var raw json.RawMessage
		if err = rows.Scan(&sequence, &id, &raw); err != nil {
			break
		}
		if len(items) == 50 {
			next = strconv.FormatInt(int64(items[49]["cursor"].(int64)), 10)
			break
		}
		var event map[string]json.RawMessage
		if err = json.Unmarshal(raw, &event); err != nil {
			break
		}
		item := map[string]any{"id": id, "cursor": sequence}
		for _, key := range []string{"run_id", "position_id", "conversation_id", "title", "status", "description", "occurred_at", "duration_ms", "failure_diagnostics", "ai_request_attempt", "error_detail"} {
			if value, exists := event[key]; exists {
				var decoded any
				if json.Unmarshal(value, &decoded) == nil {
					item[key] = sanitizeDiagnosticValue(decoded)
				}
			}
		}
		items = append(items, item)
	}
	if err == nil {
		err = rows.Err()
	}
	if err != nil {
		writeAutoReplyStoreError(w, err, "运行日志没读完整，可以重试")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "items": items, "next_cursor": next, "retention_limit": 1000})
}
