package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"goodhr4/backend/internal/service"
	"goodhr4/backend/internal/store"
)

type Handler struct {
	service       *service.Service
	allowedOrigin string
}

func New(svc *service.Service, allowedOrigin string) *Handler {
	return &Handler{service: svc, allowedOrigin: allowedOrigin}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.withCORS(h.health))
	mux.HandleFunc("/api/v1/account/bind", h.withCORS(h.bind))
	mux.HandleFunc("/api/v1/account/", h.withCORS(h.accountSettings))
	mux.HandleFunc("/api/v1/system/config", h.withCORS(h.systemConfig))
	mux.HandleFunc("/api/v1/site/register", h.withCORS(h.siteRegister))
	mux.HandleFunc("/api/v1/site/bootstrap", h.withCORS(h.siteBootstrap))
	mux.HandleFunc("/api/v1/site/updates", h.withCORS(h.siteUpdates))
}

func (h *Handler) withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", h.allowedOrigin)
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) bind(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Identifier string `json:"identifier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	account, settings, token, created, err := h.service.Bind(r.Context(), req.Identifier)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrBadIdentifier) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"created":  created,
		"token":    token,
		"account":  account,
		"settings": settings,
	})
}

func (h *Handler) accountSettings(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/account/")
	if !strings.HasSuffix(path, "/settings") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	identifier := strings.TrimSuffix(path, "/settings")
	identifier = strings.Trim(identifier, "/")
	identifier = strings.TrimSpace(identifier)

	switch r.Method {
	case http.MethodGet:
		account, settings, err := h.service.GetSettings(r.Context(), identifier)
		if err != nil {
			h.handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"account":  account,
			"settings": settings,
		})
	case http.MethodPost:
		var req struct {
			Settings map[string]any `json:"settings"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		account, settings, err := h.service.SaveSettings(r.Context(), identifier, req.Settings)
		if err != nil {
			h.handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"account":  account,
			"settings": settings,
		})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) systemConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	cfg, err := h.service.GetSystemConfig(r.Context(), key)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"config": cfg,
	})
}

func (h *Handler) siteRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Identifier string `json:"identifier"`
		Email      string `json:"email"`
		Phone      string `json:"phone"`
		InviterID  *int64 `json:"inviter_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	identifier := strings.TrimSpace(req.Identifier)
	if identifier == "" {
		identifier = strings.TrimSpace(req.Email)
	}
	if identifier == "" {
		identifier = strings.TrimSpace(req.Phone)
	}

	account, created, err := h.service.RegisterSite(r.Context(), identifier, req.InviterID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrBadIdentifier) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"code":    0,
		"message": "success",
		"data": map[string]any{
			"user": map[string]any{
				"id":         account.ID,
				"email":      account.Email,
				"phone":      account.Phone,
				"identifier": account.Identifier,
				"role":       "user",
				"inviter_id": account.InviterID,
				"balance":    account.Balance,
			},
			"is_new_user": created,
		},
	})
}

func (h *Handler) siteBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	bootstrap, err := h.service.GetSiteBootstrap(r.Context(), 20)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"code":    0,
		"message": "success",
		"data":    bootstrap,
	})
}

func (h *Handler) siteUpdates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	updates, err := h.service.ListUpdateRecords(r.Context(), 100)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"code":    0,
		"message": "success",
		"data": map[string]any{
			"items": updates,
		},
	})
}

func (h *Handler) handleServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrBadIdentifier):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "account not found")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}
