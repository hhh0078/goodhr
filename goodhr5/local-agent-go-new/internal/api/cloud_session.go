// 本文件负责本地程序的云端登录态同步接口，以及云端 WS 命令触发的岗位启动入口。
package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// cloudSessionState 保存当前命令通道凭证；已验证凭证另存于当前用户私有文件，重启后仍由云端鉴权。
type cloudSessionState struct {
	mu    sync.RWMutex
	token string
}

// handleCloudSessionSet 接收控制台页面同步过来的云端登录令牌，供 WS 客户端建连使用。
func (s *Server) handleCloudSessionSet(w http.ResponseWriter, r *http.Request) {
	if !sessionWriteAllowed(w, r) {
		return
	}
	var request struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err)
		return
	}
	token := strings.TrimSpace(request.Token)
	if token == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", fmt.Errorf("token 不能为空"))
		return
	}
	if s.cloud == nil {
		writeError(w, http.StatusServiceUnavailable, "CLOUD_SESSION_UNAVAILABLE", fmt.Errorf("云端连接还没准备好，请重启后再试"))
		return
	}
	generation := s.beginCloudSessionRequest(token)
	defer s.endCloudSessionRequest(generation)
	// 网络等待不持锁；较早的校验不能覆盖后来的换号或退出。
	session, err := s.cloud.ValidateSession(r.Context(), token)
	if err != nil {
		status := http.StatusBadGateway
		var apiErr *cloud.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized {
			status = http.StatusUnauthorized
			s.CloudSessionRejected(token)
		}
		writeError(w, status, "CLOUD_SESSION_INVALID", fmt.Errorf("云端登录状态暂时没确认好，请重试"))
		return
	}
	if session.SessionKind != "" {
		writeError(w, http.StatusForbidden, "CLOUD_SESSION_RESTRICTED", fmt.Errorf("请使用 GoodHR 网页账号连接本地程序"))
		return
	}
	s.extensionMu.Lock()
	defer s.extensionMu.Unlock()
	if generation != s.sessionGeneration {
		writeError(w, http.StatusConflict, "CLOUD_SESSION_CHANGED", fmt.Errorf("登录账号已经变化，请重新连接"))
		return
	}
	if err = s.saveCloudSessionLocked(token, session.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, "CLOUD_SESSION_SAVE_FAILED", err)
		return
	}
	s.startupSync.start(token)
	writeSuccess(w, http.StatusOK, map[string]any{"ok": true})
}

// sessionWriteAllowed 显式拒绝非信任网页写入或清理会话；CORS 响应头不能阻止简单跨域请求。
func sessionWriteAllowed(w http.ResponseWriter, r *http.Request) bool {
	if origin := r.Header.Get("Origin"); origin != "" && !allowedOrigin(origin) {
		writeError(w, http.StatusForbidden, "SESSION_ORIGIN_DENIED", fmt.Errorf("请从 GoodHR 页面连接本地程序"))
		return false
	}
	return true
}

// CloudSessionConnected 在握手成功后有界恢复现有账号绑定；离线重试沿用 WS 循环，不新增后台轮询。
func (s *Server) CloudSessionConnected(ctx context.Context, token string) {
	if s.cloud == nil {
		return
	}
	s.extensionMu.Lock()
	current := extensionSecretsEqual(token, s.currentCloudToken())
	s.extensionMu.Unlock()
	if !current {
		return
	}
	checkCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	session, err := s.cloud.ValidateSession(checkCtx, token)
	if err != nil {
		var apiErr *cloud.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized {
			s.CloudSessionRejected(token)
		}
		return
	}
	s.extensionMu.Lock()
	defer s.extensionMu.Unlock()
	if !extensionSecretsEqual(token, s.currentCloudToken()) || session.SessionKind != "" {
		return
	}
	s.bindExtensionSessionLocked(token, session.UserID)
	if s.incidents != nil {
		s.incidents.Bind(session.UserID, token)
	}
	s.startupSync.start(token)
}

// handleCloudSessionGet 返回本地程序当前的云端登录态同步情况。
func (s *Server) handleCloudSessionGet(w http.ResponseWriter, r *http.Request) {
	s.cloudSession.mu.RLock()
	hasToken := s.cloudSession.token != ""
	s.cloudSession.mu.RUnlock()
	s.cloudWSStatusMu.RLock()
	errorCode, httpStatus := "", 0
	if s.cloudWSDiagnostic != nil {
		errorCode, httpStatus = s.cloudWSDiagnostic()
	}
	s.cloudWSStatusMu.RUnlock()
	writeSuccess(w, http.StatusOK, map[string]any{
		"ok":             true,
		"has_token":      hasToken,
		"ws_connected":   s.cloudWSConnected(),
		"ws_error_code":  errorCode,
		"ws_http_status": httpStatus,
	})
}

// currentCloudToken 返回当前保存的云端令牌，可能为空。
func (s *Server) currentCloudToken() string {
	s.cloudSession.mu.RLock()
	defer s.cloudSession.mu.RUnlock()
	return s.cloudSession.token
}

// CurrentCloudToken 供 bootstrap 读取云端令牌，用于 WS 客户端建连。
func (s *Server) CurrentCloudToken() string {
	return s.currentCloudToken()
}

// cloudWSConnected 返回云端 WS 客户端连接状态；客户端由 bootstrap 注入，未注入时返回 false。
func (s *Server) cloudWSConnected() bool {
	s.cloudWSStatusMu.RLock()
	defer s.cloudWSStatusMu.RUnlock()
	if s.cloudWSStatus == nil {
		return false
	}
	return s.cloudWSStatus()
}

// SetCloudWSStatus 由 bootstrap 注入 WS 连接状态查询函数。
func (s *Server) SetCloudWSStatus(provider func() bool) {
	s.cloudWSStatusMu.Lock()
	s.cloudWSStatus = provider
	s.cloudWSStatusMu.Unlock()
}

// SetCloudWSDiagnostic 注入仅包含稳定错误码和 HTTP 状态的连接诊断，不暴露登录凭证或响应原文。
func (s *Server) SetCloudWSDiagnostic(provider func() (string, int)) {
	s.cloudWSStatusMu.Lock()
	s.cloudWSDiagnostic = provider
	s.cloudWSStatusMu.Unlock()
}

// CloudStartFromWS 是云端 WS 命令触发的岗位启动入口：
// 同步做快速校验并占用浏览器操作锁，接受后异步执行完整任务流程。
func (s *Server) CloudStartFromWS(positionID, taskType string) (bool, string) {
	positionID = strings.TrimSpace(positionID)
	taskType = strings.TrimSpace(taskType)
	if taskType == "" {
		taskType = "greeting"
	}
	if strings.EqualFold(taskType, "auto_reply") {
		return false, "自动回复任务请在控制台操作，连接器暂不支持"
	}
	token := strings.TrimSpace(s.currentCloudToken())
	if token == "" {
		return false, "本地程序还没登录云端账号，请打开 GoodHR 控制台登录"
	}
	if positionID == "" {
		return false, "岗位编号为空，我这边没法启动"
	}
	// API 曾暂时离线时，新的命令复用现有可重试检查；门闩通过前不接受必然失败的任务。
	s.startupSync.start(token)
	if s.startupSync != nil && s.startupSync.runtime.StartupCamoufoxSyncPending() {
		return false, "正在检查浏览器组件，请稍后再试"
	}
	// 浏览器操作互斥：桌面端同一时间只允许一个浏览器操作。
	if !s.browserOperationMu.TryLock() {
		return false, "本地程序正在执行其他浏览器操作，请稍后再试"
	}
	go func() {
		defer s.browserOperationMu.Unlock()
		log.Printf("[云端WS客户端] 开始执行云端下发的任务 position=%s task_type=%s", positionID, taskType)
		result, err := s.runner.StartTask(context.Background(), shared.StartRequest{
			PositionID: positionID,
			TaskType:   taskType,
			Token:      token,
			Headless:   false,
		})
		if err != nil {
			log.Printf("[云端WS客户端] 云端任务执行结束失败 position=%s err=%v", positionID, err)
			return
		}
		log.Printf("[云端WS客户端] 云端任务执行结束 position=%s task=%s status=%s", positionID, result.Task.TaskID, result.Task.Status)
	}()
	return true, "任务已下发给本地程序，马上开始筛选"
}
