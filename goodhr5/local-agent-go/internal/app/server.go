// Package app 负责注册 Go 版本本地程序 HTTP 服务和路由。
package app

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"goodhr5/local-agent-go/internal/browser"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/config"
	"goodhr5/local-agent-go/internal/localdb"
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
	return &Server{
		cfg:     cfg,
		runtime: runtimeManager,
		worker:  workerManager,
		db:      db,
		runner:  taskrunner.New(db, workerManager),
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
	return server.Serve(ln)
}

// registerRoutes 注册本地程序路由。
// mux 为 HTTP 路由器。
func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/v1/runtime/status", s.handleRuntimeStatus)
	mux.HandleFunc("/api/v1/runtime/ensure", s.handleRuntimeEnsure)
	mux.HandleFunc("/api/v1/runtime/install", s.handleRuntimeInstall)
	mux.HandleFunc("/api/v1/runtime/install-local-worker", s.handleRuntimeInstallLocalWorker)
	mux.HandleFunc("/api/v1/worker/start", s.handleWorkerStart)
	mux.HandleFunc("/api/v1/worker/stop", s.handleWorkerStop)
	mux.HandleFunc("/api/v1/worker/status", s.handleWorkerStatus)
	mux.HandleFunc("/api/v1/local/tasks", s.handleLocalTasks)
	mux.HandleFunc("/api/v1/local/tasks/", s.handleLocalTaskItem)
	mux.HandleFunc("/api/v1/local/positions", s.handleLocalPositions)
	mux.HandleFunc("/api/v1/local/positions/", s.handleLocalPositionItem)
	mux.HandleFunc("/api/v1/local/ai/config", s.handleLocalAIConfig)
	mux.HandleFunc("/api/v1/local/settings", s.handleLocalSettings)
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
	mux.HandleFunc("/api/v1/page/cookies", s.handlePageCookies)
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
		"status":  "ok",
		"version": "go-v2-dev",
		"port":    s.cfg.Port,
		"dataDir": s.cfg.DataDir,
		"dbPath":  s.db.Path(),
		"runtime": s.runtime.Status(),
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
	result, err := s.runtime.InstallFromManifest(r.Context(), manifestURL)
	if err != nil {
		response.Error(w, http.StatusConflict, err.Error())
		return
	}
	response.Success(w, result)
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
		logs, err := s.db.ListTaskLogs(taskID, 100)
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
	default:
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
	}
}

// handleLocalTaskCandidates 处理任务候选人读取和保存。
// w 为响应对象，r 为请求对象，taskID 为任务 ID。
func (s *Server) handleLocalTaskCandidates(w http.ResponseWriter, r *http.Request, taskID string) {
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
	enableGreet := boolValue(payload["enable_greet"])
	greetDelayMin := 0.0
	greetDelayMax := 0.0
	greetRetries := 0
	if enableGreet {
		greetDelayMin = floatValue(payload["greet_delay_min"], 1)
		greetDelayMax = floatValue(payload["greet_delay_max"], 2)
		greetRetries = intValue(payload["greet_retries"], 1)
	}
	result, err := s.runner.Start(r.Context(), taskID, taskrunner.StartOptions{
		CloudAPIBase:  s.cloudAPIBase(payload),
		Token:         token,
		EnableGreet:   enableGreet,
		GreetDelayMin: greetDelayMin,
		GreetDelayMax: greetDelayMax,
		GreetRetries:  greetRetries,
	})
	if err != nil {
		response.Error(w, http.StatusConflict, err.Error())
		return
	}
	response.Success(w, result)
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
	s.proxyWorkerPost(w, r, "/api/v1/page/screenshot")
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
	response.Success(w, workerData(result))
}

// handleConsole 返回本地控制台占位页面。
// w 为响应对象，r 为请求对象。
func (s *Server) handleConsole(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte("<!doctype html><html><head><meta charset=\"utf-8\"><title>GoodHR Local Agent Go</title></head><body><h1>GoodHR Local Agent Go</h1><p>Go 版本本地程序已启动。</p></body></html>"))
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

// stringValue 将请求字段转换为字符串。
// value 为原始字段值。
func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
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
