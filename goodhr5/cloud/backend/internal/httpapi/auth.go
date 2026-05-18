package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"
)

const codeTTL = 5 * time.Minute

type AuthService struct {
	mu    sync.Mutex
	codes map[string]loginCode
}

type loginCode struct {
	Code      string
	ExpiresAt time.Time
}

type sendCodeRequest struct {
	Email string `json:"email"`
}

type loginRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

func NewAuthService() *AuthService {
	return &AuthService{
		codes: make(map[string]loginCode),
	}
}

func (s *AuthService) SendCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req sendCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	email, ok := normalizeEmail(req.Email)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid email")
		return
	}

	code, err := randomDigits(4)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate code")
		return
	}

	expiresAt := time.Now().Add(codeTTL)
	s.mu.Lock()
	s.codes[email] = loginCode{Code: code, ExpiresAt: expiresAt}
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"email":      email,
		"expires_in": int(codeTTL.Seconds()),
		"debug_code": code,
	})
}

func (s *AuthService) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	email, ok := normalizeEmail(req.Email)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid email")
		return
	}

	code := strings.TrimSpace(req.Code)
	if len(code) != 4 {
		writeError(w, http.StatusBadRequest, "invalid code")
		return
	}

	if !s.verifyCode(email, code) {
		writeError(w, http.StatusUnauthorized, "code is invalid or expired")
		return
	}

	token, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"access_token": token,
		"token_type":   "Bearer",
		"user": map[string]any{
			"email": email,
		},
	})
}

func (s *AuthService) verifyCode(email string, code string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	saved, ok := s.codes[email]
	if !ok {
		return false
	}
	if time.Now().After(saved.ExpiresAt) {
		delete(s.codes, email)
		return false
	}
	if saved.Code != code {
		return false
	}

	delete(s.codes, email)
	return true
}

func normalizeEmail(value string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(value))
	if email == "" {
		return "", false
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return "", false
	}
	return email, true
}

func randomDigits(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	digits := make([]byte, length)
	for i, value := range bytes {
		digits[i] = byte('0' + value%10)
	}
	return string(digits), nil
}

func randomToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return fmt.Sprintf("gh5_%s", hex.EncodeToString(bytes)), nil
}
