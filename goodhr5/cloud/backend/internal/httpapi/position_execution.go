// 本文件负责接收本地程序同步的岗位运行状态、失败通知和运行结果。
package httpapi

import (
	"encoding/json"
	"errors"
	stdlog "log"
	"net/http"
	"strings"
	"time"
)

// PositionExecutionService 处理岗位运行状态、候选人同步和结束通知。
type PositionExecutionService struct {
	auth           *AuthService
	store          PositionStore
	positionLogs   PositionLogService
	tenantStore    TenantStore
	accounts       PlatformAccountStore
	candidateStore CandidateStore
	subscriptions  SubscriptionStore
	mailer         Mailer
	dailyStats     SystemDailyStatsStore
	userFlow       UserFlowStore
}

// NewPositionExecutionService 创建岗位运行服务。
// 所有运行状态直接归属于岗位，不再创建独立岗位运行记录。
func NewPositionExecutionService(auth *AuthService, store PositionStore, positionLogs PositionLogService, tenantStore TenantStore, accounts PlatformAccountStore, candidateStore CandidateStore, subscriptions SubscriptionStore, mailer Mailer, dailyStats SystemDailyStatsStore, userFlow UserFlowStore) *PositionExecutionService {
	return &PositionExecutionService{
		auth: auth, store: store, positionLogs: positionLogs, tenantStore: tenantStore,
		accounts: accounts, candidateStore: candidateStore, subscriptions: subscriptions,
		mailer: mailer, dailyStats: dailyStats, userFlow: userFlow,
	}
}

// Stop 接收本地程序的停止请求，并把岗位状态和原有停止通知同步到云端。
// w 为响应对象，r 为请求对象。
func (s *PositionExecutionService) Stop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	positionID := positionSubresourceID(r.URL.Path, "stop")
	tenantID, isAdmin := s.getTenantInfo(session.Email)
	position, err := s.store.PositionByID(tenantID, session.Email, positionID, isAdmin)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "position not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load position")
		return
	}
	if position.Status != "stopped" {
		_ = s.store.UpdatePositionStatus(position.ID, "stopped")
		_ = s.positionLogs.WriteLog(position.ID, position.UserEmail, "warn", "岗位运行已停止")
		if err := s.sendPositionStatusNotice(position, "stopped", ""); err != nil {
			stdlog.Printf("[岗位邮件] 发送岗位停止提醒失败 position=%s user=%s err=%v", position.ID, position.UserEmail, err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "stopped"})
}

// SyncStatus 接收本地程序同步的岗位运行状态。
func (s *PositionExecutionService) SyncStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	positionID := positionSubresourceID(r.URL.Path, "status")
	var payload struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	status := strings.ToLower(strings.TrimSpace(payload.Status))
	if status != "completed" && status != "stopped" && status != "running" {
		writeError(w, http.StatusBadRequest, "unsupported status")
		return
	}
	tenantID, isAdmin := s.getTenantInfo(session.Email)
	position, err := s.store.PositionByID(tenantID, session.Email, positionID, isAdmin)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "position not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load position")
		return
	}
	noticeSent := false
	statusChanged := position.Status != status
	if status == "completed" {
		if statusChanged {
			if err := s.sendPositionStatusNotice(position, "completed", ""); err != nil {
				writeError(w, http.StatusBadGateway, "failed to send position completion notice: "+err.Error())
				return
			}
			noticeSent = true
			if err := s.store.UpdatePositionStatus(position.ID, status); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to update position status")
				return
			}
			_ = s.positionLogs.WriteLog(position.ID, position.UserEmail, "info", "岗位运行已完成")
		} else {
			// completed 状态只会在完成邮件发送成功后写入，因此重复同步可直接确认邮件已经发送。
			noticeSent = true
		}
	} else if statusChanged {
		if err := s.store.UpdatePositionStatus(position.ID, status); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update position status")
			return
		}
		if status == "stopped" {
			_ = s.positionLogs.WriteLog(position.ID, position.UserEmail, "warn", "岗位运行已停止")
			if err := s.sendPositionStatusNotice(position, "stopped", ""); err != nil {
				stdlog.Printf("[岗位邮件] 发送岗位停止提醒失败 position=%s user=%s err=%v", position.ID, position.UserEmail, err)
			}
		}
	}
	if status == "running" {
		s.recordUserFlow(position.UserEmail, UserFlowUpdate{Step: userFlowPositionStarted, Status: "completed", Source: "local_agent", PositionID: position.ID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": status, "notice_sent": noticeSent})
}

// FailNotice 接收本地程序发送的岗位失败通知并发送邮件。
func (s *PositionExecutionService) FailNotice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.currentSessionForFailNotice(w, r)
	if !ok {
		return
	}
	var payload struct {
		PositionID   string `json:"position_id"`
		ErrorMessage string `json:"error_message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	positionID := strings.TrimSpace(payload.PositionID)
	errorMessage := strings.TrimSpace(payload.ErrorMessage)
	if positionID == "" {
		writeError(w, http.StatusBadRequest, "position_id required")
		return
	}
	tenantID, isAdmin := s.getTenantInfo(session.Email)
	position, err := s.store.PositionByID(tenantID, session.Email, positionID, isAdmin)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "position not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load position")
		return
	}
	status := "failed"
	if strings.Contains(errorMessage, "账号已在其他地方登录") {
		status = "stopped"
	}
	_ = s.store.UpdatePositionStatus(position.ID, status)
	s.recordUserFlow(position.UserEmail, UserFlowUpdate{
		Step: userFlowPositionStarted, Status: "blocked", Source: "local_agent", PositionID: position.ID,
		ReasonCode: userFlowFailureReason(errorMessage), Message: errorMessage,
	})
	if err := s.sendPositionStatusNotice(position, status, errorMessage); err != nil {
		writeError(w, http.StatusBadGateway, "failed to send position status notice: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "notified"})
}

// sendPositionStatusNotice 发送岗位结束或失败邮件提醒。
func (s *PositionExecutionService) sendPositionStatusNotice(position Position, status, errorMessage string) error {
	if s.mailer == nil {
		return errors.New("mailer not configured")
	}
	if strings.TrimSpace(position.UserEmail) == "" {
		return errors.New("position user email is empty")
	}
	notice := PositionStatusNotice{
		PositionID: position.ID, PositionName: position.Name, Status: status,
		StatusLabel: positionStatusNoticeLabel(status), PlatformID: position.PlatformID,
		Mode: positionDefaultMode(position), MatchLimit: position.MatchLimit,
		ScannedCount: position.ScannedCount, GreetedCount: position.GreetedCount,
		SkippedCount: position.SkippedCount, FailedCount: position.FailedCount,
		FinishedAt: time.Now(), ErrorMessage: strings.TrimSpace(errorMessage),
	}
	if err := s.mailer.SendPositionStatus(position.UserEmail, notice); err != nil {
		stdlog.Printf("[岗位邮件] 发送岗位状态提醒失败 position=%s user=%s err=%v", position.ID, position.UserEmail, err)
		return err
	}
	return nil
}

// positionStatusNoticeLabel 返回岗位状态邮件使用的中文状态。
func positionStatusNoticeLabel(status string) string {
	switch status {
	case "failed":
		return "岗位运行失败"
	case "stopped":
		return "岗位运行已停止"
	case "completed":
		return "岗位运行完成"
	default:
		return "岗位运行结束"
	}
}

// currentSession 从请求中读取当前登录会话。
func (s *PositionExecutionService) currentSession(w http.ResponseWriter, r *http.Request) (Session, bool) {
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session invalid or expired")
		return Session{}, false
	}
	return session, true
}

// currentSessionForFailNotice 允许旧登录会话只用于发送岗位停止通知。
func (s *PositionExecutionService) currentSessionForFailNotice(w http.ResponseWriter, r *http.Request) (Session, bool) {
	session, err := s.auth.SessionFromRequest(r)
	if err == nil {
		return session, true
	}
	session, unsafeErr := s.auth.UnsafeSessionFromRequest(r)
	if unsafeErr == nil {
		return session, true
	}
	writeError(w, http.StatusUnauthorized, "session invalid or expired")
	return Session{}, false
}

// getTenantInfo 返回用户团队标识和管理员身份。
func (s *PositionExecutionService) getTenantInfo(email string) (string, bool) {
	tenant, err := s.tenantStore.GetOrCreateTenant(email)
	if err != nil {
		return "", false
	}
	isAdmin, _ := s.tenantStore.IsTenantAdmin(tenant.ID, email)
	return tenant.ID, isAdmin
}

// recordUserFlow 写入业务流程事件，记录失败不影响岗位运行。
func (s *PositionExecutionService) recordUserFlow(email string, update UserFlowUpdate) {
	if s.userFlow == nil || strings.TrimSpace(email) == "" {
		return
	}
	if _, err := s.userFlow.Record(email, update); err != nil {
		stdlog.Printf("[用户流程] 记录失败 user=%s step=%s err=%v", email, update.Step, err)
	}
}

// userFlowFailureReason 把本地岗位运行错误归一为可筛选原因。
func userFlowFailureReason(message string) string {
	value := strings.ToLower(strings.TrimSpace(message))
	switch {
	case strings.Contains(value, "会员"), strings.Contains(value, "subscription"), strings.Contains(value, "expired"):
		return "subscription_expired"
	case strings.Contains(value, "ai"), strings.Contains(value, "模型"), strings.Contains(value, "api key"):
		return "ai_config_invalid"
	case strings.Contains(value, "登录"), strings.Contains(value, "login"), strings.Contains(value, "cookie"):
		return "platform_not_logged_in"
	case strings.Contains(value, "组件"), strings.Contains(value, "runtime"), strings.Contains(value, "node"):
		return "runtime_missing"
	default:
		return "position_start_failed"
	}
}
