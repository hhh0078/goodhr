package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"
)

const codeTTL = 5 * time.Minute
const sessionTTL = 7 * 24 * time.Hour

type AuthService struct {
	store           AuthStore
	mailer          Mailer
	exposeDebugCode bool
}

type sendCodeRequest struct {
	Email string `json:"email"`
}

type loginRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

func NewAuthService(store AuthStore, mailer Mailer, exposeDebugCode bool) *AuthService {
	return &AuthService{
		store:           store,
		mailer:          mailer,
		exposeDebugCode: exposeDebugCode,
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

	if err := s.store.SaveLoginCode(email, code, codeTTL); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save code")
		return
	}

	if err := s.mailer.SendLoginCode(email, code); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to send code")
		return
	}

	response := map[string]any{
		"ok":         true,
		"email":      email,
		"expires_in": int(codeTTL.Seconds()),
	}
	if s.exposeDebugCode {
		response["debug_code"] = code
	}
	writeJSON(w, http.StatusOK, response)
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

	matched, err := s.store.ConsumeLoginCode(email, code)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify code")
		return
	}
	if !matched {
		writeError(w, http.StatusUnauthorized, "code is invalid or expired")
		return
	}

	token, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	now := time.Now()
	if err := s.store.SaveSession(token, Session{
		Email:     email,
		CreatedAt: now,
	}, sessionTTL); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save session")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   int(sessionTTL.Seconds()),
		"user": map[string]any{
			"id":    email,
			"email": email,
		},
	})
}

func (s *AuthService) Me(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// 调用认证服务的会话解析方法，用于返回当前登录用户信息。
	session, err := s.SessionFromRequest(r)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"user": map[string]any{
			"id":    session.Email,
			"email": session.Email,
		},
		"session": map[string]any{
			"created_at": session.CreatedAt,
			"expires_at": session.ExpiresAt,
		},
	})
}

// SessionFromRequest 从请求头 Bearer token 中读取当前登录会话。
func (s *AuthService) SessionFromRequest(r *http.Request) (Session, error) {
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" {
		return Session{}, errors.New("missing bearer token")
	}
	return s.SessionFromToken(token)
}

// SessionFromToken 根据访问令牌读取当前登录会话。
// token 为验证码登录后返回的 access_token，返回会话用于 HTTP 与 WebSocket 认证。
func (s *AuthService) SessionFromToken(token string) (Session, error) {
	// 调用 AuthStore 读取会话，用于确认 token 是否有效。
	session, err := s.store.GetSession(token)
	if err != nil {
		return Session{}, err
	}
	return session, nil
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

func bearerToken(value string) string {
	prefix := "Bearer "
	if !strings.HasPrefix(value, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(value, prefix))
}
