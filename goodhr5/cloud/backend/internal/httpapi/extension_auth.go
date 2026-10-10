// 本文件负责浏览器插件的受限登录、会话交换、续期与注销，防止插件凭证访问管理和自动任务接口。
package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

const extensionSessionKind = "browser_extension"
const extensionSessionTTL = 7 * 24 * time.Hour

// extensionLoginResponse 保存既有登录接口返回的凭证，凭证降权后才允许交付插件。
type extensionLoginResponse struct {
	AccessToken string `json:"access_token"`
}

// extensionLoginRecorder 暂存原登录响应，避免完整网页会话在降权前送到插件。
type extensionLoginRecorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

// Header 返回暂存响应头。
func (r *extensionLoginRecorder) Header() http.Header { return r.header }

// WriteHeader 保存响应状态码。
func (r *extensionLoginRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}

// Write 暂存响应正文。
func (r *extensionLoginRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(body)
}

// ExtensionLogin 复用原有密码或验证码验证，成功后仅返回受限插件会话。
func (s *AuthService) ExtensionLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	var credentials loginRequest
	if json.NewDecoder(r.Body).Decode(&credentials) != nil {
		writeError(w, http.StatusBadRequest, "登录信息没有读完整，请重试")
		return
	}
	body, _ := json.Marshal(credentials)
	request := r.Clone(r.Context())
	request.Body = http.NoBody
	request.Body = io.NopCloser(bytes.NewReader(body))
	response := &extensionLoginRecorder{header: make(http.Header)}
	if credentials.Password != "" && credentials.Code == "" {
		s.PasswordLogin(response, request)
	} else {
		s.Login(response, request)
	}
	if response.status != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(response.status)
		_, _ = w.Write(response.body.Bytes())
		return
	}
	var result extensionLoginResponse
	if json.Unmarshal(response.body.Bytes(), &result) != nil || result.AccessToken == "" {
		writeError(w, http.StatusInternalServerError, "登录结果暂时没读到，请重试")
		return
	}
	session, err := s.store.GetSession(result.AccessToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "登录状态已过期，请重试")
		return
	}
	session.Kind = extensionSessionKind
	if err = s.store.SaveSession(result.AccessToken, session, extensionSessionTTL); err != nil {
		_ = s.store.SaveSession(result.AccessToken, session, -time.Second)
		writeError(w, http.StatusInternalServerError, "插件登录暂时没完成，请重试")
		return
	}
	s.writeExtensionSession(w, result.AccessToken, session.Email)
}

// ExtensionSession 使用已验证的网页会话签发独立插件凭证，原凭证不交给插件。
func (s *AuthService) ExtensionSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, err := s.SessionFromRequest(r)
	if err != nil || session.Kind == extensionSessionKind {
		writeError(w, http.StatusUnauthorized, "请先在 GoodHR 控制台登录并确认配对")
		return
	}
	s.issueExtensionSession(w, session.Email)
}

// ExtensionRefresh 原子消费有效插件会话并签发替代凭证，并发续期或退出只有一个能成功。
func (s *AuthService) ExtensionRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	oldToken := bearerToken(r.Header.Get("Authorization"))
	if oldToken == "" {
		writeError(w, http.StatusUnauthorized, "插件登录已过期，请重新登录")
		return
	}
	token, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "插件凭证暂时没准备好")
		return
	}
	session, err := s.store.ReplaceSession(oldToken, token, extensionSessionTTL, extensionSessionKind)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "插件登录已更新或退出，请重新登录")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "插件续期暂时没完成，请重试")
		return
	}
	s.writeExtensionSession(w, token, session.Email)
}

// ExtensionLogout 原子消费当前插件会话，已被续期消费的旧凭证不能报告退出成功。
func (s *AuthService) ExtensionLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" {
		writeError(w, http.StatusUnauthorized, "插件尚未登录")
		return
	}
	_, err := s.store.ReplaceSession(token, "", 0, extensionSessionKind)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "插件登录已更新或退出，请重新登录")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "退出暂时没完成，请重试")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// issueExtensionSession 创建仅允许简历业务的独立凭证。
func (s *AuthService) issueExtensionSession(w http.ResponseWriter, email string) bool {
	token, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "插件凭证暂时没准备好")
		return false
	}
	session := Session{Kind: extensionSessionKind, Email: email, CreatedAt: time.Now()}
	if err = s.store.SaveSession(token, session, extensionSessionTTL); err != nil {
		writeError(w, http.StatusInternalServerError, "插件登录暂时没完成")
		return false
	}
	s.writeExtensionSession(w, token, email)
	return true
}

// writeExtensionSession 返回插件会话公开字段，不返回网页凭证和用户密钥。
func (s *AuthService) writeExtensionSession(w http.ResponseWriter, token, email string) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "token": token, "access_token": token, "expires_at": time.Now().Add(extensionSessionTTL).UTC().Format(time.RFC3339), "user": s.publicUser(email)})
}

// extensionSessionGuard 在所有云端路由前限制插件会话，阻止绕过具体 handler 的权限边界。
func (s *Server) extensionSessionGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokens := []string{bearerToken(r.Header.Get("Authorization"))}
		// Agent WebSocket 优先读取查询参数，因此同时检查两处，防止网页请求头掩盖插件 token。
		if r.URL.Path == "/api/agents/ws" {
			tokens = append(tokens, r.URL.Query().Get("token"))
		}
		for _, token := range tokens {
			if token == "" {
				continue
			}
			if session, err := s.auth.store.GetSession(token); err == nil && session.Kind == extensionSessionKind && !extensionPathAllowed(r.Method, r.URL.Path) {
				writeError(w, http.StatusForbidden, "插件只能处理简历、备注和岗位推荐，请到控制台使用其他功能")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// extensionPathAllowed 返回插件可以使用的最小业务路由集合。
func extensionPathAllowed(method, path string) bool {
	// 分享会公开简历资料，只能由控制台登录会话明确创建或取消，插件会话不开放此入口。
	if strings.HasPrefix(path, "/api/candidates/") && strings.HasSuffix(path, "/share") {
		return false
	}
	if method == http.MethodGet && (path == "/api/auth/me" || path == "/api/positions" || path == "/api/system/browser-extension") {
		return true
	}
	if method == http.MethodPost && (path == "/api/auth/extension-refresh" || path == "/api/auth/extension-logout") {
		return true
	}
	return (path == "/api/candidates" || strings.HasPrefix(path, "/api/candidates/")) && (method == http.MethodGet || method == http.MethodPost || method == http.MethodPatch)
}
