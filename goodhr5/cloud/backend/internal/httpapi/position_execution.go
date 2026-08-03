// 本文件负责接收本地程序同步的岗位运行状态、失败通知和运行结果。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	stdlog "log"
	"net/http"
	"strings"
	"time"
)

const minimumPositionAIBalanceUnits int64 = 1000

type positionStartError struct {
	status  int
	code    string
	message string
}

// PositionExecutionService 处理岗位运行状态、候选人同步和结束通知。
type PositionExecutionService struct {
	auth           *AuthService
	store          PositionStore
	positionLogs   PositionLogService
	tenantStore    TenantStore
	accounts       PlatformAccountStore
	candidateStore CandidateStore
	subscriptions  SubscriptionStore
	systemConfigs  SystemConfigStore
	aiWallet       AIWalletStore
	mailer         Mailer
	dailyStats     SystemDailyStatsStore
	userFlow       UserFlowStore
	agents         AgentStore
	autoReply      *PostgresAutoReplyStore
}

// NewPositionExecutionService 创建岗位运行服务。
// 所有运行状态直接归属于岗位，不再创建独立岗位运行记录。
func NewPositionExecutionService(auth *AuthService, store PositionStore, positionLogs PositionLogService, tenantStore TenantStore, accounts PlatformAccountStore, candidateStore CandidateStore, subscriptions SubscriptionStore, systemConfigs SystemConfigStore, aiWallet AIWalletStore, mailer Mailer, dailyStats SystemDailyStatsStore, userFlow UserFlowStore, agents AgentStore, autoReply *PostgresAutoReplyStore) *PositionExecutionService {
	return &PositionExecutionService{
		auth: auth, store: store, positionLogs: positionLogs, tenantStore: tenantStore,
		accounts: accounts, candidateStore: candidateStore, subscriptions: subscriptions,
		systemConfigs: systemConfigs,
		aiWallet:      aiWallet, mailer: mailer, dailyStats: dailyStats, userFlow: userFlow, agents: agents,
		autoReply: autoReply,
	}
}

// Start 同步检查登录、岗位归属、会员、AI 余额和账号运行冲突，通过后才写入 running。
// w 为响应对象，r 为本地程序发起的启动许可请求。
func (s *PositionExecutionService) Start(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writePositionStartError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "这个接口只支持 POST")
		return
	}
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writePositionStartError(w, http.StatusUnauthorized, "SESSION_EXPIRED", "登录状态已经失效，请重新登录")
		return
	}
	var payload struct {
		TaskType  string `json:"task_type"`
		MachineID string `json:"machine_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writePositionStartError(w, http.StatusBadRequest, "INVALID_REQUEST", "启动参数没读明白，请重新试一次")
		return
	}
	positionID := positionSubresourceID(r.URL.Path, "start")
	tenantID, isAdmin := s.getTenantInfo(session.Email)
	position, err := s.store.PositionByID(tenantID, session.Email, positionID, isAdmin)
	if errors.Is(err, ErrNotFound) {
		writePositionStartError(w, http.StatusNotFound, "POSITION_NOT_FOUND", "这个岗位没有找到，可能已经被删除了")
		return
	}
	if err != nil {
		writePositionStartError(w, http.StatusInternalServerError, "POSITION_LOAD_FAILED", "岗位信息暂时没读出来，请稍后再试")
		return
	}
	if failure := s.verifyActiveDevice(session.Email, payload.MachineID); failure != nil {
		writePositionStartError(w, failure.status, failure.code, failure.message)
		return
	}
	if failure := s.claimPositionStart(session.Email, position, payload.TaskType); failure != nil {
		writePositionStartError(w, failure.status, failure.code, failure.message)
		return
	}
	s.recordUserFlow(position.UserEmail, UserFlowUpdate{Step: userFlowPositionStarted, Status: "completed", Source: "local_agent", PositionID: position.ID})
	_ = s.positionLogs.WriteLog(position.ID, position.UserEmail, "info", "岗位启动检查通过，已经开始运行")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "running"})
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
		if err := s.sendPositionStatusNotice(position, "stopped", "", 0, 0); err != nil {
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
		Status          string `json:"status"`
		TaskType        string `json:"task_type"`
		RunGreetedCount int    `json:"run_greeted_count"`
		RunSkippedCount int    `json:"run_skipped_count"`
		MachineID       string `json:"machine_id"`
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
	if status == "running" {
		if failure := s.verifyActiveDevice(session.Email, payload.MachineID); failure != nil {
			writePositionStartError(w, failure.status, failure.code, failure.message)
			return
		}
		if failure := s.claimPositionStart(session.Email, position, payload.TaskType); failure != nil {
			writePositionStartError(w, failure.status, failure.code, failure.message)
			return
		}
		s.recordUserFlow(position.UserEmail, UserFlowUpdate{Step: userFlowPositionStarted, Status: "completed", Source: "local_agent", PositionID: position.ID})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": status, "notice_sent": false})
		return
	}
	noticeSent := false
	statusChanged := position.Status != status
	noticePosition := position
	if statusChanged {
		noticePosition = positionWithRunGreeted(position, payload.RunGreetedCount)
	}
	if status == "completed" {
		if statusChanged {
			if err := s.sendPositionStatusNotice(noticePosition, "completed", "", payload.RunGreetedCount, payload.RunSkippedCount); err != nil {
				writeError(w, http.StatusBadGateway, "failed to send position completion notice: "+err.Error())
				return
			}
			noticeSent = true
			if err := s.store.FinishPositionRun(position.ID, status, payload.RunGreetedCount); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to update position status")
				return
			}
			_ = s.positionLogs.WriteLog(position.ID, position.UserEmail, "info", "岗位运行已完成")
		} else {
			// completed 状态只会在完成邮件发送成功后写入，因此重复同步可直接确认邮件已经发送。
			noticeSent = true
		}
	} else if statusChanged {
		if err := s.store.FinishPositionRun(position.ID, status, payload.RunGreetedCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update position status")
			return
		}
		if status == "stopped" {
			_ = s.positionLogs.WriteLog(position.ID, position.UserEmail, "warn", "岗位运行已停止")
			if err := s.sendPositionStatusNotice(noticePosition, "stopped", "", payload.RunGreetedCount, payload.RunSkippedCount); err != nil {
				stdlog.Printf("[岗位邮件] 发送岗位停止提醒失败 position=%s user=%s err=%v", position.ID, position.UserEmail, err)
			}
		}
	}
	if payload.RunGreetedCount > 0 {
		s.recordUserFlow(position.UserEmail, UserFlowUpdate{Step: userFlowFirstGreetSuccess, Status: "completed", Source: "local_agent", PositionID: position.ID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": status, "notice_sent": noticeSent})
}

// verifyActiveDevice 确认岗位启动请求来自当前账号刚刚绑定的稳定设备。
func (s *PositionExecutionService) verifyActiveDevice(email string, machineID string) *positionStartError {
	machineID = strings.TrimSpace(machineID)
	if s.agents == nil || !isStableAgentMachineID(machineID) {
		return &positionStartError{
			status: http.StatusForbidden, code: "DEVICE_BINDING_REQUIRED",
			message: "这台电脑还没有完成账号绑定，请刷新后台，等本地程序重新连接后再开始岗位。",
		}
	}
	active, err := s.agents.HasActiveBinding(email, machineID)
	if err != nil || !active {
		return &positionStartError{
			status: http.StatusForbidden, code: "DEVICE_BINDING_REQUIRED",
			message: "这台电脑还没有完成账号绑定，请刷新后台，等本地程序重新连接后再开始岗位。",
		}
	}
	return nil
}

// claimPositionStart 完成所有云端启动条件检查，并原子占用当前账号的运行岗位名额。
// email 为当前账号，position 为岗位快照，taskType 为本地主流程类型。
func (s *PositionExecutionService) claimPositionStart(email string, position Position, taskType string) *positionStartError {
	autoReply := strings.EqualFold(strings.TrimSpace(taskType), "auto_reply")
	usesAI := positionUsesAI(position) || autoReply
	if usesAI {
		subscription, err := s.subscriptions.UserSubscription(email)
		if err != nil {
			return &positionStartError{status: http.StatusServiceUnavailable, code: "SUBSCRIPTION_CHECK_FAILED", message: "会员状态暂时没查清楚，这次我先不乱启动，请稍后再试"}
		}
		access, err := subscriptionAccess(s.systemConfigs, subscription, time.Now())
		if err != nil {
			return &positionStartError{status: http.StatusServiceUnavailable, code: "SUBSCRIPTION_CHECK_FAILED", message: "会员套餐配置暂时没读明白，这次我先不乱启动，请稍后再试"}
		}
		if !access.AllowAI {
			return &positionStartError{status: http.StatusForbidden, code: "SUBSCRIPTION_REQUIRED", message: "这个岗位用了 AI 功能，会员到期后暂时不能启动，请先续费"}
		}
		if autoReply && !access.AllowAutoReply {
			return &positionStartError{status: http.StatusForbidden, code: "AUTO_REPLY_MAX_REQUIRED", message: "自动回复属于 Max 全能版，当前套餐暂时不能使用"}
		}
		if s.aiWallet == nil {
			return &positionStartError{status: http.StatusServiceUnavailable, code: "AI_BALANCE_UNAVAILABLE", message: "AI 余额暂时没查出来，这次我先不乱启动，请稍后再试"}
		}
		balance, err := s.aiWallet.BalanceUnits(email)
		if err != nil {
			return &positionStartError{status: http.StatusServiceUnavailable, code: "AI_BALANCE_UNAVAILABLE", message: "AI 余额暂时没查出来，这次我先不乱启动，请稍后再试"}
		}
		if balance < minimumPositionAIBalanceUnits {
			return &positionStartError{status: http.StatusPaymentRequired, code: "AI_BALANCE_INSUFFICIENT", message: "AI 余额不足 0.10 元，岗位这次没有启动，请先充值"}
		}
	}
	if autoReply {
		if s.autoReply == nil || s.tenantStore == nil {
			return &positionStartError{status: http.StatusServiceUnavailable, code: "AUTO_REPLY_STORAGE_UNAVAILABLE", message: "自动回复存储还没准备好，这次我先不乱启动，请稍后再试"}
		}
		tenant, err := s.tenantStore.GetOrCreateTenant(email)
		if err != nil {
			return &positionStartError{status: http.StatusServiceUnavailable, code: "AUTO_REPLY_CONFIG_LOAD_FAILED", message: "自动回复配置暂时没读出来，这次我先不乱启动"}
		}
		config, err := s.autoReply.GetPositionAutoReplyConfig(context.Background(), tenant.ID, position.ID)
		if errors.Is(err, ErrNotFound) || (err == nil && !config.Enabled) {
			return &positionStartError{status: http.StatusConflict, code: "AUTO_REPLY_NOT_ENABLED", message: "这个岗位还没有开启自动回复，请先保存自动回复配置"}
		}
		if err != nil {
			return &positionStartError{status: http.StatusServiceUnavailable, code: "AUTO_REPLY_CONFIG_LOAD_FAILED", message: "自动回复配置暂时没读出来，这次我先不乱启动"}
		}
	}
	if err := s.store.ClaimPositionStart(email, position.ID); err != nil {
		if errors.Is(err, ErrPositionAlreadyRunning) {
			return &positionStartError{status: http.StatusConflict, code: "POSITION_TASK_CONFLICT", message: "这个账号已经有岗位在运行，请先停掉当前任务再开始新的"}
		}
		if errors.Is(err, ErrNotFound) {
			return &positionStartError{status: http.StatusNotFound, code: "POSITION_NOT_FOUND", message: "这个岗位没有找到，可能已经被删除了"}
		}
		return &positionStartError{status: http.StatusInternalServerError, code: "POSITION_START_FAILED", message: "云端没能记下岗位状态，这次没有启动，请稍后再试"}
	}
	return nil
}

// positionUsesAI 判断岗位的基础筛选或详情筛选是否明确使用 AI。
func positionUsesAI(position Position) bool {
	mode := strings.ToLower(strings.TrimSpace(positionConfigString(position.CommonConfig, "mode_default")))
	detailMode := strings.ToLower(strings.TrimSpace(positionConfigString(position.CommonConfig, "detail_mode")))
	return mode != "keyword" || detailMode == "ai"
}

// positionConfigString 从岗位公共配置安全读取字符串字段。
func positionConfigString(config map[string]any, key string) string {
	if config == nil {
		return ""
	}
	value, ok := config[key]
	if !ok || value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

// writePositionStartError 返回带稳定错误码的岗位启动失败响应。
func writePositionStartError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]any{
		"ok": false,
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
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
		PositionID      string `json:"position_id"`
		ErrorMessage    string `json:"error_message"`
		RunGreetedCount int    `json:"run_greeted_count"`
		RunSkippedCount int    `json:"run_skipped_count"`
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
	noticePosition := position
	if position.Status != status {
		noticePosition = positionWithRunGreeted(position, payload.RunGreetedCount)
	}
	if err := s.store.FinishPositionRun(position.ID, status, payload.RunGreetedCount); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update position status")
		return
	}
	s.recordUserFlow(position.UserEmail, UserFlowUpdate{
		Step: userFlowPositionStarted, Status: "blocked", Source: "local_agent", PositionID: position.ID,
		ReasonCode: userFlowFailureReason(errorMessage), Message: errorMessage,
	})
	if payload.RunGreetedCount > 0 {
		s.recordUserFlow(position.UserEmail, UserFlowUpdate{Step: userFlowFirstGreetSuccess, Status: "completed", Source: "local_agent", PositionID: position.ID})
	}
	if err := s.sendPositionStatusNotice(noticePosition, status, errorMessage, payload.RunGreetedCount, payload.RunSkippedCount); err != nil {
		writeError(w, http.StatusBadGateway, "failed to send position status notice: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "notified"})
}

// sendPositionStatusNotice 发送岗位结束或失败邮件提醒。
func (s *PositionExecutionService) sendPositionStatusNotice(position Position, status, errorMessage string, runGreetedCount int, runSkippedCount int) error {
	if s.mailer == nil {
		return errors.New("mailer not configured")
	}
	if strings.TrimSpace(position.UserEmail) == "" {
		return errors.New("position user email is empty")
	}
	notice := PositionStatusNotice{
		PositionName: position.Name, Status: status, StatusLabel: positionStatusNoticeLabel(status),
		TodayGreetedCount: positionTodayGreetedCount(position),
		RunGreetedCount:   max(runGreetedCount, 0), RunSkippedCount: max(runSkippedCount, 0),
		ErrorMessage: strings.TrimSpace(errorMessage),
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
