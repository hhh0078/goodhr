// cookie HTTP API
package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

type CookieService struct {
	auth        *AuthService
	store       CookieStore
	tenantStore TenantStore
	capture     *CookieCapture
}

func NewCookieService(auth *AuthService, store CookieStore, tenantStore TenantStore) *CookieService {
	return &CookieService{auth: auth, store: store, tenantStore: tenantStore, capture: NewCookieCapture(store)}
}

func (s *CookieService) currentTenant(w http.ResponseWriter, r *http.Request) (string, bool) {
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, 401, "unauthorized")
		return "", false
	}
	t, err := s.tenantStore.GetOrCreateTenant(session.Email)
	if err != nil {
		writeError(w, 500, "failed to get tenant")
		return "", false
	}
	return t.ID, true
}

// List Cookies
func (s *CookieService) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method not allowed")
		return
	}
	tenantID, ok := s.currentTenant(w, r)
	if !ok {
		return
	}
	items, err := s.store.List(tenantID)
	if err != nil {
		writeError(w, 500, "failed to list cookies")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "cookies": items})
}

// Create Cookie (创建记录，状态=capturing，后续由任务处理)
func (s *CookieService) Create(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, 401, "unauthorized")
		return
	}
	tenantID, ok := s.currentTenant(w, r)
	if !ok {
		return
	}

	var req struct {
		PlatformID   string `json:"platform_id"`
		DisplayName  string `json:"display_name"`
		AgentBaseURL string `json:"agent_base_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid json")
		return
	}
	if req.PlatformID == "" {
		writeError(w, 400, "platform_id required")
		return
	}

	rec, err := s.store.Create(CookieRecord{
		TenantID: tenantID, UserID: session.Email, PlatformID: req.PlatformID,
		DisplayName: req.DisplayName, CookieType: "folder", Status: "capturing",
	})
	if err != nil {
		log.Printf("[cookies] create failed: %v", err)
		writeError(w, 500, "failed to create cookie")
		return
	}
	// 启动异步捕获流程
	agentBaseURL := strings.TrimSpace(r.Header.Get("X-GoodHR-Agent-BaseURL"))
	if agentBaseURL == "" {
		agentBaseURL = strings.TrimSpace(req.AgentBaseURL)
	}
	captureStarted := false
	if agentBaseURL != "" {
		s.capture.Capture(rec.ID, tenantID, req.PlatformID, agentBaseURL, rec.ID, nil)
		captureStarted = true
	}
	writeJSON(w, 200, map[string]any{"ok": true, "cookie": rec, "capture_started": captureStarted})
}

func (s *CookieService) Claim(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	tenantID, ok := s.currentTenant(w, r)
	if !ok {
		return
	}
	cookieID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/cookies/"), "/claim")
	rec, err := s.store.GetByID(tenantID, cookieID)
	if err != nil {
		writeError(w, 404, "cookie not found")
		return
	}
	if rec.Status != "available" {
		writeError(w, 409, "cookie in use")
		return
	}
	var req struct {
		TaskID string `json:"task_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	s.store.UpdateStatus(tenantID, cookieID, "in_use", req.TaskID)
	writeJSON(w, 200, map[string]any{"ok": true, "encrypted_data": base64.StdEncoding.EncodeToString(rec.EncryptedData), "encrypted_keys": rec.EncryptedKeys})
}
func (s *CookieService) Release(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	tenantID, ok := s.currentTenant(w, r)
	if !ok {
		return
	}
	cookieID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/cookies/"), "/release")
	s.store.UpdateStatus(tenantID, cookieID, "available", "")
	writeJSON(w, 200, map[string]any{"ok": true})
}

// Delete
func (s *CookieService) Delete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, 405, "method not allowed")
		return
	}
	tenantID, ok := s.currentTenant(w, r)
	if !ok {
		return
	}
	cookieID := strings.TrimPrefix(r.URL.Path, "/api/cookies/")
	if cookieID == "" {
		writeError(w, 400, "id required")
		return
	}
	if err := s.store.Delete(tenantID, cookieID); err != nil {
		writeError(w, 404, "not found")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
