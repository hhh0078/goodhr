// Package app 负责注册 Go 版本本地程序 HTTP 服务和路由。
package app

import (
	"context"
	"crypto/sha1"
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
	"goodhr5/local-agent-go/internal/localdb"
	"goodhr5/local-agent-go/internal/ocr"
	"goodhr5/local-agent-go/internal/process"
	"goodhr5/local-agent-go/internal/response"
	"goodhr5/local-agent-go/internal/runtime"
	"goodhr5/local-agent-go/internal/taskrunner"
	"goodhr5/local-agent-go/internal/version"
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
		runner:  taskrunner.New(db, workerManager, ocrEngine, cfg.ProfilesDir, cfg.DownloadsDir, cfg.ScreenshotsDir, audioDir(cfg), cfg.CloudAPIBase),
	}, nil
}

// audioDir 返回音频文件目录路径，优先使用可执行文件所在目录下的 audio 子目录。
func audioDir(cfg *config.Config) string {
	execAudioDir := ""
	execPath, err := os.Executable()
	if err == nil {
		execDir := filepath.Dir(execPath)
		execAudioDir = filepath.Join(execDir, "audio")
		if info, statErr := os.Stat(execAudioDir); statErr == nil && info.IsDir() {
			return execAudioDir
		}
	}
	// fallback：从工作目录
	if info, err := os.Stat("audio"); err == nil && info.IsDir() {
		absPath, _ := filepath.Abs("audio")
		return absPath
	}
	if execAudioDir != "" {
		return execAudioDir
	}
	return filepath.Join(cfg.DataDir, "audio")
}

// Run 启动本地 HTTP 服务。
// 固定监听配置端口，端口被占用时直接返回错误。
func (s *Server) Run() error {
	ln, port, err := process.ListenFirstAvailable(s.cfg.Host, s.cfg.Port, s.cfg.Port)
	if err != nil {
		return err
	}
	s.cfg.Port = port
	if s.worker != nil {
		s.worker.SetAgentBaseURL("http://" + net.JoinHostPort(s.cfg.Host, strconv.Itoa(port)))
	}
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
	mux.HandleFunc("/api/v1/app-update/status", s.handleAppUpdateStatus)
	mux.HandleFunc("/api/v1/app-update/start", s.handleAppUpdateStart)
	mux.HandleFunc("/api/v1/console/status", s.handleConsoleStatus)
	mux.HandleFunc("/api/v1/console/update", s.handleConsoleUpdate)
	mux.HandleFunc("/api/v1/worker/start", s.handleWorkerStart)
	mux.HandleFunc("/api/v1/worker/stop", s.handleWorkerStop)
	mux.HandleFunc("/api/v1/worker/status", s.handleWorkerStatus)
	mux.HandleFunc("/api/v1/local/tasks/", s.handleLocalTaskItem)
	mux.HandleFunc("/api/v1/tasks/init", s.handleLegacyTaskInit)
	mux.HandleFunc("/api/v1/tasks/", s.handleLegacyLocalTaskItem)
	mux.HandleFunc("/api/v1/local/ocr/status", s.handleLocalOCRStatus)
	mux.HandleFunc("/api/v1/local/ocr/recognize", s.handleLocalOCRRecognize)
	mux.HandleFunc("/api/v1/local/rules/status", s.handleLocalRulesStatus)
	mux.HandleFunc("/api/v1/local/rules/update", s.handleLocalRulesUpdate)
	mux.HandleFunc("/api/v1/local/downloads", s.handleLocalDownloads)
	mux.HandleFunc("/api/v1/local/screenshots", s.handleLocalScreenshots)
	mux.HandleFunc("/api/v1/files/open", s.handleFileOpen)
	mux.HandleFunc("/api/v1/files/reveal", s.handleFileReveal)
	mux.HandleFunc("/api/v1/downloads/notify", s.handleDownloadNotify)
	mux.HandleFunc("/api/v1/cloud/platform-config", s.handleCloudPlatformConfig)
	mux.HandleFunc("/api/v1/cloud/subscription/status", s.handleCloudSubscriptionStatus)
	mux.HandleFunc("/api/v1/browser/start", s.handleBrowserStart)
	mux.HandleFunc("/api/v1/browser/stop", s.handleBrowserStop)
	mux.HandleFunc("/api/v1/page/open", s.handlePageOpen)
	mux.HandleFunc("/api/v1/page/click", s.handlePageClick)
	mux.HandleFunc("/api/v1/page/type", s.handlePageType)
	mux.HandleFunc("/api/v1/page/scroll", s.handlePageScroll)
	mux.HandleFunc("/api/v1/page/extract-text", s.handlePageExtractText)
	mux.HandleFunc("/api/v1/page/find-elements", s.handlePageFindElements)
	mux.HandleFunc("/api/v1/page/list-click-by-index", s.handlePageListClickByIndex)
	mux.HandleFunc("/api/v1/page/press-key", s.handlePagePressKey)
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
		"version":        version.Value,
		"port":           s.cfg.Port,
		"dataDir":        s.cfg.DataDir,
		"logsDir":        s.cfg.LogsDir,
		"logPath":        filepath.Join(s.cfg.LogsDir, "local-agent.log"),
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

// handleRuntimeInstall 根据前端传入的运行组件配置下载并安装运行组件。
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
	manifest, err := runtimeManifestFromPayload(payload)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	status, err := s.runtime.StartInstall(manifest)
	if err != nil {
		response.Error(w, http.StatusConflict, err.Error())
		return
	}
	response.Success(w, status)
}

// runtimeManifestFromPayload 从请求体解析运行组件下载配置。
// payload 为前端提交的 JSON 请求体。
func runtimeManifestFromPayload(payload map[string]any) (runtime.Manifest, error) {
	raw := payload["manifest"]
	if raw == nil {
		raw = payload["runtime_components"]
	}
	if raw == nil {
		return runtime.Manifest{}, fmt.Errorf("运行组件下载配置为空")
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return runtime.Manifest{}, fmt.Errorf("运行组件下载配置格式不正确：%w", err)
	}
	var manifest runtime.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return runtime.Manifest{}, fmt.Errorf("运行组件下载配置格式不正确：%w", err)
	}
	return manifest, nil
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

// handleLocalTaskItem 处理单个本地任务相关接口。
// w 为响应对象，r 为请求对象。
func (s *Server) handleLocalTaskItem(w http.ResponseWriter, r *http.Request) {
	taskID, action := localTaskPath(r.URL.Path)
	if taskID == "" {
		response.Error(w, http.StatusBadRequest, "任务 ID 不能为空")
		return
	}
	switch action {
	case "status":
		s.handleLocalTaskStatus(w, r, taskID)
	case "logs":
		s.handleLocalTaskLogs(w, r, taskID)
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
	switch parts[1] {
	case "start-ws", "stop-ws":
		response.Error(w, http.StatusGone, "Go 本地程序不再使用云端 WebSocket，请使用本地任务运行接口")
	case "screenshots":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
			return
		}
		response.Success(w, map[string]any{"screenshots": []any{}})
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
	result, err := s.runner.Start(r.Context(), taskID, taskrunner.StartOptions{
		CloudAPIBase:           s.cloudAPIBase(payload),
		Token:                  token,
		EnableGreet:            true,
		GreetRetries:           0,
		ScrollDelayMin:         3,
		ScrollDelayMax:         8,
		ListViewDelayMin:       1,
		ListViewDelayMax:       2,
		DetailViewDelayMin:     1,
		DetailViewDelayMax:     2,
		DetailOpenDelayMin:     1,
		DetailOpenDelayMax:     2,
		DetailCloseDelayMin:    1,
		DetailCloseDelayMax:    2,
		RestAfterCandidatesMin: 30,
		RestAfterCandidatesMax: 50,
		RestTimesMin:           2,
		RestTimesMax:           4,
		RestDurationMin:        3,
		RestDurationMax:        5,
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
	payload, err := readPayload(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := s.runner.Stop(taskID)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	token := stringValue(payload["token"])
	if token == "" {
		token = bearerToken(r)
	}
	if token != "" {
		client := cloudapi.New(s.cloudAPIBase(payload))
		if err := client.StopTask(r.Context(), token, taskID); err != nil {
			result["cloud_sync_error"] = err.Error()
			log.Printf("[本地任务] 停止任务已完成，但同步云端失败 task=%s err=%v", taskID, err)
		}
	}
	response.Success(w, result)
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

// handleLocalScreenshots 兼容旧版本地截图记录接口，新版本不再写入截图记录。
// w 为响应对象，r 为请求对象。
func (s *Server) handleLocalScreenshots(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		response.Success(w, map[string]any{"screenshots": []any{}})
	case http.MethodPost:
		payload, err := readPayload(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.Success(w, map[string]any{"screenshot": payload})
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

// handlePageFindElements 转发页面元素查询请求给 Node Worker。
// w 为响应对象，r 为请求对象。
func (s *Server) handlePageFindElements(w http.ResponseWriter, r *http.Request) {
	s.proxyWorkerPost(w, r, "/api/v1/page/find-elements")
}

// handlePageListClickByIndex 转发列表元素点击请求给 Node Worker。
// w 为响应对象，r 为请求对象。
func (s *Server) handlePageListClickByIndex(w http.ResponseWriter, r *http.Request) {
	s.proxyWorkerPost(w, r, "/api/v1/page/list-click-by-index")
}

// handlePagePressKey 转发页面按键请求给 Node Worker。
// w 为响应对象，r 为请求对象。
func (s *Server) handlePagePressKey(w http.ResponseWriter, r *http.Request) {
	s.proxyWorkerPost(w, r, "/api/v1/page/press-key")
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
	if target, ok := consoleStaticFile(staticDir, requested); ok {
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
// 优先使用已下载目录，其次使用仓库内新版 Next 静态导出目录和旧版前端目录。
func (s *Server) consoleStaticDir() string {
	candidates := []string{
		s.cfg.FrontendDir,
		filepath.Join(s.cfg.FrontendDir, "dist"),
		filepath.Join("..", "cloud", "frontend-next", "out"),
		filepath.Join("goodhr5", "cloud", "frontend-next", "out"),
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

// consoleStaticFile 根据请求路径查找静态前端文件，兼容 Next 静态导出的 .html 和目录形式路由。
// staticDir 为前端根目录，requested 为清理后的相对路径。
func consoleStaticFile(staticDir string, requested string) (string, bool) {
	candidates := []string{filepath.Join(staticDir, requested)}
	if filepath.Ext(requested) == "" {
		candidates = append(candidates,
			filepath.Join(staticDir, requested+".html"),
			filepath.Join(staticDir, requested, "index.html"),
		)
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

// proxyWorkerPost 读取请求体并转发给 Node Worker。
// w 为响应对象，r 为请求对象，path 为 Worker API 路径。
func (s *Server) proxyWorkerPost(w http.ResponseWriter, r *http.Request, path string) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	startedAt := time.Now()
	var payload map[string]any
	payload, err := readPayload(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	s.prepareBrowserPayload(path, payload)
	log.Printf("[浏览器代理] 收到请求 path=%s profile=%s url=%s timeout=%s", path, logPayloadValue(payload["user_data_dir"]), logPayloadValue(payload["url"]), workerCallTimeout(path))
	callCtx, cancel := context.WithTimeout(r.Context(), workerCallTimeout(path))
	defer cancel()
	if err := s.ensureWorkerReadyForProxy(callCtx, path); err != nil {
		log.Printf("[浏览器代理] Worker 准备失败 path=%s elapsed=%s err=%s", path, time.Since(startedAt).Round(time.Millisecond), err.Error())
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	result, err := s.worker.Call(callCtx, path, payload)
	if err != nil {
		msg := "浏览器 Worker 调用失败"
		if err.Error() != "" {
			msg = err.Error()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			msg = "浏览器启动或操作超时，请关闭残留浏览器后重试"
		}
		log.Printf("[浏览器代理] 请求失败 path=%s elapsed=%s err=%s", path, time.Since(startedAt).Round(time.Millisecond), msg)
		response.Error(w, http.StatusBadGateway, msg)
		return
	}
	log.Printf("[浏览器代理] 请求成功 path=%s elapsed=%s", path, time.Since(startedAt).Round(time.Millisecond))
	response.Success(w, workerData(result))
}

// ensureWorkerReadyForProxy 在浏览器代理请求前确保 Node Worker 已启动。
// ctx 为请求上下文，path 为 Worker API 路径；停止浏览器时不主动拉起 Worker。
func (s *Server) ensureWorkerReadyForProxy(ctx context.Context, path string) error {
	if path == "/api/v1/browser/stop" {
		return nil
	}
	status, err := s.worker.Start(ctx)
	if err != nil {
		return err
	}
	log.Printf("[浏览器代理] Worker 已准备 path=%s running=%v base_url=%s pid=%d managed=%v", path, status.Running, status.BaseURL, status.PID, status.Managed)
	return nil
}

// logPayloadValue 返回适合日志展示的请求字段。
// value 为请求字段值，过长时会截断。
func logPayloadValue(value any) string {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return "-"
	}
	runes := []rune(text)
	if len(runes) > 160 {
		return string(runes[:160]) + "..."
	}
	return text
}

// workerCallTimeout 返回 Worker 接口最大等待时间。
// path 为 Worker 路由，不同操作按耗时给出不同超时。
func workerCallTimeout(path string) time.Duration {
	switch path {
	case "/api/v1/browser/start":
		return 45 * time.Second
	case "/api/v1/page/screenshot", "/api/v1/boss/candidates/detail":
		return 120 * time.Second
	default:
		return 60 * time.Second
	}
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
		return 1100, 780
	}
	width := clampInt(int(float64(screenWidth)*0.75), 960, 1180)
	height := clampInt(int(float64(screenHeight)*0.78), 680, 820)
	if width > screenWidth-120 {
		width = screenWidth - 120
	}
	if height > screenHeight-120 {
		height = screenHeight - 120
	}
	return clampInt(width, 900, 1180), clampInt(height, 640, 820)
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
