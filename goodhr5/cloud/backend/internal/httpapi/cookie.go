// cookie HTTP API
package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
)

var errNoAgentPublicKey = errors.New("no local agent public key registered")

type CookieService struct {
	auth        *AuthService
	store       CookieStore
	tenantStore TenantStore
	agentStore  AgentStore
	agentWS     *AgentWSHub
}

// NewCookieService 创建 cookie 管理服务。
// agentWS 用于向当前用户在线 Local Agent 下发扫码登录和 cookie 捕获指令。
func NewCookieService(auth *AuthService, store CookieStore, tenantStore TenantStore, agentStore AgentStore, agentWS *AgentWSHub) *CookieService {
	return &CookieService{auth: auth, store: store, tenantStore: tenantStore, agentStore: agentStore, agentWS: agentWS}
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
		PlatformID  string `json:"platform_id"`
		DisplayName string `json:"display_name"`
		Cookies     any    `json:"cookies"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid json")
		return
	}
	if req.PlatformID == "" {
		writeError(w, 400, "platform_id required")
		return
	}
	log.Printf("[cookies] create request tenant=%s user=%s platform=%s name=%s", tenantID, session.Email, req.PlatformID, req.DisplayName)
	if isCookieNameDuplicate(s.store, tenantID, req.DisplayName) {
		log.Printf("[cookies] create rejected duplicate tenant=%s platform=%s name=%s", tenantID, req.PlatformID, req.DisplayName)
		writeError(w, http.StatusConflict, "display_name already exists")
		return
	}
	if req.Cookies == nil {
		writeError(w, 400, "cookies required")
		return
	}
	cookieJSON, err := json.Marshal(req.Cookies)
	if err != nil {
		writeError(w, 400, "invalid cookies")
		return
	}
	encryptedData, encryptedKeys, err := s.encryptCookieForTenant(tenantID, cookieJSON)
	if err != nil {
		log.Printf("[cookies] encrypt failed: %v", err)
		if errors.Is(err, errNoAgentPublicKey) {
			writeError(w, http.StatusConflict, "no local agent public key registered")
			return
		}
		writeError(w, 500, "failed to encrypt cookie")
		return
	}

	rec, err := s.store.Create(CookieRecord{
		TenantID: tenantID, UserID: session.Email, PlatformID: req.PlatformID,
		DisplayName: req.DisplayName, CookieType: "json", Status: "available",
		EncryptedData: encryptedData, EncryptedKeys: encryptedKeys, SizeBytes: int64(len(cookieJSON)),
	})
	if err != nil {
		log.Printf("[cookies] create failed: %v", err)
		writeError(w, 500, "failed to create cookie")
		return
	}
	log.Printf("[cookies] create success cookie=%s tenant=%s platform=%s name=%s size=%d", rec.ID, tenantID, req.PlatformID, req.DisplayName, len(cookieJSON))
	writeJSON(w, 200, map[string]any{"ok": true, "cookie": rec})
}

func isCookieNameDuplicate(store CookieStore, tenantID string, displayName string) bool {
	target := normalizeCookieName(displayName)
	if target == "" {
		return false
	}
	items, err := store.List(tenantID)
	if err != nil {
		return false
	}
	for _, item := range items {
		if normalizeCookieName(item.DisplayName) == target {
			return true
		}
	}
	return false
}

func normalizeCookieName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// encryptCookieForTenant 为租户内已登记公钥的 Agent 自动加密 cookie 数据密钥。
// 用户无需手动分享密钥，数据库只保存密文和每台 Agent 的加密数据密钥。
func (s *CookieService) encryptCookieForTenant(tenantID string, cookieJSON []byte) ([]byte, map[string]string, error) {
	sk, err := GenerateSK()
	if err != nil {
		return nil, nil, err
	}
	encryptedData, err := EncryptData(cookieJSON, sk)
	if err != nil {
		return nil, nil, err
	}
	members, err := s.tenantStore.ListMembers(tenantID)
	if err != nil {
		return nil, nil, err
	}
	encryptedKeys := map[string]string{}
	for _, member := range members {
		if member.Email == "" {
			continue
		}
		binding, err := s.agentStore.CurrentBinding(member.Email)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		if binding.PublicKey == "" || binding.MachineID == "" {
			continue
		}
		encryptedKey, err := EncryptSKForAgent(binding.PublicKey, sk)
		if err != nil {
			return nil, nil, err
		}
		encryptedKeys[binding.MachineID] = encryptedKey
	}
	if len(encryptedKeys) == 0 {
		return nil, nil, errNoAgentPublicKey
	}
	return encryptedData, encryptedKeys, nil
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
