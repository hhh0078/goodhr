// Package api 文件作用：保留现有 GoodHR 控制台仍在使用的强类型本地接口兼容层。
package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/storage"
)

// pageOpenRequest 表示现有控制台打开招聘平台页面时使用的参数。
type pageOpenRequest struct {
	URL               string          `json:"url"`
	UserDataDir       string          `json:"user_data_dir"`
	ProfileID         string          `json:"profile_id"`
	PlatformID        string          `json:"platform_id"`
	PlatformAccountID string          `json:"platform_account_id"`
	DownloadsPath     string          `json:"downloads_path"`
	Headless          bool            `json:"headless"`
	Humanize          *bool           `json:"humanize"`
	GeoIP             *bool           `json:"geoip"`
	Persistent        bool            `json:"persistent"`
	WaitUntil         string          `json:"wait_until"`
	TimeoutMS         int             `json:"timeout_ms"`
	NewTab            *bool           `json:"new_tab"`
	NewPage           *bool           `json:"new_page"`
	Locale            string          `json:"locale"`
	Timezone          string          `json:"timezone"`
	UserAgent         string          `json:"user_agent"`
	ViewportWidth     int             `json:"viewport_width"`
	ViewportHeight    int             `json:"viewport_height"`
	Proxy             json.RawMessage `json:"proxy"`
	Args              []string        `json:"args"`
}

// localPositionRunRequest 表示现有控制台启动岗位时提交的参数。
type localPositionRunRequest struct {
	Token       string `json:"token"`
	EnableGreet *bool  `json:"enable_greet"`
	Headless    bool   `json:"headless"`
	TaskType    string `json:"task_type"`
}

// screenshotCompatibilityRequest 表示旧控制台提交的本地截图元数据。
type screenshotCompatibilityRequest struct {
	FilePath   string `json:"file_path"`
	Path       string `json:"path"`
	PositionID string `json:"position_id"`
	Label      string `json:"label"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
}

// handlePageOpen 启动或复用指定 Profile，并通过 CloakBrowser 打开平台页面。
func (s *Server) handlePageOpen(w http.ResponseWriter, r *http.Request) {
	if s.runner.HasActive() {
		writeError(w, http.StatusConflict, "TASK_RUNNING", fmt.Errorf("任务正在使用浏览器，现在不能切换页面"))
		return
	}
	var request pageOpenRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err)
		return
	}
	if strings.TrimSpace(request.URL) == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", fmt.Errorf("页面地址不能为空"))
		return
	}
	profileValue := firstNonEmpty(request.UserDataDir, request.ProfileID, request.PlatformAccountID, "default")
	profilePath, err := s.profiles.Resolve(profileValue)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PROFILE", err)
		return
	}
	if err = s.runtime.EnsureWorker(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "RUNTIME_NOT_READY", err)
		return
	}
	humanize := true
	if request.Humanize != nil {
		humanize = *request.Humanize
	}
	proxy, err := decodeProxy(request.Proxy)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PROXY", err)
		return
	}
	geoIP := request.GeoIP
	if geoIP == nil {
		enabled := proxy != nil
		geoIP = &enabled
	}
	locale := strings.TrimSpace(request.Locale)
	timezone := strings.TrimSpace(request.Timezone)
	if proxy == nil || !*geoIP {
		locale = firstNonEmpty(locale, "zh-CN")
		timezone = firstNonEmpty(timezone, "Asia/Shanghai")
	}
	newTab := requestedNewTab(request)
	downloadsPath := firstNonEmpty(request.DownloadsPath, s.cfg.DownloadsDir)
	downloadsPath, err = normalizeDownloadRoot(downloadsPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err)
		return
	}
	result, err := s.browser.StartBrowser(r.Context(), contract.BrowserStartRequest{
		UserDataDir: profilePath, DownloadsPath: downloadsPath,
		Headless: &request.Headless, Humanize: &humanize, GeoIP: geoIP,
		URL: request.URL, WaitUntil: request.WaitUntil, TimeoutMS: request.TimeoutMS, NewTab: newTab,
		Locale: locale, Timezone: timezone,
		UserAgent: request.UserAgent, ViewportWidth: request.ViewportWidth,
		ViewportHeight: request.ViewportHeight, Proxy: proxy, Args: request.Args,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "PAGE_OPEN_FAILED", err)
		return
	}
	s.rememberDownloadRoot(downloadsPath)
	writeSuccess(w, http.StatusOK, result)
}

// requestedNewTab 优先读取新版字段，并兼容旧版 new_page 字段。
func requestedNewTab(request pageOpenRequest) *bool {
	if request.NewTab != nil {
		return request.NewTab
	}
	return request.NewPage
}

// handlePageURL 返回当前 CloakBrowser 页面地址。
func (s *Server) handlePageURL(w http.ResponseWriter, r *http.Request) {
	result, err := s.browser.BrowserStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "PAGE_URL_FAILED", err)
		return
	}
	writeSuccess(w, http.StatusOK, struct {
		URL string `json:"url"`
	}{URL: result.CurrentURL})
}

// handleLocalPosition 分发旧控制台使用的岗位运行、停止、状态和日志接口。
func (s *Server) handleLocalPosition(w http.ResponseWriter, r *http.Request) {
	positionID := strings.TrimSpace(r.PathValue("position_id"))
	if positionID == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", fmt.Errorf("岗位编号不能为空"))
		return
	}
	switch r.PathValue("action") {
	case "run":
		s.handleLocalPositionRun(w, r, positionID)
	case "stop":
		s.handleLocalPositionStop(w, r, positionID)
	case "status":
		s.handleLocalPositionStatus(w, r, positionID)
	case "logs":
		s.handleLocalPositionLogs(w, r, positionID)
	default:
		writeError(w, http.StatusNotFound, "NOT_FOUND", fmt.Errorf("这个岗位接口不存在"))
	}
}

// handleLocalPositionRun 把旧岗位启动入口转换成新版统一任务请求。
func (s *Server) handleLocalPositionRun(w http.ResponseWriter, r *http.Request, positionID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", fmt.Errorf("这个接口只支持 POST"))
		return
	}
	var request localPositionRunRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err)
		return
	}
	taskType := strings.ToLower(strings.TrimSpace(request.TaskType))
	if taskType == "" {
		taskType = "greeting"
	}
	taskID := positionID + "-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 36)
	result, err := s.runner.StartTask(r.Context(), shared.StartRequest{
		TaskID: taskID, PositionID: positionID, TaskType: taskType,
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
	writeSuccess(w, http.StatusAccepted, result)
}

// handleLocalPositionStop 安全停止指定岗位当前正在运行的任务。
func (s *Server) handleLocalPositionStop(w http.ResponseWriter, r *http.Request, positionID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", fmt.Errorf("这个接口只支持 POST"))
		return
	}
	var request struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err)
		return
	}
	task, err := s.runner.StopPosition(r.Context(), positionID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, status, "TASK_NOT_FOUND", err)
		return
	}
	writeSuccess(w, http.StatusOK, task)
}

// handleLocalPositionStatus 返回指定岗位最近一次本地任务状态。
func (s *Server) handleLocalPositionStatus(w http.ResponseWriter, r *http.Request, positionID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", fmt.Errorf("这个接口只支持 GET"))
		return
	}
	task, err := s.store.LatestTaskForPosition(r.Context(), positionID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, status, "TASK_NOT_FOUND", err)
		return
	}
	writeSuccess(w, http.StatusOK, task)
}

// handleLocalPositionLogs 读取、追加或清空指定岗位的本地步骤日志。
func (s *Server) handleLocalPositionLogs(w http.ResponseWriter, r *http.Request, positionID string) {
	switch r.Method {
	case http.MethodGet:
		limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
		logs, err := s.store.ListPositionLogs(r.Context(), positionID, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LOG_LIST_FAILED", err)
			return
		}
		var latestTask *storage.TaskRun
		task, taskErr := s.store.LatestTaskForPosition(r.Context(), positionID)
		if taskErr == nil {
			latestTask = &task
		} else if !errors.Is(taskErr, sql.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "TASK_STATUS_READ_FAILED", taskErr)
			return
		}
		writeSuccess(w, http.StatusOK, struct {
			Logs []storage.TaskLog `json:"logs"`
			Task *storage.TaskRun  `json:"task"`
		}{Logs: logs, Task: latestTask})
	case http.MethodPost:
		var request struct {
			Level   string `json:"level"`
			Message string `json:"message"`
		}
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err)
			return
		}
		item, err := s.store.SaveTaskLog(r.Context(), storage.TaskLog{
			PositionID: positionID, Level: request.Level, Message: request.Message,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "LOG_SAVE_FAILED", err)
			return
		}
		writeSuccess(w, http.StatusOK, struct {
			Log storage.TaskLog `json:"log"`
		}{Log: item})
	case http.MethodDelete:
		if err := s.store.ClearPositionLogs(r.Context(), positionID); err != nil {
			writeError(w, http.StatusBadRequest, "LOG_CLEAR_FAILED", err)
			return
		}
		writeSuccess(w, http.StatusOK, struct {
			Cleared bool `json:"cleared"`
		}{Cleared: true})
	default:
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", fmt.Errorf("这个日志接口不支持当前请求方式"))
	}
}

// handleWorkerStart 启动并健康检查 TypeScript Browser Worker。
func (s *Server) handleWorkerStart(w http.ResponseWriter, r *http.Request) {
	if err := s.runtime.EnsureWorker(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "WORKER_START_FAILED", err)
		return
	}
	writeSuccess(w, http.StatusOK, struct {
		Running bool `json:"running"`
	}{Running: true})
}

// handleWorkerStop 停止本地程序管理的 TypeScript Browser Worker。
func (s *Server) handleWorkerStop(w http.ResponseWriter, _ *http.Request) {
	if s.runner.HasActive() {
		writeError(w, http.StatusConflict, "TASK_RUNNING", fmt.Errorf("任务还在运行，请先安全停止任务"))
		return
	}
	if err := s.runtime.StopWorker(); err != nil {
		writeError(w, http.StatusInternalServerError, "WORKER_STOP_FAILED", err)
		return
	}
	writeSuccess(w, http.StatusOK, struct {
		Running bool `json:"running"`
	}{Running: false})
}

// handleWorkerStatus 返回 Worker 编译和健康状态。
func (s *Server) handleWorkerStatus(w http.ResponseWriter, r *http.Request) {
	buildErr := s.runtime.CheckWorkerBuild()
	healthErr := s.browser.Health(r.Context())
	writeSuccess(w, http.StatusOK, struct {
		Installed bool `json:"installed"`
		Running   bool `json:"running"`
	}{Installed: buildErr == nil, Running: healthErr == nil})
}

// handleOCRStatus 返回本机 OCR 组件是否完整可用。
func (s *Server) handleOCRStatus(w http.ResponseWriter, _ *http.Request) {
	err := s.ocr.Ready()
	message := ""
	if err != nil {
		message = err.Error()
	}
	writeSuccess(w, http.StatusOK, struct {
		Installed bool   `json:"installed"`
		Message   string `json:"message"`
	}{Installed: err == nil, Message: message})
}

// handleOCRRecognize 校验本地图片路径后调用常驻 OCR 组件。
func (s *Server) handleOCRRecognize(w http.ResponseWriter, r *http.Request) {
	var request struct {
		FilePath       string `json:"file_path"`
		Path           string `json:"path"`
		ScreenshotPath string `json:"screenshot_path"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err)
		return
	}
	imagePath := firstNonEmpty(request.FilePath, request.Path, request.ScreenshotPath)
	if !pathWithin(imagePath, s.cfg.DataDir) && !pathWithin(imagePath, s.cfg.ScreenshotsDir) {
		writeError(w, http.StatusBadRequest, "INVALID_IMAGE_PATH", fmt.Errorf("只能识别 GoodHR 本地数据目录内的图片"))
		return
	}
	result, err := s.ocr.Recognize(r.Context(), imagePath)
	if err != nil {
		writeError(w, http.StatusBadRequest, "OCR_FAILED", err)
		return
	}
	writeSuccess(w, http.StatusOK, result)
}

// handleRulesStatus 返回云端平台配置模式的兼容状态。
func (s *Server) handleRulesStatus(w http.ResponseWriter, _ *http.Request) {
	writeSuccess(w, http.StatusOK, struct {
		Status  string   `json:"status"`
		Message string   `json:"message"`
		Rules   []string `json:"rules"`
	}{
		Status: "cloud", Message: "平台规则由云端配置统一下发", Rules: []string{},
	})
}

// handleRulesUpdate 保留旧控制台更新入口，明确告知无需下载独立规则包。
func (s *Server) handleRulesUpdate(w http.ResponseWriter, _ *http.Request) {
	writeSuccess(w, http.StatusOK, struct {
		Updated []string `json:"updated"`
		Message string   `json:"message"`
	}{Updated: []string{}, Message: "平台规则已经由云端统一管理，不需要单独更新"})
}

// handleScreenshots 兼容旧控制台截图记录接口，但不把候选人截图信息写入数据库。
func (s *Server) handleScreenshots(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeSuccess(w, http.StatusOK, struct {
			Screenshots []string `json:"screenshots"`
		}{Screenshots: []string{}})
	case http.MethodPost:
		var request screenshotCompatibilityRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err)
			return
		}
		writeSuccess(w, http.StatusOK, struct {
			Screenshot screenshotCompatibilityRequest `json:"screenshot"`
		}{Screenshot: request})
	default:
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", fmt.Errorf("这个截图接口不支持当前请求方式"))
	}
}

// decodeProxy 解析兼容字符串或对象形式的浏览器代理参数。
func decodeProxy(raw json.RawMessage) (*contract.ProxyConfig, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var server string
	if json.Unmarshal(raw, &server) == nil {
		if strings.TrimSpace(server) == "" {
			return nil, nil
		}
		return &contract.ProxyConfig{Server: strings.TrimSpace(server)}, nil
	}
	var proxy contract.ProxyConfig
	if err := json.Unmarshal(raw, &proxy); err != nil {
		return nil, fmt.Errorf("代理配置格式不正确")
	}
	if strings.TrimSpace(proxy.Server) == "" {
		return nil, fmt.Errorf("代理地址不能为空")
	}
	return &proxy, nil
}

// pathWithin 判断文件是否位于指定根目录内。
func pathWithin(value string, root string) bool {
	value = strings.TrimSpace(value)
	root = strings.TrimSpace(root)
	if value == "" || root == "" || !filepath.IsAbs(value) {
		return false
	}
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(value))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
