// 本文件负责接收浏览器异常退出日志，并向超级管理员提供全账号日志查询。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const maxBrowserEventsPerAccount = 1000

var browserEventIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var browserPositionIDPattern = regexp.MustCompile(`^[a-fA-F0-9-]{36}$`)

// BrowserEventService 保存异常退出事件，并查询岗位与无岗位日志。
type BrowserEventService struct {
	auth *AuthService
	db   *sql.DB
}

// NewBrowserEventService 创建浏览器异常退出日志服务。
func NewBrowserEventService(auth *AuthService, db *sql.DB) *BrowserEventService {
	return &BrowserEventService{auth: auth, db: db}
}

// Upload 接收最多二百条事件，并由登录态确定账号和岗位归属。
func (s *BrowserEventService) Upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	if s.db == nil {
		writeError(w, http.StatusServiceUnavailable, "log storage unavailable")
		return
	}
	var request struct {
		Events []struct {
			EventID    string          `json:"event_id"`
			PositionID string          `json:"position_id"`
			Payload    json.RawMessage `json:"payload"`
			CreatedAt  time.Time       `json:"created_at"`
		} `json:"events"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid events")
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid events")
		return
	}
	if len(request.Events) == 0 || len(request.Events) > 200 {
		writeError(w, http.StatusBadRequest, "events count must be 1 to 200")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	userID, err := ensureUserID(ctx, s.db, session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve account")
		return
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save events")
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save events")
		return
	}
	for _, item := range request.Events {
		if !browserEventIDPattern.MatchString(item.EventID) || len(item.Payload) == 0 || len(item.Payload) > 16<<10 || !json.Valid(item.Payload) || item.CreatedAt.IsZero() {
			writeError(w, http.StatusBadRequest, "invalid browser event")
			return
		}
		var payload struct {
			Action   string `json:"action"`
			Category string `json:"category"`
			IsError  bool   `json:"is_error"`
		}
		if json.Unmarshal(item.Payload, &payload) != nil || !validRuntimeEvent(payload.Action, payload.Category, payload.IsError) {
			writeError(w, http.StatusBadRequest, "invalid browser exit event")
			return
		}
		var positionID any
		if browserPositionIDPattern.MatchString(item.PositionID) {
			var ownedID string
			if err := tx.QueryRowContext(ctx, `SELECT id FROM positions WHERE id=$1 AND user_id=$2`, item.PositionID, userID).Scan(&ownedID); err == nil {
				positionID = ownedID
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO browser_events(event_id,user_id,position_id,payload,created_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(event_id) DO NOTHING`,
			item.EventID, userID, positionID, string(item.Payload), item.CreatedAt.UTC()); err != nil {
			log.Printf("保存浏览器退出日志失败：%v", err)
			writeError(w, http.StatusInternalServerError, "failed to save events")
			return
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM browser_events WHERE user_id=$1 AND event_id NOT IN (SELECT event_id FROM browser_events WHERE user_id=$1 ORDER BY created_at DESC,event_id DESC LIMIT $2)`, userID, maxBrowserEventsPerAccount); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to trim events")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save events")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "accepted": len(request.Events)})
}

// AdminLogs 根据类别、账号、岗位名称和时间范围查询所有账号的日志。
func (s *BrowserEventService) AdminLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	if !s.auth.IsSuperAdmin(session.Email) {
		writeError(w, http.StatusForbidden, "super admin access required")
		return
	}
	if s.db == nil {
		writeError(w, http.StatusServiceUnavailable, "log storage unavailable")
		return
	}
	query := r.URL.Query()
	category := strings.TrimSpace(query.Get("category"))
	if category != "" && category != "运行" && category != "错误" && category != "异常退出" {
		writeError(w, http.StatusBadRequest, "invalid category")
		return
	}
	page, _ := strconv.Atoi(query.Get("page"))
	if page < 1 {
		page = 1
	}
	if page > 10000 {
		page = 10000
	}
	pageSize, _ := strconv.Atoi(query.Get("page_size"))
	if pageSize < 1 {
		pageSize = 50
	}
	if pageSize > 100 {
		pageSize = 100
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	cte := `WITH all_logs AS (
		SELECT pl.id::text AS id, u.email AS account, p.name AS position_name, pl.position_id::text AS position_id,
		CASE WHEN pl.message LIKE '%"category":"异常退出"%' THEN '异常退出' WHEN pl.level='error' THEN '错误' ELSE '运行' END AS category,
		pl.message AS message, pl.created_at AS created_at
		FROM position_logs pl JOIN users u ON u.id=pl.user_id LEFT JOIN positions p ON p.id=pl.position_id
		UNION ALL
		SELECT be.event_id AS id, u.email AS account, p.name AS position_name, be.position_id::text AS position_id,
		CASE WHEN be.payload->>'category'='异常退出' THEN '异常退出' WHEN be.payload->>'category'='错误' THEN '错误' ELSE '运行' END AS category, be.payload::text AS message, be.created_at AS created_at
		FROM browser_events be JOIN users u ON u.id=be.user_id LEFT JOIN positions p ON p.id=be.position_id
	), filtered AS (SELECT * FROM all_logs WHERE 1=1`
	args := []any{}
	addFilter := func(condition string, value any) {
		args = append(args, value)
		cte += fmt.Sprintf(" AND "+condition, len(args))
	}
	if category != "" {
		addFilter("category=$%d", category)
	}
	if account := strings.TrimSpace(query.Get("account")); account != "" {
		addFilter("account ILIKE '%%' || $%d || '%%'", account)
	}
	if position := strings.TrimSpace(query.Get("position")); position != "" {
		addFilter("COALESCE(position_name,'') ILIKE '%%' || $%d || '%%'", position)
	}
	for _, bound := range []struct{ key, comparison string }{{"from", ">="}, {"to", "<="}} {
		if value := strings.TrimSpace(query.Get(bound.key)); value != "" {
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid time range")
				return
			}
			addFilter("created_at "+bound.comparison+" $%d", parsed)
		}
	}
	cte += `)`
	var total int
	if err := s.db.QueryRowContext(ctx, cte+` SELECT COUNT(*) FROM filtered`, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count logs")
		return
	}
	listArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)
	rows, err := s.db.QueryContext(ctx, cte+fmt.Sprintf(` SELECT id,account,position_name,position_id,category,message,created_at FROM filtered ORDER BY created_at DESC,id DESC LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2), listArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list logs")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0, pageSize)
	for rows.Next() {
		var id, account, category, message string
		var positionName, positionID sql.NullString
		var createdAt time.Time
		if err := rows.Scan(&id, &account, &positionName, &positionID, &category, &message, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read logs")
			return
		}
		item := map[string]any{"id": id, "account": account, "position_name": positionName.String, "position_id": positionID.String, "category": category, "message": message, "created_at": createdAt}
		var event map[string]any
		if json.Unmarshal([]byte(message), &event) == nil {
			item["event"] = event
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read logs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "logs": items, "total": total, "page": page, "page_size": pageSize})
}

// validRuntimeEvent 只接受本地程序和浏览器生命周期的运行或退出事件。
func validRuntimeEvent(action, category string, isError bool) bool {
	switch action {
	case "browser.exit", "worker.exit", "agent.previous_exit":
		return category == "异常退出" && isError
	case "browser.start", "browser.stop", "agent.start", "agent.exit":
		return (category == "运行" && !isError) || (action == "agent.exit" && category == "异常退出" && isError) || ((action == "browser.start" || action == "browser.stop") && category == "错误" && isError)
	default:
		return false
	}
}
