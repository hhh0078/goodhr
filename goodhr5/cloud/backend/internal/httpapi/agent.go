// 本文件负责提供云端 Agent 机器绑定相关 HTTP API。
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// AgentService 处理本地 Agent 机器绑定和查询请求。
type AgentService struct {
	auth  *AuthService
	store AgentStore
}

type bindAgentRequest struct {
	MachineID    string `json:"machine_id"`
	AgentVersion string `json:"agent_version"`
	LocalPort    int    `json:"local_port"`
}

// NewAgentService 创建 Agent API 服务，并注入认证服务和机器绑定存储。
func NewAgentService(auth *AuthService, store AgentStore) *AgentService {
	return &AgentService{
		auth:  auth,
		store: store,
	}
}

// Bind 保存当前登录用户与本地 Agent 机器码的绑定关系。
func (s *AgentService) Bind(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// 调用认证服务读取当前会话，用于确认是哪一个云端账号在绑定机器。
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	var req bindAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	machineID := strings.TrimSpace(req.MachineID)
	if machineID == "" {
		writeError(w, http.StatusBadRequest, "machine_id is required")
		return
	}

	// 调用 AgentStore 保存绑定关系，后续会替换为 PostgreSQL 实现。
	binding, err := s.store.SaveBinding(AgentBinding{
		UserEmail:    session.Email,
		MachineID:    machineID,
		AgentVersion: strings.TrimSpace(req.AgentVersion),
		LocalPort:    req.LocalPort,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to bind agent")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"agent": map[string]any{
			"machine_id":    binding.MachineID,
			"agent_version": binding.AgentVersion,
			"local_port":    binding.LocalPort,
			"bind_status":   binding.BindStatus,
			"last_seen_at":  binding.LastSeenAt,
		},
	})
}

// Current 返回当前登录用户已经绑定的本地 Agent 信息。
func (s *AgentService) Current(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// 调用认证服务读取当前会话，用于限定只能查询自己的绑定机器。
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	// 调用 AgentStore 查询当前用户的绑定信息，用于云端页面展示机器状态。
	binding, err := s.store.CurrentBinding(session.Email)
	if errors.Is(err, ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":    true,
			"agent": nil,
		})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"agent": map[string]any{
			"machine_id":    binding.MachineID,
			"agent_version": binding.AgentVersion,
			"local_port":    binding.LocalPort,
			"bind_status":   binding.BindStatus,
			"last_seen_at":  binding.LastSeenAt,
		},
	})
}

// currentSession 从请求头中读取 Bearer token 并返回当前登录会话。
func (s *AgentService) currentSession(w http.ResponseWriter, r *http.Request) (Session, bool) {
	// 调用认证服务解析请求会话，避免 Agent API 自己重复处理 token 规则。
	session, err := s.auth.SessionFromRequest(r)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return Session{}, false
	}
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return Session{}, false
	}
	return session, true
}
