// Package api 文件作用：提供自动回复全局启动、停止和状态接口。
package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/lifecycle"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	cloudintegration "goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform"
)

type localAutoReplyStartRequest struct {
	Token    string `json:"token"`
	Headless bool   `json:"headless"`
}

// handleLocalAutoReply 分发自动回复全局启动、停止和状态接口。
func (s *Server) handleLocalAutoReply(w http.ResponseWriter, r *http.Request) {
	switch r.PathValue("action") {
	case "start":
		s.handleLocalAutoReplyStart(w, r)
	case "stop":
		s.handleLocalAutoReplyStop(w, r)
	case "status":
		s.handleLocalAutoReplyStatus(w, r)
	default:
		writeError(w, http.StatusNotFound, "NOT_FOUND", fmt.Errorf("这个自动回复接口不存在"))
	}
}

// handleLocalAutoReplyStart 根据当前浏览器页面平台启动全局自动回复任务。
func (s *Server) handleLocalAutoReplyStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", fmt.Errorf("这个接口只支持 POST"))
		return
	}
	var request localAutoReplyStartRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err)
		return
	}
	request.Token = strings.TrimSpace(request.Token)
	if request.Token == "" {
		writeError(w, http.StatusBadRequest, "TOKEN_REQUIRED", fmt.Errorf("登录凭证不能为空，请重新登录"))
		return
	}
	platformID, err := s.currentAutoReplyPlatformID(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, "PLATFORM_NOT_DETECTED", err)
		return
	}
	machineID, err := s.currentMachineID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DEVICE_ID_UNAVAILABLE", err)
		return
	}
	positions, err := s.cloud.AutoReplySnapshots(r.Context(), cloudintegration.AgentCredentials{
		Token: request.Token, MachineID: machineID,
	}, platformID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "AUTO_REPLY_POSITIONS_FAILED", err)
		return
	}
	if len(positions) == 0 {
		writeError(w, http.StatusConflict, "AUTO_REPLY_NO_ENABLED_POSITIONS", fmt.Errorf("当前平台没有开启自动回复的岗位，我先不乱接消息"))
		return
	}
	taskID := "auto-reply-" + platformID + "-" + strconvNano36()
	result, err := s.runner.StartTask(r.Context(), shared.StartRequest{
		TaskID: taskID, PositionID: positions[0].Position.ID, TaskType: "auto_reply",
		Token: request.Token, Headless: request.Headless,
	})
	if err != nil {
		writeJSON(w, http.StatusConflict, struct {
			OK        bool                 `json:"ok"`
			Error     errorBody            `json:"error"`
			Preflight []preflightStepAlias `json:"preflight"`
		}{
			OK: false, Error: errorBody{Code: "TASK_START_FAILED", Message: err.Error()},
			Preflight: convertPreflightSteps(result.Preflight),
		})
		return
	}
	writeSuccess(w, http.StatusAccepted, struct {
		lifecycle.StartResult
		PlatformID       string `json:"platform_id"`
		EnabledPositions int    `json:"enabled_positions"`
	}{StartResult: result, PlatformID: platformID, EnabledPositions: len(positions)})
}

// handleLocalAutoReplyStop 请求自动回复任务在安全点停止。
func (s *Server) handleLocalAutoReplyStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", fmt.Errorf("这个接口只支持 POST"))
		return
	}
	task, err := s.runner.StopTaskType(r.Context(), "auto_reply")
	if errors.Is(err, sql.ErrNoRows) {
		writeSuccess(w, http.StatusOK, struct {
			Status string `json:"status"`
		}{Status: "stopped"})
		return
	}
	if err != nil {
		writeError(w, http.StatusNotFound, "TASK_NOT_FOUND", err)
		return
	}
	writeSuccess(w, http.StatusOK, task)
}

// handleLocalAutoReplyStatus 返回自动回复全局状态。
func (s *Server) handleLocalAutoReplyStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", fmt.Errorf("这个接口只支持 GET"))
		return
	}
	snapshot, err := s.runner.TaskTypeStatus(r.Context(), "auto_reply")
	if errors.Is(err, sql.ErrNoRows) {
		writeSuccess(w, http.StatusOK, struct {
			Status string `json:"status"`
		}{Status: "stopped"})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "AUTO_REPLY_STATUS_FAILED", err)
		return
	}
	writeSuccess(w, http.StatusOK, snapshot)
}

// currentAutoReplyPlatformID 根据当前浏览器页面地址识别自动回复平台。
func (s *Server) currentAutoReplyPlatformID(ctx context.Context) (string, error) {
	status, err := s.browser.BrowserStatus(ctx)
	if err != nil {
		return "", fmt.Errorf("读取当前浏览器页面失败：%w", err)
	}
	currentURL := strings.TrimSpace(status.CurrentURL)
	if currentURL == "" {
		return "", fmt.Errorf("当前浏览器还没有打开招聘平台页面")
	}
	matched := make([]string, 0, 1)
	for _, platformID := range []string{"liepin", "zhaopin", "boss", "hliepin"} {
		cfg, err := platform.LoadConfig(platformID)
		if err != nil {
			continue
		}
		if urlHostMatches(currentURL, cfg.EntryURL) || urlHostMatches(currentURL, cfg.MessagesURL) || urlHostMatches(currentURL, cfg.LoginURL) {
			matched = append(matched, platformID)
		}
	}
	if len(matched) == 1 {
		return matched[0], nil
	}
	if len(matched) > 1 {
		return "", fmt.Errorf("当前页面同时像多个招聘平台，我有点拿不准，请只保留一个平台页面后再开始")
	}
	return "", fmt.Errorf("当前页面不是已支持的招聘平台，请先切到招聘平台页面")
}

// currentMachineID 返回本机稳定设备编号。
func (s *Server) currentMachineID() (string, error) {
	if s.deviceID == nil {
		return "", fmt.Errorf("设备绑定能力还没准备好，请重启本地程序后再试")
	}
	machineID, err := s.deviceID()
	if err != nil {
		return "", fmt.Errorf("这台电脑的设备编号暂时没读出来：%w", err)
	}
	machineID = strings.TrimSpace(machineID)
	if machineID == "" {
		return "", fmt.Errorf("这台电脑的设备编号是空的，请重启本地程序后再试")
	}
	return machineID, nil
}

// urlHostMatches 判断当前地址是否属于指定平台配置地址的主机。
func urlHostMatches(currentRawURL string, configuredRawURL string) bool {
	configuredRawURL = strings.TrimSpace(configuredRawURL)
	if configuredRawURL == "" {
		return false
	}
	current, err := url.Parse(currentRawURL)
	if err != nil {
		return false
	}
	configured, err := url.Parse(configuredRawURL)
	if err != nil {
		return false
	}
	currentHost := strings.TrimPrefix(strings.ToLower(current.Hostname()), "www.")
	configuredHost := strings.TrimPrefix(strings.ToLower(configured.Hostname()), "www.")
	return currentHost != "" && configuredHost != "" && currentHost == configuredHost
}

// strconvNano36 返回适合任务编号使用的 36 进制时间戳。
func strconvNano36() string {
	return strconv.FormatInt(time.Now().UTC().UnixNano(), 36)
}
