// Package api 文件作用：验证网页完整会话或插件安装配对后分派本地摘要草稿，永不交付原会话、模型密钥或源简历正文。
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	summaryflow "goodhr5/local-agent-go-new/internal/flow/resume_summary"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"io"
	"net/http"
	"strings"
)

// summaryRequest 使用强类型职业字段，年龄、头像和来源授权等额外字段不会送入AI。
type summaryRequest struct {
	InstallationID string                      `json:"installation_id"`
	PairingToken   string                      `json:"pairing_token"`
	RequestKey     string                      `json:"request_key"`
	Candidate      summaryflow.SourceCandidate `json:"candidate"`
}

// summaryAccess 只在本次内存调用持有认证信息，不参与JSON序列化。
type summaryAccess struct {
	owner, scope, token string
	check               summaryflow.AuthCheck
}

// summaryAccessError 提供插件可判断的稳定错误码，禁止包含第三方响应或任何密钥。
type summaryAccessError struct{ code, message string }

// Error 返回可安全显示的简短认证说明。
func (e *summaryAccessError) Error() string { return e.message }

// initResumeSummaries 复用SQLite和单次AI客户端，摘要流程无浏览器或云端写入依赖。
func (s *Server) initResumeSummaries(manager *summaryflow.Manager) {
	s.resumeSummaries = manager
	if manager != nil || s.store == nil || s.cloud == nil {
		return
	}
	if value, err := summaryflow.New(s.store, s.cloud, ai.New()); err == nil {
		s.resumeSummaries = value
	}
}

// registerResumeSummaryRoutes 为网页和插件注册明确不同的授权入口。
func (s *Server) registerResumeSummaryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/resume-summary/tasks", s.handleWebResumeSummary)
	mux.HandleFunc("GET /api/v1/resume-summary/tasks/{summary_id}", s.handleWebResumeSummary)
	mux.HandleFunc("POST /api/v1/resume-summary/tasks/{summary_id}/{summary_action}", s.handleWebResumeSummary)
	mux.HandleFunc("POST /api/v1/extensions/resume-summary", s.handleExtensionResumeSummary)
	mux.HandleFunc("POST /api/v1/extensions/resume-summary/{summary_id}", s.handleExtensionResumeSummary)
	mux.HandleFunc("POST /api/v1/extensions/resume-summary/{summary_id}/{summary_action}", s.handleExtensionResumeSummary)
}

// handleWebResumeSummary 接受当前网页完整Bearer会话，拒绝插件受限会话。
func (s *Server) handleWebResumeSummary(w http.ResponseWriter, r *http.Request) {
	if !s.summaryAvailable(w) {
		return
	}
	var req summaryRequest
	if r.Method == http.MethodPost && !decodeSummaryRequest(w, r, &req) {
		return
	}
	header := strings.Fields(r.Header.Get("Authorization"))
	if len(header) != 2 || !strings.EqualFold(header[0], "Bearer") {
		s.writeSummaryAccessError(w, &summaryAccessError{"LOGIN_REQUIRED", "先登录GoodHR，再整理摘要。"})
		return
	}
	token := header[1]
	session, err := s.cloud.ValidateSession(r.Context(), token)
	if err != nil {
		s.writeSummaryAccessError(w, &summaryAccessError{"SESSION_INVALID", "登录已过期，请重新登录后继续，草稿还在。"})
		return
	}
	if session.SessionKind == "browser_extension" {
		s.writeSummaryAccessError(w, &summaryAccessError{"WEB_SESSION_REQUIRED", "受限插件会话不能调用网页AI接口，请连接本地程序。"})
		return
	}
	s.extensionMu.Lock()
	boundUser, generation := s.extensionUserID, s.extensionGeneration
	s.extensionMu.Unlock()
	check := func(ctx context.Context) error {
		current, err := s.cloud.ValidateSession(ctx, token)
		if err != nil || current.UserID != session.UserID || current.SessionKind == "browser_extension" {
			return &summaryAccessError{"SESSION_INVALID", "登录已过期，请重新登录后继续。"}
		}
		s.extensionMu.Lock()
		defer s.extensionMu.Unlock()
		if s.extensionUserID != "" && s.extensionUserID != session.UserID || boundUser != "" && generation != s.extensionGeneration {
			return &summaryAccessError{"PAIR_ACCOUNT_CHANGED", "本地账号已变化，请重新连接后继续。"}
		}
		return nil
	}
	access := summaryAccess{session.UserID, "web", token, check}
	if err := check(r.Context()); err != nil {
		s.writeSummaryAccessError(w, err)
		return
	}
	s.dispatchResumeSummary(w, r, req, access)
}

// handleExtensionResumeSummary 仅用安装密钥访问私有缓存网页会话，不能接受插件Token作为完整会话。
func (s *Server) handleExtensionResumeSummary(w http.ResponseWriter, r *http.Request) {
	if !s.summaryAvailable(w) {
		return
	}
	var req summaryRequest
	if !decodeSummaryRequest(w, r, &req) {
		return
	}
	access, err := s.extensionSummaryAccess(r.Context(), req)
	if err != nil {
		s.writeSummaryAccessError(w, err)
		return
	}
	s.dispatchResumeSummary(w, r, req, access)
}

// extensionSummaryAccess 每次验证配对摘要、当前云账号和登录代次；所有凭证只存在闭包内存中。
func (s *Server) extensionSummaryAccess(ctx context.Context, req summaryRequest) (summaryAccess, error) {
	denied := &summaryAccessError{"PAIR_REQUIRED", "本地未登录或配对已失效，请重新连接插件；简历草稿还在。"}
	if len(req.InstallationID) < 16 || len(req.InstallationID) > 128 || len(req.PairingToken) < 32 || len(req.PairingToken) > 128 || s.store == nil {
		return summaryAccess{}, denied
	}
	s.extensionMu.Lock()
	token, userID, generation := s.extensionToken, s.extensionUserID, s.extensionGeneration
	s.extensionMu.Unlock()
	value, err := s.store.GetSetting(ctx, extensionIdentityKey(req.InstallationID))
	var identity extensionPairIdentity
	if err != nil || json.Unmarshal([]byte(value), &identity) != nil || token == "" || userID == "" || identity.UserID != userID || !extensionSecretsEqual(identity.SecretHash, extensionHash(req.PairingToken)) {
		return summaryAccess{}, denied
	}
	check := func(ctx context.Context) error {
		current, err := s.cloud.ValidateSession(ctx, token)
		if err != nil || current.UserID != userID || current.SessionKind == "browser_extension" {
			return &summaryAccessError{"SESSION_INVALID", "本地登录已过期，请登录控制台并重新连接插件。"}
		}
		s.extensionMu.Lock()
		defer s.extensionMu.Unlock()
		// 配对撤销也使用此锁，读取与代次检查放在同一临界区，拒绝撤销后的迟到轮询。
		latest, err := s.store.GetSetting(ctx, extensionIdentityKey(req.InstallationID))
		if err != nil || latest != value || generation != s.extensionGeneration || userID != s.extensionUserID || token != s.extensionToken {
			return &summaryAccessError{"PAIR_ACCOUNT_CHANGED", "本地账号或配对已变化，请重新连接；简历草稿还在。"}
		}
		return nil
	}
	if err := check(ctx); err != nil {
		return summaryAccess{}, err
	}
	return summaryAccess{userID, "extension:" + extensionHash(req.InstallationID), token, check}, nil
}

// dispatchResumeSummary 只调用摘要管理器，不调用任何候选人、备注或推荐写入。
func (s *Server) dispatchResumeSummary(w http.ResponseWriter, r *http.Request, req summaryRequest, access summaryAccess) {
	var task summaryflow.Task
	var err error
	id, action := r.PathValue("summary_id"), r.PathValue("summary_action")
	switch {
	case id == "":
		task, err = s.resumeSummaries.Create(access.owner, access.scope, req.RequestKey, access.token, req.Candidate.Candidate(), access.check)
	case action == "":
		task, err = s.resumeSummaries.Get(access.owner, access.scope, id)
	case action == "cancel":
		task, err = s.resumeSummaries.Cancel(access.owner, access.scope, id)
	case action == "retry":
		task, err = s.resumeSummaries.Retry(access.owner, access.scope, id, access.token, access.check)
	default:
		writeError(w, 404, "SUMMARY_ACTION_NOT_FOUND", fmt.Errorf("摘要操作没找到，请重新打开"))
		return
	}
	if err != nil {
		status, code := 400, "SUMMARY_INVALID"
		if errors.Is(err, summaryflow.ErrNotFound) {
			status, code = 404, "SUMMARY_NOT_FOUND"
		} else if errors.Is(err, summaryflow.ErrConflict) || errors.Is(err, summaryflow.ErrBusy) {
			status, code = 409, "SUMMARY_CONFLICT"
		}
		writeError(w, status, code, err)
		return
	}
	if err := access.check(r.Context()); err != nil {
		s.writeSummaryAccessError(w, err)
		return
	}
	writeSuccess(w, 200, struct {
		Task summaryflow.Task `json:"task"`
	}{task})
}

// summaryAvailable 返回明确更新入口，而非在未初始化时访问空管理器。
func (s *Server) summaryAvailable(w http.ResponseWriter) bool {
	if s.resumeSummaries == nil || s.cloud == nil {
		writeError(w, 503, "SUMMARY_UNAVAILABLE", fmt.Errorf("本地摘要还没准备好，请更新并重启本地程序"))
		return false
	}
	return true
}

// decodeSummaryRequest 限制职业源请求大小并拒绝额外对象，防止把无限正文送入模型。
func decodeSummaryRequest(w http.ResponseWriter, r *http.Request, target *summaryRequest) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	decoder := json.NewDecoder(r.Body)
	if decoder.Decode(target) != nil {
		writeError(w, 400, "SUMMARY_BODY_INVALID", fmt.Errorf("摘要资料没读出来，请精简后重试"))
		return false
	}
	var extra json.RawMessage
	if decoder.Decode(&extra) != io.EOF {
		writeError(w, 400, "SUMMARY_BODY_INVALID", fmt.Errorf("摘要资料不完整，请重新读取"))
		return false
	}
	return true
}

// writeSummaryAccessError 输出稳定授权错误，不透传第三方响应和私有凭证。
func (s *Server) writeSummaryAccessError(w http.ResponseWriter, err error) {
	var accessErr *summaryAccessError
	if !errors.As(err, &accessErr) {
		accessErr = &summaryAccessError{"SESSION_INVALID", "登录验证没完成，请重新连接后继续。"}
	}
	status := 401
	if accessErr.code == "WEB_SESSION_REQUIRED" {
		status = 403
	}
	writeError(w, status, accessErr.code, accessErr)
}
