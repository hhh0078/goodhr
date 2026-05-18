package httpapi

import (
	"encoding/json"
	"net/http"
)

type Server struct {
	auth *AuthService
}

func NewServer() *Server {
	return &Server{
		auth: NewAuthService(NewMemoryAuthStore()),
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.health)
	mux.HandleFunc("/api/auth/send-code", s.auth.SendCode)
	mux.HandleFunc("/api/auth/login", s.auth.Login)
	mux.HandleFunc("/api/auth/me", s.auth.Me)
	return mux
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"name":    "GoodHR 5 Cloud API",
		"version": "0.1.0",
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"ok":    false,
		"error": message,
	})
}
