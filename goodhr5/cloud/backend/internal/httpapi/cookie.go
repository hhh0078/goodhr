// cookie HTTP API
package httpapi

import ("encoding/json"; "net/http"; "strings")

type CookieService struct { auth *AuthService; store CookieStore; tenantStore TenantStore; capture *CookieCapture }

func NewCookieService(auth *AuthService, store CookieStore, tenantStore TenantStore) *CookieService {
	return &CookieService{auth: auth, store: store, tenantStore: tenantStore, capture: NewCookieCapture(store)}
}

func (s *CookieService) currentTenant(w http.ResponseWriter, r *http.Request) (string, bool) {
	session, err := s.auth.SessionFromRequest(r)
	if err != nil { writeError(w, 401, "unauthorized"); return "", false }
	t, err := s.tenantStore.GetOrCreateTenant(session.Email)
	if err != nil { writeError(w, 500, "failed to get tenant"); return "", false }
	return t.ID, true
}

// List Cookies
func (s *CookieService) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet { writeError(w, 405, "method not allowed"); return }
	tenantID, ok := s.currentTenant(w, r); if !ok { return }
	items, err := s.store.List(tenantID)
	if err != nil { writeError(w, 500, "failed to list cookies"); return }
	writeJSON(w, 200, map[string]any{"ok": true, "cookies": items})
}

// Create Cookie (创建记录，状态=capturing，后续由任务处理)
func (s *CookieService) Create(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { writeError(w, 405, "method not allowed"); return }
	session, err := s.auth.SessionFromRequest(r)
	if err != nil { writeError(w, 401, "unauthorized"); return }
	tenantID, ok := s.currentTenant(w, r); if !ok { return }

	var req struct { PlatformID string `json:"platform_id"`; DisplayName string `json:"display_name"` }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { writeError(w, 400, "invalid json"); return }
	if req.PlatformID == "" { writeError(w, 400, "platform_id required"); return }

	rec, err := s.store.Create(CookieRecord{
		TenantID: tenantID, UserID: session.Email, PlatformID: req.PlatformID,
		DisplayName: req.DisplayName, CookieType: "folder", Status: "capturing",
	})
	if err != nil { writeError(w, 500, "failed to create cookie"); return }
	// 启动异步捕获流程
	agentBaseURL := strings.TrimSpace(r.Header.Get("X-GoodHR-Agent-BaseURL"))
	if agentBaseURL != "" {
		s.capture.Capture(rec.ID, tenantID, req.PlatformID, agentBaseURL, rec.ID, nil)
	}
	writeJSON(w, 200, map[string]any{"ok": true, "cookie": rec})
}

// Delete
func (s *CookieService) Delete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete { writeError(w, 405, "method not allowed"); return }
	tenantID, ok := s.currentTenant(w, r); if !ok { return }
	cookieID := strings.TrimPrefix(r.URL.Path, "/api/cookies/")
	if cookieID == "" { writeError(w, 400, "id required"); return }
	if err := s.store.Delete(tenantID, cookieID); err != nil { writeError(w, 404, "not found"); return }
	writeJSON(w, 200, map[string]any{"ok": true})
}
