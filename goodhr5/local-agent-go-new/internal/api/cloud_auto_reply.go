// Package api 本文件将自动回复启动和状态交给服务器，本机仅串行执行一次性浏览器命令。
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	downloadflow "goodhr5/local-agent-go-new/internal/flow/download"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/replybridge"
	"goodhr5/local-agent-go-new/internal/storage"
	"goodhr5/local-agent-go-new/internal/system/failurediag"
)

// cloudBrowser 复用现有标准浏览器操作和原生下载监听，不包含任何模型或业务存储。
type cloudBrowser struct {
	*client.Client
	*downloadflow.Monitor
}

type cloudRuntimeStatus struct {
	Analysis     *shared.AnalysisStatus `json:"analysis,omitempty"`
	Migration    cloudMigrationState    `json:"migration,omitzero"`
	ErrorMessage string                 `json:"error_message,omitempty"`
	RunID        string                 `json:"run_id"`
	Status       string                 `json:"status"`
	CurrentStep  string                 `json:"current_step"`
	Error        string                 `json:"error"`
	ScannedCount int                    `json:"scanned_count"`
	GreetedCount int                    `json:"greeted_count"`
	SkippedCount int                    `json:"skipped_count"`
}

var errCloudReplySessionChanged = errors.New("当前账号已经变化，请刷新后重试")
var errCloudReplyWebSession = errors.New("请从当前GoodHR网页账号操作")

// validateCloudReplySession 在付费或开始运行前验证网页账号，再检查网络等待期间是否切号。
func (s *Server) validateCloudReplySession(ctx context.Context, credentials cloud.AgentCredentials) error {
	s.extensionMu.Lock()
	owner, generation := s.extensionUserID, s.extensionGeneration
	current := extensionSecretsEqual(credentials.Token, s.extensionToken)
	s.extensionMu.Unlock()
	if !current {
		return errCloudReplySessionChanged
	}
	session, err := s.cloud.ValidateSession(ctx, credentials.Token)
	if err != nil {
		return err
	}
	if session.SessionKind == "browser_extension" || session.UserID != owner {
		return errCloudReplyWebSession
	}
	s.extensionMu.Lock()
	current = generation == s.extensionGeneration && owner == s.extensionUserID && extensionSecretsEqual(credentials.Token, s.extensionToken)
	s.extensionMu.Unlock()
	if !current {
		return errCloudReplySessionChanged
	}
	return nil
}

// cloudReplyCredentials 核对当前网页会话和设备身份，业务请求始终发往服务器。
func (s *Server) cloudReplyCredentials() (cloud.AgentCredentials, error) {
	s.extensionMu.Lock()
	token := s.extensionToken
	owner := s.extensionUserID
	s.extensionMu.Unlock()
	if token == "" || owner == "" {
		return cloud.AgentCredentials{}, fmt.Errorf("请先连接当前GoodHR账号")
	}
	machine, err := s.currentMachineID()
	return cloud.AgentCredentials{Token: token, MachineID: machine}, err
}

// startCloudReply 在服务器创建流程；本机循环只持有连接期间的临时命令和回执。
func (s *Server) startCloudReply(w http.ResponseWriter, r *http.Request, request localAutoReplyStartRequest, platformID, taskID, positionID string) {
	credentials, err := s.cloudReplyCredentials()
	if err != nil || !extensionSecretsEqual(request.Token, credentials.Token) {
		writeError(w, 409, "SESSION_CHANGED", fmt.Errorf("账号已变化，请刷新后重试"))
		return
	}
	if err = s.validateCloudReplySession(r.Context(), credentials); err != nil {
		writeError(w, 409, "SESSION_CHANGED", err)
		return
	}
	s.extensionMu.Lock()
	owner, ownerGeneration := s.extensionUserID, s.extensionOwnerGeneration
	current := extensionSecretsEqual(credentials.Token, s.extensionToken)
	s.extensionMu.Unlock()
	if !current {
		writeError(w, 409, "SESSION_CHANGED", errCloudReplySessionChanged)
		return
	}
	if s.runner != nil && s.runner.HasActive() {
		writeError(w, 409, "LOCAL_TASK_RUNNING", fmt.Errorf("请先停止旧版自动回复任务"))
		return
	}
	if err = s.migrateCloudAutoReply(r.Context(), credentials); err != nil {
		writeError(w, 502, "AUTO_REPLY_MIGRATION_FAILED", err)
		return
	}
	executor, err := replybridge.NewExecutor(platformID, &cloudBrowser{s.browser, s.downloads})
	if err != nil {
		writeError(w, 400, "PLATFORM_UNAVAILABLE", err)
		return
	}
	var status cloudRuntimeStatus
	err = s.cloud.AutoReplyRuntimeRequest(r.Context(), credentials, "start", map[string]any{"platform_id": platformID, "position_id": positionID, "platform_account_id": request.PlatformAccountID, "task_id": taskID, "auto_reply_target": request.AutoReplyTarget}, &status)
	if err != nil {
		writeError(w, 502, "CLOUD_AUTO_REPLY_START_FAILED", err)
		return
	}
	if status.RunID == "" {
		writeError(w, 502, "CLOUD_RUN_REQUIRED", fmt.Errorf("服务器尚未确认任务编号"))
		return
	}
	s.extensionMu.Lock()
	if s.extensionUserID != owner || s.extensionOwnerGeneration != ownerGeneration {
		s.extensionMu.Unlock()
		writeError(w, 409, "SESSION_CHANGED", errCloudReplySessionChanged)
		return
	}
	s.cloudReplyMu.Lock()
	if s.cloudReplyCancel != nil {
		s.cloudReplyCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cloudReplyCancel = cancel
	s.cloudReplyRun = status.RunID
	s.cloudReplyActive = true
	s.cloudReplyMu.Unlock()
	s.extensionMu.Unlock()
	go s.pumpCloudReply(ctx, credentials, owner, ownerGeneration, status.RunID, executor)
	writeSuccess(w, http.StatusAccepted, status)
}

// pumpCloudReply 断线时只重交同一回执，页面操作不会因HTTP重试执行第二次。
func (s *Server) pumpCloudReply(ctx context.Context, credentials cloud.AgentCredentials, owner string, ownerGeneration uint64, runID string, executor *replybridge.Executor) {
	defer func() {
		s.cloudReplyMu.Lock()
		if s.cloudReplyRun == runID {
			s.cloudReplyActive = false
		}
		s.cloudReplyMu.Unlock()
	}()
	cursorKey := "cloud_reply_log_cursor:" + owner + ":" + runID
	var after int64
	if s.store != nil {
		value, _ := s.store.GetSetting(ctx, cursorKey)
		after, _ = strconv.ParseInt(value, 10, 64)
	}
	for ctx.Err() == nil {
		current, err := s.cloudReplyRunCredentials(credentials, owner, ownerGeneration)
		if err != nil {
			return
		}
		var response struct {
			Failures []cloud.AutoReplyFailureNotice `json:"failures"`
			OK       bool                           `json:"ok"`
			Command  *replybridge.Command           `json:"command"`
			Running  *bool                          `json:"running"`
		}
		pollCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		err = s.cloud.AutoReplyRuntimeRequest(pollCtx, current, "next", map[string]any{"run_id": runID, "after_failure": after}, &response)
		cancel()
		if err != nil {
			if cloudDeviceTerminal(err) {
				return
			}
			if !waitCloudDevice(ctx) {
				return
			}
			continue
		}
		if len(response.Failures) > 0 {
			if _, err = s.cloudReplyRunCredentials(credentials, owner, ownerGeneration); err != nil || ctx.Err() != nil {
				return
			}
			for _, notice := range response.Failures {
				if notice.Sequence <= after {
					continue
				}
				if err = s.saveCloudFailure(ctx, runID, cursorKey, notice); err != nil {
					break
				}
				after = notice.Sequence
			}
			// 本次浏览器命令只执行一次。若仅剩日志补传，保留轮询直到它落盘或用户退出。
			if err != nil && response.Command == nil {
				if !waitCloudDevice(ctx) {
					return
				}
				continue
			}
		}
		if response.Running != nil && !*response.Running {
			return
		}
		if !response.OK {
			return
		}
		if response.Command == nil {
			continue
		}
		command := *response.Command
		if command.RunID != runID {
			return
		}
		if _, err = s.cloudReplyRunCredentials(credentials, owner, ownerGeneration); err != nil || ctx.Err() != nil {
			return
		}
		result := executor.Execute(ctx, command)
		for ctx.Err() == nil {
			current, checkErr := s.cloudReplyRunCredentials(credentials, owner, ownerGeneration)
			if checkErr != nil {
				return
			}
			var accepted struct {
				OK bool `json:"ok"`
			}
			err = s.cloud.AutoReplyRuntimeRequest(ctx, current, "complete", map[string]any{"run_id": runID, "result": result}, &accepted)
			if err == nil && accepted.OK {
				executor.Acknowledge(command.ID)
				break
			}
			if err == nil {
				return
			}
			if cloudDeviceTerminal(err) {
				return
			}
			if !waitCloudDevice(ctx) {
				return
			}
		}
	}
}

// cloudReplyRunCredentials 允许已验证的同账号凭证续签；退出再登录也不能续接旧运行。
func (s *Server) cloudReplyRunCredentials(initial cloud.AgentCredentials, owner string, ownerGeneration uint64) (cloud.AgentCredentials, error) {
	s.extensionMu.Lock()
	defer s.extensionMu.Unlock()
	if owner == "" || s.extensionUserID != owner || s.extensionOwnerGeneration != ownerGeneration || s.extensionToken == "" {
		return cloud.AgentCredentials{}, errCloudReplySessionChanged
	}
	return cloud.AgentCredentials{Token: s.extensionToken, MachineID: initial.MachineID}, nil
}

// cloudDeviceTerminal 拒绝、过期任务和无效回执不通过轮询重试，短暂网络错误仍可重交同一结果。
func cloudDeviceTerminal(err error) bool {
	var apiErr *cloud.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 && apiErr.StatusCode != 408 && apiErr.StatusCode != 429
}

// waitCloudDevice 有界退避等待设备重新连接，取消后不再领取或执行命令。
func waitCloudDevice(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(time.Second):
		return true
	}
}

// cloudReplyStatus 从服务器读取运行状态，前端刷新时也能看到同一任务。
func (s *Server) cloudReplyStatus(w http.ResponseWriter, r *http.Request) {
	credentials, err := s.cloudReplyCredentials()
	if err != nil {
		writeSuccess(w, 200, cloudRuntimeStatus{Status: "stopped"})
		return
	}
	var status cloudRuntimeStatus
	if err = s.cloud.AutoReplyRuntimeRequest(r.Context(), credentials, "status", map[string]any{}, &status); err != nil {
		writeError(w, 502, "CLOUD_STATUS_UNAVAILABLE", err)
		return
	}
	status.Migration = s.cloudMigrationProgress()
	if status.Migration.Status == "failed" {
		status.Status = "failed"
		status.CurrentStep = status.Migration.Step
		status.ErrorMessage = status.Migration.Error
	}
	writeSuccess(w, 200, status)
}

// stopCloudReply 先让服务器停止调度，再停止本机领取动作；所有最终记录保存在服务器。
func (s *Server) stopCloudReply(w http.ResponseWriter, r *http.Request) {
	credentials, err := s.cloudReplyCredentials()
	if err != nil {
		writeSuccess(w, 200, cloudRuntimeStatus{Status: "stopped"})
		return
	}
	var status cloudRuntimeStatus
	if err = s.cloud.AutoReplyRuntimeRequest(r.Context(), credentials, "status", map[string]any{}, &status); err != nil {
		writeError(w, 502, "CLOUD_STOP_UNAVAILABLE", err)
		return
	}
	if status.RunID == "" {
		writeSuccess(w, 200, status)
		return
	}
	if err = s.cloud.AutoReplyRuntimeRequest(r.Context(), credentials, "stop", map[string]any{"run_id": status.RunID}, &status); err != nil {
		writeError(w, 502, "CLOUD_STOP_UNAVAILABLE", err)
		return
	}
	s.cloudReplyMu.Lock()
	if s.cloudReplyCancel != nil {
		s.cloudReplyCancel()
		s.cloudReplyCancel = nil
	}
	s.cloudReplyMu.Unlock()
	writeSuccess(w, 200, status)
}

// retryCloudReplyReports 让服务器恢复简历和报告待办，不启动本机AI或重新发送聊天。
func (s *Server) retryCloudReplyReports(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "METHOD_NOT_ALLOWED", fmt.Errorf("请点击重试按钮"))
		return
	}
	var request agentBindRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, 400, "INVALID_REQUEST", err)
		return
	}
	if request.Token == "" {
		writeError(w, 400, "TOKEN_REQUIRED", fmt.Errorf("请先登录"))
		return
	}
	credentials, err := s.cloudReplyCredentials()
	if err != nil || !extensionSecretsEqual(credentials.Token, request.Token) {
		writeError(w, 409, "SESSION_CHANGED", fmt.Errorf("请重新连接当前账号"))
		return
	}
	if err = s.validateCloudReplySession(r.Context(), credentials); err != nil {
		status := 409
		if errors.Is(err, errCloudReplyWebSession) {
			status = 403
		}
		writeError(w, status, "SESSION_CHANGED", err)
		return
	}
	var result map[string]any
	if err = s.cloud.AutoReplyRuntimeRequest(r.Context(), credentials, "retry-reports", map[string]any{}, &result); err != nil {
		writeError(w, 502, "CLOUD_RETRY_FAILED", err)
		return
	}
	writeSuccess(w, 200, result)
}

// saveCloudFailure 只镜像当前已认证运行的安全分类，不读取候选人资料，也不会重放网页命令。
func (s *Server) saveCloudFailure(ctx context.Context, runID, cursor string, n cloud.AutoReplyFailureNotice) error {
	if s.store == nil {
		return fmt.Errorf("本地岗位日志未准备好")
	}
	if len(n.Details) == 0 || n.At.IsZero() || n.PositionID == "" {
		return fmt.Errorf("服务器岗位故障小票不完整")
	}
	out, err := json.Marshal(struct {
		Details []failurediag.Detail `json:"failure_diagnostics"`
	}{n.Details})
	if err != nil {
		return err
	}
	summary := failurediag.Summary(n.Details)
	status, level := "failed", "error"
	if n.Status == "warning" || n.Status == "warn" {
		status, level = "warning", "warn"
	}
	return s.store.SaveServerFailureLog(ctx, storage.TaskLog{PositionID: n.PositionID, TaskID: runID, Flow: "auto_reply", Step: n.Step, Status: status, Level: level, Category: "服务器自动回复",
		Message: "服务器自动回复步骤：" + summary, ErrorMessage: summary, Output: out, CreatedAt: n.At.UTC().Format(time.RFC3339Nano)}, cursor, n.Sequence)
}
