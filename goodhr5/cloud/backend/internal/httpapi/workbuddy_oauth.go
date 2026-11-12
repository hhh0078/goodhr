// 本文件负责 WorkBuddy 连接器的 OAuth 2.0 授权服务：
// 元数据发现、动态客户端注册、授权码下发（PKCE S256）和令牌签发/刷新。
package httpapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// workbuddySessionKind 表示 WorkBuddy 连接器专用会话，仅能访问 /mcp。
const workbuddySessionKind = "workbuddy_connector"

const (
	workbuddyAccessTokenTTL  = 30 * 24 * time.Hour
	workbuddyRefreshTokenTTL = 90 * 24 * time.Hour
	workbuddyAuthCodeTTL     = 5 * time.Minute
	workbuddyAuthorizePath   = "/workbuddy/authorize"
)

// WorkBuddyOAuthService 提供 WorkBuddy 连接器接入的 OAuth 授权能力。
type WorkBuddyOAuthService struct {
	auth          *AuthService
	store         WorkBuddyOAuthStore
	systemConfigs SystemConfigStore
}

// NewWorkBuddyOAuthService 创建连接器 OAuth 授权服务。
func NewWorkBuddyOAuthService(auth *AuthService, store WorkBuddyOAuthStore, systemConfigs SystemConfigStore) *WorkBuddyOAuthService {
	return &WorkBuddyOAuthService{auth: auth, store: store, systemConfigs: systemConfigs}
}

// workBuddyBaseURL 返回对外访问的云端基地址，优先读取环境变量，默认正式域名。
func workBuddyBaseURL(r *http.Request) string {
	if base := strings.TrimSpace(os.Getenv("GOODHR_PUBLIC_BASE_URL")); base != "" {
		return strings.TrimRight(base, "/")
	}
	scheme := "https"
	if r != nil {
		if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
			scheme = proto
		}
		if host := strings.TrimSpace(r.Host); host != "" {
			return scheme + "://" + host
		}
	}
	return "https://goodhr.58it.cn"
}

// WellKnownProtectedResource 返回 RFC9728 受保护资源元数据，供 MCP 客户端发现授权服务器。
func (s *WorkBuddyOAuthService) WellKnownProtectedResource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	base := workBuddyBaseURL(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":               base + "/mcp",
		"authorization_servers":  []string{base},
		"scopes_supported":       []string{"mcp"},
		"bearer_methods_supported": []string{"header"},
	})
}

// WellKnownAuthorizationServer 返回 RFC8414 授权服务器元数据。
func (s *WorkBuddyOAuthService) WellKnownAuthorizationServer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	base := workBuddyBaseURL(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/oauth/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"registration_endpoint":                 base + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      []string{"mcp"},
	})
}

// Register 处理 RFC7591 动态客户端注册，连接器以公共客户端（PKCE）方式接入。
func (s *WorkBuddyOAuthService) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var payload struct {
		ClientName              string   `json:"client_name"`
		RedirectURIs            []string `json:"redirect_uris"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "注册信息没有读完整，请重试")
		return
	}
	uris := make([]string, 0, len(payload.RedirectURIs))
	for _, uri := range payload.RedirectURIs {
		uri = strings.TrimSpace(uri)
		if uri == "" {
			continue
		}
		parsed, err := url.Parse(uri)
		if err != nil || parsed.Host == "" && parsed.Scheme != "http" && parsed.Scheme != "https" {
			writeError(w, http.StatusBadRequest, "回调地址格式没认出来，请检查 redirect_uris")
			return
		}
		uris = append(uris, uri)
	}
	if len(uris) == 0 {
		writeError(w, http.StatusBadRequest, "至少需要一个回调地址 redirect_uris")
		return
	}
	clientID, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "客户端凭证暂时没准备好，请重试")
		return
	}
	clientID = "wb_" + strings.TrimPrefix(clientID, "gh5_")
	client := WorkBuddyOAuthClient{
		ClientID:    clientID,
		ClientName:  strings.TrimSpace(payload.ClientName),
		RedirectURI: uris,
		CreatedAt:   time.Now(),
	}
	if err := s.store.SaveClient(client); err != nil {
		log.Printf("[WorkBuddyOAuth] 保存客户端失败 client=%s err=%v", clientID, err)
		writeError(w, http.StatusInternalServerError, "注册暂时没完成，请重试")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"client_id":                  clientID,
		"client_id_issued_at":        time.Now().Unix(),
		"client_name":                client.ClientName,
		"redirect_uris":              uris,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
}

// Authorize 处理浏览器发起的授权请求，统一转发到前端授权确认页完成登录和同意。
func (s *WorkBuddyOAuthService) Authorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	query := r.URL.RawQuery
	if query == "" {
		writeError(w, http.StatusBadRequest, "缺少授权参数")
		return
	}
	// 授权确认页在前端完成登录和同意，这里只负责把参数原样带过去。
	http.Redirect(w, r, workbuddyAuthorizePath+"?"+query, http.StatusFound)
}

type workBuddyAuthorizeRequest struct {
	ClientID            string `json:"client_id"`
	RedirectURI         string `json:"redirect_uri"`
	State               string `json:"state"`
	Scope               string `json:"scope"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
	Decision            string `json:"decision"`
}

// AuthorizeConsent 由前端授权确认页调用：校验登录会话和授权参数，同意后下发一次性授权码。
func (s *WorkBuddyOAuthService) AuthorizeConsent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "请先在 GoodHR 控制台登录")
		return
	}
	// 连接器会话和插件会话都不能替用户做授权决定。
	if session.Kind != "" {
		writeError(w, http.StatusForbidden, "请在 GoodHR 控制台用网页登录后操作")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	var payload workBuddyAuthorizeRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "授权参数没有读完整，请重试")
		return
	}
	client, err := s.store.ClientByID(strings.TrimSpace(payload.ClientID))
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusBadRequest, "这个客户端没有注册过，请在 WorkBuddy 重新连接")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "客户端信息暂时没读出来，请重试")
		return
	}
	if !workBuddyRedirectURIsAllowed(client, payload.RedirectURI) {
		writeError(w, http.StatusBadRequest, "回调地址和注册时不一致，我这边不能放行")
		return
	}
	if strings.TrimSpace(payload.CodeChallenge) == "" || strings.EqualFold(payload.CodeChallengeMethod, "plain") {
		writeError(w, http.StatusBadRequest, "授权请求必须使用 PKCE S256")
		return
	}
	if strings.EqualFold(strings.TrimSpace(payload.Decision), "deny") {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "redirect_url": workBuddyRedirectWith(payload.RedirectURI, url.Values{"error": []string{"access_denied"}, "state": []string{payload.State}})})
		return
	}
	code, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "授权码暂时没准备好，请重试")
		return
	}
	record := WorkBuddyOAuthCode{
		Code:          code,
		ClientID:      client.ClientID,
		UserEmail:     session.Email,
		RedirectURI:   strings.TrimSpace(payload.RedirectURI),
		Scope:         strings.TrimSpace(payload.Scope),
		CodeChallenge: strings.TrimSpace(payload.CodeChallenge),
		ExpiresAt:     time.Now().Add(workbuddyAuthCodeTTL),
	}
	if err := s.store.SaveCode(record); err != nil {
		log.Printf("[WorkBuddyOAuth] 保存授权码失败 client=%s user=%s err=%v", client.ClientID, session.Email, err)
		writeError(w, http.StatusInternalServerError, "授权暂时没完成，请重试")
		return
	}
	_ = s.store.TouchClient(client.ClientID)
	redirect := workBuddyRedirectWith(payload.RedirectURI, url.Values{"code": []string{code}, "state": []string{payload.State}})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "redirect_url": redirect})
}

// workBuddyRedirectWith 把查询参数拼到回调地址上，保留回调地址自带的参数。
func workBuddyRedirectWith(redirectURI string, values url.Values) string {
	parsed, err := url.Parse(redirectURI)
	if err != nil {
		return redirectURI
	}
	query := parsed.Query()
	for key, list := range values {
		for _, item := range list {
			query.Set(key, item)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// Token 处理授权码换令牌和刷新令牌轮换。
func (s *WorkBuddyOAuthService) Token(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	var payload struct {
		GrantType    string `json:"grant_type"`
		Code         string `json:"code"`
		ClientID     string `json:"client_id"`
		RedirectURI  string `json:"redirect_uri"`
		CodeVerifier string `json:"code_verifier"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
	}
	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	// 标准客户端按表单提交，兼容 JSON 方便测试。
	if strings.Contains(contentType, "application/x-www-form-urlencoded") {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "令牌请求没有读完整，请重试")
			return
		}
		payload.GrantType = r.PostFormValue("grant_type")
		payload.Code = r.PostFormValue("code")
		payload.ClientID = r.PostFormValue("client_id")
		payload.RedirectURI = r.PostFormValue("redirect_uri")
		payload.CodeVerifier = r.PostFormValue("code_verifier")
		payload.RefreshToken = r.PostFormValue("refresh_token")
		payload.Scope = r.PostFormValue("scope")
	} else if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "令牌请求没有读完整，请重试")
		return
	}
	switch strings.TrimSpace(payload.GrantType) {
	case "authorization_code":
		s.tokenByAuthorizationCode(w, payload)
	case "refresh_token":
		s.tokenByRefreshToken(w, payload)
	default:
		writeError(w, http.StatusBadRequest, "grant_type 只支持 authorization_code 和 refresh_token")
	}
}

// tokenByAuthorizationCode 校验授权码和 PKCE，签发连接器访问令牌和刷新令牌。
func (s *WorkBuddyOAuthService) tokenByAuthorizationCode(w http.ResponseWriter, payload struct {
	GrantType    string `json:"grant_type"`
	Code         string `json:"code"`
	ClientID     string `json:"client_id"`
	RedirectURI  string `json:"redirect_uri"`
	CodeVerifier string `json:"code_verifier"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}) {
	record, err := s.store.ConsumeCode(strings.TrimSpace(payload.Code))
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusBadRequest, "授权码已使用或已过期，请重新发起授权")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "授权码校验暂时没完成，请重试")
		return
	}
	if record.ClientID != strings.TrimSpace(payload.ClientID) || record.RedirectURI != strings.TrimSpace(payload.RedirectURI) {
		writeError(w, http.StatusBadRequest, "令牌请求参数和授权时不一致，请重新发起授权")
		return
	}
	if !workBuddyVerifyPKCE(record.CodeChallenge, payload.CodeVerifier) {
		writeError(w, http.StatusBadRequest, "PKCE 校验没通过，请重新发起授权")
		return
	}
	s.issueWorkBuddyTokens(w, record.ClientID, record.UserEmail, record.Scope)
}

// tokenByRefreshToken 校验刷新令牌并轮换签发新令牌对。
func (s *WorkBuddyOAuthService) tokenByRefreshToken(w http.ResponseWriter, payload struct {
	GrantType    string `json:"grant_type"`
	Code         string `json:"code"`
	ClientID     string `json:"client_id"`
	RedirectURI  string `json:"redirect_uri"`
	CodeVerifier string `json:"code_verifier"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}) {
	record, err := s.store.ConsumeRefreshToken(strings.TrimSpace(payload.RefreshToken))
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "刷新令牌已失效，请重新连接授权")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "刷新令牌校验暂时没完成，请重试")
		return
	}
	if record.ClientID != strings.TrimSpace(payload.ClientID) {
		writeError(w, http.StatusBadRequest, "客户端信息和令牌不一致，请重新连接授权")
		return
	}
	s.issueWorkBuddyTokens(w, record.ClientID, record.UserEmail, record.Scope)
}

// workBuddyVerifyPKCE 校验 PKCE S256 的 code_verifier。
func workBuddyVerifyPKCE(challenge, verifier string) bool {
	verifier = strings.TrimSpace(verifier)
	if challenge == "" || verifier == "" || len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	digest := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(digest[:])
	return computed == strings.TrimSpace(challenge)
}

// issueWorkBuddyTokens 签发连接器访问令牌（复用云端会话存储）和刷新令牌。
func (s *WorkBuddyOAuthService) issueWorkBuddyTokens(w http.ResponseWriter, clientID, email, scope string) {
	accessToken, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "令牌暂时没准备好，请重试")
		return
	}
	session := Session{Kind: workbuddySessionKind, LoginMethod: "workbuddy_oauth", Email: email, CreatedAt: time.Now()}
	if err := s.auth.store.SaveSession(accessToken, session, workbuddyAccessTokenTTL); err != nil {
		log.Printf("[WorkBuddyOAuth] 保存访问令牌失败 client=%s user=%s err=%v", clientID, email, err)
		writeError(w, http.StatusInternalServerError, "令牌暂时没准备好，请重试")
		return
	}
	refreshToken, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "令牌暂时没准备好，请重试")
		return
	}
	refreshToken = "gh5wbr_" + strings.TrimPrefix(refreshToken, "gh5_")
	if err := s.store.SaveRefreshToken(WorkBuddyRefreshToken{
		Token:     refreshToken,
		ClientID:  clientID,
		UserEmail: email,
		Scope:     scope,
		ExpiresAt: time.Now().Add(workbuddyRefreshTokenTTL),
	}); err != nil {
		// 访问令牌已在会话存储中，刷新令牌保存失败时回收访问令牌，避免出现无法续期的孤儿令牌。
		_ = s.auth.store.SaveSession(accessToken, Session{}, -time.Second)
		log.Printf("[WorkBuddyOAuth] 保存刷新令牌失败 client=%s user=%s err=%v", clientID, email, err)
		writeError(w, http.StatusInternalServerError, "令牌暂时没准备好，请重试")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  accessToken,
		"token_type":    "Bearer",
		"expires_in":    int(workbuddyAccessTokenTTL.Seconds()),
		"refresh_token": refreshToken,
		"scope":         scope,
	})
}

// workBuddySessionGuard 限制连接器会话只能访问 MCP 端点，挂在全部云端路由之前。
func (s *Server) workBuddySessionGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r.Header.Get("Authorization"))
		if token != "" {
			if session, err := s.auth.store.GetSession(token); err == nil && session.Kind == workbuddySessionKind && r.URL.Path != "/mcp" {
				writeError(w, http.StatusForbidden, "连接器会话只能访问 /mcp，其他功能请在 GoodHR 控制台操作")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
