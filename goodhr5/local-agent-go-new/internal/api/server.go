// Package api 提供本地 HTTP 接口，只负责参数解析、响应和调用应用服务。
package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/config"
	downloadflow "goodhr5/local-agent-go-new/internal/flow/download"
	"goodhr5/local-agent-go-new/internal/flow/lifecycle"
	"goodhr5/local-agent-go-new/internal/flow/preflight"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ocr"
	"goodhr5/local-agent-go-new/internal/profile"
	"goodhr5/local-agent-go-new/internal/runtime"
	"goodhr5/local-agent-go-new/internal/storage"
	"goodhr5/local-agent-go-new/internal/updater"
	"goodhr5/local-agent-go-new/internal/version"
)

// Server 组装本地 HTTP 路由和应用服务。
type Server struct {
	cfg             config.Config
	runner          *lifecycle.Runner
	runtime         *runtime.Manager
	browser         *client.Client
	downloads       *downloadflow.Monitor
	store           *storage.Store
	profiles        *profile.Manager
	ocr             *ocr.Client
	updater         *updater.Manager
	http            *http.Server
	downloadRootsMu sync.RWMutex
	downloadRoots   map[string]struct{}
}

// Dependencies 保存本地 HTTP 接口调用的应用服务。
type Dependencies struct {
	Runner    *lifecycle.Runner
	Runtime   *runtime.Manager
	Browser   *client.Client
	Downloads *downloadflow.Monitor
	Store     *storage.Store
	Profiles  *profile.Manager
	OCR       *ocr.Client
	Updater   *updater.Manager
}

// NewServer 创建本地 HTTP 服务。
func NewServer(cfg config.Config, dependencies Dependencies) *Server {
	server := &Server{
		cfg: cfg, runner: dependencies.Runner, runtime: dependencies.Runtime,
		browser: dependencies.Browser, downloads: dependencies.Downloads,
		store: dependencies.Store, profiles: dependencies.Profiles,
		ocr: dependencies.OCR, updater: dependencies.Updater,
		downloadRoots: map[string]struct{}{filepath.Clean(cfg.DownloadsDir): {}},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", server.handleHealth)
	mux.HandleFunc("GET /api/v1/diagnostics", server.handleDiagnostics)
	mux.HandleFunc("POST /api/v1/tasks/start", server.handleTaskStart)
	mux.HandleFunc("POST /api/v1/tasks/stop", server.handleTaskStop)
	mux.HandleFunc("GET /api/v1/tasks/{task_id}", server.handleTaskStatus)
	mux.HandleFunc("/api/v1/local/positions/{position_id}/{action}", server.handleLocalPosition)
	mux.HandleFunc("GET /api/v1/runtime/status", server.handleRuntimeStatus)
	mux.HandleFunc("POST /api/v1/runtime/ensure", server.handleRuntimeEnsure)
	mux.HandleFunc("POST /api/v1/runtime/install", server.handleRuntimeInstall)
	mux.HandleFunc("POST /api/v1/worker/start", server.handleWorkerStart)
	mux.HandleFunc("POST /api/v1/worker/stop", server.handleWorkerStop)
	mux.HandleFunc("GET /api/v1/worker/status", server.handleWorkerStatus)
	mux.HandleFunc("GET /api/v1/browser/status", server.handleBrowserStatus)
	mux.HandleFunc("POST /api/v1/browser/stop", server.handleBrowserStop)
	mux.HandleFunc("POST /api/v1/page/open", server.handlePageOpen)
	mux.HandleFunc("GET /api/v1/page/url", server.handlePageURL)
	mux.HandleFunc("GET /api/v1/local/ocr/status", server.handleOCRStatus)
	mux.HandleFunc("POST /api/v1/local/ocr/recognize", server.handleOCRRecognize)
	mux.HandleFunc("GET /api/v1/local/rules/status", server.handleRulesStatus)
	mux.HandleFunc("POST /api/v1/local/rules/update", server.handleRulesUpdate)
	mux.HandleFunc("GET /api/v1/local/screenshots", server.handleScreenshots)
	mux.HandleFunc("POST /api/v1/local/screenshots", server.handleScreenshots)
	mux.HandleFunc("GET /api/v1/app-update/status", server.handleAppUpdateStatus)
	mux.HandleFunc("POST /api/v1/app-update/start", server.handleAppUpdateStart)
	mux.HandleFunc("GET /api/v1/downloads", server.handleDownloads)
	mux.HandleFunc("GET /api/v1/downloads/history", server.handleDownloadHistory)
	mux.HandleFunc("GET /api/v1/local/downloads", server.handleDownloadHistory)
	mux.HandleFunc("POST /api/v1/downloads/configure", server.handleDownloadsConfigure)
	mux.HandleFunc("POST /api/v1/downloads/clear", server.handleDownloadsClear)
	mux.HandleFunc("POST /api/v1/files/open", server.handleFileOpen)
	mux.HandleFunc("POST /api/v1/files/reveal", server.handleFileReveal)
	server.http = &http.Server{
		Addr:              cfg.Address(),
		Handler:           server.middleware(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	return server
}

// ListenAndServe 启动本地 HTTP 服务。
func (s *Server) ListenAndServe() error {
	log.Printf("GoodHR 新本地程序已监听 http://%s", s.cfg.Address())
	err := s.http.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown 优雅关闭本地 HTTP 服务。
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

// handleHealth 返回本地程序健康状态。
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeSuccess(w, http.StatusOK, struct {
		Status         string `json:"status"`
		Version        string `json:"version"`
		AgentVersion   string `json:"agent_version"`
		Port           int    `json:"port"`
		DataDir        string `json:"dataDir"`
		DataDirAlias   string `json:"data_dir"`
		LogsDir        string `json:"logsDir"`
		ProfilesDir    string `json:"profilesDir"`
		DownloadsDir   string `json:"downloadsDir"`
		ScreenshotsDir string `json:"screenshotsDir"`
		DatabasePath   string `json:"dbPath"`
	}{
		Status: "ok", Version: version.Value, AgentVersion: version.Value,
		Port: s.cfg.Port, DataDir: s.cfg.DataDir, DataDirAlias: s.cfg.DataDir,
		LogsDir: s.cfg.LogsDir, ProfilesDir: s.cfg.ProfilesDir,
		DownloadsDir: s.cfg.DownloadsDir, ScreenshotsDir: s.cfg.ScreenshotsDir,
		DatabasePath: s.cfg.DatabasePath,
	})
}

// handleTaskStart 解析强类型请求并启动统一任务流程。
func (s *Server) handleTaskStart(w http.ResponseWriter, r *http.Request) {
	var request shared.StartRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err)
		return
	}
	result, err := s.runner.StartTask(r.Context(), request)
	if err != nil {
		writeJSON(w, http.StatusConflict, struct {
			OK        bool                 `json:"ok"`
			Error     errorBody            `json:"error"`
			Preflight []preflightStepAlias `json:"preflight"`
		}{
			OK:        false,
			Error:     errorBody{Code: "TASK_START_FAILED", Message: err.Error()},
			Preflight: convertPreflightSteps(result.Preflight),
		})
		return
	}
	writeSuccess(w, http.StatusAccepted, result)
}

// handleTaskStop 请求任务安全停止。
func (s *Server) handleTaskStop(w http.ResponseWriter, r *http.Request) {
	var request struct {
		TaskID string `json:"task_id"`
	}
	if err := decodeJSON(w, r, &request); err != nil || strings.TrimSpace(request.TaskID) == "" {
		if err == nil {
			err = fmt.Errorf("task_id 不能为空")
		}
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err)
		return
	}
	task, err := s.runner.StopTask(r.Context(), request.TaskID)
	if err != nil {
		writeError(w, http.StatusNotFound, "TASK_NOT_FOUND", err)
		return
	}
	writeSuccess(w, http.StatusOK, task)
}

// handleTaskStatus 返回指定任务状态。
func (s *Server) handleTaskStatus(w http.ResponseWriter, r *http.Request) {
	task, err := s.runner.TaskStatus(r.Context(), r.PathValue("task_id"))
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

// handleRuntimeStatus 返回 Node、Worker 编译产物和 Worker 健康状态。
func (s *Server) handleRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	status := s.runtime.Status()
	status.Version = version.Value
	status.AgentVersion = version.Value
	status.DataDir = s.cfg.DataDir
	status.NodeReady = status.NodeInstalled
	status.WorkerBuilt = status.NodeWorkerInstalled
	workerErr := s.browser.Health(r.Context())
	var cloakStatus contract.WorkerRuntimeStatus
	var cloakErr error
	if workerErr == nil {
		cloakStatus, cloakErr = s.browser.RuntimeStatus(r.Context())
	}
	status.WorkerReady = workerErr == nil
	if cloakErr == nil && cloakStatus.Installed {
		status.CloakBrowserReady = true
		status.CloakBrowserInstalled = true
		status.CloakBrowserVersion = cloakStatus.CloakBrowserVersion
		if strings.TrimSpace(cloakStatus.BinaryPath) != "" {
			status.CloakBrowserPath = cloakStatus.BinaryPath
		}
	}
	writeSuccess(w, http.StatusOK, status)
}

// handleRuntimeInstall 根据云端清单异步安装当前系统所需运行组件。
func (s *Server) handleRuntimeInstall(w http.ResponseWriter, r *http.Request) {
	if s.runner.HasActive() {
		writeError(w, http.StatusConflict, "TASK_RUNNING", fmt.Errorf("任务正在运行，组件更新先等这一轮结束"))
		return
	}
	var request runtime.InstallRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err)
		return
	}
	status, err := s.runtime.StartInstall(request.Manifest)
	if err != nil {
		writeError(w, http.StatusConflict, "RUNTIME_INSTALL_FAILED", err)
		return
	}
	status.Version = version.Value
	status.AgentVersion = version.Value
	status.DataDir = s.cfg.DataDir
	writeSuccess(w, http.StatusAccepted, status)
}

// handleRuntimeEnsure 检查运行组件并启动 Worker。
func (s *Server) handleRuntimeEnsure(w http.ResponseWriter, r *http.Request) {
	if err := s.runtime.EnsureWorker(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "RUNTIME_NOT_READY", err)
		return
	}
	writeSuccess(w, http.StatusOK, struct {
		Ready bool `json:"ready"`
	}{Ready: true})
}

// handleBrowserStatus 返回 CloakBrowser 会话状态。
func (s *Server) handleBrowserStatus(w http.ResponseWriter, r *http.Request) {
	result, err := s.browser.BrowserStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "BROWSER_STATUS_FAILED", err)
		return
	}
	writeSuccess(w, http.StatusOK, result)
}

// handleBrowserStop 关闭 CloakBrowser 会话。
func (s *Server) handleBrowserStop(w http.ResponseWriter, r *http.Request) {
	if s.runner.HasActive() {
		writeError(w, http.StatusConflict, "TASK_RUNNING", fmt.Errorf("任务还在运行，请先安全停止任务"))
		return
	}
	result, err := s.browser.StopBrowser(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "BROWSER_STOP_FAILED", err)
		return
	}
	writeSuccess(w, http.StatusOK, result)
}

// middleware 添加本机接口安全响应头和受限跨域支持。
func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		origin := r.Header.Get("Origin")
		if allowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Trace-ID")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// allowedOrigin 只允许 GoodHR 和本机控制台跨域访问。
func allowedOrigin(origin string) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	hostname := strings.ToLower(parsed.Hostname())
	if scheme == "https" && parsed.Port() == "" && hostname == "goodhr5.58it.cn" {
		return true
	}
	return scheme == "http" && (hostname == "127.0.0.1" || hostname == "localhost")
}

// decodeJSON 解码单个强类型 JSON 对象并拒绝未知字段。
func decodeJSON(w http.ResponseWriter, r *http.Request, result any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(result); err != nil {
		return fmt.Errorf("请求内容不正确：%w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("请求只能包含一个 JSON 对象")
	}
	return nil
}

// writeSuccess 写入统一成功响应。
func writeSuccess(w http.ResponseWriter, status int, data any) {
	writeJSON(w, status, struct {
		OK   bool `json:"ok"`
		Data any  `json:"data"`
	}{OK: true, Data: data})
}

// writeError 写入统一错误响应。
func writeError(w http.ResponseWriter, status int, code string, err error) {
	message := "我没处理成功，但问题不大，我们再来一次"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = err.Error()
	}
	writeJSON(w, status, struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{
		OK: false,
		Error: struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}{Code: code, Message: message},
	})
}

// errorBody 表示本地接口统一错误内容。
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// preflightStepAlias 避免 API 响应层依赖未类型化数据。
type preflightStepAlias struct {
	Name       string `json:"name"`
	Success    bool   `json:"success"`
	Optional   bool   `json:"optional"`
	Message    string `json:"message"`
	DurationMS int64  `json:"duration_ms"`
}

// convertPreflightSteps 把流程检查结果转换成本地 API 响应结构。
func convertPreflightSteps(steps []preflight.StepResult) []preflightStepAlias {
	result := make([]preflightStepAlias, 0, len(steps))
	for _, step := range steps {
		result = append(result, preflightStepAlias{
			Name: step.Name, Success: step.Success, Optional: step.Optional,
			Message: step.Message, DurationMS: step.DurationMS,
		})
	}
	return result
}

// writeJSON 写入 JSON 并兜底处理编码失败。
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("写入本地接口响应失败：%v", err)
	}
}
