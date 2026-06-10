// Package app 负责注册 Go 版本本地程序 HTTP 服务和路由。
package app

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"goodhr5/local-agent-go/internal/browser"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/config"
	"goodhr5/local-agent-go/internal/localai"
	"goodhr5/local-agent-go/internal/localdb"
	"goodhr5/local-agent-go/internal/ocr"
	"goodhr5/local-agent-go/internal/process"
	"goodhr5/local-agent-go/internal/response"
	"goodhr5/local-agent-go/internal/runtime"
	"goodhr5/local-agent-go/internal/taskrunner"
)

// Server 是 Go 版本 Local Agent HTTP 服务。
type Server struct {
	cfg     *config.Config
	runtime *runtime.Manager
	worker  *browser.WorkerManager
	ocr     *ocr.Engine
	db      *localdb.DB
	runner  *taskrunner.Runner
}

// NewServer 创建本地 HTTP 服务。
// cfg 为本地程序配置。
func NewServer(cfg *config.Config) (*Server, error) {
	runtimeManager := runtime.NewManager(cfg)
	db, err := localdb.Open(cfg)
	if err != nil {
		return nil, err
	}
	workerManager := browser.NewWorkerManager(runtimeManager)
	ocrEngine := ocr.New(cfg)
	return &Server{
		cfg:     cfg,
		runtime: runtimeManager,
		worker:  workerManager,
		ocr:     ocrEngine,
		db:      db,
		runner:  taskrunner.New(db, workerManager, ocrEngine, cfg.ProfilesDir, cfg.DownloadsDir, cfg.ScreenshotsDir),
	}, nil
}

// Run 启动本地 HTTP 服务。
// 会优先监听配置端口，失败时尝试到 9009。
func (s *Server) Run() error {
	ln, port, err := process.ListenFirstAvailable(s.cfg.Host, s.cfg.Port, config.MaxPort)
	if err != nil {
		return err
	}
	s.cfg.Port = port
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	server := &http.Server{
		Handler:           s.withCORS(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("GoodHR 5 Go Local Agent started on http://%s", net.JoinHostPort(s.cfg.Host, strconv.Itoa(port)))
	s.openConsoleAfterStart(port)
	return server.Serve(ln)
}

// registerRoutes 注册本地程序路由。
// mux 为 HTTP 路由器。
func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/v1/diagnostics", s.handleDiagnostics)
	mux.HandleFunc("/api/v1/runtime/status", s.handleRuntimeStatus)
	mux.HandleFunc("/api/v1/runtime/ensure", s.handleRuntimeEnsure)
	mux.HandleFunc("/api/v1/runtime/install", s.handleRuntimeInstall)
	mux.HandleFunc("/api/v1/runtime/install-local-worker", s.handleRuntimeInstallLocalWorker)
	mux.HandleFunc("/api/v1/console/status", s.handleConsoleStatus)
	mux.HandleFunc("/api/v1/console/update", s.handleConsoleUpdate)
	mux.HandleFunc("/api/v1/worker/start", s.handleWorkerStart)
	mux.HandleFunc("/api/v1/worker/stop", s.handleWorkerStop)
	mux.HandleFunc("/api/v1/worker/status", s.handleWorkerStatus)
	mux.HandleFunc("/api/v1/local/tasks", s.handleLocalTasks)
	mux.HandleFunc("/api/v1/local/tasks/", s.handleLocalTaskItem)
	mux.HandleFunc("/api/v1/local/candidates", s.handleLocalCandidates)
	mux.HandleFunc("/api/v1/local/candidates/", s.handleLocalCandidateItem)
	mux.HandleFunc("/api/v1/tasks/init", s.handleLegacyTaskInit)
	mux.HandleFunc("/api/v1/tasks/", s.handleLegacyLocalTaskItem)
	mux.HandleFunc("/api/v1/local/positions", s.handleLocalPositions)
	mux.HandleFunc("/api/v1/local/positions/", s.handleLocalPositionItem)
	mux.HandleFunc("/api/v1/local/ai/config", s.handleLocalAIConfig)
	mux.HandleFunc("/api/v1/local/ai/chat", s.handleLocalAIChat)
	mux.HandleFunc("/api/v1/local/ai/vision", s.handleLocalAIVision)
	mux.HandleFunc("/api/v1/local/ocr/status", s.handleLocalOCRStatus)
	mux.HandleFunc("/api/v1/local/ocr/recognize", s.handleLocalOCRRecognize)
	mux.HandleFunc("/api/v1/local/settings", s.handleLocalSettings)
	mux.HandleFunc("/api/v1/local/rules/status", s.handleLocalRulesStatus)
	mux.HandleFunc("/api/v1/local/rules/update", s.handleLocalRulesUpdate)
	mux.HandleFunc("/api/v1/profiles", s.handleProfiles)
	mux.HandleFunc("/api/v1/profiles/", s.handleProfileItem)
	mux.HandleFunc("/api/v1/local/downloads", s.handleLocalDownloads)
	mux.HandleFunc("/api/v1/local/screenshots", s.handleLocalScreenshots)
	mux.HandleFunc("/api/v1/cloud/platform-config", s.handleCloudPlatformConfig)
	mux.HandleFunc("/api/v1/cloud/subscription/status", s.handleCloudSubscriptionStatus)
	mux.HandleFunc("/api/v1/browser/start", s.handleBrowserStart)
	mux.HandleFunc("/api/v1/browser/stop", s.handleBrowserStop)
	mux.HandleFunc("/api/v1/page/open", s.handlePageOpen)
	mux.HandleFunc("/api/v1/page/click", s.handlePageClick)
	mux.HandleFunc("/api/v1/page/type", s.handlePageType)
	mux.HandleFunc("/api/v1/page/scroll", s.handlePageScroll)
	mux.HandleFunc("/api/v1/page/extract-text", s.handlePageExtractText)
	mux.HandleFunc("/api/v1/page/screenshot", s.handlePageScreenshot)
	mux.HandleFunc("/api/v1/page/url", s.handlePageURL)
	mux.HandleFunc("/api/v1/page/cookies", s.handlePageCookies)
	mux.HandleFunc("/api/v1/boss/candidates/scroll", s.handleBossCandidatesScroll)
	mux.HandleFunc("/api/v1/boss/candidates/detail", s.handleBossCandidateDetail)
	mux.HandleFunc("/api/v1/downloads", s.handleDownloads)
	mux.HandleFunc("/", s.handleConsole)
}

// handleHealth 返回本地程序健康状态。
// w 为响应对象，r 为请求对象。
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	response.Success(w, map[string]any{
		"status":         "ok",
		"version":        "go-v2-dev",
		"port":           s.cfg.Port,
		"dataDir":        s.cfg.DataDir,
		"profilesDir":    s.cfg.ProfilesDir,
		"downloadsDir":   s.cfg.DownloadsDir,
		"screenshotsDir": s.cfg.ScreenshotsDir,
		"dbPath":         s.db.Path(),
		"runtime":        s.runtime.Status(),
		"ocr":            s.ocr.Status(),
	})
}

// handleRuntimeStatus 返回运行组件状态。
// w 为响应对象，r 为请求对象。
func (s *Server) handleRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	response.Success(w, s.runtime.Status())
}

// handleRuntimeInstallLocalWorker 从本地源码安装 Node Worker。
// w 为响应对象，r 为请求对象。
func (s *Server) handleRuntimeInstallLocalWorker(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	payload, err := readPayload(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	sourceDir := stringValue(payload["source_dir"])
	if sourceDir == "" {
		sourceDir = defaultWorkerSourceDir()
	}
	result, err := s.runtime.InstallLocalWorker(sourceDir)
	if err != nil {
		response.Error(w, http.StatusConflict, err.Error())
		return
	}
	response.Success(w, result)
}

// handleRuntimeEnsure 检查运行组件是否可用。
// w 为响应对象，r 为请求对象。
func (s *Server) handleRuntimeEnsure(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	status, err := s.runtime.Ensure()
	if err != nil {
		response.Error(w, http.StatusConflict, err.Error())
		return
	}
	response.Success(w, status)
}

// handleRuntimeInstall 根据 manifest 下载并安装运行组件。
// w 为响应对象，r 为请求对象。
func (s *Server) handleRuntimeInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	payload, err := readPayload(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	manifestURL := ""
	if value, ok := payload["manifest_url"].(string); ok {
		manifestURL = value
	}
	status, err := s.runtime.StartInstallFromManifest(manifestURL)
	if err != nil {
		response.Error(w, http.StatusConflict, err.Error())
		return
	}
	response.Success(w, status)
}

// handleWorkerStart 启动 Node Browser Worker。
// w 为响应对象，r 为请求对象。
func (s *Server) handleWorkerStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	status, err := s.worker.Start(r.Context())
	if err != nil {
		response.Error(w, http.StatusConflict, err.Error())
		return
	}
	response.Success(w, status)
}

// handleWorkerStop 停止 Node Browser Worker。
// w 为响应对象，r 为请求对象。
func (s *Server) handleWorkerStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	response.Success(w, s.worker.Stop())
}

// handleWorkerStatus 返回 Node Browser Worker 状态。
// w 为响应对象，r 为请求对象。
func (s *Server) handleWorkerStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	response.Success(w, s.worker.Status())
}

// handleLocalTasks 处理本地任务列表和创建。
// w 为响应对象，r 为请求对象。
func (s *Server) handleLocalTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		tasks, err := s.db.ListTasks()
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.Success(w, map[string]any{"tasks": tasks})
	case http.MethodPost:
		payload, err := readPayload(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		task, err := s.db.CreateTask(payload)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.Success(w, map[string]any{"task": task})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
	}
}

// handleLocalTaskItem 处理单个本地任务相关接口。
// w 为响应对象，r 为请求对象。
func (s *Server) handleLocalTaskItem(w http.ResponseWriter, r *http.Request) {
	taskID, action := localTaskPath(r.URL.Path)
	if taskID == "" {
		response.Error(w, http.StatusBadRequest, "任务 ID 不能为空")
		return
	}
	switch action {
	case "":
		s.handleLocalTaskDetail(w, r, taskID)
	case "status":
		s.handleLocalTaskStatus(w, r, taskID)
	case "logs":
		s.handleLocalTaskLogs(w, r, taskID)
	case "candidates":
		s.handleLocalTaskCandidates(w, r, taskID)
	case "run":
		s.handleLocalTaskRun(w, r, taskID)
	case "stop":
		s.handleLocalTaskStop(w, r, taskID)
	default:
		response.Error(w, http.StatusNotFound, "接口不存在")
	}
}

// handleLegacyLocalTaskItem 兼容前端旧的本地任务路径。
// w 为响应对象，r 为请求对象。
func (s *Server) handleLegacyLocalTaskItem(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) < 2 || parts[0] == "" {
		response.Error(w, http.StatusNotFound, "接口不存在")
		return
	}
	taskID := parts[0]
	switch parts[1] {
	case "start-ws", "stop-ws":
		response.Error(w, http.StatusGone, "Go 本地程序不再使用云端 WebSocket，请使用本地任务运行接口")
	case "candidates":
		s.handleLocalTaskCandidates(w, r, taskID)
	case "screenshots":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
			return
		}
		screenshots, err := s.db.ListScreenshots(taskID)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.Success(w, map[string]any{"screenshots": screenshots})
	case "ocr":
		response.Error(w, http.StatusNotImplemented, "Go 本地程序暂未接入 OCR，请先使用页面解析或图片 AI 详情接口")
	default:
		response.Error(w, http.StatusNotFound, "接口不存在")
	}
}

// handleLegacyTaskInit 兼容旧版任务初始化接口。
// w 为响应对象，r 为请求对象。
func (s *Server) handleLegacyTaskInit(w http.ResponseWriter, r *http.Request) {
	response.Error(w, http.StatusGone, "Go 本地程序不需要旧版任务初始化接口，请使用本地任务接口")
}

// handleLocalTaskDetail 处理单个任务读取和删除。
// w 为响应对象，r 为请求对象，taskID 为任务 ID。
func (s *Server) handleLocalTaskDetail(w http.ResponseWriter, r *http.Request, taskID string) {
	switch r.Method {
	case http.MethodGet:
		task, err := s.db.GetTask(taskID)
		if err != nil {
			response.Error(w, http.StatusNotFound, err.Error())
			return
		}
		response.Success(w, map[string]any{"task": task})
	case http.MethodPut:
		payload, err := readPayload(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		task, err := s.db.UpdateTask(taskID, payload)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.Success(w, map[string]any{"task": task})
	case http.MethodDelete:
		if err := s.db.DeleteTask(taskID); err != nil {
			response.Error(w, http.StatusNotFound, err.Error())
			return
		}
		response.Success(w, map[string]any{"deleted": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
	}
}

// handleLocalTaskStatus 处理任务状态更新。
// w 为响应对象，r 为请求对象，taskID 为任务 ID。
func (s *Server) handleLocalTaskStatus(w http.ResponseWriter, r *http.Request, taskID string) {
	if r.Method == http.MethodGet {
		result, err := s.runner.Status(taskID)
		if err != nil {
			response.Error(w, http.StatusNotFound, err.Error())
			return
		}
		response.Success(w, result)
		return
	}
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	payload, err := readPayload(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	task, err := s.db.UpdateTaskStatus(taskID, stringValue(payload["status"]))
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(w, map[string]any{"task": task})
}

// handleLocalTaskLogs 处理任务日志读取和新增。
// w 为响应对象，r 为请求对象，taskID 为任务 ID。
func (s *Server) handleLocalTaskLogs(w http.ResponseWriter, r *http.Request, taskID string) {
	switch r.Method {
	case http.MethodGet:
		logs, err := s.db.ListTaskLogs(taskID, intValue(r.URL.Query().Get("limit"), 100))
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.Success(w, map[string]any{"logs": logs})
	case http.MethodPost:
		payload, err := readPayload(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		item, err := s.db.AddTaskLog(taskID, stringValue(payload["level"]), stringValue(payload["message"]))
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.Success(w, map[string]any{"log": item})
	case http.MethodDelete:
		if err := s.db.ClearTaskLogs(taskID); err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.Success(w, map[string]any{"cleared": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
	}
}

// handleLocalTaskCandidates 处理任务候选人读取和保存。
// w 为响应对象，r 为请求对象，taskID 为任务 ID。
func (s *Server) handleLocalTaskCandidates(w http.ResponseWriter, r *http.Request, taskID string) {
	candidateID := localCandidateID(r.URL.Path)
	if candidateID != "" {
		if r.Method != http.MethodDelete {
			response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
			return
		}
		if err := s.db.DeleteCandidate(taskID, candidateID); err != nil {
			response.Error(w, http.StatusNotFound, err.Error())
			return
		}
		response.Success(w, map[string]any{"deleted": true})
		return
	}
	switch r.Method {
	case http.MethodGet:
		candidates, err := s.db.ListCandidates(taskID)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.Success(w, map[string]any{"candidates": candidates})
	case http.MethodPost:
		payload, err := readPayload(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		candidate, err := s.db.SaveCandidate(taskID, payload)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.Success(w, map[string]any{"candidate": candidate})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
	}
}

// handleLocalCandidates 处理本地候选人列表和清空。
// w 为响应对象，r 为请求对象。
func (s *Server) handleLocalCandidates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
		filter := localdb.CandidateFilter{
			TaskID:     strings.TrimSpace(r.URL.Query().Get("task_id")),
			PositionID: strings.TrimSpace(r.URL.Query().Get("position_id")),
			Keyword:    strings.TrimSpace(r.URL.Query().Get("keyword")),
			Page:       page,
			PageSize:   pageSize,
		}
		candidates, total, err := s.db.ListCandidatesFiltered(filter)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		if filter.Page <= 0 {
			filter.Page = 1
		}
		if filter.PageSize <= 0 {
			filter.PageSize = 20
		}
		response.Success(w, map[string]any{
			"candidates": candidates,
			"total":      total,
			"page":       filter.Page,
			"page_size":  filter.PageSize,
		})
	case http.MethodDelete:
		deleted, err := s.db.ClearCandidates()
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.Success(w, map[string]any{"deleted": deleted})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
	}
}

// handleLocalCandidateItem 处理本地候选人详情。
// w 为响应对象，r 为请求对象。
func (s *Server) handleLocalCandidateItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	candidateID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/local/candidates/"), "/")
	if candidateID == "" {
		response.Error(w, http.StatusBadRequest, "候选人 ID 不能为空")
		return
	}
	candidate, err := s.db.GetCandidate(candidateID, strings.TrimSpace(r.URL.Query().Get("task_id")))
	if err != nil {
		response.Error(w, http.StatusNotFound, err.Error())
		return
	}
	response.Success(w, map[string]any{"candidate": candidate})
}

// handleLocalTaskRun 启动本地任务运行器。
// w 为响应对象，r 为请求对象，taskID 为任务 ID。
func (s *Server) handleLocalTaskRun(w http.ResponseWriter, r *http.Request, taskID string) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	payload, err := readPayload(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	token := stringValue(payload["token"])
	if token == "" {
		token = bearerToken(r)
	}
	allSettings, _ := s.db.GetSettings()
	greetDelayMin, greetDelayMax := s.localGreetDelaySettings()
	result, err := s.runner.Start(r.Context(), taskID, taskrunner.StartOptions{
		CloudAPIBase:  s.cloudAPIBase(payload),
		Token:         token,
		EnableGreet:   true,
		GreetBeforeDelayMin: greetDelayMin,
		GreetBeforeDelayMax: greetDelayMax,
		GreetRetries:  0,
		ScrollDelayMin:      intValue(allSettings["scroll_delay_min"], 3),
		ScrollDelayMax:      intValue(allSettings["scroll_delay_max"], 8),
		ListViewDelayMin:    floatValue(allSettings["list_view_delay_min"], 1),
		ListViewDelayMax:    floatValue(allSettings["list_view_delay_max"], 2),
		DetailViewDelayMin:  floatValue(allSettings["detail_view_delay_min"], 1),
		DetailViewDelayMax:  floatValue(allSettings["detail_view_delay_max"], 2),
		DetailOpenDelayMin:  floatValue(allSettings["detail_open_delay_min"], 1),
		DetailOpenDelayMax:  floatValue(allSettings["detail_open_delay_max"], 2),
		DetailCloseDelayMin: floatValue(allSettings["detail_close_delay_min"], 1),
		DetailCloseDelayMax: floatValue(allSettings["detail_close_delay_max"], 2),
		RestAfterCandidatesMin: intValue(allSettings["rest_after_candidates_min"], 30),
		RestAfterCandidatesMax: intValue(allSettings["rest_after_candidates_max"], 50),
		RestTimesMin:           intValue(allSettings["rest_times_min"], 2),
		RestTimesMax:           intValue(allSettings["rest_times_max"], 4),
		RestDurationMin:        floatValue(allSettings["rest_duration_min"], 3),
		RestDurationMax:        floatValue(allSettings["rest_duration_max"], 5),
	})
	if err != nil {
		response.Error(w, http.StatusConflict, err.Error())
		return
	}
	response.Success(w, result)
}

// localGreetDelaySettings 读取本地个人配置中的打招呼前等待时间。
// 返回值为最小等待秒数和最大等待秒数，没有配置时使用 1-2 秒。
func (s *Server) localGreetDelaySettings() (float64, float64) {
	settings, err := s.db.GetSettings()
	if err != nil {
		return 1, 2
	}
	minDelay := floatValue(settings["greet_before_delay_min"], 1)
	maxDelay := floatValue(settings["greet_before_delay_max"], 2)
	if minDelay < 0 {
		minDelay = 0
	}
	if maxDelay < minDelay {
		maxDelay = minDelay
	}
	return minDelay, maxDelay
}

// handleLocalTaskStop 停止本地任务运行器。
// w 为响应对象，r 为请求对象，taskID 为任务 ID。
func (s *Server) handleLocalTaskStop(w http.ResponseWriter, r *http.Request, taskID string) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	result, err := s.runner.Stop(taskID)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(w, result)
}

// handleLocalPositions 处理本地岗位模板列表和保存。
// w 为响应对象，r 为请求对象。
func (s *Server) handleLocalPositions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		positions, err := s.db.ListPositions()
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.Success(w, map[string]any{"positions": positions})
	case http.MethodPost:
		payload, err := readPayload(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		position, err := s.db.SavePosition(payload)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.Success(w, map[string]any{"position": position})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
	}
}

// handleLocalPositionItem 处理单个本地岗位模板。
// w 为响应对象，r 为请求对象。
func (s *Server) handleLocalPositionItem(w http.ResponseWriter, r *http.Request) {
	positionID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/local/positions/"), "/")
	if positionID == "" {
		response.Error(w, http.StatusBadRequest, "岗位模板 ID 不能为空")
		return
	}
	if positionID == "default-prompts" {
		s.handleLocalPositionDefaultPrompts(w, r)
		return
	}
	if positionID == "optimize-requirement" {
		s.handleLocalPositionOptimizeRequirement(w, r)
		return
	}
	if r.Method != http.MethodDelete {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	if err := s.db.DeletePosition(positionID); err != nil {
		response.Error(w, http.StatusNotFound, err.Error())
		return
	}
	response.Success(w, map[string]any{"deleted": true})
}

// handleLocalPositionDefaultPrompts 返回本地岗位默认提示词。
// w 为响应对象，r 为请求对象。
func (s *Server) handleLocalPositionDefaultPrompts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	response.Success(w, map[string]any{"prompts": map[string]any{
		"greet_prompt":    "请根据岗位要求和候选人信息，判断是否值得打招呼，并输出 JSON：{\"score\":80,\"reason\":\"理由\"}",
		"optimize_prompt": "请把下面的岗位要求整理得更清晰，保留招聘重点，不要编造信息。",
	}})
}

// handleLocalPositionOptimizeRequirement 使用本地 AI 优化岗位要求。
// w 为响应对象，r 为请求对象。
func (s *Server) handleLocalPositionOptimizeRequirement(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	payload, err := readPayload(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	text := stringValue(payload["text"])
	if text == "" {
		response.Error(w, http.StatusBadRequest, "岗位要求不能为空")
		return
	}
	config, err := s.db.GetAIConfig()
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	client := localai.New(config)
	result, err := client.Chat(r.Context(), map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "请优化下面的岗位要求，输出中文正文即可：\n" + text}},
	})
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(w, map[string]any{"optimized": result.Content, "usage": result.Usage, "elapsed_ms": result.ElapsedMS})
}

// handleLocalAIConfig 处理本地 AI 配置读取和保存。
// w 为响应对象，r 为请求对象。
func (s *Server) handleLocalAIConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		config, err := s.db.GetAIConfig()
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.Success(w, map[string]any{"config": config})
	case http.MethodPost:
		payload, err := readPayload(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		config, err := s.db.SaveAIConfig(payload)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.Success(w, map[string]any{"config": config})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
	}
}

// handleLocalAIChat 处理本地 AI 通用聊天请求。
// w 为响应对象，r 为请求对象。
func (s *Server) handleLocalAIChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	payload, err := readPayload(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	config, err := s.db.GetAIConfig()
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	config = overrideAIConfig(config, mapValue(payload["config"]))
	delete(payload, "config")
	result, err := localai.New(config).Chat(r.Context(), payload)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(w, map[string]any{"content": result.Content, "usage": result.Usage, "elapsed_ms": result.ElapsedMS})
}

// overrideAIConfig 使用请求中的临时配置覆盖本地已保存 AI 配置。
// config 为数据库配置，override 为请求里的 config 字段。
func overrideAIConfig(config localdb.AIConfig, override map[string]any) localdb.AIConfig {
	if len(override) == 0 {
		return config
	}
	if value := firstNonEmptyString(stringValue(override["base_url"]), stringValue(override["api_url"])); value != "" {
		config.BaseURL = value
	}
	if value := stringValue(override["api_key"]); value != "" {
		config.APIKey = value
	}
	if value := firstNonEmptyString(stringValue(override["model"]), stringValue(override["model_id"])); value != "" {
		config.Model = value
	}
	if value, ok := override["temperature"]; ok {
		config.Temperature = floatValue(value, config.Temperature)
	}
	if value, ok := override["timeout"]; ok {
		config.Timeout = intValue(value, config.Timeout)
	}
	if extra := mapValue(override["extra"]); len(extra) > 0 {
		config.Extra = extra
	}
	if extra := mapValue(override["extra_body"]); len(extra) > 0 {
		config.Extra = extra
	}
	return config
}

// handleLocalAIVision 处理本地图片 AI 识别请求。
// w 为响应对象，r 为请求对象。
func (s *Server) handleLocalAIVision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	payload, err := readPayload(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	imageData, err := s.readVisionImage(payload)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	prompt := firstNonEmptyString(stringValue(payload["prompt"]), "请识别图片中的简历或候选人详情文字，输出中文文本。")
	imageFormat := firstNonEmptyString(stringValue(payload["image_format"]), "png")
	config, err := s.db.GetAIConfig()
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	content := []map[string]any{
		{"type": "text", "text": prompt},
		{"type": "image_url", "image_url": map[string]any{"url": "data:image/" + imageFormat + ";base64," + base64.StdEncoding.EncodeToString(imageData)}},
	}
	result, err := localai.New(config).Chat(r.Context(), map[string]any{
		"messages":    []map[string]any{{"role": "user", "content": content}},
		"temperature": 0.1,
	})
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(w, map[string]any{"text": result.Content, "content": result.Content, "usage": result.Usage, "elapsed_ms": result.ElapsedMS})
}

// handleLocalOCRStatus 返回本地 OCR 组件状态。
// w 为响应对象，r 为请求对象。
func (s *Server) handleLocalOCRStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	response.Success(w, map[string]any{"ocr": s.ocr.Status()})
}

// handleLocalOCRRecognize 处理本地 OCR 图片识别请求。
// w 为响应对象，r 为请求对象。
func (s *Server) handleLocalOCRRecognize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	payload, err := readPayload(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	imagePath := firstNonEmptyString(stringValue(payload["file_path"]), stringValue(payload["path"]), stringValue(payload["screenshot_path"]))
	if err := s.validateLocalImagePath(imagePath); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := s.ocr.Recognize(r.Context(), imagePath)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(w, map[string]any{"text": result.Text, "raw": result.Raw})
}

// readVisionImage 读取图片 AI 识别请求中的图片内容。
// payload 可传 image_base64、file_path、path 或 screenshot_path。
func (s *Server) readVisionImage(payload map[string]any) ([]byte, error) {
	if raw := stringValue(payload["image_base64"]); raw != "" {
		if comma := strings.Index(raw, ","); comma >= 0 {
			raw = raw[comma+1:]
		}
		data, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return nil, fmt.Errorf("图片 base64 格式不正确")
		}
		return data, nil
	}
	imagePath := firstNonEmptyString(stringValue(payload["file_path"]), stringValue(payload["path"]), stringValue(payload["screenshot_path"]))
	if err := s.validateLocalImagePath(imagePath); err != nil {
		if imagePath == "" {
			return nil, fmt.Errorf("请传入图片路径或图片 base64")
		}
		return nil, err
	}
	data, err := os.ReadFile(filepath.Clean(imagePath))
	if err != nil {
		return nil, fmt.Errorf("读取图片失败：%w", err)
	}
	return data, nil
}

// validateLocalImagePath 校验本地图片路径是否位于 GoodHR 数据目录。
// imagePath 为图片绝对路径。
func (s *Server) validateLocalImagePath(imagePath string) error {
	if strings.TrimSpace(imagePath) == "" {
		return fmt.Errorf("请传入图片路径")
	}
	cleanPath := filepath.Clean(imagePath)
	if !filepath.IsAbs(cleanPath) {
		return fmt.Errorf("图片路径必须是绝对路径")
	}
	dataDir := filepath.Clean(s.cfg.DataDir) + string(os.PathSeparator)
	screenshotDir := filepath.Clean(s.cfg.ScreenshotsDir) + string(os.PathSeparator)
	if !strings.HasPrefix(cleanPath, dataDir) && !strings.HasPrefix(cleanPath, screenshotDir) {
		return fmt.Errorf("只能识别 GoodHR 本地数据目录内的图片")
	}
	return nil
}

// handleLocalRulesStatus 返回本地规则包状态。
// w 为响应对象，r 为请求对象。
func (s *Server) handleLocalRulesStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	response.Success(w, map[string]any{"status": "builtin", "message": "当前使用云端平台配置，无需单独更新规则包"})
}

// handleLocalRulesUpdate 兼容本地规则包更新入口。
// w 为响应对象，r 为请求对象。
func (s *Server) handleLocalRulesUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	response.Success(w, map[string]any{"updated": false, "message": "当前规则由云端平台配置实时读取，无需更新"})
}

// handleLocalSettings 处理本地设置读取和保存。
// w 为响应对象，r 为请求对象。
func (s *Server) handleLocalSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		settings, err := s.db.GetSettings()
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.Success(w, map[string]any{"settings": settings})
	case http.MethodPost:
		payload, err := readPayload(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		settings, err := s.db.SaveSettings(payload)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.Success(w, map[string]any{"settings": settings})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
	}
}

// handleProfiles 处理浏览器 Profile 列表和保存。
// w 为响应对象，r 为请求对象。
func (s *Server) handleProfiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		profiles, err := s.db.ListProfiles(r.URL.Query().Get("platform_id"))
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.Success(w, map[string]any{"profiles": profiles})
	case http.MethodPost:
		payload, err := readPayload(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		profile, err := s.db.SaveProfile(payload)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.Success(w, map[string]any{"profile": profile})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
	}
}

// handleProfileItem 处理单个浏览器 Profile。
// w 为响应对象，r 为请求对象。
func (s *Server) handleProfileItem(w http.ResponseWriter, r *http.Request) {
	profileID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/profiles/"), "/")
	if profileID == "" {
		response.Error(w, http.StatusBadRequest, "浏览器 Profile ID 不能为空")
		return
	}
	switch r.Method {
	case http.MethodPut:
		payload, err := readPayload(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		payload["id"] = profileID
		profile, err := s.db.SaveProfile(payload)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.Success(w, map[string]any{"profile": profile})
	case http.MethodDelete:
		if err := s.db.DeleteProfile(profileID); err != nil {
			response.Error(w, http.StatusNotFound, err.Error())
			return
		}
		response.Success(w, map[string]any{"deleted": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
	}
}

// handleLocalDownloads 处理本地下载记录读取和保存。
// w 为响应对象，r 为请求对象。
func (s *Server) handleLocalDownloads(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		downloads, err := s.db.ListDownloads(r.URL.Query().Get("task_id"))
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.Success(w, map[string]any{"downloads": downloads})
	case http.MethodPost:
		payload, err := readPayload(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		item, err := s.db.SaveDownload(payload)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.Success(w, map[string]any{"download": item})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
	}
}

// handleLocalScreenshots 处理本地截图记录读取和保存。
// w 为响应对象，r 为请求对象。
func (s *Server) handleLocalScreenshots(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		screenshots, err := s.db.ListScreenshots(r.URL.Query().Get("task_id"))
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.Success(w, map[string]any{"screenshots": screenshots})
	case http.MethodPost:
		payload, err := readPayload(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		item, err := s.db.SaveScreenshot(payload)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.Success(w, map[string]any{"screenshot": item})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
	}
}

// handleCloudPlatformConfig 从云端公开接口读取平台配置。
// w 为响应对象，r 为请求对象。
func (s *Server) handleCloudPlatformConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	payload, err := payloadOrQuery(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	client := cloudapi.New(s.cloudAPIBase(payload))
	config, err := client.FetchPlatformConfig(r.Context(), stringValue(payload["platform_id"]))
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.Success(w, map[string]any{"config": config})
}

// handleCloudSubscriptionStatus 从云端读取会员状态。
// w 为响应对象，r 为请求对象。
func (s *Server) handleCloudSubscriptionStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	payload, err := payloadOrQuery(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	token := stringValue(payload["token"])
	if token == "" {
		token = bearerToken(r)
	}
	client := cloudapi.New(s.cloudAPIBase(payload))
	subscription, err := client.FetchSubscription(r.Context(), token)
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.Success(w, map[string]any{"subscription": subscription})
}

// handleBrowserStart 转发浏览器启动请求给 Node Worker。
// w 为响应对象，r 为请求对象。
func (s *Server) handleBrowserStart(w http.ResponseWriter, r *http.Request) {
	s.proxyWorkerPost(w, r, "/api/v1/browser/start")
}

// handleBrowserStop 转发浏览器停止请求给 Node Worker。
// w 为响应对象，r 为请求对象。
func (s *Server) handleBrowserStop(w http.ResponseWriter, r *http.Request) {
	if stopped := s.runner.StopAll("浏览器已关闭，任务已自动结束"); stopped > 0 {
		log.Printf("浏览器停止前已结束运行任务：count=%d", stopped)
	}
	s.proxyWorkerPost(w, r, "/api/v1/browser/stop")
}

// handlePageOpen 转发页面打开请求给 Node Worker。
// w 为响应对象，r 为请求对象。
func (s *Server) handlePageOpen(w http.ResponseWriter, r *http.Request) {
	s.proxyWorkerPost(w, r, "/api/v1/page/open")
}

// handlePageClick 转发页面点击请求给 Node Worker。
// w 为响应对象，r 为请求对象。
func (s *Server) handlePageClick(w http.ResponseWriter, r *http.Request) {
	s.proxyWorkerPost(w, r, "/api/v1/page/click")
}

// handlePageType 转发页面输入请求给 Node Worker。
// w 为响应对象，r 为请求对象。
func (s *Server) handlePageType(w http.ResponseWriter, r *http.Request) {
	s.proxyWorkerPost(w, r, "/api/v1/page/type")
}

// handlePageScroll 转发页面滚动请求给 Node Worker。
// w 为响应对象，r 为请求对象。
func (s *Server) handlePageScroll(w http.ResponseWriter, r *http.Request) {
	s.proxyWorkerPost(w, r, "/api/v1/page/scroll")
}

// handlePageExtractText 转发页面文本提取请求给 Node Worker。
// w 为响应对象，r 为请求对象。
func (s *Server) handlePageExtractText(w http.ResponseWriter, r *http.Request) {
	s.proxyWorkerPost(w, r, "/api/v1/page/extract-text")
}

// handlePageScreenshot 转发页面截图请求给 Node Worker。
// w 为响应对象，r 为请求对象。
func (s *Server) handlePageScreenshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	payload, err := readPayload(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if stringValue(payload["dir"]) == "" && stringValue(payload["directory"]) == "" {
		payload["dir"] = s.cfg.ScreenshotsDir
	}
	if stringValue(payload["filename"]) == "" {
		payload["filename"] = fmt.Sprintf("screenshot-%d.png", time.Now().UnixMilli())
	}
	result, err := s.worker.Call(r.Context(), "/api/v1/page/screenshot", payload)
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	data, _ := workerData(result).(map[string]any)
	if stringValue(payload["task_id"]) != "" || stringValue(data["file_path"]) != "" || stringValue(data["path"]) != "" {
		recordPayload := map[string]any{
			"task_id":   stringValue(payload["task_id"]),
			"file_path": firstNonEmptyString(stringValue(data["file_path"]), stringValue(data["path"])),
			"label":     firstNonEmptyString(stringValue(payload["label"]), "页面截图"),
			"width":     data["width"],
			"height":    data["height"],
		}
		if screenshot, err := s.db.SaveScreenshot(recordPayload); err == nil {
			data["record"] = screenshot
		} else {
			log.Printf("保存截图记录失败：%v", err)
		}
	}
	response.Success(w, data)
}

// handlePageURL 读取当前页面 URL。
// w 为响应对象，r 为请求对象。
func (s *Server) handlePageURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	result, err := s.worker.CallGet(r.Context(), "/api/v1/page/url")
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.Success(w, workerData(result))
}

// handleBossCandidatesScroll 按平台配置滚动候选人列表。
// w 为响应对象，r 为请求对象。
func (s *Server) handleBossCandidatesScroll(w http.ResponseWriter, r *http.Request) {
	s.proxyWorkerPost(w, r, "/api/v1/boss/candidates/scroll")
}

// handleBossCandidateDetail 提取 Boss 候选人详情文本。
// w 为响应对象，r 为请求对象。
func (s *Server) handleBossCandidateDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	payload, err := readPayload(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if stringValue(payload["dir"]) == "" && stringValue(payload["directory"]) == "" {
		payload["dir"] = s.cfg.ScreenshotsDir
	}
	result, err := s.worker.Call(r.Context(), "/api/v1/boss/candidates/detail", payload)
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.Success(w, workerData(result))
}

// handlePageCookies 导出或导入当前浏览器 Cookie。
// w 为响应对象，r 为请求对象。
func (s *Server) handlePageCookies(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		result, err := s.worker.CallGet(r.Context(), "/api/v1/page/cookies")
		if err != nil {
			response.Error(w, http.StatusBadGateway, err.Error())
			return
		}
		response.Success(w, workerData(result))
		return
	}
	s.proxyWorkerPost(w, r, "/api/v1/page/cookies")
}

// handleDownloads 返回 Node Worker 记录的下载文件列表。
// w 为响应对象，r 为请求对象。
func (s *Server) handleDownloads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	result, err := s.worker.CallGet(r.Context(), "/api/v1/downloads")
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	data, _ := workerData(result).(map[string]any)
	for _, item := range mapListValue(data["downloads"]) {
		if _, err := s.db.SaveDownload(item); err != nil {
			log.Printf("保存下载记录失败：%v", err)
		}
	}
	response.Success(w, data)
}

// handleConsole 返回本地控制台占位页面。
// w 为响应对象，r 为请求对象。
func (s *Server) handleConsole(w http.ResponseWriter, r *http.Request) {
	if devURL := s.consoleDevURL(); devURL != "" {
		target, err := url.Parse(devURL)
		if err == nil {
			proxy := httputil.NewSingleHostReverseProxy(target)
			proxy.ServeHTTP(w, r)
			return
		}
	}
	staticDir := s.consoleStaticDir()
	if staticDir == "" {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><html><head><meta charset=\"utf-8\"><title>GoodHR Local Agent Go</title></head><body><h1>GoodHR Local Agent Go</h1><p>Go 版本本地程序已启动，但未找到控制台前端文件。</p></body></html>"))
		return
	}
	requested := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if requested == "." || requested == "/" {
		requested = "index.html"
	}
	if strings.HasPrefix(requested, "..") {
		requested = "index.html"
	}
	target := filepath.Join(staticDir, requested)
	if info, err := os.Stat(target); err == nil && !info.IsDir() {
		http.ServeFile(w, r, target)
		return
	}
	http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
}

// consoleDevURL 返回开发环境前端地址。
// 开发服务可用时，本地程序会直接代理它。
func (s *Server) consoleDevURL() string {
	if value := strings.TrimSpace(os.Getenv("GOODHR_CONSOLE_DEV_URL")); value != "" {
		return strings.TrimRight(value, "/")
	}
	target := "http://127.0.0.1:5173"
	client := http.Client{Timeout: 120 * time.Millisecond}
	resp, err := client.Get(target)
	if err != nil {
		return ""
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 500 {
		return target
	}
	return ""
}

// consoleStaticDir 返回可用的前端构建目录。
// 优先使用已下载目录，其次使用仓库内 cloud/frontend/dist。
func (s *Server) consoleStaticDir() string {
	candidates := []string{
		s.cfg.FrontendDir,
		filepath.Join(s.cfg.FrontendDir, "dist"),
		filepath.Join("..", "cloud", "frontend", "dist"),
		filepath.Join("goodhr5", "cloud", "frontend", "dist"),
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(filepath.Join(candidate, "index.html")); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

// proxyWorkerPost 读取请求体并转发给 Node Worker。
// w 为响应对象，r 为请求对象，path 为 Worker API 路径。
func (s *Server) proxyWorkerPost(w http.ResponseWriter, r *http.Request, path string) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	var payload map[string]any
	payload, err := readPayload(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	s.prepareBrowserPayload(path, payload)
	result, err := s.worker.Call(r.Context(), path, payload)
	if err != nil {
		msg := "浏览器 Worker 调用失败"
		if err.Error() != "" {
			msg = err.Error()
		}
		response.Error(w, http.StatusBadGateway, msg)
		return
	}
	response.Success(w, workerData(result))
}

// prepareBrowserPayload 补齐浏览器请求的本机目录参数。
// path 为 Worker 路径，payload 为请求参数。
func (s *Server) prepareBrowserPayload(path string, payload map[string]any) {
	if payload == nil || (path != "/api/v1/browser/start" && path != "/api/v1/page/open") {
		return
	}
	if stringValue(payload["downloads_path"]) == "" {
		payload["downloads_path"] = s.browserDownloadDir()
	}
	s.prepareBrowserViewport(payload)
	rawProfile := stringValue(payload["user_data_dir"])
	if rawProfile == "" {
		rawProfile = stringValue(payload["profile_id"])
	}
	if rawProfile == "" {
		return
	}
	if filepath.IsAbs(rawProfile) {
		payload["user_data_dir"] = rawProfile
		return
	}
	payload["user_data_dir"] = filepath.Join(s.cfg.ProfilesDir, safeLocalName(rawProfile))
}

// prepareBrowserViewport 补齐浏览器默认窗口尺寸。
// payload 为浏览器启动参数，已有宽高时不覆盖。
func (s *Server) prepareBrowserViewport(payload map[string]any) {
	if payload == nil {
		return
	}
	if numberFromAny(payload["viewport_width"]) > 0 && numberFromAny(payload["viewport_height"]) > 0 {
		return
	}
	width, height := adaptiveBrowserViewport()
	payload["viewport_width"] = width
	payload["viewport_height"] = height
}

// adaptiveBrowserViewport 根据当前屏幕返回适合浏览器的窗口尺寸。
// 读取失败时返回保守默认值。
func adaptiveBrowserViewport() (int, int) {
	screenWidth, screenHeight := currentScreenSize()
	if screenWidth <= 0 || screenHeight <= 0 {
		return 1280, 900
	}
	width := clampInt(int(float64(screenWidth)*0.85), 1024, 1320)
	height := clampInt(int(float64(screenHeight)*0.88), 760, 900)
	if width > screenWidth-80 {
		width = screenWidth - 80
	}
	if height > screenHeight-80 {
		height = screenHeight - 80
	}
	return clampInt(width, 960, 1320), clampInt(height, 700, 900)
}

// currentScreenSize 读取当前主屏幕尺寸。
// macOS 优先使用 osascript，Windows 使用 PowerShell，失败返回 0。
func currentScreenSize() (int, int) {
	if path, err := exec.LookPath("osascript"); err == nil {
		if out, err := exec.Command("/bin/sh", "-c", `osascript -l JavaScript -e 'ObjC.import("AppKit"); const f=$.NSScreen.mainScreen.visibleFrame; console.log(Math.round(f.size.width)+","+Math.round(f.size.height));'`).Output(); err == nil {
			if width, height := parseScreenPair(string(out)); width > 0 && height > 0 {
				return width, height
			}
		}
		out, err := exec.Command(path, "-e", `tell application "Finder" to get bounds of window of desktop`).Output()
		if err == nil {
			parts := strings.Split(strings.TrimSpace(string(out)), ",")
			if len(parts) >= 4 {
				width := parseLooseInt(parts[2]) - parseLooseInt(parts[0])
				height := parseLooseInt(parts[3]) - parseLooseInt(parts[1])
				if width > 0 && height > 0 {
					return width, height
				}
			}
		}
	}
	if path, err := exec.LookPath("powershell"); err == nil {
		script := `Add-Type -AssemblyName System.Windows.Forms; $r=[System.Windows.Forms.Screen]::PrimaryScreen.WorkingArea; Write-Output "$($r.Width),$($r.Height)"`
		out, err := exec.Command(path, "-NoProfile", "-Command", script).Output()
		if err == nil {
			if width, height := parseScreenPair(string(out)); width > 0 && height > 0 {
				return width, height
			}
		}
	}
	return 0, 0
}

// parseScreenPair 从宽高字符串中读取屏幕尺寸。
// value 格式通常为 width,height。
func parseScreenPair(value string) (int, int) {
	parts := strings.Split(strings.TrimSpace(value), ",")
	if len(parts) < 2 {
		return 0, 0
	}
	return parseLooseInt(parts[0]), parseLooseInt(parts[1])
}

// clampInt 将数值限制在指定范围内。
// value 为原始值，min 和 max 为边界。
func clampInt(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// parseLooseInt 从字符串中读取整数。
// value 可包含空格或换行，解析失败返回 0。
func parseLooseInt(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}

// numberFromAny 从任意值读取整数。
// value 支持数字和字符串，解析失败返回 0。
func numberFromAny(value any) int {
	switch item := value.(type) {
	case int:
		return item
	case int64:
		return int(item)
	case float64:
		return int(item)
	case string:
		return parseLooseInt(item)
	default:
		return 0
	}
}

// browserDownloadDir 返回当前浏览器下载目录。
// 优先读取本地设置，未设置时使用配置默认值。
func (s *Server) browserDownloadDir() string {
	settings, err := s.db.GetSettings()
	if err == nil {
		if value := stringValue(settings["browser_download_dir"]); value != "" {
			return value
		}
		if value := stringValue(settings["downloads_dir"]); value != "" {
			return value
		}
	}
	return s.cfg.DownloadsDir
}

// safeLocalName 清理本机目录名称。
// value 为原始目录名。
func safeLocalName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}
	var builder strings.Builder
	for _, item := range value {
		if unicode.IsLetter(item) || unicode.IsDigit(item) || item == '-' || item == '_' || item == '.' {
			builder.WriteRune(item)
			continue
		}
		builder.WriteRune('_')
	}
	result := strings.Trim(builder.String(), "._ ")
	if result == "" {
		return "default"
	}
	if len(result) > 80 {
		return result[:80]
	}
	return result
}

// mapListValue 将任意值转换为 map 列表。
// value 为原始 JSON 字段。
func mapListValue(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return []map[string]any{}
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if row, ok := item.(map[string]any); ok {
			if stringValue(row["id"]) == "" {
				sum := sha1.Sum([]byte(fmt.Sprintf("%v|%v", row["path"], row["url"])))
				row["id"] = fmt.Sprintf("download_%x", sum[:8])
			}
			result = append(result, row)
		}
	}
	return result
}

// readPayload 读取请求 JSON 参数。
// r 为 HTTP 请求对象，空 body 返回空 map。
func readPayload(r *http.Request) (map[string]any, error) {
	var payload map[string]any
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil && !errors.Is(err, io.EOF) {
		return nil, errors.New("请求参数不是有效 JSON")
	}
	if payload == nil {
		payload = map[string]any{}
	}
	return payload, nil
}

// payloadOrQuery 读取 JSON 请求体或查询参数。
// r 为请求对象。
func payloadOrQuery(r *http.Request) (map[string]any, error) {
	if r.Method == http.MethodGet {
		result := map[string]any{}
		for key, values := range r.URL.Query() {
			if len(values) > 0 {
				result[key] = values[0]
			}
		}
		return result, nil
	}
	return readPayload(r)
}

// cloudAPIBase 返回本次请求使用的云端接口地址。
// payload 为请求参数。
func (s *Server) cloudAPIBase(payload map[string]any) string {
	if base := stringValue(payload["cloud_api_base"]); base != "" {
		return base
	}
	if base := stringValue(payload["api_base"]); base != "" {
		return base
	}
	return s.cfg.CloudAPIBase
}

// bearerToken 从请求头读取 Bearer token。
// r 为请求对象。
func bearerToken(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return ""
	}
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(token)
}

// localTaskPath 解析本地任务子路径。
// rawPath 为请求路径，返回任务 ID 和动作名称。
func localTaskPath(rawPath string) (string, string) {
	rest := strings.TrimPrefix(rawPath, "/api/v1/local/tasks/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", ""
	}
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	return parts[0], action
}

// localCandidateID 从候选人子路径中解析候选人 ID。
// rawPath 为请求路径，找不到时返回空字符串。
func localCandidateID(rawPath string) string {
	marker := "/candidates/"
	index := strings.Index(rawPath, marker)
	if index < 0 {
		return ""
	}
	return strings.Trim(strings.TrimPrefix(rawPath[index:], marker), "/")
}

// stringValue 将请求字段转换为字符串。
// value 为原始字段值。
func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

// mapValue 将请求字段转换为 map。
// value 为原始字段值，不是对象时返回空 map。
func mapValue(value any) map[string]any {
	if item, ok := value.(map[string]any); ok {
		return item
	}
	return map[string]any{}
}

// firstNonEmptyString 返回第一个非空字符串。
// values 为候选字符串。
func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// boolValue 将请求字段转换为布尔值。
// value 为原始字段值。
func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

// floatValue 将请求字段转换为浮点数。
// value 为原始字段值，fallback 为默认值。
func floatValue(value any, fallback float64) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case json.Number:
		if parsed, err := typed.Float64(); err == nil {
			return parsed
		}
	case string:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64); err == nil {
			return parsed
		}
	default:
		return fallback
	}
	return fallback
}

// intValue 将请求字段转换为整数。
// value 为原始字段值，fallback 为默认值。
func intValue(value any, fallback int) int {
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return int(parsed)
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
			return parsed
		}
	default:
		return fallback
	}
	return fallback
}

// workerData 提取 Worker 统一响应中的 data 字段。
// result 为 Worker 原始响应。
func workerData(result map[string]any) any {
	if result == nil {
		return nil
	}
	if data, ok := result["data"]; ok {
		return data
	}
	return result
}

// defaultWorkerSourceDir 返回开发环境默认 Node Worker 源码目录。
// 找不到时返回当前目录下的 worker-node。
func defaultWorkerSourceDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return "worker-node"
	}
	candidates := []string{
		filepath.Join(wd, "worker-node"),
		filepath.Join(wd, "goodhr5", "local-agent-go", "worker-node"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return filepath.Join(wd, "worker-node")
}

// withCORS 为本地 API 增加跨域响应头。
// next 为下一个 HTTP 处理器。
func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-GoodHR-Agent-BaseURL")
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
		if r.Method == http.MethodOptions {
			response.Success(w, map[string]any{"ok": true})
			return
		}
		next.ServeHTTP(w, r)
	})
}
