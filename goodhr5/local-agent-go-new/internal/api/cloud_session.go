// 本文件负责本地程序的云端登录态同步接口，以及云端 WS 命令触发的岗位启动入口。
package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"goodhr5/local-agent-go-new/internal/flow/shared"
)

// cloudSessionState 保存在内存里的云端会话令牌，只存最新一份，程序重启后失效。
type cloudSessionState struct {
	mu    sync.RWMutex
	token string
}

// handleCloudSessionSet 接收控制台页面同步过来的云端登录令牌，供 WS 客户端建连使用。
func (s *Server) handleCloudSessionSet(w http.ResponseWriter, r *http.Request) {
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
	// 先验证令牌有效，再保存，避免本地 WS 客户端拿失效令牌反复重连。
	if _, err := s.cloud.ValidateSession(r.Context(), token); err != nil {
		writeError(w, http.StatusUnauthorized, "CLOUD_SESSION_INVALID", err)
		return
	}
	s.cloudSession.mu.Lock()
	s.cloudSession.token = token
	s.cloudSession.mu.Unlock()
	writeSuccess(w, http.StatusOK, map[string]any{"ok": true})
}

// handleCloudSessionGet 返回本地程序当前的云端登录态同步情况。
func (s *Server) handleCloudSessionGet(w http.ResponseWriter, r *http.Request) {
	s.cloudSession.mu.RLock()
	hasToken := s.cloudSession.token != ""
	s.cloudSession.mu.RUnlock()
	writeSuccess(w, http.StatusOK, map[string]any{
		"ok":           true,
		"has_token":    hasToken,
		"ws_connected": s.cloudWSConnected(),
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
