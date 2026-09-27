// 本文件负责创建七天有效的只读岗位排障链接，并读取创建时固定的脱敏日志。
package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
)

const positionLogShareTTL = 7 * 24 * time.Hour

// Share 仅允许岗位所属账号生成一次性随机链接，新的链接会替换旧链接。
func (s *PositionLogService) Share(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	positionID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/positions/"), "/logs/share")
	if positionID == "" || strings.Contains(positionID, "/") {
		writeError(w, http.StatusBadRequest, "position id is required")
		return
	}
	tenantID, isAdmin := s.getTenantInfo(session.Email)
	if _, err := s.positions.PositionByID(tenantID, session.Email, positionID, isAdmin); errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "position not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load position")
		return
	}
	store, ok := s.logStore.(PositionLogShareStore)
	if !ok {
		writeError(w, http.StatusInternalServerError, "share store unavailable")
		return
	}
	logs, _, err := s.logStore.ListPositionLogs(tenantID, session.Email, positionID, isAdmin, PositionLogQuery{Limit: maxPositionLogsPerPosition})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load position logs")
		return
	}
	if len(logs) == 0 {
		writeError(w, http.StatusConflict, "no position logs available")
		return
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create share link")
		return
	}
	token := hex.EncodeToString(secret)
	digest := sha256.Sum256([]byte(token))
	expiresAt := time.Now().UTC().Add(positionLogShareTTL)
	if err := store.SavePositionLogShare(hex.EncodeToString(digest[:]), positionID, session.Email, logs, expiresAt); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save share link")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "token": token, "expires_at": expiresAt, "count": len(logs)})
}

// PublicShare 通过不可猜测的令牌读取固定快照，不要求作者登录。
func (s *PositionLogService) PublicShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	token := strings.TrimPrefix(r.URL.Path, "/api/public/position-log-shares/")
	if len(token) != 64 {
		writeError(w, http.StatusNotFound, "share link not found")
		return
	}
	if _, err := hex.DecodeString(token); err != nil {
		writeError(w, http.StatusNotFound, "share link not found")
		return
	}
	digest := sha256.Sum256([]byte(token))
	store, ok := s.logStore.(PositionLogShareStore)
	if !ok {
		writeError(w, http.StatusInternalServerError, "share store unavailable")
		return
	}
	logs, expiresAt, err := store.LoadPositionLogShare(hex.EncodeToString(digest[:]))
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "share link expired or replaced")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load share link")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "logs": publicPositionLogs(logs), "expires_at": expiresAt, "count": len(logs)})
}
